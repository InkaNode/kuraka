package store

import (
	"testing"
	"time"
)

func makeDomain(t *testing.T, s *Store, tenantID int64, name string) Domain {
	t.Helper()
	domain, err := s.CreateDomain(testContext, tenantID, name, "sid-"+name)
	mustOK(t, err)
	return domain
}

func TestIntegrationsMigration(t *testing.T) {
	s := newTestStore(t)
	var count int
	mustOK(t, s.db.QueryRow(`SELECT count(*) FROM schema_migrations WHERE version = 2`).Scan(&count))
	mustEqual(t, count, 1)
	for _, index := range []string{"dns_providers_tenant_id", "domains_dns_provider_id", "helpdesks_tenant_id",
		"helpdesk_links_domain_id", "helpdesk_links_helpdesk_id", "migration_jobs_domain_id", "migration_jobs_status"} {
		mustOK(t, s.db.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE type = 'index' AND name = ?`, index).Scan(&count))
		mustEqual(t, count, 1)
	}
}

func TestDNSProviders(t *testing.T) {
	s := newTestStore(t)
	tenant := makeTenant(t, s, "Tenant")
	other := makeTenant(t, s, "Other")
	owned, err := s.CreateDNSProvider(testContext, DNSProvider{TenantID: &tenant.ID, Kind: "cloudflare", Name: "cf", TokenEnc: "enc-1"})
	mustOK(t, err)
	if owned.ID <= 0 || owned.CreatedAt.Location() != time.UTC || owned.CreatedAt.Nanosecond() != 0 {
		t.Fatalf("invalid provider: %+v", owned)
	}
	global, err := s.CreateDNSProvider(testContext, DNSProvider{Kind: "cloudflare", Name: "global", TokenEnc: "enc-2"})
	mustOK(t, err)
	foreign, err := s.CreateDNSProvider(testContext, DNSProvider{TenantID: &other.ID, Kind: "cloudflare", Name: "foreign", TokenEnc: "enc-3"})
	mustOK(t, err)
	_, err = s.CreateDNSProvider(testContext, DNSProvider{Kind: "route53", Name: "bad", TokenEnc: "x"})
	mustErrorIs(t, err, ErrConflict)
	_, err = s.CreateDNSProvider(testContext, DNSProvider{TenantID: ptr(int64(999)), Kind: "cloudflare", Name: "bad", TokenEnc: "x"})
	mustErrorIs(t, err, ErrConflict)

	got, err := s.GetDNSProvider(testContext, owned.ID)
	mustOK(t, err)
	mustEqual(t, got, owned)
	got, err = s.GetDNSProvider(testContext, global.ID)
	mustOK(t, err)
	mustEqual(t, got, global)
	_, err = s.GetDNSProvider(testContext, 999)
	mustErrorIs(t, err, ErrNotFound)

	all, err := s.ListDNSProviders(testContext, nil, false)
	mustOK(t, err)
	mustEqual(t, all, []DNSProvider{owned, global, foreign})
	own, err := s.ListDNSProviders(testContext, &tenant.ID, false)
	mustOK(t, err)
	mustEqual(t, own, []DNSProvider{owned})
	usable, err := s.ListDNSProviders(testContext, &tenant.ID, true)
	mustOK(t, err)
	mustEqual(t, usable, []DNSProvider{owned, global})

	// Tenants with providers cannot be deleted.
	mustErrorIs(t, s.DeleteTenant(testContext, other.ID), ErrConflict)

	domain := makeDomain(t, s, tenant.ID, "example.com")
	mustEqual(t, domain.DNSProviderID, (*int64)(nil))
	mustOK(t, s.SetDomainDNSProvider(testContext, domain.ID, &owned.ID))
	gotDomain, err := s.GetDomain(testContext, domain.ID)
	mustOK(t, err)
	mustEqual(t, gotDomain.DNSProviderID, &owned.ID)
	domains, err := s.ListDomains(testContext, &tenant.ID)
	mustOK(t, err)
	mustEqual(t, domains, []Domain{gotDomain})
	mustErrorIs(t, s.SetDomainDNSProvider(testContext, domain.ID, ptr(int64(999))), ErrConflict)
	mustErrorIs(t, s.SetDomainDNSProvider(testContext, 999, &owned.ID), ErrNotFound)
	mustOK(t, s.SetDomainDNSProvider(testContext, domain.ID, nil))
	gotDomain, err = s.GetDomain(testContext, domain.ID)
	mustOK(t, err)
	mustEqual(t, gotDomain.DNSProviderID, (*int64)(nil))

	// Deleting a provider clears it from domains.
	mustOK(t, s.SetDomainDNSProvider(testContext, domain.ID, &owned.ID))
	mustOK(t, s.DeleteDNSProvider(testContext, owned.ID))
	gotDomain, err = s.GetDomain(testContext, domain.ID)
	mustOK(t, err)
	mustEqual(t, gotDomain.DNSProviderID, (*int64)(nil))
	_, err = s.GetDNSProvider(testContext, owned.ID)
	mustErrorIs(t, err, ErrNotFound)
	mustErrorIs(t, s.DeleteDNSProvider(testContext, owned.ID), ErrNotFound)
}

func TestHelpdesks(t *testing.T) {
	s := newTestStore(t)
	tenant := makeTenant(t, s, "Tenant")
	other := makeTenant(t, s, "Other")
	desk, err := s.CreateHelpdesk(testContext, Helpdesk{TenantID: tenant.ID, Kind: "libredesk", Name: "Desk",
		URL: "https://desk.example.com", APIKey: "key", APISecretEnc: "enc"})
	mustOK(t, err)
	if desk.ID <= 0 || desk.CreatedAt.IsZero() {
		t.Fatalf("invalid helpdesk: %+v", desk)
	}
	otherDesk, err := s.CreateHelpdesk(testContext, Helpdesk{TenantID: other.ID, Kind: "libredesk", Name: "Other",
		URL: "https://other.example.com", APIKey: "key2", APISecretEnc: "enc2"})
	mustOK(t, err)
	_, err = s.CreateHelpdesk(testContext, Helpdesk{TenantID: tenant.ID, Kind: "zendesk", Name: "bad", URL: "u", APIKey: "k", APISecretEnc: "e"})
	mustErrorIs(t, err, ErrConflict)
	_, err = s.CreateHelpdesk(testContext, Helpdesk{TenantID: 999, Kind: "libredesk", Name: "bad", URL: "u", APIKey: "k", APISecretEnc: "e"})
	mustErrorIs(t, err, ErrConflict)

	got, err := s.GetHelpdesk(testContext, desk.ID)
	mustOK(t, err)
	mustEqual(t, got, desk)
	_, err = s.GetHelpdesk(testContext, 999)
	mustErrorIs(t, err, ErrNotFound)
	all, err := s.ListHelpdesks(testContext, nil)
	mustOK(t, err)
	mustEqual(t, all, []Helpdesk{desk, otherDesk})
	own, err := s.ListHelpdesks(testContext, &tenant.ID)
	mustOK(t, err)
	mustEqual(t, own, []Helpdesk{desk})
	mustErrorIs(t, s.DeleteTenant(testContext, other.ID), ErrConflict)

	domain := makeDomain(t, s, tenant.ID, "example.com")
	second := makeDomain(t, s, tenant.ID, "example.org")
	link, err := s.CreateHelpdeskLink(testContext, HelpdeskLink{DomainID: domain.ID, AccountID: "acc1",
		HelpdeskID: desk.ID, InboxID: 7, AppPasswordID: "ap1"})
	mustOK(t, err)
	if link.ID <= 0 || link.CreatedAt.IsZero() {
		t.Fatalf("invalid link: %+v", link)
	}
	_, err = s.CreateHelpdeskLink(testContext, HelpdeskLink{DomainID: domain.ID, AccountID: "acc1",
		HelpdeskID: desk.ID, InboxID: 8, AppPasswordID: "ap2"})
	mustErrorIs(t, err, ErrConflict)
	_, err = s.CreateHelpdeskLink(testContext, HelpdeskLink{DomainID: 999, AccountID: "acc9",
		HelpdeskID: desk.ID, InboxID: 8, AppPasswordID: "ap2"})
	mustErrorIs(t, err, ErrConflict)
	link2, err := s.CreateHelpdeskLink(testContext, HelpdeskLink{DomainID: domain.ID, AccountID: "acc2",
		HelpdeskID: desk.ID, InboxID: 8, AppPasswordID: "ap2"})
	mustOK(t, err)
	link3, err := s.CreateHelpdeskLink(testContext, HelpdeskLink{DomainID: domain.ID, AccountID: "acc1",
		HelpdeskID: otherDesk.ID, InboxID: 9, AppPasswordID: "ap3"})
	mustOK(t, err)
	link4, err := s.CreateHelpdeskLink(testContext, HelpdeskLink{DomainID: second.ID, AccountID: "acc4",
		HelpdeskID: desk.ID, InboxID: 10, AppPasswordID: "ap4"})
	mustOK(t, err)

	gotLink, err := s.GetHelpdeskLink(testContext, link.ID)
	mustOK(t, err)
	mustEqual(t, gotLink, link)
	_, err = s.GetHelpdeskLink(testContext, 999)
	mustErrorIs(t, err, ErrNotFound)
	links, err := s.ListHelpdeskLinks(testContext, domain.ID, "")
	mustOK(t, err)
	mustEqual(t, links, []HelpdeskLink{link, link2, link3})
	links, err = s.ListHelpdeskLinks(testContext, domain.ID, "acc1")
	mustOK(t, err)
	mustEqual(t, links, []HelpdeskLink{link, link3})

	mustOK(t, s.DeleteHelpdeskLink(testContext, link2.ID))
	mustErrorIs(t, s.DeleteHelpdeskLink(testContext, link2.ID), ErrNotFound)

	// Deleting a helpdesk cascades to its links.
	mustOK(t, s.DeleteHelpdesk(testContext, otherDesk.ID))
	_, err = s.GetHelpdeskLink(testContext, link3.ID)
	mustErrorIs(t, err, ErrNotFound)
	mustErrorIs(t, s.DeleteHelpdesk(testContext, otherDesk.ID), ErrNotFound)

	// Deleting a domain cascades to its links only.
	mustOK(t, s.DeleteDomain(testContext, domain.ID))
	links, err = s.ListHelpdeskLinks(testContext, domain.ID, "")
	mustOK(t, err)
	mustEqual(t, len(links), 0)
	links, err = s.ListHelpdeskLinks(testContext, second.ID, "")
	mustOK(t, err)
	mustEqual(t, links, []HelpdeskLink{link4})
}

func newJob(domainID int64, account string) MigrationJob {
	return MigrationJob{DomainID: domainID, AccountID: account, SourceHost: "imap.old.example",
		SourcePort: 993, SourceTLS: "tls", SourceUser: account + "@old.example", SourceSecretEnc: "secret-" + account}
}

func TestMigrationJobs(t *testing.T) {
	s := newTestStore(t)
	tenant := makeTenant(t, s, "Tenant")
	admin := makeAdmin(t, s, "admin@example.com", &tenant.ID)
	domain := makeDomain(t, s, tenant.ID, "example.com")
	other := makeDomain(t, s, tenant.ID, "example.org")

	base := time.Unix(1_700_000_000, 0).UTC()
	// Created out of ID order to check that the oldest is claimed first.
	input := newJob(domain.ID, "b")
	input.CreatedBy = &admin.ID
	input.CreatedAt = base.Add(time.Minute)
	input.Status, input.Error, input.FoldersDone = "done", "ignored", 5
	jobB, err := s.CreateMigrationJob(testContext, input)
	mustOK(t, err)
	mustEqual(t, jobB.Status, "queued")
	mustEqual(t, jobB.Error, "")
	mustEqual(t, jobB.FoldersDone, 0)
	input = newJob(domain.ID, "a")
	input.CreatedAt = base
	jobA, err := s.CreateMigrationJob(testContext, input)
	mustOK(t, err)
	input = newJob(other.ID, "c")
	input.CreatedAt = base.Add(2 * time.Minute)
	jobC, err := s.CreateMigrationJob(testContext, input)
	mustOK(t, err)
	_, err = s.CreateMigrationJob(testContext, newJob(999, "x"))
	mustErrorIs(t, err, ErrConflict)

	got, err := s.GetMigrationJob(testContext, jobB.ID)
	mustOK(t, err)
	mustEqual(t, got, jobB)
	mustEqual(t, got.CreatedBy, &admin.ID)
	_, err = s.GetMigrationJob(testContext, 999)
	mustErrorIs(t, err, ErrNotFound)

	list, err := s.ListMigrationJobs(testContext, domain.ID, 10)
	mustOK(t, err)
	mustEqual(t, list, []MigrationJob{jobA, jobB})
	list, err = s.ListMigrationJobs(testContext, domain.ID, 0)
	mustOK(t, err)
	mustEqual(t, list, []MigrationJob{jobA})

	now := base.Add(time.Hour)
	claimed, err := s.ClaimNextMigrationJob(testContext, now)
	mustOK(t, err)
	mustEqual(t, claimed.ID, jobA.ID)
	mustEqual(t, claimed.Status, "running")
	mustEqual(t, claimed.StartedAt, &now)
	mustEqual(t, claimed.SourceSecretEnc, "secret-a")
	claimed, err = s.ClaimNextMigrationJob(testContext, now)
	mustOK(t, err)
	mustEqual(t, claimed.ID, jobB.ID)

	progress := MigrationProgress{FoldersTotal: 4, FoldersDone: 2, MessagesTotal: 100, MessagesCopied: 40, MessagesSkipped: 3}
	mustOK(t, s.UpdateMigrationProgress(testContext, jobA.ID, progress))
	mustErrorIs(t, s.UpdateMigrationProgress(testContext, 999, progress), ErrNotFound)
	got, err = s.GetMigrationJob(testContext, jobA.ID)
	mustOK(t, err)
	mustEqual(t, MigrationProgress{got.FoldersTotal, got.FoldersDone, got.MessagesTotal, got.MessagesCopied, got.MessagesSkipped}, progress)

	finished := now.Add(time.Minute)
	mustOK(t, s.FinishMigrationJob(testContext, jobA.ID, "failed", "boom", finished))
	got, err = s.GetMigrationJob(testContext, jobA.ID)
	mustOK(t, err)
	mustEqual(t, got.Status, "failed")
	mustEqual(t, got.Error, "boom")
	mustEqual(t, got.SourceSecretEnc, "")
	mustEqual(t, got.FinishedAt, &finished)
	if err := s.FinishMigrationJob(testContext, jobA.ID, "running", "", finished); err == nil {
		t.Fatal("non-final status accepted")
	}
	mustErrorIs(t, s.FinishMigrationJob(testContext, 999, "done", "", finished), ErrNotFound)

	// Running job: cancel is only flagged.
	requested, err := s.IsMigrationCancelRequested(testContext, jobB.ID)
	mustOK(t, err)
	mustEqual(t, requested, false)
	mustOK(t, s.RequestCancelMigration(testContext, jobB.ID))
	requested, err = s.IsMigrationCancelRequested(testContext, jobB.ID)
	mustOK(t, err)
	mustEqual(t, requested, true)
	got, err = s.GetMigrationJob(testContext, jobB.ID)
	mustOK(t, err)
	mustEqual(t, got.Status, "running")
	mustEqual(t, got.SourceSecretEnc, "secret-b")

	// Queued job: cancelled immediately and secret wiped.
	mustOK(t, s.RequestCancelMigration(testContext, jobC.ID))
	got, err = s.GetMigrationJob(testContext, jobC.ID)
	mustOK(t, err)
	mustEqual(t, got.Status, "cancelled")
	mustEqual(t, got.SourceSecretEnc, "")
	if got.FinishedAt == nil {
		t.Fatal("cancelled job has no finish time")
	}
	mustErrorIs(t, s.RequestCancelMigration(testContext, jobC.ID), ErrConflict)
	mustErrorIs(t, s.RequestCancelMigration(testContext, 999), ErrNotFound)
	_, err = s.IsMigrationCancelRequested(testContext, 999)
	mustErrorIs(t, err, ErrNotFound)

	_, err = s.ClaimNextMigrationJob(testContext, now)
	mustErrorIs(t, err, ErrNotFound)

	// Restart: running jobs fail as interrupted.
	restart := now.Add(time.Hour)
	n, err := s.FailInterruptedMigrations(testContext, restart)
	mustOK(t, err)
	mustEqual(t, n, int64(1))
	got, err = s.GetMigrationJob(testContext, jobB.ID)
	mustOK(t, err)
	mustEqual(t, got.Status, "failed")
	mustEqual(t, got.Error, "interrupted")
	mustEqual(t, got.SourceSecretEnc, "")
	mustEqual(t, got.FinishedAt, &restart)
	got, err = s.GetMigrationJob(testContext, jobA.ID)
	mustOK(t, err)
	mustEqual(t, got.Error, "boom")
	n, err = s.FailInterruptedMigrations(testContext, restart)
	mustOK(t, err)
	mustEqual(t, n, int64(0))

	// Deleting a domain cascades to its jobs.
	mustOK(t, s.DeleteDomain(testContext, domain.ID))
	_, err = s.GetMigrationJob(testContext, jobA.ID)
	mustErrorIs(t, err, ErrNotFound)
	list, err = s.ListMigrationJobs(testContext, domain.ID, 10)
	mustOK(t, err)
	mustEqual(t, len(list), 0)
	_, err = s.GetMigrationJob(testContext, jobC.ID)
	mustOK(t, err)
}
