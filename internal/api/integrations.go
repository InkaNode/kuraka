package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"
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
	ListRecords(context.Context, string) ([]cloudflare.Record, error)
	CreateRecord(context.Context, string, cloudflare.Record) (cloudflare.Record, error)
	UpdateRecord(context.Context, string, string, cloudflare.Record) (cloudflare.Record, error)
	DeleteRecord(context.Context, string, string) error
	SetRedirect(context.Context, string, string) error
}

type HelpdeskAPI interface {
	Ping(context.Context) error
	ListInboxes(context.Context) ([]libredesk.Inbox, error)
	CreateEmailInbox(context.Context, libredesk.NewEmailInbox) (int64, error)
	DeleteInbox(context.Context, int64) error
}

var _ DNSAPI = (*cloudflare.Client)(nil)
var _ HelpdeskAPI = (*libredesk.Client)(nil)

func (s *Server) integrationRoutes(route func(string, string, bool, bool, endpoint)) {
	route("GET /api/v1/dns-providers", "dns_provider.list", false, false, s.dnsProviders)
	route("POST /api/v1/dns-providers", "dns_provider.create", false, false, s.createDNSProvider)
	route("DELETE /api/v1/dns-providers/{id}", "dns_provider.delete", false, false, s.deleteDNSProvider)
	route("PUT /api/v1/domains/{id}/dns-provider", "domain.dns_provider", false, false, s.assignDNSProvider)
	route("GET /api/v1/domains/{id}/dns/check", "dns.check", false, false, s.checkDNS)
	route("POST /api/v1/domains/{id}/dns/plan", "dns.plan", false, false, s.planDNS)
	route("POST /api/v1/domains/{id}/dns/apply", "dns.apply", false, false, s.applyDNS)
	route("PUT /api/v1/domains/{id}/alias-redirect", "domain.alias_redirect", false, false, s.aliasRedirect)
	route("GET /api/v1/helpdesks", "helpdesk.list", false, false, s.helpdesks)
	route("POST /api/v1/helpdesks", "helpdesk.create", false, false, s.createHelpdesk)
	route("DELETE /api/v1/helpdesks/{id}", "helpdesk.delete", false, false, s.deleteHelpdesk)
	route("GET /api/v1/helpdesks/bundle", "helpdesk.bundle", false, true, s.helpdeskBundle)
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
		p, err := s.st.GetDNSProvider(r.Context(), *in.ProviderID)
		if err != nil {
			return nil, err
		}
		if p.TenantID == nil {
			if err := authz.RequireSuperadmin(state(r).principal); err != nil {
				return nil, err
			}
		} else if *p.TenantID != d.TenantID {
			return nil, authz.ErrNotFound
		}
	}
	if err := s.st.SetDomainDNSProvider(r.Context(), d.ID, in.ProviderID); err != nil {
		return nil, err
	}
	d.DNSProviderID = in.ProviderID
	return domainJSON(d), nil
}

func helpdeskJSON(h store.Helpdesk) map[string]any {
	return map[string]any{"id": h.ID, "tenant_id": h.TenantID, "kind": h.Kind, "name": h.Name, "url": h.URL, "created_at": h.CreatedAt}
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

func (s *Server) helpdeskBundle(_ http.ResponseWriter, r *http.Request) (any, error) {
	id, err := positiveID(r.URL.Query().Get("tenant_id"), "tenant_id")
	if err != nil {
		return nil, err
	}
	tenant, err := s.st.GetTenant(r.Context(), id)
	if err != nil {
		return nil, err
	}
	data, err := provision.Bundle(tenant.Name, provision.Options{})
	return download{"application/zip", data}, err
}
