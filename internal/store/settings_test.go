package store

import (
	"context"
	"testing"
)

func TestSettings(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if v, err := s.GetSetting(ctx, "k"); err != nil || v != "" {
		t.Fatalf("unset: %q %v", v, err)
	}
	mustOK(t, s.SetSetting(ctx, "k", "v1"))
	mustOK(t, s.SetSetting(ctx, "k", "v2"))
	if v, err := s.GetSetting(ctx, "k"); err != nil || v != "v2" {
		t.Fatalf("got %q %v", v, err)
	}
}
