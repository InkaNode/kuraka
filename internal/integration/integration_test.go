//go:build integration

package integration_test

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/InkaNode/kuraka/internal/stalwart"
)

func TestStalwartContract(t *testing.T) {
	sw := realStalwart(t)
	ctx := t.Context()
	suffix := randomHex(t, 8)
	name, aliasDomain := "contract-"+suffix+".test", "contract-"+suffix+"-alt.test"
	domainID, err := sw.CreateDomain(ctx, name, "CI contract", []string{aliasDomain})
	if err != nil {
		t.Fatalf("CreateDomain: %v", err)
	}
	domain, err := sw.GetDomain(ctx, domainID)
	if err != nil {
		t.Fatalf("GetDomain: %v", err)
	}
	if domain.ID != domainID || domain.Name != name || !slices.Contains(domain.Aliases, aliasDomain) {
		t.Fatalf("domain or alias did not round trip: %+v", domain)
	}
	records, err := stalwart.ParseZone(domain.ZoneFile)
	if err != nil {
		t.Fatalf("ParseZone: %v", err)
	}
	var mx, spf, dmarc bool
	for _, record := range records {
		mx = mx || (record.Type == "MX" && record.Name == name && record.Value != "")
		spf = spf || (record.Type == "TXT" && record.Name == name && strings.HasPrefix(record.Value, "v=spf1"))
		dmarc = dmarc || (record.Type == "TXT" && record.Name == "_dmarc."+name && strings.HasPrefix(record.Value, "v=DMARC1"))
	}
	if !mx || !spf || !dmarc {
		t.Fatalf("zone missing required records: MX=%t SPF=%t DMARC=%t", mx, spf, dmarc)
	}
	waitForDkim(t, sw, domainID)

	aliases := []stalwart.Alias{{Name: "alias-" + suffix, DomainID: domainID, Enable: true}}
	accountName := "user-" + suffix
	accountID, err := sw.CreateAccount(ctx, stalwart.NewAccount{
		Kind:     stalwart.KindUser,
		Name:     accountName,
		DomainID: domainID,
		Password: randomHex(t, 16),
		Aliases:  aliases,
	})
	if err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	cleanupAccount(t, sw, accountID)
	checkAccount := func(wantAliases []stalwart.Alias) {
		t.Helper()
		account, err := sw.GetAccount(ctx, accountID)
		if err != nil {
			t.Fatalf("GetAccount: %v", err)
		}
		if account.ID != accountID || account.Kind != stalwart.KindUser || account.Name != accountName || account.DomainID != domainID {
			t.Fatalf("account did not round trip: %+v", account)
		}
		if !slices.Equal(account.Aliases, wantAliases) {
			t.Fatalf("account aliases = %+v, want %+v", account.Aliases, wantAliases)
		}
	}
	checkAccount(aliases)
	password := randomHex(t, 16)
	aliases = []stalwart.Alias{{Name: "updated-" + suffix, DomainID: domainID, Enable: true}}
	if err := sw.UpdateAccount(ctx, accountID, stalwart.AccountPatch{Password: &password, Aliases: &aliases}); err != nil {
		t.Fatalf("UpdateAccount password and aliases: %v", err)
	}
	checkAccount(aliases)
	appID, secret, err := sw.CreateAppPassword(ctx, accountID, "CI contract")
	if err != nil {
		t.Fatalf("CreateAppPassword: %v", err)
	}
	if appID == "" || secret == "" {
		t.Fatal("CreateAppPassword returned an empty ID or secret")
	}
	if err := sw.DeleteAppPassword(ctx, accountID, appID); err != nil {
		t.Fatalf("DeleteAppPassword: %v", err)
	}
	if err := sw.DeleteAccount(ctx, accountID); err != nil {
		t.Fatalf("DeleteAccount: %v", err)
	}
	if _, err := sw.GetAccount(ctx, accountID); !errors.Is(err, stalwart.ErrNotFound) {
		t.Fatalf("GetAccount after deletion: got %v, want ErrNotFound", err)
	}
}

func TestTenantIsolationAndSMTP(t *testing.T) {
	sw := realStalwart(t)
	root := newAPI(t, sw)
	suffix := randomHex(t, 8)
	tenantA := decode[resource](t, root.do("POST", "/tenants", map[string]string{"name": "A-" + suffix}, 201))
	tenantB := decode[resource](t, root.do("POST", "/tenants", map[string]string{"name": "B-" + suffix}, 201))
	nameA, aliasDomain := "a-"+suffix+".test", "a-"+suffix+"-alt.test"
	nameB := "b-" + suffix + ".test"
	domainA := decode[resource](t, root.do("POST", fmt.Sprintf("/tenants/%d/domains", tenantA.ID), map[string]any{"name": nameA, "aliases": []string{aliasDomain}}, 201))
	domainB := decode[resource](t, root.do("POST", fmt.Sprintf("/tenants/%d/domains", tenantB.ID), map[string]any{"name": nameB}, 201))
	pathA, pathB := domainPath(domainA.ID), domainPath(domainB.ID)
	email := "admin-" + suffix + "@ci.test"
	admin := decode[resource](t, root.do("POST", "/users", map[string]any{
		"email": email, "name": "CI Tenant A", "role": "tenant_admin", "tenant_id": tenantA.ID,
	}, 201))
	if len(admin.GeneratedPassword) < 12 {
		t.Fatal("tenant admin has no valid generated password")
	}
	a := newAPIClient(t, root.base)
	a.login(email, admin.GeneratedPassword)
	local, alias := "mailbox-"+suffix, "alias-"+suffix
	mailboxA := decode[mailbox](t, a.do("POST", pathA+"/mailboxes", map[string]any{
		"name": local, "aliases": []map[string]any{{"name": alias, "domain_id": domainA.ID}},
	}, 201))
	cleanupAccount(t, sw, mailboxA.ID)
	mailboxB := decode[mailbox](t, root.do("POST", pathB+"/mailboxes", map[string]any{"name": "mailbox-" + suffix}, 201))
	cleanupAccount(t, sw, mailboxB.ID)

	t.Run("Isolation", func(t *testing.T) {
		// Give failures the subtest's context and attribution, sharing its cookie jar.
		a := &apiClient{t: t, base: a.base, client: a.client}
		root := &apiClient{t: t, base: root.base, client: root.client}
		assertErrorCode(t, a.do("GET", pathB, nil, 404), "not_found")
		assertErrorCode(t, a.do("GET", pathB+"/mailboxes", nil, 404), "not_found")
		assertErrorCode(t, a.do("PATCH", pathA+"/mailboxes/"+mailboxA.ID, map[string]any{
			"aliases": []map[string]any{{"name": "stolen-" + suffix, "domain_id": domainB.ID}},
		}, 422), "foreign_alias")
		assertErrorCode(t, a.do("DELETE", pathA+"/mailboxes/"+mailboxB.ID, nil, 404), "not_found")
		// Rejected writes must preserve both the legitimate alias and B's mailbox.
		gotA := decode[mailbox](t, a.do("GET", pathA+"/mailboxes/"+mailboxA.ID, nil, 200))
		if len(gotA.Aliases) != 1 || gotA.Aliases[0]["email"] != alias+"@"+nameA {
			t.Fatal("denied alias update changed tenant A's aliases")
		}
		gotB := decode[mailbox](t, root.do("GET", pathB+"/mailboxes/"+mailboxB.ID, nil, 200))
		if gotB.ID != mailboxB.ID {
			t.Fatal("denied delete changed tenant B's mailbox")
		}
		entries := decode[[]auditEntry](t, a.do("GET", "/audit?limit=200", nil, 200))
		wantDenied := map[string]bool{}
		for _, target := range []string{
			"domain.get /api/v1" + pathB,
			"mailbox.list /api/v1" + pathB + "/mailboxes",
			"mailbox.update /api/v1" + pathA + "/mailboxes/" + mailboxA.ID,
			"mailbox.delete /api/v1" + pathA + "/mailboxes/" + mailboxB.ID,
		} {
			wantDenied[target] = false
		}
		for _, entry := range entries {
			if entry.TenantID != tenantA.ID {
				t.Fatal("foreign tenant audit row exposed")
			}
			key := entry.Action + " " + entry.Target
			if _, ok := wantDenied[key]; ok && entry.UserID == admin.ID && entry.Result == "denied" {
				wantDenied[key] = true
			}
		}
		for key, found := range wantDenied {
			if !found {
				t.Errorf("missing denied audit row for %s", key)
			}
		}
	})

	t.Run("SMTPRecipients", func(t *testing.T) {
		addr := os.Getenv("KURAKA_IT_SMTP_ADDR")
		if addr == "" {
			t.Skip("set KURAKA_IT_SMTP_ADDR to exercise SMTP RCPT")
		}
		for _, tc := range []struct {
			name, recipient string
			accepted        bool
		}{
			{"mailbox", local + "@" + nameA, true},
			{"alias", alias + "@" + nameA, true},
			{"alias_domain", local + "@" + aliasDomain, true},
			{"unknown", "unknown-" + suffix + "@" + nameA, false},
		} {
			t.Run(tc.name, func(t *testing.T) { assertRecipient(t, addr, tc.recipient, tc.accepted) })
		}
	})
}
