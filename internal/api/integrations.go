package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/InkaNode/kuraka/internal/authz"
	"github.com/InkaNode/kuraka/internal/cloudflare"
	"github.com/InkaNode/kuraka/internal/libredesk"
	"github.com/InkaNode/kuraka/internal/provision"
	"github.com/InkaNode/kuraka/internal/store"
)

// DNSAPI is the provider capability set used by integration handlers.
type DNSAPI interface {
	VerifyToken(context.Context) error
	FindZone(context.Context, string) (cloudflare.Zone, error)
	CreateZone(context.Context, string) (cloudflare.Zone, error)
	ListRecords(context.Context, string) ([]cloudflare.Record, error)
	CreateRecord(context.Context, string, cloudflare.Record) (cloudflare.Record, error)
	UpdateRecord(context.Context, string, string, cloudflare.Record) (cloudflare.Record, error)
	DeleteRecord(context.Context, string, string) error
	SetRedirect(context.Context, string, string, string) error
}

type HelpdeskAPI interface {
	SystemAPIKey(context.Context, string) (string, string, error)
	Ping(context.Context) error
	ListInboxes(context.Context) ([]libredesk.Inbox, error)
	CreateEmailInbox(context.Context, libredesk.NewEmailInbox) (int64, error)
	DeleteInbox(context.Context, int64) error
	CreateAgent(context.Context, libredesk.NewAgent) (int64, error)
	SetAgentPassword(context.Context, int64, libredesk.NewAgent, string) error
	SyncAgentPassword(context.Context, int64, string) error
	ApplyBranding(context.Context, string, string, string) error
}

var _ DNSAPI = (*cloudflare.Client)(nil)
var _ HelpdeskAPI = (*libredesk.Client)(nil)

func (s *Server) integrationRoutes(route func(string, string, bool, bool, endpoint)) {
	route("GET /api/v1/dns-providers", "dns_provider.list", false, false, s.dnsProviders)
	route("POST /api/v1/dns-providers", "dns_provider.create", false, false, s.createDNSProvider)
	route("DELETE /api/v1/dns-providers/{id}", "dns_provider.delete", false, false, s.deleteDNSProvider)
	route("PUT /api/v1/domains/{id}/dns-provider", "domain.dns_provider", false, false, s.assignDNSProvider)
	route("POST /api/v1/domains/{id}/dns/zone", "dns.zone", false, false, s.createZone)
	route("GET /api/v1/domains/{id}/dns/check", "dns.check", false, false, s.checkDNS)
	route("POST /api/v1/domains/{id}/dns/plan", "dns.plan", false, false, s.planDNS)
	route("POST /api/v1/domains/{id}/dns/apply", "dns.apply", false, false, s.applyDNS)
	route("PUT /api/v1/domains/{id}/alias-redirect", "domain.alias_redirect", false, false, s.aliasRedirect)
	route("GET /api/v1/helpdesks", "helpdesk.list", false, false, s.helpdesks)
	route("POST /api/v1/helpdesks", "helpdesk.create", false, false, s.createHelpdesk)
	route("DELETE /api/v1/helpdesks/{id}", "helpdesk.delete", false, false, s.deleteHelpdesk)
	route("POST /api/v1/helpdesks/provision", "helpdesk.provision", false, true, s.provisionHelpdesk)
	route("POST /api/v1/helpdesks/{id}/agents", "helpdesk.agent.create", false, true, s.createHelpdeskAgent)
	route("POST /api/v1/helpdesks/{id}/activate", "helpdesk.activate", false, false, s.activateHelpdesk)
	route("GET /api/v1/domains/{id}/mailboxes/{mid}/helpdesk", "mailbox.helpdesk.list", false, false, s.mailboxHelpdesks)
	route("POST /api/v1/domains/{id}/mailboxes/{mid}/helpdesk", "mailbox.helpdesk.connect", false, false, s.connectHelpdesk)
	route("DELETE /api/v1/domains/{id}/mailboxes/{mid}/helpdesk/{link}", "mailbox.helpdesk.disconnect", false, false, s.disconnectHelpdesk)
	route("POST /api/v1/domains/{id}/migrations", "migration.create", false, false, s.createMigration)
	route("GET /api/v1/domains/{id}/migrations", "migration.list", false, false, s.migrations)
	route("POST /api/v1/domains/{id}/migrations/{mig}/cancel", "migration.cancel", false, false, s.cancelMigration)
}

// ownerScope also rejects explicit foreign list filters instead of silently
// returning another tenant's resources.
func ownerScope(r *http.Request, tenant *int64) error {
	p := state(r).principal
	if p.IsSuperadmin() {
		return nil
	}
	if p.Role != authz.RoleTenantAdmin || p.TenantID == nil {
		return authz.ErrForbidden
	}
	if tenant == nil || *tenant != *p.TenantID {
		return authz.ErrNotFound
	}
	return nil
}

func listScope(r *http.Request) (*int64, error) {
	scope := authz.TenantScope(state(r).principal)
	if raw := r.URL.Query().Get("tenant_id"); raw != "" {
		id, err := positiveID(raw, "tenant_id")
		if err != nil {
			return nil, err
		}
		if err := ownerScope(r, &id); err != nil {
			return nil, err
		}
		scope = &id
	}
	return scope, nil
}

func providerJSON(p store.DNSProvider) map[string]any {
	return map[string]any{"id": p.ID, "tenant_id": p.TenantID, "kind": p.Kind, "name": p.Name, "created_at": p.CreatedAt}
}

func (s *Server) dnsProviders(_ http.ResponseWriter, r *http.Request) (any, error) {
	scope, err := listScope(r)
	if err != nil {
		return nil, err
	}
	rows, err := s.st.ListDNSProviders(r.Context(), scope, false)
	out := make([]any, 0, len(rows))
	for _, p := range rows {
		out = append(out, providerJSON(p))
	}
	return out, err
}

func (s *Server) createDNSProvider(_ http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		TenantID *int64 `json:"tenant_id"`
		Kind     string `json:"kind"`
		Name     string `json:"name"`
		Token    string `json:"token"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	if err := ownerScope(r, in.TenantID); err != nil {
		return nil, err
	}
	if in.Kind != "" && in.Kind != "cloudflare" || strings.TrimSpace(in.Name) == "" || strings.TrimSpace(in.Token) == "" {
		return nil, invalid("provider")
	}
	if in.TenantID != nil {
		if _, err := s.st.GetTenant(r.Context(), *in.TenantID); err != nil {
			return nil, err
		}
	}
	state(r).tenant = in.TenantID
	if err := s.newDNS(in.Token).VerifyToken(r.Context()); err != nil {
		// Cloudflare answers 4xx for a wrong, expired or under-scoped token: that is bad input, not an outage.
		var ce *cloudflare.APIError
		if errors.As(err, &ce) && ce.Status >= 400 && ce.Status < 500 {
			return nil, &apiError{status: 422, code: "invalid_input", message: "Cloudflare rejected the token", cause: err}
		}
		return nil, integrationFailure("DNS token verification failed", err)
	}
	enc, err := s.box.Seal([]byte(in.Token))
	if err != nil {
		return nil, err
	}
	p, err := s.st.CreateDNSProvider(r.Context(), store.DNSProvider{TenantID: in.TenantID, Kind: "cloudflare", Name: strings.TrimSpace(in.Name), TokenEnc: enc})
	return reply{201, providerJSON(p)}, err
}

// Provider and helpdesk errors can contain upstream-controlled text. Keep
// credentials out of both HTTP responses and request failure logs.
func integrationFailure(message string, cause error) error {
	return &apiError{status: 502, code: "upstream_error", message: message, cause: cause}
}

func (s *Server) deleteDNSProvider(_ http.ResponseWriter, r *http.Request) (any, error) {
	id, err := pathID(r)
	if err != nil {
		return nil, err
	}
	p, err := s.st.GetDNSProvider(r.Context(), id)
	if err != nil {
		return nil, err
	}
	if err := ownerScope(r, p.TenantID); err != nil {
		return nil, err
	}
	state(r).tenant = p.TenantID
	return reply{204, nil}, s.st.DeleteDNSProvider(r.Context(), id)
}

func (s *Server) assignDNSProvider(_ http.ResponseWriter, r *http.Request) (any, error) {
	d, err := s.authorizedDomain(r)
	if err != nil {
		return nil, err
	}
	var in struct {
		ProviderID *int64 `json:"provider_id"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	if in.ProviderID != nil {
		if _, err := s.usableProvider(r, d, *in.ProviderID); err != nil {
			return nil, err
		}
	}
	if err := s.st.SetDomainDNSProvider(r.Context(), d.ID, in.ProviderID); err != nil {
		return nil, err
	}
	d.DNSProviderID = in.ProviderID
	return domainJSON(d), nil
}

func helpdeskJSON(h store.Helpdesk) map[string]any {
	return map[string]any{"id": h.ID, "tenant_id": h.TenantID, "kind": h.Kind, "name": h.Name, "url": h.URL, "status": h.Status, "created_at": h.CreatedAt}
}

func (s *Server) helpdesks(_ http.ResponseWriter, r *http.Request) (any, error) {
	scope, err := listScope(r)
	if err != nil {
		return nil, err
	}
	rows, err := s.st.ListHelpdesks(r.Context(), scope)
	out := make([]any, 0, len(rows))
	for _, h := range rows {
		out = append(out, helpdeskJSON(h))
	}
	return out, err
}

func (s *Server) createHelpdesk(_ http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		TenantID  int64  `json:"tenant_id"`
		Kind      string `json:"kind"`
		Name      string `json:"name"`
		URL       string `json:"url"`
		APIKey    string `json:"api_key"`
		APISecret string `json:"api_secret"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	if err := ownerScope(r, &in.TenantID); err != nil {
		return nil, err
	}
	u, err := url.Parse(in.URL)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || in.TenantID <= 0 || strings.TrimSpace(in.Name) == "" || in.APIKey == "" || in.APISecret == "" || (in.Kind != "" && in.Kind != "libredesk") {
		return nil, invalid("helpdesk")
	}
	if _, err := s.st.GetTenant(r.Context(), in.TenantID); err != nil {
		return nil, err
	}
	state(r).tenant = &in.TenantID
	if err := s.newHelpdesk(in.URL, in.APIKey, in.APISecret).Ping(r.Context()); err != nil {
		return nil, integrationFailure("Helpdesk verification failed", err)
	}
	enc, err := s.box.Seal([]byte(in.APISecret))
	if err != nil {
		return nil, err
	}
	h, err := s.st.CreateHelpdesk(r.Context(), store.Helpdesk{TenantID: in.TenantID, Kind: "libredesk", Name: in.Name, URL: strings.TrimRight(in.URL, "/"), APIKey: in.APIKey, APISecretEnc: enc})
	return reply{201, helpdeskJSON(h)}, err
}

func (s *Server) helpdeskClient(h store.Helpdesk) (HelpdeskAPI, error) {
	secret, err := s.box.Open(h.APISecretEnc)
	if err != nil {
		return nil, err
	}
	return s.newHelpdesk(h.URL, h.APIKey, string(secret)), nil
}

func (s *Server) deleteHelpdesk(_ http.ResponseWriter, r *http.Request) (any, error) {
	s.helpdeskMu.Lock()
	defer s.helpdeskMu.Unlock()
	id, err := pathID(r)
	if err != nil {
		return nil, err
	}
	h, err := s.st.GetHelpdesk(r.Context(), id)
	if err != nil {
		return nil, err
	}
	if err := ownerScope(r, &h.TenantID); err != nil {
		return nil, err
	}
	state(r).tenant = &h.TenantID
	if h.Status == "pending" {
		return reply{204, nil}, s.st.DeleteHelpdesk(r.Context(), id)
	}
	// Revoke credentials before the store cascades the link rows.
	domains, err := s.st.ListDomains(r.Context(), &h.TenantID)
	if err != nil {
		return nil, err
	}
	client, err := s.helpdeskClient(h)
	if err != nil {
		return nil, err
	}
	for _, d := range domains {
		links, err := s.st.ListHelpdeskLinks(r.Context(), d.ID, "")
		if err != nil {
			return nil, err
		}
		for _, link := range links {
			if link.HelpdeskID == id {
				if err := s.revokeHelpdeskLink(r.Context(), client, link); err != nil {
					return nil, err
				}
			}
		}
	}
	return reply{204, nil}, s.st.DeleteHelpdesk(r.Context(), id)
}

type download struct {
	contentType string
	data        []byte
}

func (s *Server) provisionHelpdesk(w http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		TenantID int64  `json:"tenant_id"`
		Host     string `json:"host"`
		Install  bool   `json:"install"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	if in.Install && s.cfg.UpdatesDir == "" {
		return nil, &apiError{status: 409, code: "updates_disabled", message: "KURAKA_UPDATES_DIR is not set"}
	}
	host, err := hostname(in.Host, "helpdesk")
	if err != nil || !strings.Contains(host, ".") || in.TenantID <= 0 {
		return nil, invalid("helpdesk")
	}
	tenant, err := s.st.GetTenant(r.Context(), in.TenantID)
	if err != nil {
		return nil, err
	}
	state(r).tenant = &tenant.ID
	s.helpdeskMu.Lock()
	defer s.helpdeskMu.Unlock()
	bundle, err := provision.Bundle(tenant.Name, host, provision.Options{})
	if err != nil {
		return nil, err
	}
	existing, err := s.st.ListHelpdesks(r.Context(), &tenant.ID)
	if err != nil {
		return nil, err
	}
	for _, e := range existing {
		// A new bundle has new secrets that the installed database would reject.
		if e.URL == bundle.InternalURL {
			return nil, conflict("Helpdesk already provisioned for this tenant")
		}
	}
	enc, err := s.box.Seal([]byte(bundle.SystemPassword))
	if err != nil {
		return nil, err
	}
	h, err := s.st.CreateHelpdesk(r.Context(), store.Helpdesk{TenantID: tenant.ID, Kind: "libredesk", Name: host,
		URL: bundle.InternalURL, Status: "pending", SetupSecretEnc: enc})
	if err != nil {
		return nil, err
	}
	dns := s.provisionHelpdeskDNS(r.Context(), tenant.ID, host)
	if in.Install {
		if err := writeInstallRequest(s.cfg.UpdatesDir, provision.Name(tenant.Name), host, bundle.SystemPassword); err != nil {
			return nil, errors.Join(err, s.st.DeleteHelpdesk(context.WithoutCancel(r.Context()), h.ID))
		}
		out := helpdeskJSON(h)
		out["dns"] = dns
		return reply{201, out}, nil
	}
	w.Header().Set("X-Kuraka-DNS", dns)
	return download{"application/zip", bundle.Zip}, nil
}

func (s *Server) provisionHelpdeskDNS(ctx context.Context, tenantID int64, host string) string {
	domains, err := s.st.ListDomains(ctx, &tenantID)
	if err != nil {
		return "error"
	}
	var domain *store.Domain
	for _, d := range domains {
		if d.DNSProviderID != nil && strings.HasSuffix(host, "."+d.Name) && (domain == nil || len(d.Name) > len(domain.Name)) {
			domain = &d
		}
	}
	if domain == nil {
		return "manual"
	}
	if s.cfg.MailHost == "" {
		return "error"
	}
	client, err := s.dnsClient(ctx, *domain)
	if err != nil {
		return "error"
	}
	zone, err := client.FindZone(ctx, domain.Name)
	if err != nil {
		return "error"
	}
	records, err := client.ListRecords(ctx, zone.ID)
	if err != nil {
		return "error"
	}
	for _, record := range records {
		if dnsName(record.Name) == host {
			return "exists"
		}
	}
	_, err = client.CreateRecord(ctx, zone.ID, cloudflare.Record{Type: "CNAME", Name: host,
		Content: s.cfg.MailHost, TTL: 1, Proxied: false, Comment: managedComment})
	if err != nil {
		return "error"
	}
	return "created"
}

var helpdeskNotReady = &apiError{status: 409, code: "helpdesk_not_ready", message: "Libredesk is not ready"}

var sharedMailbox = &apiError{status: 409, code: "shared_mailbox", message: "Shared mailboxes cannot connect to a helpdesk"}

var helpdeskPasswordClasses = []*regexp.Regexp{regexp.MustCompile(`[A-Z]`), regexp.MustCompile(`[a-z]`), regexp.MustCompile(`[0-9]`), regexp.MustCompile(`[\W_]`)}

// strongHelpdeskPassword mirrors Libredesk v2.9.2 user.IsStrongPassword.
func strongHelpdeskPassword(p string) bool {
	if len(p) < 10 || len(p) > 72 {
		return false
	}
	for _, re := range helpdeskPasswordClasses {
		if !re.MatchString(p) {
			return false
		}
	}
	return true
}

// createHelpdeskAgent creates a Libredesk account for the signed-in user
// ("self", reusing the Kuraka password when Libredesk accepts it) or for
// another person by name and email. Generated passwords are returned once.
func (s *Server) createHelpdeskAgent(_ http.ResponseWriter, r *http.Request) (any, error) {
	id, err := pathID(r)
	if err != nil {
		return nil, err
	}
	var in struct {
		Mode     string `json:"mode"`
		Password string `json:"password"`
		Name     string `json:"name"`
		Email    string `json:"email"`
		Role     string `json:"role"`
		Sync     *bool  `json:"sync"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	if in.Role == "" {
		in.Role = "Admin"
	}
	if in.Role != "Admin" && in.Role != "Agent" {
		return nil, invalid("role")
	}
	name, email := strings.TrimSpace(in.Name), strings.TrimSpace(in.Email)
	switch in.Mode {
	case "self":
		if in.Password == "" {
			return nil, invalid("password")
		}
		u := state(r).user
		name, email = u.Name, u.Email
	case "other":
		if name == "" || len(name) > 200 || !validEmail(email) {
			return nil, invalid("agent")
		}
	default:
		return nil, invalid("mode")
	}
	h, err := s.st.GetHelpdesk(r.Context(), id)
	if err != nil {
		return nil, err
	}
	state(r).tenant = &h.TenantID
	state(r).target = email
	if h.Status != "ready" {
		return nil, helpdeskNotReady
	}
	password := ""
	if in.Mode == "self" {
		if err := s.checkPassword(r, in.Password); err != nil {
			return nil, err
		}
		if strongHelpdeskPassword(in.Password) {
			password = in.Password
		}
	}
	generated := password == ""
	if generated {
		var random [12]byte
		if _, err := rand.Read(random[:]); err != nil {
			return nil, err
		}
		password = "Aa9!" + hex.EncodeToString(random[:])
	}
	first, last, _ := strings.Cut(strings.TrimSpace(name), " ")
	if first == "" {
		first, _, _ = strings.Cut(email, "@")
	}
	a := libredesk.NewAgent{FirstName: first, LastName: strings.TrimSpace(last), Email: email, Roles: []string{in.Role}}
	client, err := s.helpdeskClient(h)
	if err != nil {
		return nil, err
	}
	agentID, err := client.CreateAgent(r.Context(), a)
	if errors.Is(err, libredesk.ErrConflict) {
		return nil, conflict("Agent already exists in this helpdesk")
	}
	if err != nil {
		return nil, integrationFailure("Helpdesk agent creation failed", err)
	}
	if err := client.SetAgentPassword(r.Context(), agentID, a, password); err != nil {
		return nil, integrationFailure("Helpdesk agent creation failed", err)
	}
	// The account exists either way; a lost link only disables the sync.
	if in.Mode == "self" && (in.Sync == nil || *in.Sync) {
		if err := s.st.LinkHelpdeskAgent(r.Context(), h.ID, state(r).user.ID, agentID); err != nil {
			s.log.Error("helpdesk agent link failed", "helpdesk_id", h.ID, "agent_id", agentID, "error", err)
		}
	}
	out := map[string]any{"email": email, "url": "https://" + h.Name}
	if generated {
		out["password"] = password
	}
	return reply{201, out}, nil
}

func (s *Server) activateHelpdesk(_ http.ResponseWriter, r *http.Request) (any, error) {
	s.helpdeskMu.Lock()
	defer s.helpdeskMu.Unlock()
	id, err := pathID(r)
	if err != nil {
		return nil, err
	}
	h, err := s.st.GetHelpdesk(r.Context(), id)
	if err != nil {
		return nil, err
	}
	if err := ownerScope(r, &h.TenantID); err != nil {
		return nil, err
	}
	state(r).tenant = &h.TenantID
	if h.Status == "ready" {
		return helpdeskJSON(h), nil
	}
	h, err = s.activate(r.Context(), h)
	if err != nil {
		return nil, err
	}
	return helpdeskJSON(h), nil
}

func (s *Server) activate(ctx context.Context, h store.Helpdesk) (store.Helpdesk, error) {
	password, err := s.box.Open(h.SetupSecretEnc)
	if err != nil {
		return store.Helpdesk{}, err
	}
	key, secret, err := s.newHelpdesk(h.URL, "", "").SystemAPIKey(ctx, string(password))
	if err != nil {
		return store.Helpdesk{}, helpdeskNotReady
	}
	if err := s.newHelpdesk(h.URL, key, secret).Ping(ctx); err != nil {
		return store.Helpdesk{}, helpdeskNotReady
	}
	enc, err := s.box.Seal([]byte(secret))
	if err != nil {
		return store.Helpdesk{}, err
	}
	if err := s.st.ActivateHelpdesk(ctx, h.ID, key, enc); err != nil {
		return store.Helpdesk{}, err
	}
	h, err = s.st.GetHelpdesk(ctx, h.ID)
	if err != nil {
		return store.Helpdesk{}, err
	}
	if _, err := s.applyDeskBranding(ctx, h); err != nil {
		s.log.Warn("helpdesk branding failed", "helpdesk_id", h.ID, "error", err)
	}
	return h, nil
}
