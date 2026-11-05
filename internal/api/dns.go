package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/InkaNode/kuraka/internal/authz"
	"github.com/InkaNode/kuraka/internal/cloudflare"
	"github.com/InkaNode/kuraka/internal/dnsplan"
	"github.com/InkaNode/kuraka/internal/stalwart"
	"github.com/InkaNode/kuraka/internal/store"
)

const managedComment = "managed by Kuraka"

func (s *Server) domainDNS(r *http.Request, d store.Domain) (stalwart.Domain, []stalwart.Record, error) {
	remote, err := s.sw.GetDomain(r.Context(), d.StalwartID)
	if err != nil {
		return remote, nil, upstream(err)
	}
	desired, err := dnsplan.Desired(d.Name, remote.ZoneFile, remote.Aliases)
	return remote, desired, err
}

func (s *Server) dnsClient(ctx context.Context, d store.Domain) (DNSAPI, error) {
	if d.DNSProviderID == nil {
		return nil, conflict("Domain has no DNS provider")
	}
	p, err := s.st.GetDNSProvider(ctx, *d.DNSProviderID)
	if err != nil {
		return nil, err
	}
	if p.TenantID != nil && *p.TenantID != d.TenantID {
		return nil, authz.ErrNotFound
	}
	// A global assignment was authorized by a superadmin; tenant admins can
	// operate their domain through that existing assignment.
	token, err := s.box.Open(p.TokenEnc)
	if err != nil {
		return nil, err
	}
	return s.newDNS(string(token)), nil
}

// usableProvider returns provider id when the caller may assign it to d:
// global providers need a superadmin, tenant ones must belong to d's tenant.
func (s *Server) usableProvider(r *http.Request, d store.Domain, id int64) (store.DNSProvider, error) {
	p, err := s.st.GetDNSProvider(r.Context(), id)
	if err != nil {
		return p, err
	}
	if p.TenantID == nil {
		return p, authz.RequireSuperadmin(state(r).principal)
	}
	if *p.TenantID != d.TenantID {
		return p, authz.ErrNotFound
	}
	return p, nil
}

// detectDNSProvider assigns the first usable provider that already hosts the
// domain's zone. It never fails the request: errors are logged and skipped.
func (s *Server) detectDNSProvider(r *http.Request, d *store.Domain) {
	ctx := r.Context()
	providers, err := s.st.ListDNSProviders(ctx, &d.TenantID, state(r).principal.IsSuperadmin())
	if err != nil {
		s.log.Warn("DNS provider detection failed", "domain_id", d.ID, "error", err)
		return
	}
	sort.SliceStable(providers, func(i, j int) bool { return providers[i].TenantID != nil && providers[j].TenantID == nil })
	for _, p := range providers {
		token, err := s.box.Open(p.TokenEnc)
		if err == nil {
			_, err = s.newDNS(string(token)).FindZone(ctx, d.Name)
		}
		if err == nil {
			err = s.st.SetDomainDNSProvider(ctx, d.ID, &p.ID)
		}
		if err != nil {
			s.log.Warn("DNS provider detection skipped provider", "domain_id", d.ID, "provider_id", p.ID, "error", err)
			continue
		}
		d.DNSProviderID = &p.ID
		return
	}
}

func (s *Server) createZone(_ http.ResponseWriter, r *http.Request) (any, error) {
	d, err := s.authorizedDomain(r)
	if err != nil {
		return nil, err
	}
	var in struct {
		ProviderID int64 `json:"provider_id"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	if in.ProviderID <= 0 {
		return nil, invalid("provider")
	}
	p, err := s.usableProvider(r, d, in.ProviderID)
	if err != nil {
		return nil, err
	}
	token, err := s.box.Open(p.TokenEnc)
	if err != nil {
		return nil, err
	}
	client := s.newDNS(string(token))
	if _, err := client.FindZone(r.Context(), d.Name); err == nil {
		return nil, conflict("Zone already exists in Cloudflare")
	} else if !errors.Is(err, cloudflare.ErrZoneNotFound) {
		return nil, integrationFailure("DNS zone lookup failed", err)
	}
	zone, err := client.CreateZone(r.Context(), d.Name)
	var ce *cloudflare.APIError
	if errors.As(err, &ce) && ce.Status >= 400 && ce.Status < 500 {
		return nil, &apiError{status: 422, code: "zone_permission", message: "Cloudflare refused to create the zone; the token needs Zone:Edit on the account", cause: err}
	}
	if err != nil {
		return nil, integrationFailure("DNS zone creation failed", err)
	}
	if err := s.st.SetDomainDNSProvider(r.Context(), d.ID, &p.ID); err != nil {
		return nil, err
	}
	d.DNSProviderID = &p.ID
	return reply{201, map[string]any{"domain": domainJSON(d), "zone": map[string]any{"name": zone.Name, "status": zone.Status, "name_servers": nonNilStrings(zone.NameServers)}}}, nil
}

type dnsSnapshot struct {
	actual []dnsplan.Existing
	zones  map[string]string // primary/alias name -> zone ID
}

func loadDNS(ctx context.Context, client DNSAPI, names []string) (dnsSnapshot, error) {
	snap := dnsSnapshot{zones: map[string]string{}}
	seen := map[string]bool{}
	for _, name := range names {
		zone, err := client.FindZone(ctx, name)
		if err != nil {
			return snap, integrationFailure("DNS zone lookup failed", err)
		}
		snap.zones[dnsName(name)] = zone.ID
		if seen[zone.ID] {
			continue
		}
		seen[zone.ID] = true
		records, err := client.ListRecords(ctx, zone.ID)
		if err != nil {
			return snap, integrationFailure("DNS records lookup failed", err)
		}
		for _, record := range records {
			content, priority := record.Content, record.Priority
			if record.Type == "SRV" && record.Data != nil {
				priority = record.Data.Priority
				content = fmt.Sprintf("%d %d %s", record.Data.Weight, record.Data.Port, record.Data.Target)
			}
			if record.Type == "TLSA" && record.TLSA != nil {
				content = fmt.Sprintf("%d %d %d %s", record.TLSA.Usage, record.TLSA.Selector, record.TLSA.MatchingType, record.TLSA.Certificate)
			}
			snap.actual = append(snap.actual, dnsplan.Existing{ID: record.ID, Type: record.Type, Name: record.Name, Content: content, Priority: priority, Managed: record.Comment == managedComment})
		}
	}
	return snap, nil
}

func dnsName(name string) string { return strings.ToLower(strings.TrimRight(name, ".")) }

func (snap dnsSnapshot) zoneFor(name string) string {
	name = dnsName(name)
	best, zone := "", ""
	for suffix, id := range snap.zones {
		if (name == suffix || strings.HasSuffix(name, "."+suffix)) && len(suffix) > len(best) {
			best, zone = suffix, id
		}
	}
	return zone
}

func (s *Server) checkDNS(_ http.ResponseWriter, r *http.Request) (any, error) {
	d, err := s.authorizedDomain(r)
	if err != nil {
		return nil, err
	}
	remote, desired, err := s.domainDNS(r, d)
	if err != nil {
		return nil, err
	}
	var statuses []dnsplan.RecordStatus
	if d.DNSProviderID == nil {
		statuses = dnsplan.CheckPublic(r.Context(), s.resolver, desired)
	} else {
		client, err := s.dnsClient(r.Context(), d)
		if err != nil {
			return nil, err
		}
		snap, err := loadDNS(r.Context(), client, append([]string{d.Name}, remote.Aliases...))
		if err != nil {
			return nil, err
		}
		statuses = dnsplan.CheckExisting(desired, snap.actual)
	}
	records := make([]any, 0, len(statuses))
	summary := map[string]int{"ok": 0, "missing": 0, "different": 0}
	for _, status := range statuses {
		found := status.Found
		if found == nil {
			found = []string{}
		}
		records = append(records, map[string]any{"name": status.Record.Name, "type": status.Record.Type, "value": status.Record.Value, "priority": status.Record.Priority, "status": status.Status, "found": found})
		summary[string(status.Status)]++
	}
	return map[string]any{"records": records, "summary": summary}, nil
}

func cloudflareRecord(r stalwart.Record) (cloudflare.Record, error) {
	out := cloudflare.Record{Type: r.Type, Name: r.Name, Content: r.Value, Priority: r.Priority, TTL: 1, Proxied: false, Comment: managedComment}
	if r.Type == "SRV" {
		fields := strings.Fields(r.Value)
		if len(fields) != 3 {
			return out, fmt.Errorf("invalid SRV record")
		}
		weight, e1 := strconv.Atoi(fields[0])
		port, e2 := strconv.Atoi(fields[1])
		if e1 != nil || e2 != nil || weight < 0 || weight > 65535 || port < 0 || port > 65535 || r.Priority < 0 || r.Priority > 65535 {
			return out, fmt.Errorf("invalid SRV record")
		}
		out.Content = ""
		out.Data = &cloudflare.SRVData{Priority: r.Priority, Weight: weight, Port: port, Target: fields[2]}
	}
	if r.Type == "TLSA" {
		fields := strings.Fields(r.Value)
		if len(fields) != 4 {
			return out, fmt.Errorf("invalid TLSA record")
		}
		usage, e1 := strconv.Atoi(fields[0])
		selector, e2 := strconv.Atoi(fields[1])
		matching, e3 := strconv.Atoi(fields[2])
		if e1 != nil || e2 != nil || e3 != nil || usage < 0 || usage > 255 || selector < 0 || selector > 255 || matching < 0 || matching > 255 {
			return out, fmt.Errorf("invalid TLSA record")
		}
		out.Content = ""
		out.TLSA = &cloudflare.TLSAData{Usage: usage, Selector: selector, MatchingType: matching, Certificate: fields[3]}
	}
	return out, nil
}

func (s *Server) planDNS(_ http.ResponseWriter, r *http.Request) (any, error) {
	return s.changeDNS(r, false)
}
func (s *Server) applyDNS(_ http.ResponseWriter, r *http.Request) (any, error) {
	return s.changeDNS(r, true)
}

func (s *Server) changeDNS(r *http.Request, apply bool) (any, error) {
	d, err := s.authorizedDomain(r)
	if err != nil {
		return nil, err
	}
	var in struct {
		ReplaceConflicts bool `json:"replace_conflicts"`
	}
	if r.ContentLength != 0 || len(r.TransferEncoding) > 0 {
		if err := decode(r, &in); err != nil {
			return nil, err
		}
	}
	remote, desired, err := s.domainDNS(r, d)
	if err != nil {
		return nil, err
	}
	client, err := s.dnsClient(r.Context(), d)
	if err != nil {
		return nil, err
	}
	snap, err := loadDNS(r.Context(), client, append([]string{d.Name}, remote.Aliases...))
	if err != nil {
		return nil, err
	}
	changes := dnsplan.Plan(desired, snap.actual)
	out := make([]any, 0, len(changes))
	applied := 0
	// Validate every outgoing record before making the first provider mutation.
	for _, c := range changes {
		if _, err := cloudflareRecord(c.Desired); err != nil {
			return nil, err
		}
	}
	deleted := map[string]bool{}
	for _, c := range changes {
		conflicts := make([]any, 0, len(c.Conflicts))
		for _, e := range c.Conflicts {
			conflicts = append(conflicts, map[string]any{"id": e.ID, "name": e.Name, "type": e.Type, "value": e.Content, "priority": e.Priority, "managed": e.Managed})
		}
		out = append(out, map[string]any{"op": c.Op, "name": c.Desired.Name, "type": c.Desired.Type, "value": c.Desired.Value, "priority": c.Desired.Priority, "conflicts": conflicts})
		if !apply || c.Op == dnsplan.OK || (c.Op == dnsplan.Conflict && !in.ReplaceConflicts) {
			continue
		}
		zone := snap.zoneFor(c.Desired.Name)
		if zone == "" {
			return nil, integrationFailure("DNS record has no matching zone", nil)
		}
		record, _ := cloudflareRecord(c.Desired)
		if c.Op == dnsplan.Update {
			_, err = client.UpdateRecord(r.Context(), zone, c.Existing.ID, record)
			if err != nil {
				return nil, integrationFailure("DNS update failed", err)
			}
		}
		for _, e := range c.Conflicts {
			key := zone + "/" + e.ID
			if deleted[key] {
				continue
			}
			if err := client.DeleteRecord(r.Context(), zone, e.ID); err != nil {
				return nil, integrationFailure("DNS conflict removal failed", err)
			}
			deleted[key] = true
		}
		if c.Op != dnsplan.Update {
			if _, err := client.CreateRecord(r.Context(), zone, record); err != nil {
				return nil, integrationFailure("DNS record creation failed", err)
			}
		}
		applied++
		state(r).detail = fmt.Sprintf("applied=%d", applied)
	}
	return map[string]any{"changes": out, "applied": applied}, nil
}

func (s *Server) aliasRedirect(_ http.ResponseWriter, r *http.Request) (any, error) {
	d, err := s.authorizedDomain(r)
	if err != nil {
		return nil, err
	}
	var in struct {
		Target string `json:"target"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	u, err := url.Parse(in.Target)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || strings.ContainsAny(in.Target, "\"'`\\?# \t\r\n") {
		return nil, invalid("target")
	}
	client, err := s.dnsClient(r.Context(), d)
	if err != nil {
		return nil, err
	}
	remote, err := s.sw.GetDomain(r.Context(), d.StalwartID)
	if err != nil {
		return nil, upstream(err)
	}
	type redirect struct {
		zone   cloudflare.Zone
		create bool
	}
	var plans []redirect
	for _, alias := range remote.Aliases {
		zone, err := client.FindZone(r.Context(), alias)
		if err != nil {
			return nil, integrationFailure("Alias zone lookup failed", err)
		}
		// SetRedirect affects an entire zone. Never redirect a parent zone for
		// an alias that is only a subdomain within that zone.
		if dnsName(zone.Name) != dnsName(alias) {
			return nil, conflict("Alias must have its own zone for a redirect")
		}
		records, err := client.ListRecords(r.Context(), zone.ID)
		if err != nil {
			return nil, integrationFailure("Alias DNS lookup failed", err)
		}
		create := true
		for _, record := range records {
			if dnsName(record.Name) != dnsName(alias) || (record.Type != "A" && record.Type != "AAAA" && record.Type != "CNAME") {
				continue
			}
			if record.Type != "A" || record.Content != "192.0.2.1" || !record.Proxied {
				return nil, conflict("Alias apex already has an address record")
			}
			create = false
		}
		plans = append(plans, redirect{zone, create})
	}
	for _, plan := range plans {
		if plan.create {
			if _, err := client.CreateRecord(r.Context(), plan.zone.ID, cloudflare.Record{Type: "A", Name: plan.zone.Name, Content: "192.0.2.1", TTL: 1, Proxied: true, Comment: managedComment}); err != nil {
				return nil, integrationFailure("Alias address creation failed", err)
			}
		}
		if err := client.SetRedirect(r.Context(), plan.zone.ID, in.Target); err != nil {
			return nil, integrationFailure("Alias redirect failed", err)
		}
	}
	return map[string]any{"redirected": len(plans)}, nil
}
