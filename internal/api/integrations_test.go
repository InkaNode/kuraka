package api

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"

	"github.com/InkaNode/kuraka/internal/cloudflare"
	"github.com/InkaNode/kuraka/internal/libredesk"
	"github.com/InkaNode/kuraka/internal/stalwart"
)

type fakeDNS struct {
	mu                        sync.Mutex
	records                   map[string][]cloudflare.Record
	found                     []string
	created, updated, deleted []cloudflare.Record
	redirects                 map[string]string
	verifyErr                 error
	next                      int
}

func (f *fakeDNS) VerifyToken(context.Context) error { return f.verifyErr }
func (f *fakeDNS) FindZone(_ context.Context, name string) (cloudflare.Zone, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.found = append(f.found, name)
	return cloudflare.Zone{ID: name, Name: name}, nil
}
func (f *fakeDNS) ListRecords(_ context.Context, zone string) ([]cloudflare.Record, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]cloudflare.Record{}, f.records[zone]...), nil
}
func (f *fakeDNS) CreateRecord(_ context.Context, zone string, r cloudflare.Record) (cloudflare.Record, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next++
	r.ID = fmt.Sprintf("r%d", f.next)
	if f.records == nil {
		f.records = map[string][]cloudflare.Record{}
	}
	f.records[zone] = append(f.records[zone], r)
	f.created = append(f.created, r)
	return r, nil
}
func (f *fakeDNS) UpdateRecord(_ context.Context, zone, id string, r cloudflare.Record) (cloudflare.Record, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r.ID = id
	for i, old := range f.records[zone] {
		if old.ID == id {
			f.records[zone][i] = r
		}
	}
	f.updated = append(f.updated, r)
	return r, nil
}
func (f *fakeDNS) DeleteRecord(_ context.Context, zone, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []cloudflare.Record{}
	for _, r := range f.records[zone] {
		if r.ID == id {
			f.deleted = append(f.deleted, r)
		} else {
			out = append(out, r)
		}
	}
	f.records[zone] = out
	return nil
}
func (f *fakeDNS) SetRedirect(_ context.Context, zone, target string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.redirects == nil {
		f.redirects = map[string]string{}
	}
	f.redirects[zone] = target
	return nil
}

type fakeHelpdesk struct {
	mu                            sync.Mutex
	created                       []libredesk.NewEmailInbox
	deleted                       []int64
	pingErr, createErr, deleteErr error
}

func (f *fakeHelpdesk) Ping(context.Context) error { return f.pingErr }
func (f *fakeHelpdesk) ListInboxes(context.Context) ([]libredesk.Inbox, error) {
	return []libredesk.Inbox{}, nil
}
func (f *fakeHelpdesk) CreateEmailInbox(_ context.Context, in libredesk.NewEmailInbox) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.created = append(f.created, in)
	return int64(len(f.created)), f.createErr
}
func (f *fakeHelpdesk) DeleteInbox(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, id)
	return f.deleteErr
}

type fakeResolver struct{}

func (fakeResolver) LookupMX(context.Context, string) ([]*net.MX, error) {
	return []*net.MX{{Host: "mail.test.", Pref: 10}}, nil
}
func (fakeResolver) LookupTXT(context.Context, string) ([]string, error) {
	return []string{"v=spf1 -all"}, nil
}
func (fakeResolver) LookupCNAME(context.Context, string) (string, error) { return "", nil }
func (fakeResolver) LookupSRV(context.Context, string, string, string) (string, []*net.SRV, error) {
	return "", nil, nil
}

func integrations(t *testing.T) (*harness, *fakeDNS, *fakeHelpdesk) {
	h := newHarness(t, true)
	dns := &fakeDNS{records: map[string][]cloudflare.Record{}}
	hd := &fakeHelpdesk{}
	h.s.newDNS = func(string) DNSAPI { return dns }
	h.s.newHelpdesk = func(string, string, string) HelpdeskAPI { return hd }
	h.s.resolver = fakeResolver{}
	h.s.cfg.MailHost = "mail.test"
	h.s.cfg.MailTLSSkipVerify = true
	return h, dns, hd
}

func provider(t *testing.T, c *testClient, tenant any) int64 {
	t.Helper()
	b := c.do("POST", "/dns-providers", map[string]any{"tenant_id": tenant, "name": "DNS", "token": "private-token"}, 201)
	if strings.Contains(string(b), "token") {
		t.Fatalf("token echoed: %s", b)
	}
	return int64(object(t, b)["id"].(float64))
}
func helpdesk(t *testing.T, c *testClient, tenant int64) int64 {
	t.Helper()
	b := c.do("POST", "/helpdesks", map[string]any{"tenant_id": tenant, "name": "Support", "url": "http://desk.test", "api_key": "private-key", "api_secret": "private-secret"}, 201)
	if strings.Contains(string(b), "private-") || strings.Contains(string(b), "api_secret") {
		t.Fatalf("credentials echoed: %s", b)
	}
	return int64(object(t, b)["id"].(float64))
}
func mailboxPath(t *testing.T, h *harness, domain int64) string {
	t.Helper()
	base := fmt.Sprintf("/domains/%d/mailboxes", domain)
	b := h.root.do("POST", base, map[string]any{"name": "support", "display_name": "Support Team"}, 201)
	return base + "/" + object(t, b)["id"].(string)
}
func deniedAudit(t *testing.T, c *testClient, action string) {
	t.Helper()
	for _, row := range array(t, c.do("GET", "/audit", nil, 200)) {
		if row["action"] == action && row["result"] == "denied" {
			return
		}
	}
	t.Fatalf("missing denied audit %s", action)
}

func TestProviderOwnershipAndSecrets(t *testing.T) {
	h, dns, _ := integrations(t)
	a, b := h.tenant("A"), h.tenant("B")
	da, db := h.domain(a, "a.test"), h.domain(b, "b.test")
	ca, cb := h.admin(a, "a@admin.test"), h.admin(b, "b@admin.test")
	pa := provider(t, ca, a)
	global := provider(t, h.root, nil)
	stored, err := h.st.GetDNSProvider(context.Background(), pa)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := h.s.box.Open(stored.TokenEnc)
	if err != nil || string(plain) != "private-token" || stored.TokenEnc == "private-token" {
		t.Fatal("provider token not encrypted")
	}
	if list := ca.do("GET", "/dns-providers", nil, 200); len(array(t, list)) != 1 || strings.Contains(string(list), "token") {
		t.Fatalf("bad provider list %s", list)
	}
	ca.do("POST", "/dns-providers", map[string]any{"name": "global", "token": "secret"}, 404)
	ca.do("POST", "/dns-providers", map[string]any{"tenant_id": b, "name": "foreign", "token": "secret"}, 404)
	cb.do("DELETE", fmt.Sprintf("/dns-providers/%d", pa), nil, 404)
	cb.do("GET", fmt.Sprintf("/dns-providers?tenant_id=%d", a), nil, 404)
	cb.do("PUT", fmt.Sprintf("/domains/%d/dns-provider", db), map[string]any{"provider_id": pa}, 404)
	ca.do("PUT", fmt.Sprintf("/domains/%d/dns-provider", da), map[string]any{"provider_id": global}, 403)
	h.root.do("PUT", fmt.Sprintf("/domains/%d/dns-provider", db), map[string]any{"provider_id": pa}, 404)
	ca.do("PUT", fmt.Sprintf("/domains/%d/dns-provider", da), map[string]any{"provider_id": pa}, 200)
	h.root.do("PUT", fmt.Sprintf("/domains/%d/dns-provider", db), map[string]any{"provider_id": global}, 200)
	cb.do("POST", fmt.Sprintf("/domains/%d/dns/plan", db), nil, 200)
	deniedAudit(t, cb, "domain.dns_provider")
	deniedAudit(t, cb, "dns_provider.delete")
	deniedAudit(t, cb, "dns_provider.list")
	ca.do("PUT", fmt.Sprintf("/domains/%d/dns-provider", da), map[string]any{"provider_id": nil}, 200)
	ca.do("DELETE", fmt.Sprintf("/dns-providers/%d", pa), nil, 204)
	dns.verifyErr = errors.New("bad private-token")
	h.root.do("POST", "/dns-providers", map[string]any{"tenant_id": a, "name": "bad", "token": "private-token"}, 502)
	if rows, err := h.st.ListDNSProviders(context.Background(), &a, false); err != nil || len(rows) != 0 {
		t.Fatal("invalid token persisted")
	}
}

func TestDNSPlanApplyAndAliasZones(t *testing.T) {
	h, dns, _ := integrations(t)
	tenant := h.tenant("A")
	domain := h.domain(tenant, "a.test")
	p := provider(t, h.root, tenant)
	base := fmt.Sprintf("/domains/%d", domain)
	h.root.do("PUT", base+"/aliases", map[string]any{"aliases": []string{"alias.test"}}, 200)
	h.root.do("PUT", base+"/dns-provider", map[string]any{"provider_id": p}, 200)
	dns.records["a.test"] = []cloudflare.Record{{ID: "old-mx", Name: "a.test", Type: "MX", Content: "purelymail.test", Priority: 10}, {ID: "other", Name: "www.a.test", Type: "A", Content: "192.0.2.9"}}
	plan := object(t, h.root.do("POST", base+"/dns/plan", nil, 200))
	changes := plan["changes"].([]any)
	if changes[0].(map[string]any)["op"] != "conflict" {
		t.Fatalf("plan: %v", plan)
	}
	h.root.do("POST", base+"/dns/apply", map[string]any{"replace_conflicts": false}, 200)
	if len(dns.deleted) != 0 {
		t.Fatal("unmanaged MX removed without consent")
	}
	h.root.do("POST", base+"/dns/apply", map[string]any{"replace_conflicts": true}, 200)
	if len(dns.deleted) != 1 || dns.deleted[0].ID != "old-mx" {
		t.Fatalf("deleted: %v", dns.deleted)
	}
	check := object(t, h.root.do("GET", base+"/dns/check", nil, 200))
	if check["summary"].(map[string]any)["missing"] != float64(0) {
		t.Fatalf("check: %v", check)
	}
	if !strings.Contains(strings.Join(dns.found, ","), "alias.test") {
		t.Fatal("alias zone not queried")
	}
	for _, r := range dns.created {
		if r.Comment != managedComment || r.TTL != 1 || r.Proxied {
			t.Fatalf("record: %+v", r)
		}
	}
	// Managed updates also remove additional differing managed records.
	dns.records["a.test"] = []cloudflare.Record{{ID: "managed1", Name: "a.test", Type: "MX", Content: "old.test", Priority: 10, Comment: managedComment}, {ID: "managed2", Name: "a.test", Type: "MX", Content: "older.test", Priority: 10, Comment: managedComment}}
	h.root.do("POST", base+"/dns/apply", nil, 200)
	if len(dns.updated) != 1 || dns.updated[0].ID != "managed1" || len(dns.deleted) != 2 {
		t.Fatal("managed duplicates not updated/removed")
	}
	for _, row := range array(t, h.root.do("GET", "/audit", nil, 200)) {
		if row["action"] == "dns.apply" && row["result"] == "ok" {
			return
		}
	}
	t.Fatal("missing DNS audit")
}

func TestPublicDNSAndRedirect(t *testing.T) {
	h, dns, _ := integrations(t)
	tenant := h.tenant("A")
	domain := h.domain(tenant, "a.test")
	base := fmt.Sprintf("/domains/%d", domain)
	check := object(t, h.root.do("GET", base+"/dns/check", nil, 200))
	if check["summary"].(map[string]any)["ok"] != float64(2) {
		t.Fatalf("public check: %v", check)
	}
	h.root.do("POST", base+"/dns/plan", nil, 409)
	h.root.do("PUT", base+"/aliases", map[string]any{"aliases": []string{"alias.test"}}, 200)
	p := provider(t, h.root, tenant)
	h.root.do("PUT", base+"/dns-provider", map[string]any{"provider_id": p}, 200)
	h.root.do("PUT", base+"/alias-redirect", map[string]any{"target": "https://a.test"}, 200)
	if dns.redirects["alias.test"] != "https://a.test" || len(dns.created) != 1 || !dns.created[0].Proxied || dns.created[0].Content != "192.0.2.1" {
		t.Fatal("redirect not configured")
	}
	h.root.do("PUT", base+"/alias-redirect", map[string]any{"target": "https://a.test"}, 200)
	if len(dns.created) != 1 {
		t.Fatal("duplicate alias address")
	}
	dns.records["alias.test"] = append(dns.records["alias.test"], cloudflare.Record{ID: "conflict", Name: "alias.test", Type: "AAAA", Content: "::1"})
	h.root.do("PUT", base+"/alias-redirect", map[string]any{"target": "https://a.test"}, 409)
	h.root.do("PUT", base+"/alias-redirect", map[string]any{"target": "http://a.test"}, 422)
}

func TestDNSRecordMapping(t *testing.T) {
	for _, desired := range []stalwart.Record{
		{Type: "MX", Name: "example.test", Value: "mail.test", Priority: 10},
		{Type: "TXT", Name: "example.test", Value: "v=spf1 -all"},
		{Type: "SRV", Name: "_imaps._tcp.example.test", Value: "5 993 mail.test", Priority: 10},
	} {
		r, err := cloudflareRecord(desired)
		if err != nil {
			t.Fatal(err)
		}
		if r.TTL != 1 || r.Proxied || r.Comment != managedComment || r.Priority != desired.Priority {
			t.Fatalf("bad record: %+v", r)
		}
		if desired.Type == "SRV" {
			if r.Data == nil || r.Data.Priority != 10 || r.Data.Weight != 5 || r.Data.Port != 993 || r.Data.Target != "mail.test" {
				t.Fatalf("bad SRV: %+v", r.Data)
			}
		} else if r.Content != desired.Value {
			t.Fatal("content altered")
		}
	}
	for _, value := range []string{"invalid", "x 993 mail.test", "5 x mail.test", "5 70000 mail.test"} {
		if _, err := cloudflareRecord(stalwart.Record{Type: "SRV", Value: value}); err == nil {
			t.Fatalf("accepted SRV %q", value)
		}
	}
}

func TestHelpdeskVerificationAndDeleteRevokesLinks(t *testing.T) {
	h, _, hd := integrations(t)
	tenant := h.tenant("A")
	domain := h.domain(tenant, "a.test")
	path := mailboxPath(t, h, domain)
	hd.pingErr = errors.New("private-secret")
	h.root.do("POST", "/helpdesks", map[string]any{"tenant_id": tenant, "name": "bad", "url": "http://desk.test", "api_key": "private-key", "api_secret": "private-secret"}, 502)
	if list := array(t, h.root.do("GET", "/helpdesks", nil, 200)); len(list) != 0 {
		t.Fatal("unverified helpdesk saved")
	}
	hd.pingErr = nil
	id := helpdesk(t, h.root, tenant)
	h.root.do("POST", path+"/helpdesk", map[string]any{"helpdesk_id": id}, 201)
	hd.deleteErr = errors.New("unavailable")
	h.root.do("DELETE", fmt.Sprintf("/helpdesks/%d", id), nil, 204)
	if h.sw.appDeleted != 1 || len(hd.deleted) != 1 || len(h.sw.appPasswords) != 0 || len(array(t, h.root.do("GET", path+"/helpdesk", nil, 200))) != 0 {
		t.Fatal("helpdesk deletion leaked link credential")
	}
}

func TestHelpdeskConnectDisconnectRollbackAndOwnership(t *testing.T) {
	h, _, hd := integrations(t)
	a, b := h.tenant("A"), h.tenant("B")
	da, db := h.domain(a, "a.test"), h.domain(b, "b.test")
	ca, cb := h.admin(a, "a@admin.test"), h.admin(b, "b@admin.test")
	ha := helpdesk(t, ca, a)
	pa, pb := mailboxPath(t, h, da), mailboxPath(t, h, db)
	stored, err := h.st.GetHelpdesk(context.Background(), ha)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := h.s.box.Open(stored.APISecretEnc)
	if err != nil || string(plain) != "private-secret" {
		t.Fatal("helpdesk secret not encrypted")
	}
	cb.do("POST", pb+"/helpdesk", map[string]any{"helpdesk_id": ha}, 404)
	h.root.do("POST", pb+"/helpdesk", map[string]any{"helpdesk_id": ha}, 404)
	cb.do("DELETE", fmt.Sprintf("/helpdesks/%d", ha), nil, 404)
	cb.do("GET", fmt.Sprintf("/helpdesks?tenant_id=%d", a), nil, 404)
	if list := ca.do("GET", "/helpdesks", nil, 200); len(array(t, list)) != 1 || strings.Contains(string(list), "private-") {
		t.Fatal("helpdesk list leaked")
	}
	link := object(t, ca.do("POST", pa+"/helpdesk", map[string]any{"helpdesk_id": ha, "inbox_name": "Support"}, 201))
	if h.sw.appCreated != 1 || len(hd.created) != 1 || h.sw.appDescriptions[0] != "libredesk:Support" {
		t.Fatal("missing password/inbox")
	}
	in := hd.created[0]
	if in.From != "Support Team <support@a.test>" || in.IMAP.Host != "mail.test" || in.IMAP.Port != 993 || in.SMTP.Port != 465 || in.IMAP.TLS != "tls" || in.SMTP.TLS != "tls" || !in.IMAP.SkipVerify || in.IMAP.Username != "support@a.test" || in.IMAP.Password == "" || in.SMTP.Password != in.IMAP.Password {
		t.Fatalf("inbox: %+v", in)
	}
	if links := ca.do("GET", pa+"/helpdesk", nil, 200); len(array(t, links)) != 1 || strings.Contains(string(links), "app-secret") {
		t.Fatalf("links: %s", links)
	}
	ca.do("POST", pa+"/helpdesk", map[string]any{"helpdesk_id": ha}, 409)
	cb.do("DELETE", fmt.Sprintf("%s/helpdesk/%.0f", pb, link["id"]), nil, 404)
	hd.deleteErr = libredesk.ErrNotFound
	ca.do("DELETE", fmt.Sprintf("%s/helpdesk/%.0f", pa, link["id"]), nil, 204)
	if h.sw.appDeleted != 1 || len(hd.deleted) != 1 || len(h.sw.appPasswords) != 0 {
		t.Fatal("disconnect did not revoke both")
	}
	hd.createErr = errors.New("inbox failure")
	ca.do("POST", pa+"/helpdesk", map[string]any{"helpdesk_id": ha}, 502)
	if h.sw.appCreated != 2 || h.sw.appDeleted != 2 || len(h.sw.appPasswords) != 0 {
		t.Fatal("failed inbox leaked app password")
	}
	if len(array(t, ca.do("GET", pa+"/helpdesk", nil, 200))) != 0 {
		t.Fatal("rollback persisted link")
	}
	deniedAudit(t, cb, "mailbox.helpdesk.connect")
	deniedAudit(t, cb, "mailbox.helpdesk.disconnect")
	ca.do("DELETE", fmt.Sprintf("/helpdesks/%d", ha), nil, 204)
}

func TestMailHostAndBundle(t *testing.T) {
	h, _, _ := integrations(t)
	tenant := h.tenant("A")
	domain := h.domain(tenant, "a.test")
	path := mailboxPath(t, h, domain)
	h.s.cfg.MailHost = ""
	assertCode(t, h.root.do("POST", path+"/helpdesk", map[string]any{"helpdesk_id": 1}, 409), "mail_host_not_configured")
	parts := strings.Split(path, "/")
	body := map[string]any{"account_id": parts[len(parts)-1], "source_host": "source.test", "source_port": 993, "source_password": "secret", "source_user": "user"}
	assertCode(t, h.root.do("POST", fmt.Sprintf("/domains/%d/migrations", domain), body, 409), "mail_host_not_configured")
	status, data, headers := h.root.raw("GET", fmt.Sprintf("/helpdesks/bundle?tenant_id=%d", tenant), "", "", "")
	if status != 200 || headers.Get("Content-Type") != "application/zip" {
		t.Fatalf("bundle status %d", status)
	}
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil || len(z.File) != 4 {
		t.Fatalf("bundle: %v", err)
	}
	c := h.admin(tenant, "admin@a.test")
	c.do("GET", fmt.Sprintf("/helpdesks/bundle?tenant_id=%d", tenant), nil, 403)
}

func TestMigrationsCreateListCancel(t *testing.T) {
	h, _, _ := integrations(t)
	a, b := h.tenant("A"), h.tenant("B")
	da, db := h.domain(a, "a.test"), h.domain(b, "b.test")
	ca, cb := h.admin(a, "a@admin.test"), h.admin(b, "b@admin.test")
	pa, pb := mailboxPath(t, h, da), mailboxPath(t, h, db)
	parts := strings.Split(pa, "/")
	account := parts[len(parts)-1]
	parts = strings.Split(pb, "/")
	foreign := parts[len(parts)-1]
	base := fmt.Sprintf("/domains/%d/migrations", da)
	body := map[string]any{"account_id": account, "source_host": "source.test", "source_port": 993, "source_tls": "tls", "source_user": "user", "source_password": "never-return-this"}
	created := ca.do("POST", base, body, 201)
	if strings.Contains(string(created), "never-return") || strings.Contains(string(created), "secret") || strings.Contains(string(created), "password") {
		t.Fatalf("migration leaks secret: %s", created)
	}
	id := int64(object(t, created)["id"].(float64))
	stored, err := h.st.GetMigrationJob(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := h.s.box.Open(stored.SourceSecretEnc)
	if err != nil || string(plain) != "never-return-this" {
		t.Fatal("migration secret not encrypted")
	}
	list := ca.do("GET", base, nil, 200)
	if len(array(t, list)) != 1 || strings.Contains(string(list), "never-return") || strings.Contains(string(list), stored.SourceSecretEnc) {
		t.Fatal("migration list leaks")
	}
	cb.do("GET", base, nil, 404)
	cb.do("POST", fmt.Sprintf("%s/%d/cancel", base, id), nil, 404)
	body["account_id"] = foreign
	ca.do("POST", base, body, 404)
	body["account_id"] = account
	for _, bad := range []struct {
		key   string
		value any
	}{{"source_host", "https://source.test"}, {"source_port", 0}, {"source_port", 65536}, {"source_tls", "invalid"}} {
		old := body[bad.key]
		body[bad.key] = bad.value
		ca.do("POST", base, body, 422)
		body[bad.key] = old
	}
	ca.do("POST", fmt.Sprintf("%s/%d/cancel", base, id), nil, 200)
	stored, err = h.st.GetMigrationJob(context.Background(), id)
	if err != nil || stored.Status != "cancelled" || stored.SourceSecretEnc != "" {
		t.Fatalf("cancel: %+v %v", stored, err)
	}
	ca.do("POST", fmt.Sprintf("%s/%d/cancel", base, id), nil, 409)
	deniedAudit(t, cb, "migration.cancel")
	deniedAudit(t, cb, "migration.list")
	deniedAudit(t, ca, "migration.create")
}
