package api

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/InkaNode/kuraka/internal/libredesk"
	"github.com/InkaNode/kuraka/internal/meta"
	"github.com/InkaNode/kuraka/internal/store"
)

// metaApp is the installation's own Meta app, kept in settings. One app
// serves every domain: its login connects their Pages and numbers, and its
// webhook reaches Kuraka, which passes each event to the owning helpdesk.
type metaApp struct {
	ID, Secret, PagesConfig, WhatsAppConfig, VerifyToken string
}

const (
	metaKeyID       = "meta.app_id"
	metaKeySecret   = "meta.app_secret"
	metaKeyPages    = "meta.pages_config_id"
	metaKeyWhatsApp = "meta.whatsapp_config_id"
	metaKeyVerify   = "meta.verify_token"
)

var metaAppMissing = &apiError{status: 409, code: "meta_app_missing", message: "The Meta app is not set up"}
var metaExpired = &apiError{status: 422, code: "meta_expired", message: "The Meta connection expired"}

func (s *Server) metaAppRoutes(route func(string, string, bool, bool, endpoint)) {
	route("GET /api/v1/meta-app", "meta_app.get", false, true, s.getMetaApp)
	route("PUT /api/v1/meta-app", "meta_app.update", false, true, s.putMetaApp)
	route("DELETE /api/v1/meta-app", "meta_app.delete", false, true, s.deleteMetaApp)
	route("GET /api/v1/meta-app/status", "meta_app.status", false, false, s.metaAppStatus)
	route("POST /api/v1/domains/{id}/meta/login", "meta.login", false, false, s.metaLogin)
	route("POST /api/v1/domains/{id}/meta/oauth", "meta.oauth", false, false, s.metaOAuth)
	route("POST /api/v1/domains/{id}/meta/import", "meta.import", false, false, s.metaImport)
}

func (s *Server) loadMetaApp(ctx context.Context) (metaApp, error) {
	var a metaApp
	var secret string
	for key, p := range map[string]*string{metaKeyID: &a.ID, metaKeySecret: &secret, metaKeyPages: &a.PagesConfig,
		metaKeyWhatsApp: &a.WhatsAppConfig, metaKeyVerify: &a.VerifyToken} {
		v, err := s.st.GetSetting(ctx, key)
		if err != nil {
			return a, err
		}
		*p = v
	}
	if secret != "" {
		plain, err := s.box.Open(secret)
		if err != nil {
			return a, err
		}
		a.Secret = string(plain)
	}
	return a, nil
}

func (s *Server) graph(a metaApp) meta.App {
	return meta.App{ID: a.ID, Secret: a.Secret, BaseURL: s.graphURL}
}

func (s *Server) metaRedirect() string { return s.cfg.PublicURL + "/meta/callback" }
func (s *Server) metaWebhook() string  { return s.cfg.PublicURL + "/webhooks/meta" }

func (s *Server) metaAppJSON(a metaApp) map[string]any {
	return map[string]any{
		"configured": a.ID != "", "app_id": a.ID, "pages_config_id": a.PagesConfig, "whatsapp_config_id": a.WhatsAppConfig,
		"redirect_url": s.metaRedirect(), "webhook": map[string]string{"url": s.metaWebhook(), "verify_token": a.VerifyToken},
	}
}

func (s *Server) getMetaApp(_ http.ResponseWriter, r *http.Request) (any, error) {
	a, err := s.loadMetaApp(r.Context())
	if err != nil {
		return nil, err
	}
	return s.metaAppJSON(a), nil
}

// metaFailed tells apart what Meta refused from Meta being unreachable.
func metaFailed(code, message string, err error) error {
	if me, ok := errors.AsType[*meta.APIError](err); ok && me.Status >= 400 && me.Status < 500 {
		return &apiError{status: 422, code: code, message: message, cause: err}
	}
	return integrationFailure(message, err)
}

func (s *Server) putMetaApp(_ http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		AppID          string `json:"app_id"`
		AppSecret      string `json:"app_secret"`
		PagesConfig    string `json:"pages_config_id"`
		WhatsAppConfig string `json:"whatsapp_config_id"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	for _, p := range []*string{&in.AppID, &in.AppSecret, &in.PagesConfig, &in.WhatsAppConfig} {
		*p = strings.TrimSpace(*p)
	}
	old, err := s.loadMetaApp(r.Context())
	if err != nil {
		return nil, err
	}
	if in.AppSecret == "" && in.AppID == old.ID {
		in.AppSecret = old.Secret
	}
	switch {
	case !metaID(in.AppID):
		return nil, invalid("app_id")
	case in.AppSecret == "" || !metaSecret(in.AppSecret):
		return nil, invalid("app_secret")
	case in.PagesConfig != "" && !metaID(in.PagesConfig):
		return nil, invalid("pages_config_id")
	case in.WhatsAppConfig != "" && !metaID(in.WhatsAppConfig):
		return nil, invalid("whatsapp_config_id")
	}
	a := metaApp{ID: in.AppID, Secret: in.AppSecret, PagesConfig: in.PagesConfig, WhatsAppConfig: in.WhatsAppConfig, VerifyToken: old.VerifyToken}
	if _, err := s.graph(a).Check(r.Context()); err != nil {
		return nil, metaFailed("meta_app_rejected", "Meta rejected the app ID or secret", err)
	}
	if a.VerifyToken == "" {
		a.VerifyToken = rand.Text()
	}
	enc, err := s.box.Seal([]byte(a.Secret))
	if err != nil {
		return nil, err
	}
	if err := s.saveMetaApp(r.Context(), a, enc); err != nil {
		return nil, err
	}
	out := s.metaAppJSON(a)
	// Meta checks the webhook right away, so the settings are saved first.
	out["webhook_ok"] = strings.HasPrefix(s.cfg.PublicURL, "https://") && s.graph(a).SubscribeApp(r.Context(), s.metaWebhook(), a.VerifyToken) == nil
	return out, nil
}

func (s *Server) saveMetaApp(ctx context.Context, a metaApp, secretEnc string) error {
	for key, v := range map[string]string{metaKeyID: a.ID, metaKeySecret: secretEnc, metaKeyPages: a.PagesConfig,
		metaKeyWhatsApp: a.WhatsAppConfig, metaKeyVerify: a.VerifyToken} {
		if err := s.st.SetSetting(ctx, key, v); err != nil {
			return err
		}
	}
	return nil
}

// deleteMetaApp forgets the app. Channels already connected keep working
// in their helpdesk, but no longer get the app's webhook events.
func (s *Server) deleteMetaApp(_ http.ResponseWriter, r *http.Request) (any, error) {
	return reply{204, nil}, s.saveMetaApp(r.Context(), metaApp{}, "")
}

func (s *Server) metaAppStatus(_ http.ResponseWriter, r *http.Request) (any, error) {
	a, err := s.loadMetaApp(r.Context())
	ok := a.ID != "" && a.Secret != ""
	return map[string]bool{"pages": ok && a.PagesConfig != "", "whatsapp": ok && a.WhatsAppConfig != ""}, err
}

// metaFlow is a Meta login in progress: first its state, then the token it
// granted, until the user picks what to connect.
type metaFlow struct {
	domain, user int64
	kind, token  string
	expires      time.Time
}

// frugal: kept in memory, so a restart cancels logins in progress; a table is the upgrade for several Kuraka processes.
type metaFlows struct {
	mu sync.Mutex
	m  map[string]metaFlow
}

func (f *metaFlows) put(fl metaFlow, now time.Time) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.m == nil {
		f.m = map[string]metaFlow{}
	}
	for id, old := range f.m {
		if now.After(old.expires) {
			delete(f.m, id)
		}
	}
	id := rand.Text()
	f.m[id] = fl
	return id
}

// get returns the flow id for user on domain d, deleting it when once is set.
func (f *metaFlows) get(id string, d, user int64, now time.Time, once bool) (metaFlow, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fl, ok := f.m[id]
	if !ok || fl.domain != d || fl.user != user {
		return metaFlow{}, false
	}
	if once || now.After(fl.expires) {
		delete(f.m, id)
	}
	return fl, !now.After(fl.expires)
}

func (s *Server) metaLogin(_ http.ResponseWriter, r *http.Request) (any, error) {
	d, err := s.authorizedDomain(r)
	if err != nil {
		return nil, err
	}
	var in struct {
		Kind string `json:"kind"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	if in.Kind != "pages" && in.Kind != "whatsapp" {
		return nil, invalid("kind")
	}
	a, err := s.loadMetaApp(r.Context())
	if err != nil {
		return nil, err
	}
	config := map[string]string{"pages": a.PagesConfig, "whatsapp": a.WhatsAppConfig}[in.Kind]
	if a.ID == "" || a.Secret == "" || config == "" {
		return nil, metaAppMissing
	}
	nonce := s.metaFlows.put(metaFlow{domain: d.ID, user: state(r).user.ID, kind: in.Kind, expires: s.now().Add(15 * time.Minute)}, s.now())
	// The domain id lets the page Meta sends back find its way home.
	st := strconv.FormatInt(d.ID, 10) + "." + nonce
	return map[string]string{"url": s.graph(a).LoginURL(config, s.metaRedirect(), st, in.Kind == "whatsapp")}, nil
}

func metaPageJSON(p meta.Page) map[string]any {
	out := map[string]any{"id": p.ID, "name": p.Name, "instagram": nil}
	if p.Instagram != nil {
		out["instagram"] = map[string]string{"id": p.Instagram.ID, "username": p.Instagram.Username}
	}
	return out
}

func (s *Server) metaOAuth(_ http.ResponseWriter, r *http.Request) (any, error) {
	d, err := s.authorizedDomain(r)
	if err != nil {
		return nil, err
	}
	var in struct {
		Code  string `json:"code"`
		State string `json:"state"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	prefix, nonce, _ := strings.Cut(in.State, ".")
	if in.Code == "" || len(in.Code) > 2048 || prefix != strconv.FormatInt(d.ID, 10) {
		return nil, invalid("code")
	}
	user := state(r).user.ID
	fl, ok := s.metaFlows.get(nonce, d.ID, user, s.now(), true)
	if !ok || fl.token != "" {
		return nil, metaExpired
	}
	a, err := s.loadMetaApp(r.Context())
	if err != nil {
		return nil, err
	}
	if a.ID == "" {
		return nil, metaAppMissing
	}
	g := s.graph(a)
	token, err := g.Exchange(r.Context(), in.Code, s.metaRedirect())
	if err != nil {
		return nil, metaFailed("meta_rejected", "Meta login failed", err)
	}
	out := map[string]any{"kind": fl.kind, "pages": []any{}, "phones": []any{}}
	if fl.kind == "pages" {
		pages, err := g.Pages(r.Context(), token)
		if err != nil {
			return nil, metaFailed("meta_rejected", "Meta Pages could not be read", err)
		}
		list := make([]any, 0, len(pages))
		for _, p := range pages {
			list = append(list, metaPageJSON(p))
		}
		out["pages"] = list
	} else {
		phones, err := g.Phones(r.Context(), token)
		if err != nil {
			return nil, metaFailed("meta_rejected", "WhatsApp numbers could not be read", err)
		}
		list := make([]any, 0, len(phones))
		for _, p := range phones {
			list = append(list, map[string]string{"id": p.ID, "waba_id": p.WABAID, "number": p.Display, "name": p.VerifiedName})
		}
		out["phones"] = list
	}
	out["grant"] = s.metaFlows.put(metaFlow{domain: d.ID, user: user, kind: fl.kind, token: token, expires: s.now().Add(time.Hour)}, s.now())
	return out, nil
}

func clip(name string) string {
	name = strings.TrimSpace(name)
	for utf8.RuneCountInString(name) > 128 {
		_, size := utf8.DecodeLastRuneInString(name)
		name = name[:len(name)-size]
	}
	return name
}

func whatsAppPIN() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	return fmt.Sprintf("%06d", n), err
}

// metaImport connects what the user picked from a Meta login: each Page to
// Messenger, its Instagram account to Instagram, each number to WhatsApp.
func (s *Server) metaImport(_ http.ResponseWriter, r *http.Request) (any, error) {
	d, err := s.authorizedDomain(r)
	if err != nil {
		return nil, err
	}
	var in struct {
		Grant      string   `json:"grant"`
		HelpdeskID int64    `json:"helpdesk_id"`
		Messenger  []string `json:"messenger"`
		Instagram  []string `json:"instagram"`
		WhatsApp   []string `json:"whatsapp"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	picked := map[string][]string{"messenger": in.Messenger, "instagram": in.Instagram, "whatsapp": in.WhatsApp}
	total := 0
	for channel, ids := range picked {
		total += len(ids)
		for _, id := range ids {
			if !metaID(id) {
				return nil, invalid(channel)
			}
		}
	}
	if total == 0 || total > 100 {
		return nil, invalid("selection")
	}
	fl, ok := s.metaFlows.get(in.Grant, d.ID, state(r).user.ID, s.now(), false)
	if !ok || fl.token == "" {
		return nil, metaExpired
	}
	_, client, err := s.tenantHelpdesk(r.Context(), d, in.HelpdeskID)
	if err != nil {
		return nil, err
	}
	a, err := s.loadMetaApp(r.Context())
	if err != nil {
		return nil, err
	}
	if a.ID == "" {
		return nil, metaAppMissing
	}
	g := s.graph(a)
	// Assets are read again from Meta, so only granted ones connect.
	var adds []metaAdd
	if fl.kind == "pages" {
		pages, err := g.Pages(r.Context(), fl.token)
		if err != nil {
			return nil, metaFailed("meta_rejected", "Meta Pages could not be read", err)
		}
		adds = pageAdds(pages, picked, a.Secret)
	} else {
		phones, err := g.Phones(r.Context(), fl.token)
		if err != nil {
			return nil, metaFailed("meta_rejected", "WhatsApp numbers could not be read", err)
		}
		adds = phoneAdds(phones, picked["whatsapp"], fl.token, a.Secret)
	}
	created, failed := []any{}, []any{}
	for _, add := range adds {
		mc, code := s.importMeta(r.Context(), g, d, in.HelpdeskID, client, add)
		if code != "" {
			failed = append(failed, map[string]string{"channel": add.channel, "name": add.in.Name, "code": code})
			continue
		}
		created = append(created, metaJSON(mc))
	}
	if len(adds) < total {
		failed = append(failed, map[string]string{"code": "meta_missing"})
	}
	return map[string]any{"created": created, "failed": failed}, nil
}

// metaAdd is one channel to connect from a Meta login.
type metaAdd struct {
	channel string
	in      metaInput
	page    meta.Page
	phone   meta.Phone
}

func pageAdds(pages []meta.Page, picked map[string][]string, secret string) []metaAdd {
	var out []metaAdd
	for _, p := range pages {
		in := metaInput{WhatsApp: libredesk.WhatsApp{Name: clip(p.Name), AccessToken: p.AccessToken, AppSecret: secret}, PageID: p.ID}
		if slices.Contains(picked["messenger"], p.ID) {
			out = append(out, metaAdd{channel: "messenger", in: in, page: p})
		}
		if p.Instagram != nil && slices.Contains(picked["instagram"], p.Instagram.ID) {
			in.Name, in.PageID = clip("@"+p.Instagram.Username), p.Instagram.ID
			out = append(out, metaAdd{channel: "instagram", in: in, page: p})
		}
	}
	return out
}

func phoneAdds(phones []meta.Phone, picked []string, token, secret string) []metaAdd {
	var out []metaAdd
	for _, p := range phones {
		if slices.Contains(picked, p.ID) {
			name := strings.TrimSpace(p.VerifiedName + " " + p.Display)
			out = append(out, metaAdd{channel: "whatsapp", phone: p, in: metaInput{WhatsApp: libredesk.WhatsApp{
				Name: clip(name), PhoneNumberID: p.ID, WABAID: p.WABAID, AccessToken: token, AppSecret: secret}}})
		}
	}
	return out
}

// importMeta subscribes one asset to the app webhook and connects it. It
// returns an error code for the UI instead of failing the whole import.
func (s *Server) importMeta(ctx context.Context, g meta.App, d store.Domain, helpdeskID int64, client HelpdeskAPI, add metaAdd) (store.MetaChannel, string) {
	existing, err := s.st.MetaChannelsByExternalID(ctx, add.channel, add.in.externalID(add.channel))
	if err != nil {
		return store.MetaChannel{}, "internal_error"
	}
	if len(existing) > 0 {
		return store.MetaChannel{}, "meta_exists"
	}
	if add.channel == "whatsapp" {
		err = g.SubscribeWABA(ctx, add.phone.WABAID, add.in.AccessToken)
		if err == nil && add.phone.PlatformType != "CLOUD_API" {
			// frugal: the PIN is not shown; the owner resets it in WhatsApp Manager if they ever need it.
			var pin string
			if pin, err = whatsAppPIN(); err == nil {
				err = g.Register(ctx, add.phone.ID, add.in.AccessToken, pin)
			}
		}
	} else {
		err = g.SubscribePage(ctx, add.page.ID, add.page.AccessToken)
	}
	if err != nil {
		s.log.Warn("meta subscription failed", "channel", add.channel, "id", add.in.externalID(add.channel), "error", err)
		return store.MetaChannel{}, "meta_rejected"
	}
	mc, err := s.addMetaChannel(ctx, d, helpdeskID, client, add.channel, add.in)
	if err != nil {
		s.log.Warn("meta channel import failed", "channel", add.channel, "error", err)
		if ae, ok := errors.AsType[*apiError](err); ok {
			return mc, ae.code
		}
		return mc, "helpdesk_error"
	}
	return mc, ""
}

// metaWebhookVerify answers Meta's check of the app webhook.
func (s *Server) metaWebhookVerify(w http.ResponseWriter, r *http.Request) {
	a, err := s.loadMetaApp(r.Context())
	q := r.URL.Query()
	if err != nil || a.VerifyToken == "" || q.Get("hub.mode") != "subscribe" ||
		subtle.ConstantTimeCompare([]byte(q.Get("hub.verify_token")), []byte(a.VerifyToken)) != 1 {
		writeError(w, 403, "forbidden", "Invalid verify token")
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, q.Get("hub.challenge"))
}

// metaWebhookEvent passes a signed app webhook event, unchanged, to every
// helpdesk with a channel it names. Libredesk checks the signature again and
// ignores entries for channels it does not have.
func (s *Server) metaWebhookEvent(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, 400, "invalid_input", "Invalid body")
		return
	}
	a, err := s.loadMetaApp(r.Context())
	sig := r.Header.Get("X-Hub-Signature-256")
	if err != nil || !meta.ValidSignature(body, sig, a.Secret) {
		writeError(w, 403, "forbidden", "Invalid signature")
		return
	}
	var p struct {
		Object string `json:"object"`
		Entry  []struct {
			ID      string `json:"id"`
			Changes []struct {
				Value struct {
					Metadata struct {
						PhoneNumberID string `json:"phone_number_id"`
					} `json:"metadata"`
				} `json:"value"`
			} `json:"changes"`
		} `json:"entry"`
	}
	if err := json.Unmarshal(body, &p); err != nil {
		writeError(w, 400, "invalid_input", "Invalid body")
		return
	}
	channel, path := map[string]string{"page": "messenger", "instagram": "instagram", "whatsapp_business_account": "whatsapp"}[p.Object], "/webhooks/meta/"
	if channel == "whatsapp" {
		path = "/webhooks/whatsapp/"
	}
	// One inbox per helpdesk is enough: Libredesk routes by the ids inside.
	targets := map[int64]store.MetaChannel{}
	for _, e := range p.Entry {
		ids := []string{e.ID}
		if channel == "whatsapp" {
			ids = ids[:0]
			for _, c := range e.Changes {
				ids = append(ids, c.Value.Metadata.PhoneNumberID)
			}
		}
		for _, id := range ids {
			list, err := s.st.MetaChannelsByExternalID(r.Context(), channel, id)
			if err != nil {
				writeError(w, 500, "internal_error", "Internal error")
				return
			}
			for _, mc := range list {
				if _, ok := targets[mc.HelpdeskID]; !ok {
					targets[mc.HelpdeskID] = mc
				}
			}
		}
	}
	retry := false
	for _, mc := range targets {
		err := s.forwardMeta(r.Context(), mc, path, body, sig)
		if err == nil {
			continue
		}
		s.log.Warn("meta webhook forward failed", "helpdesk_id", mc.HelpdeskID, "inbox_id", mc.InboxID, "error", err)
		// A refusal will not change on retry; an outage may.
		if ae, ok := errors.AsType[*libredesk.APIError](err); !ok || ae.Status >= 500 {
			retry = true
		}
	}
	if retry {
		writeError(w, 502, "upstream_error", "Helpdesk unavailable")
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) forwardMeta(ctx context.Context, mc store.MetaChannel, path string, body []byte, sig string) error {
	h, err := s.st.GetHelpdesk(ctx, mc.HelpdeskID)
	if err != nil {
		return err
	}
	client, err := s.helpdeskClient(h)
	if err != nil {
		return err
	}
	return client.ForwardWebhook(ctx, path+strconv.FormatInt(mc.InboxID, 10), body, sig)
}
