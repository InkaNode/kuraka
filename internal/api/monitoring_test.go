package api

import (
	"context"
	"testing"

	"github.com/InkaNode/kuraka/internal/stalwart"
)

func TestQueueVisibility(t *testing.T) {
	for _, tc := range []struct {
		sender  string
		domains map[string]bool
		want    bool
	}{
		{"sender@EXAMPLE.test", map[string]bool{"example.test": true}, true},
		{"sender@foreign.test", map[string]bool{"example.test": true}, false},
		{"sender@sub.example.test", map[string]bool{"example.test": true}, false},
		{"sender@example.test@foreign.test", map[string]bool{"example.test": true}, false},
		{"", map[string]bool{"example.test": true}, false},
		{"sender", map[string]bool{"example.test": true}, false},
		{"sender@example.test", map[string]bool{}, false},
		{"", nil, true},
	} {
		if got := queueVisible(stalwart.QueuedMessage{ReturnPath: tc.sender}, tc.domains); got != tc.want {
			t.Errorf("queueVisible(%q, %v) = %v", tc.sender, tc.domains, got)
		}
	}
}

func TestMonitoringAuthorization(t *testing.T) {
	h := newHarness(t, true)
	a, b := h.tenant("A"), h.tenant("B")
	h.domain(a, "a.test")
	h.domain(b, "b.test")
	admin := h.admin(a, "admin@a.test")
	h.sw.queued = map[string]stalwart.QueuedMessage{
		"own":     {ID: "own", ReturnPath: "sender@A.TEST"},
		"foreign": {ID: "foreign", ReturnPath: "sender@b.test"},
		"bounce":  {ID: "bounce", ReturnPath: ""},
	}
	rows := array(t, admin.do("GET", "/queue", nil, 200))
	if len(rows) != 1 || rows[0]["id"] != "own" {
		t.Fatalf("tenant queue = %+v", rows)
	}
	rows = array(t, h.root.do("GET", "/queue", nil, 200))
	if len(rows) != 3 {
		t.Fatalf("superadmin queue = %+v", rows)
	}
	before := h.sw.mutationCount()
	admin.do("POST", "/queue/foreign/retry", nil, 404)
	admin.do("DELETE", "/queue/foreign", nil, 404)
	admin.do("POST", "/queue/bounce/retry", nil, 404)
	admin.do("DELETE", "/queue/missing", nil, 404)
	if got := h.sw.mutationCount(); got != before {
		t.Fatalf("unauthorized mutations = %d", got-before)
	}
	admin.do("POST", "/queue/own/retry", nil, 204)
	admin.do("DELETE", "/queue/own", nil, 204)
	h.root.do("DELETE", "/queue/foreign", nil, 204)
	admin.do("GET", "/metrics", nil, 403)
	admin.do("GET", "/alerts", nil, 403)
	admin.do("POST", "/alerts", map[string]any{"metric": "stalwart_queue_messages", "threshold": 10}, 403)
	admin.do("DELETE", "/alerts/1", nil, 403)
	h.newClient().do("GET", "/queue", nil, 401)
	h.root.do("GET", "/metrics", nil, 200)
	h.root.do("GET", "/alerts", nil, 200)
	h.root.do("POST", "/alerts", map[string]any{"metric": "unknown", "threshold": 10}, 422)
	rule := object(t, h.root.do("POST", "/alerts", map[string]any{"metric": "stalwart_queue_messages", "threshold": 10}, 201))
	if rule["enabled"] != true {
		t.Fatalf("rule = %v", rule)
	}
	h.root.do("DELETE", "/alerts/1", nil, 204)
	entries, err := h.st.ListAudit(context.Background(), nil, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, entry := range entries {
		if entry.Result == "ok" {
			seen[entry.Action] = true
		}
	}
	for _, action := range []string{"queue.retry", "queue.cancel", "alert.create", "alert.delete"} {
		if !seen[action] {
			t.Errorf("missing audit action %s", action)
		}
	}
}
