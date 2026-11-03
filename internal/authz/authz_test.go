package authz

import (
	"context"
	"errors"
	"testing"

	"github.com/InkaNode/kuraka/internal/stalwart"
	"github.com/InkaNode/kuraka/internal/store"
)

type fakeDomains map[int64]store.Domain

func (f fakeDomains) GetDomain(_ context.Context, id int64) (store.Domain, error) {
	if d, ok := f[id]; ok {
		return d, nil
	}
	return store.Domain{}, store.ErrNotFound
}

func (f fakeDomains) GetDomainByStalwartID(_ context.Context, sid string) (store.Domain, error) {
	for _, d := range f {
		if d.StalwartID == sid {
			return d, nil
		}
	}
	return store.Domain{}, store.ErrNotFound
}

type fakeAccounts map[string]stalwart.Account

func (f fakeAccounts) GetAccount(_ context.Context, id string) (stalwart.Account, error) {
	if a, ok := f[id]; ok {
		return a, nil
	}
	return stalwart.Account{}, stalwart.ErrNotFound
}

func ptr(v int64) *int64 { return &v }

// Tenant 1 owns domain 10 (stalwart "a"), tenant 2 owns domain 20 (stalwart "b").
func fixture() *Authz {
	domains := fakeDomains{
		10: {ID: 10, TenantID: 1, Name: "one.test", StalwartID: "a"},
		20: {ID: 20, TenantID: 2, Name: "two.test", StalwartID: "b"},
	}
	accounts := fakeAccounts{
		"u1": {ID: "u1", Name: "juan", DomainID: "a"},
		"u2": {ID: "u2", Name: "ana", DomainID: "b"},
	}
	return New(domains, accounts)
}

var (
	super   = Principal{UserID: 1, Role: RoleSuperadmin}
	tenant1 = Principal{UserID: 2, Role: RoleTenantAdmin, TenantID: ptr(1)}
	tenant2 = Principal{UserID: 3, Role: RoleTenantAdmin, TenantID: ptr(2)}
	broken  = Principal{UserID: 4, Role: RoleTenantAdmin}
)

func TestDomainIsolation(t *testing.T) {
	az, ctx := fixture(), context.Background()
	cases := []struct {
		name string
		p    Principal
		id   int64
		want error
	}{
		{"own domain", tenant1, 10, nil},
		{"other tenant domain", tenant1, 20, ErrNotFound},
		{"missing domain", tenant1, 99, ErrNotFound},
		{"superadmin any domain", super, 20, nil},
		{"tenant admin without tenant", broken, 10, ErrNotFound},
		{"unknown role", Principal{Role: "x", TenantID: ptr(1)}, 10, ErrNotFound},
	}
	for _, c := range cases {
		if _, err := az.Domain(ctx, c.p, c.id); !errors.Is(err, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, err, c.want)
		}
	}
}

func TestMailboxIsolation(t *testing.T) {
	az, ctx := fixture(), context.Background()
	if _, acc, err := az.Mailbox(ctx, tenant1, 10, "u1"); err != nil || acc.ID != "u1" {
		t.Fatalf("own mailbox: %v", err)
	}
	// Mailbox of tenant 2 requested through tenant 1's own domain.
	if _, _, err := az.Mailbox(ctx, tenant1, 10, "u2"); !errors.Is(err, ErrNotFound) {
		t.Errorf("foreign mailbox via own domain: got %v", err)
	}
	// Mailbox of tenant 2 through tenant 2's domain.
	if _, _, err := az.Mailbox(ctx, tenant1, 20, "u2"); !errors.Is(err, ErrNotFound) {
		t.Errorf("foreign domain: got %v", err)
	}
	if _, _, err := az.Mailbox(ctx, tenant1, 10, "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing mailbox: got %v", err)
	}
	// Superadmin still cannot reach a mailbox through the wrong domain.
	if _, _, err := az.Mailbox(ctx, super, 10, "u2"); !errors.Is(err, ErrNotFound) {
		t.Errorf("superadmin wrong domain: got %v", err)
	}
}

func TestCheckAliases(t *testing.T) {
	az, ctx := fixture(), context.Background()
	if err := az.CheckAliases(ctx, 1, []stalwart.Alias{{Name: "contacto", DomainID: "a"}, {Name: "info", DomainID: "a"}}); err != nil {
		t.Fatalf("own aliases: %v", err)
	}
	if err := az.CheckAliases(ctx, 1, []stalwart.Alias{{Name: "ceo", DomainID: "b"}}); !errors.Is(err, ErrForeignAlias) {
		t.Errorf("alias in other tenant: got %v", err)
	}
	if err := az.CheckAliases(ctx, 1, []stalwart.Alias{{Name: "x", DomainID: "unmanaged"}}); !errors.Is(err, ErrForeignAlias) {
		t.Errorf("alias in unmanaged domain: got %v", err)
	}
	if err := az.CheckAliases(ctx, 1, nil); err != nil {
		t.Errorf("no aliases: %v", err)
	}
}

func TestRolesAndScope(t *testing.T) {
	if RequireSuperadmin(super) != nil || !errors.Is(RequireSuperadmin(tenant1), ErrForbidden) {
		t.Error("RequireSuperadmin")
	}
	if TenantScope(super) != nil {
		t.Error("superadmin scope should be nil")
	}
	if s := TenantScope(tenant2); s == nil || *s != 2 {
		t.Error("tenant scope")
	}
}
