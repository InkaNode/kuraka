package api

import (
	"slices"
	"testing"

	"github.com/InkaNode/kuraka/internal/stalwart"
)

func TestTrustNetwork(t *testing.T) {
	for address, want := range map[string]string{
		"172.30.0.3":         "172.30.0.0/24",
		"10.1.2.3/8":         "10.1.2.0/24",
		"127.0.0.1":          "127.0.0.0/24",
		"::ffff:192.168.1.5": "192.168.1.0/24",
		"fd00::1:2":          "fd00::/64",
		"::1":                "::1/128",
		"1.2.3.4":            "",
		"2001:db8::1":        "",
		"not an ip":          "",
	} {
		if got := trustNetwork(address); got != want {
			t.Errorf("trustNetwork(%q) = %q, want %q", address, got, want)
		}
	}
}

func TestBlockedIPs(t *testing.T) {
	h := newHarness(t, true)
	h.sw.blocked = []stalwart.BlockedIP{
		{ID: "in", Address: "172.30.0.3", Reason: "portScanning", CreatedAt: "2026-11-04T06:50:51Z"},
		{ID: "ex", Address: "1.2.3.4", Reason: "portScanning", CreatedAt: "2026-11-04T06:50:51Z"},
	}
	rows := array(t, h.root.do("GET", "/blocked-ips", nil, 200))
	if len(rows) != 2 || rows[0]["internal"] != true || rows[0]["trust"] != "172.30.0.0/24" || rows[0]["expiresAt"] != nil ||
		rows[1]["internal"] != false || rows[1]["trust"] != "" {
		t.Fatalf("blocked ips = %+v", rows)
	}
	admin := h.admin(h.tenant("A"), "admin@a.test")
	before := h.sw.mutationCount()
	admin.do("GET", "/blocked-ips", nil, 403)
	admin.do("DELETE", "/blocked-ips/in", nil, 403)
	h.root.do("DELETE", "/blocked-ips/missing", nil, 404)
	if got := h.sw.mutationCount(); got != before {
		t.Fatalf("unauthorized mutations = %d", got-before)
	}
	h.root.do("DELETE", "/blocked-ips/in", nil, 204)
	h.root.do("DELETE", "/blocked-ips/ex", nil, 204)
	if want := []string{"in 172.30.0.0/24", "ex "}; !slices.Equal(h.sw.unblocked, want) {
		t.Fatalf("unblocked = %q, want %q", h.sw.unblocked, want)
	}
}
