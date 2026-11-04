// Package api implements Kuraka's HTTP API and browser security boundary.
package api

import (
	"container/list"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/InkaNode/kuraka/internal/auth"
	"github.com/InkaNode/kuraka/internal/authz"
	"github.com/InkaNode/kuraka/internal/cloudflare"
	"github.com/InkaNode/kuraka/internal/config"
	"github.com/InkaNode/kuraka/internal/dnsplan"
	"github.com/InkaNode/kuraka/internal/libredesk"
	"github.com/InkaNode/kuraka/internal/releases"
	"github.com/InkaNode/kuraka/internal/stalwart"
	"github.com/InkaNode/kuraka/internal/store"
	"github.com/InkaNode/kuraka/internal/version"
)

// Stalwart is the complete upstream capability set used by this API.
type Stalwart interface {
	Ping(context.Context) error
	GetDomain(context.Context, string) (stalwart.Domain, error)
	FindDomainByName(context.Context, string) (stalwart.Domain, error)
	CreateDomain(context.Context, string, string, []string) (string, error)
	SetDomainAliases(context.Context, string, []string) error
	ListAccounts(context.Context, string) ([]stalwart.Account, error)
	GetAccount(context.Context, string) (stalwart.Account, error)
	CreateAccount(context.Context, stalwart.NewAccount) (string, error)
	UpdateAccount(context.Context, string, stalwart.AccountPatch) error
	DeleteAccount(context.Context, string) error
	ListDkim(context.Context, string) ([]stalwart.DkimKey, error)
	CreateAppPassword(context.Context, string, string) (string, string, error)
	DeleteAppPassword(context.Context, string, string) error
	ListQueue(context.Context) ([]stalwart.QueuedMessage, error)
	GetQueued(context.Context, string) (stalwart.QueuedMessage, error)
	RetryQueued(context.Context, string) error
	CancelQueued(context.Context, string) error
	UpdateApps(context.Context) error
}

var _ Stalwart = (*stalwart.Client)(nil)

type Server struct {
	cfg         config.Config
	st          *store.Store
	sw          Stalwart
	log         *slog.Logger
	box         *auth.Box
	az          *authz.Authz
	now         func() time.Time
	adminMu     sync.Mutex // serializes setup and last-enabled-superadmin checks
	domainMu    sync.Mutex // serializes domain and alias name reservations
	helpdeskMu  sync.Mutex // serializes activation and pending deletion
	setupToken  string
	limiter     failureLimiter
	ipLimiter   failureLimiter
	ui          fs.FS
	newDNS      func(string) DNSAPI
	newHelpdesk func(string, string, string) HelpdeskAPI
	resolver    dnsplan.Resolver
	releases    *releases.Client
	updates     updateState
}

func New(cfg config.Config, st *store.Store, sw Stalwart, log *slog.Logger) (*Server, error) {
	box, err := auth.NewBox(cfg.SecretKeyHex)
	if err != nil {
		return nil, err
	}
	n, err := st.CountSuperadmins(context.Background())
	if err != nil {
		return nil, err
	}
	if log == nil {
		log = slog.Default()
	}
	s := &Server{cfg: cfg, st: st, sw: sw, log: log, box: box, az: authz.New(st, sw), now: time.Now}
	s.ipLimiter.limit = 30
	s.newDNS = func(token string) DNSAPI { return cloudflare.New(token) }
	s.newHelpdesk = func(url, key, secret string) HelpdeskAPI { return libredesk.New(url, key, secret) }
	s.resolver = dnsplan.NewResolver(cfg.DNSResolver)
	s.releases = &releases.Client{}
	if n == 0 {
		s.setupToken, err = auth.NewToken()
	}
	return s, err
}

func (s *Server) SetupToken() string {
	s.adminMu.Lock()
	defer s.adminMu.Unlock()
	n, err := s.st.CountSuperadmins(context.Background())
	if err != nil || n != 0 {
		return ""
	}
	return s.setupToken
}

type requestState struct {
	user                   store.User
	session                store.Session
	principal              authz.Principal
	action, target, detail string
	auditResult            string
	tenant                 *int64
}

type stateKey struct{}

func state(r *http.Request) *requestState {
	return r.Context().Value(stateKey{}).(*requestState)
}

type endpoint func(http.ResponseWriter, *http.Request) (any, error)
type reply struct {
	status int
	body   any
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	route := func(pattern, action string, public, admin bool, h endpoint) {
		mux.HandleFunc(pattern, s.endpoint(action, public, admin, h))
	}
	route("GET /healthz", "health", true, false, s.health)
	route("POST /api/v1/setup", "setup", true, false, s.setup)
	route("POST /api/v1/auth/login", "auth.login", true, false, s.login)
	route("POST /api/v1/auth/totp", "auth.totp", false, false, s.totp)
	route("POST /api/v1/auth/logout", "auth.logout", false, false, s.logout)
	route("GET /api/v1/auth/me", "auth.me", false, false, s.me)
	route("POST /api/v1/auth/password", "auth.password", false, false, s.password)
	route("POST /api/v1/auth/totp/enroll", "auth.totp.enroll", false, false, s.enroll)
	route("POST /api/v1/auth/totp/confirm", "auth.totp.confirm", false, false, s.confirm)
	route("POST /api/v1/auth/totp/disable", "auth.totp.disable", false, false, s.disableTOTP)
	route("GET /api/v1/tenants", "tenant.list", false, true, s.tenants)
	route("POST /api/v1/tenants", "tenant.create", false, true, s.createTenant)
	route("PATCH /api/v1/tenants/{id}", "tenant.update", false, true, s.updateTenant)
	route("DELETE /api/v1/tenants/{id}", "tenant.delete", false, true, s.deleteTenant)
	route("GET /api/v1/users", "user.list", false, true, s.users)
	route("POST /api/v1/users", "user.create", false, true, s.createUser)
	route("PATCH /api/v1/users/{id}", "user.update", false, true, s.updateUser)
	route("POST /api/v1/tenants/{id}/domains", "domain.create", false, true, s.createDomain)
	route("POST /api/v1/tenants/{id}/domains/import", "domain.import", false, true, s.importDomain)
	route("DELETE /api/v1/domains/{id}", "domain.unlink", false, true, s.unlinkDomain)
	route("GET /api/v1/domains", "domain.list", false, false, s.domains)
	route("GET /api/v1/domains/{id}", "domain.get", false, false, s.domain)
	route("PUT /api/v1/domains/{id}/aliases", "domain.aliases", false, false, s.domainAliases)
	route("GET /api/v1/domains/{id}/mailboxes", "mailbox.list", false, false, s.mailboxes)
	route("POST /api/v1/domains/{id}/mailboxes", "mailbox.create", false, false, s.createMailbox)
	route("GET /api/v1/domains/{id}/mailboxes/{mid}", "mailbox.get", false, false, s.mailbox)
	route("PATCH /api/v1/domains/{id}/mailboxes/{mid}", "mailbox.update", false, false, s.updateMailbox)
	route("DELETE /api/v1/domains/{id}/mailboxes/{mid}", "mailbox.delete", false, false, s.deleteMailbox)
	route("GET /api/v1/audit", "audit.list", false, false, s.audit)
	route("GET /api/v1/queue", "queue.list", false, false, s.queue)
	route("POST /api/v1/queue/{id}/retry", "queue.retry", false, false, s.retryQueued)
	route("DELETE /api/v1/queue/{id}", "queue.cancel", false, false, s.cancelQueued)
	route("GET /api/v1/metrics", "metrics.get", false, true, s.metrics)
	route("GET /api/v1/alerts", "alert.list", false, true, s.alerts)
	route("POST /api/v1/alerts", "alert.create", false, true, s.createAlert)
	route("DELETE /api/v1/alerts/{id}", "alert.delete", false, true, s.deleteAlert)
	s.integrationRoutes(route)
	s.updateRoutes(route)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
			writeError(w, 404, "not_found", "Resource not found")
			return
		}
		s.serveUI(w, r)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", csp)
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		if mutating(r.Method) {
			origin := r.Header.Get("Origin")
			valid := origin == s.cfg.PublicURL
			if origin == "" {
				valid = strings.HasPrefix(r.Header.Get("Referer"), s.cfg.PublicURL+"/")
			}
			if r.ContentLength != 0 || len(r.TransferEncoding) > 0 {
				ct, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
				valid = valid && err == nil && ct == "application/json"
			}
			if !valid {
				s.record(r, &requestState{action: "csrf", target: r.URL.Path}, "denied")
				writeError(w, 403, "csrf", "Invalid request origin or content type")
				return
			}
		}
		mux.ServeHTTP(w, r)
	})
}

func mutating(method string) bool {
	return method == "POST" || method == "PUT" || method == "PATCH" || method == "DELETE"
}

func (s *Server) endpoint(action string, public, admin bool, h endpoint) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := &requestState{action: action, target: r.URL.Path}
		r = r.WithContext(context.WithValue(r.Context(), stateKey{}, q))
		var body any
		var err error
		if !public {
			err = s.authenticate(w, r)
		} else if c, cookieErr := r.Cookie(cookieName); cookieErr == nil && c.Value != "" {
			// Anonymous routes cannot be used to escape an existing MFA fence.
			sess, sessionErr := s.st.GetSession(r.Context(), auth.HashToken(c.Value), s.now())
			if sessionErr == nil && sess.MFAPending {
				err = s.authenticate(w, r)
			} else if sessionErr != nil && !errors.Is(sessionErr, store.ErrNotFound) {
				err = sessionErr
			}
		}
		if err == nil && admin {
			err = authz.RequireSuperadmin(q.principal)
		}
		if err == nil {
			body, err = h(w, r)
		}
		denied := errors.Is(err, authz.ErrNotFound) || errors.Is(err, authz.ErrForeignAlias) || errors.Is(err, authz.ErrForbidden)
		if mutating(r.Method) || denied {
			result := "ok"
			if err != nil {
				result = "error"
				status, _, _ := classify(err)
				if status < 500 {
					result = "denied"
				}
			}
			if q.auditResult != "" {
				result = q.auditResult
			}
			s.record(r, q, result)
		}
		if err != nil {
			s.fail(w, err)
			return
		}
		if v, ok := body.(download); ok {
			w.Header().Set("Content-Type", v.contentType)
			w.Header().Set("Content-Disposition", "attachment; filename=\"libredesk-bundle.zip\"")
			w.WriteHeader(200)
			_, _ = w.Write(v.data)
		} else if v, ok := body.(reply); ok {
			writeJSON(w, v.status, v.body)
		} else {
			writeJSON(w, 200, body)
		}
	}
}

func (s *Server) record(r *http.Request, q *requestState, result string) {
	entry := store.AuditEntry{At: s.now(), TenantID: q.tenant, Action: q.action, Target: q.target, Result: result, Detail: q.detail, IP: clientIP(r)}
	if q.user.ID != 0 {
		entry.UserID = &q.user.ID
		if entry.TenantID == nil {
			entry.TenantID = q.user.TenantID
		}
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
	defer cancel()
	if err := s.st.AddAudit(ctx, entry); err != nil {
		s.log.Error("audit write failed", "action", q.action, "error", err)
	}
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip != nil && (ip.IsLoopback() || ip.IsPrivate()) {
		if x := r.Header.Get("X-Forwarded-For"); x != "" {
			parts := strings.Split(x, ",")
			return strings.TrimSpace(parts[len(parts)-1])
		}
	}
	return host
}

type apiError struct {
	status        int
	code, message string
	cause         error // logged, never sent to the client
}

func (e *apiError) Error() string {
	if e.cause != nil {
		return e.message + ": " + e.cause.Error()
	}
	return e.message
}
func (e *apiError) Unwrap() error { return e.cause }
func invalid(field string) error {
	return &apiError{status: 422, code: "invalid_input", message: "Invalid " + field}
}
func conflict(message string) error {
	return &apiError{status: 409, code: "conflict", message: message}
}

var unauthorized = &apiError{status: 401, code: "unauthorized", message: "Authentication required"}
var invalidCredentials = &apiError{status: 401, code: "invalid_credentials", message: "Invalid credentials"}

// wrongCredentials is a failed re-check by a signed-in user: a form error,
// never 401, which the browser treats as a lost session.
var wrongCredentials = &apiError{status: 422, code: "invalid_credentials", message: "Invalid credentials"}

func reauthFailed(err error) error {
	if err == invalidCredentials {
		return wrongCredentials
	}
	return err
}

var rateLimited = &apiError{status: 429, code: "rate_limited", message: "Too many attempts; try again later"}

type upstreamError struct{ error }

func upstream(err error) error {
	if err == nil {
		return nil
	}
	return upstreamError{err}
}
func (e upstreamError) Unwrap() error { return e.error }

func classify(err error) (int, string, string) {
	var ae *apiError
	if errors.As(err, &ae) {
		return ae.status, ae.code, ae.message
	}
	switch {
	case errors.Is(err, authz.ErrNotFound), errors.Is(err, store.ErrNotFound), errors.Is(err, stalwart.ErrNotFound):
		return 404, "not_found", "Resource not found"
	case errors.Is(err, authz.ErrForbidden):
		return 403, "forbidden", "Action forbidden"
	case errors.Is(err, authz.ErrForeignAlias):
		return 422, "foreign_alias", "Alias domain must belong to the same tenant"
	case errors.Is(err, store.ErrConflict):
		return 409, "conflict", "Resource conflicts with existing data"
	}
	var ue upstreamError
	var he *stalwart.HTTPError
	var me *stalwart.MethodError
	var se *stalwart.SetError
	// Stalwart refuses changes to accounts holding permissions Kuraka's key lacks.
	if errors.As(err, &se) && se.Type == "forbidden" {
		return 403, "upstream_forbidden", "The mail server does not allow Kuraka to change this resource"
	}
	if errors.As(err, &ue) || errors.As(err, &he) || errors.As(err, &me) || errors.As(err, &se) {
		return 502, "upstream_error", "Mail service unavailable"
	}
	return 500, "internal_error", "Internal server error"
}

func (s *Server) fail(w http.ResponseWriter, err error) {
	status, code, message := classify(err)
	if status >= 500 {
		s.log.Error("request failed", "error", err)
	}
	writeError(w, status, code, message)
}
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if status != http.StatusNoContent {
		_ = json.NewEncoder(w).Encode(body)
	}
}
func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return &apiError{status: 422, code: "invalid_input", message: fmt.Sprintf("Invalid JSON body: %v", err)}
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return invalid("JSON body: expected one object")
	}
	return nil
}
func pathID(r *http.Request) (int64, error) {
	return positiveID(r.PathValue("id"), "id")
}
func positiveID(value, field string) (int64, error) {
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 {
		return 0, invalid(field)
	}
	return id, nil
}

func (s *Server) health(_ http.ResponseWriter, r *http.Request) (any, error) {
	status, sw, health := 200, "ok", "ok"
	if err := s.sw.Ping(r.Context()); err != nil {
		status, sw, health = 503, "unreachable", "degraded"
		s.log.Warn("stalwart health check failed", "error", err)
	}
	return reply{status, map[string]string{"status": health, "version": version.Version, "stalwart": sw}}, nil
}

func (s *Server) audit(_ http.ResponseWriter, r *http.Request) (any, error) {
	before := int64(0)
	limit := 100
	if raw := r.URL.Query().Get("before"); raw != "" {
		var err error
		before, err = positiveID(raw, "before")
		if err != nil {
			return nil, err
		}
	}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 200 {
			return nil, invalid("limit")
		}
		limit = n
	}
	rows, err := s.st.ListAudit(r.Context(), authz.TenantScope(state(r).principal), before, limit)
	out := make([]any, 0, len(rows))
	for _, a := range rows {
		out = append(out, map[string]any{"id": a.ID, "at": a.At, "user_id": a.UserID, "tenant_id": a.TenantID, "action": a.Action, "target": a.Target, "result": a.Result, "detail": a.Detail, "ip": a.IP})
	}
	return out, err
}

const maxFailureKeys = 100_000

// Failure windows and locks are independent: reaching the limit starts a full
// fifteen minute lock, even if it occurs near the end of the failure window.
type failureWindow struct {
	start, locked time.Time
	failures      int
	order         *list.Element
}
type failureLimiter struct {
	mu      sync.Mutex
	entries map[string]failureWindow
	limit   int // zero preserves the five-failure IP+email limit
	oldest  list.List
	pruned  time.Time
}

// prune runs under mu, at most once per minute. Expired windows are also reset
// on failure; the list permits constant-time eviction at capacity.
func (l *failureLimiter) prune(now time.Time) {
	if !l.pruned.IsZero() && now.Before(l.pruned.Add(time.Minute)) {
		return
	}
	for key, v := range l.entries {
		if !now.Before(v.locked) && !now.Before(v.start.Add(15*time.Minute)) {
			l.remove(key)
		}
	}
	l.pruned = now
}

func (l *failureLimiter) remove(key string) {
	if v, ok := l.entries[key]; ok {
		l.oldest.Remove(v.order)
		delete(l.entries, key)
	}
}

func (l *failureLimiter) blocked(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.prune(now)
	return now.Before(l.entries[key].locked)
}
func (l *failureLimiter) fail(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.prune(now)
	if l.entries == nil {
		l.entries = map[string]failureWindow{}
	}
	v, exists := l.entries[key]
	if now.Before(v.locked) {
		return true
	}
	if !exists || !now.Before(v.start.Add(15*time.Minute)) {
		l.remove(key)
		if len(l.entries) >= maxFailureKeys {
			l.remove(l.oldest.Front().Value.(string))
		}
		v = failureWindow{start: now, order: l.oldest.PushBack(key)}
	}
	v.failures++
	limit := l.limit
	if limit == 0 {
		limit = 5
	}
	if v.failures >= limit {
		v.locked = now.Add(15 * time.Minute)
	}
	l.entries[key] = v
	return now.Before(v.locked)
}
func (l *failureLimiter) clear(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.remove(key)
}
