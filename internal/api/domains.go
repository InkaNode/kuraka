package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/InkaNode/kuraka/internal/authz"
	"github.com/InkaNode/kuraka/internal/config"
	"github.com/InkaNode/kuraka/internal/stalwart"
	"github.com/InkaNode/kuraka/internal/store"
)

func hostname(raw, field string) (string, error) {
	name := strings.ToLower(strings.TrimSpace(raw))
	if !config.ValidHostname(name) {
		return "", invalid(field)
	}
	return name, nil
}
func domainJSON(d store.Domain) map[string]any {
	return map[string]any{"id": d.ID, "name": d.Name, "tenant_id": d.TenantID, "dns_provider_id": d.DNSProviderID, "created_at": d.CreatedAt}
}
func (s *Server) domains(_ http.ResponseWriter, r *http.Request) (any, error) {
	rows, err := s.st.ListDomains(r.Context(), authz.TenantScope(state(r).principal))
	out := make([]any, 0, len(rows))
	for _, d := range rows {
		out = append(out, domainJSON(d))
	}
	return out, err
}
func (s *Server) authorizedDomain(r *http.Request) (store.Domain, error) {
	id, err := pathID(r)
	if err != nil {
		return store.Domain{}, err
	}
	d, err := s.az.Domain(r.Context(), state(r).principal, id)
	if err == nil {
		state(r).tenant = &d.TenantID
	}
	return d, err
}

// Besides primary names, reserve aliases of linked domains. Stalwart's name
// lookup finds primary domains; aliases are exposed on the domain itself.
func (s *Server) availableName(r *http.Request, name, exceptSID string) error {
	if _, err := s.st.GetDomainByName(r.Context(), name); err == nil {
		return conflict("Domain already linked")
	} else if !errors.Is(err, store.ErrNotFound) {
		return err
	}
	if _, err := s.sw.FindDomainByName(r.Context(), name); err == nil {
		return conflict("Domain exists in Stalwart; use import to link it")
	} else if !errors.Is(err, stalwart.ErrNotFound) {
		return upstream(err)
	}
	domains, err := s.st.ListDomains(r.Context(), nil)
	if err != nil {
		return err
	}
	for _, d := range domains {
		if d.StalwartID == exceptSID {
			continue
		}
		remote, err := s.sw.GetDomain(r.Context(), d.StalwartID)
		if err != nil {
			return upstream(err)
		}
		for _, alias := range remote.Aliases {
			if strings.EqualFold(alias, name) {
				return conflict("Domain name is already an alias")
			}
		}
	}
	return nil
}
func (s *Server) validateDomainAliases(r *http.Request, aliases []string, primary string, current stalwart.Domain) ([]string, error) {
	out := make([]string, 0, len(aliases))
	seen := map[string]bool{}
	existing := map[string]bool{}
	for _, a := range current.Aliases {
		existing[strings.ToLower(a)] = true
	}
	for _, raw := range aliases {
		name, err := hostname(raw, "aliases")
		if err != nil {
			return nil, err
		}
		if name == primary || seen[name] {
			return nil, invalid("aliases: duplicate or primary domain")
		}
		seen[name] = true
		if !existing[name] {
			if err := s.availableName(r, name, current.ID); err != nil {
				return nil, err
			}
		}
		out = append(out, name)
	}
	return out, nil
}
func (s *Server) createDomain(_ http.ResponseWriter, r *http.Request) (any, error) {
	tenant, err := pathID(r)
	if err != nil {
		return nil, err
	}
	state(r).tenant = &tenant
	var in struct {
		Name    string   `json:"name"`
		Aliases []string `json:"aliases"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	name, err := hostname(in.Name, "name")
	if err != nil {
		return nil, err
	}
	s.domainMu.Lock()
	defer s.domainMu.Unlock()
	if _, err := s.st.GetTenant(r.Context(), tenant); err != nil {
		return nil, err
	}
	if err := s.availableName(r, name, ""); err != nil {
		return nil, err
	}
	aliases, err := s.validateDomainAliases(r, in.Aliases, name, stalwart.Domain{})
	if err != nil {
		return nil, err
	}
	sid, err := s.sw.CreateDomain(r.Context(), name, "", aliases)
	if err != nil {
		return nil, upstream(err)
	}
	d, err := s.st.CreateDomain(r.Context(), tenant, name, sid)
	if err != nil {
		state(r).target = sid
		state(r).detail = fmt.Sprintf("Stalwart domain %s created but could not be linked", sid)
		// Preserve the store error's HTTP mapping, but audit this partial
		// operation as an error: the upstream domain needs a manual import.
		state(r).auditResult = "error"
		s.log.Error("could not link created domain", "stalwart_id", sid, "error", err)
		return nil, err
	}
	out := domainJSON(d)
	out["aliases"] = aliases
	return reply{201, out}, nil
}
func (s *Server) importDomain(_ http.ResponseWriter, r *http.Request) (any, error) {
	tenant, err := pathID(r)
	if err != nil {
		return nil, err
	}
	state(r).tenant = &tenant
	var in struct {
		Name string `json:"name"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	name, err := hostname(in.Name, "name")
	if err != nil {
		return nil, err
	}
	s.domainMu.Lock()
	defer s.domainMu.Unlock()
	if _, err := s.st.GetTenant(r.Context(), tenant); err != nil {
		return nil, err
	}
	if _, err := s.st.GetDomainByName(r.Context(), name); err == nil {
		return nil, conflict("Domain already linked")
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	remote, err := s.sw.FindDomainByName(r.Context(), name)
	if err != nil {
		return nil, upstream(err)
	}
	// Imported aliases obey the same ownership/name reservations as new ones.
	if _, err := s.validateDomainAliases(r, remote.Aliases, name, stalwart.Domain{ID: remote.ID}); err != nil {
		return nil, err
	}
	d, err := s.st.CreateDomain(r.Context(), tenant, name, remote.ID)
	if err != nil {
		return nil, err
	}
	out := domainJSON(d)
	out["aliases"] = nonNilStrings(remote.Aliases)
	return reply{201, out}, nil
}
func (s *Server) unlinkDomain(_ http.ResponseWriter, r *http.Request) (any, error) {
	s.domainMu.Lock()
	defer s.domainMu.Unlock()
	d, err := s.authorizedDomain(r)
	if err != nil {
		return nil, err
	}
	return reply{204, nil}, s.st.DeleteDomain(r.Context(), d.ID)
}
func nonNilStrings(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}
func recordsJSON(records []stalwart.Record) []any {
	out := make([]any, 0, len(records))
	for _, v := range records {
		out = append(out, map[string]any{"name": v.Name, "type": v.Type, "value": v.Value, "priority": v.Priority})
	}
	return out
}
func (s *Server) domain(_ http.ResponseWriter, r *http.Request) (any, error) {
	d, err := s.authorizedDomain(r)
	if err != nil {
		return nil, err
	}
	remote, err := s.sw.GetDomain(r.Context(), d.StalwartID)
	if err != nil {
		return nil, upstream(err)
	}
	keys, err := s.sw.ListDkim(r.Context(), d.StalwartID)
	if err != nil {
		return nil, upstream(err)
	}
	records, err := stalwart.ParseZone(remote.ZoneFile)
	if err != nil {
		return nil, upstream(err)
	}
	dkim := make([]any, 0, len(keys))
	for _, k := range keys {
		dkim = append(dkim, map[string]string{"selector": k.Selector, "algorithm": k.Algorithm, "stage": k.Stage})
	}
	aliasRecords := map[string]any{}
	for _, alias := range remote.Aliases {
		aliasRecords[alias] = recordsJSON(stalwart.RewriteForAlias(records, d.Name, alias))
	}
	out := domainJSON(d)
	out["aliases"] = nonNilStrings(remote.Aliases)
	out["dkim"] = dkim
	out["dns_records"] = recordsJSON(records)
	out["alias_dns_records"] = aliasRecords
	return out, nil
}
func (s *Server) domainAliases(_ http.ResponseWriter, r *http.Request) (any, error) {
	d, err := s.authorizedDomain(r)
	if err != nil {
		return nil, err
	}
	var in struct {
		Aliases *[]string `json:"aliases"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	if in.Aliases == nil {
		return nil, invalid("aliases")
	}
	s.domainMu.Lock()
	defer s.domainMu.Unlock()
	remote, err := s.sw.GetDomain(r.Context(), d.StalwartID)
	if err != nil {
		return nil, upstream(err)
	}
	aliases, err := s.validateDomainAliases(r, *in.Aliases, d.Name, remote)
	if err != nil {
		return nil, err
	}
	if err := s.sw.SetDomainAliases(r.Context(), d.StalwartID, aliases); err != nil {
		return nil, upstream(err)
	}
	return map[string]any{"aliases": aliases}, nil
}
