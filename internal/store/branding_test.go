package store

import (
	"context"
	"testing"
	"time"
)

func TestBranding(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	tenant, err := s.CreateTenant(ctx, "Acme")
	mustOK(t, err)
	mustOK(t, s.SetBranding(ctx, nil, "default", Branding{Name: "Global", Logo: "old"}))
	mustOK(t, s.SetBranding(ctx, nil, "default", Branding{Name: "Global", Logo: "logo"}))
	mustOK(t, s.SetBranding(ctx, &tenant.ID, "default", Branding{Name: "Acme"}))
	global, err := s.GetBranding(ctx, nil)
	if err != nil || len(global) != 1 || global["default"] != (Branding{Name: "Global", Logo: "logo"}) {
		t.Fatalf("global: %+v %v", global, err)
	}
	scoped, err := s.GetBranding(ctx, &tenant.ID)
	if err != nil || len(scoped) != 1 || scoped["default"] != (Branding{Name: "Acme"}) {
		t.Fatalf("tenant: %+v %v", scoped, err)
	}
	mustOK(t, s.SetBranding(ctx, &tenant.ID, "default", Branding{}))
	if scoped, err := s.GetBranding(ctx, &tenant.ID); err != nil || len(scoped) != 0 {
		t.Fatalf("empty branding kept: %+v %v", scoped, err)
	}
	mustErrorIs(t, s.SetBranding(ctx, nil, "other", Branding{Name: "x"}), ErrConflict)
	mustOK(t, s.SetBranding(ctx, &tenant.ID, "kuraka", Branding{Color: "#000000"}))
	mustOK(t, s.DeleteTenant(ctx, tenant.ID))
	var n int
	mustOK(t, s.db.QueryRowContext(ctx, `SELECT count(*) FROM branding WHERE tenant_id IS NOT NULL`).Scan(&n))
	if n != 0 {
		t.Fatalf("tenant branding not cascaded: %d rows", n)
	}
}

func TestBrandingAssets(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for _, id := range []string{"logo", "unused"} {
		mustOK(t, s.CreateBrandingAsset(ctx, id, "image/png", []byte(id)))
	}
	mustErrorIs(t, s.CreateBrandingAsset(ctx, "logo", "image/png", []byte("x")), ErrConflict)
	mustOK(t, s.SetBranding(ctx, nil, "kuraka", Branding{Logo: "logo"}))
	mustOK(t, s.DeleteUnusedBrandingAssets(ctx, time.Now().Add(-time.Hour)))
	if _, _, err := s.GetBrandingAsset(ctx, "unused"); err != nil {
		t.Fatalf("fresh asset deleted: %v", err)
	}
	mustOK(t, s.DeleteUnusedBrandingAssets(ctx, time.Now().Add(time.Hour)))
	contentType, data, err := s.GetBrandingAsset(ctx, "logo")
	if err != nil || contentType != "image/png" || string(data) != "logo" {
		t.Fatalf("used asset: %q %q %v", contentType, data, err)
	}
	_, _, err = s.GetBrandingAsset(ctx, "unused")
	mustErrorIs(t, err, ErrNotFound)
}
