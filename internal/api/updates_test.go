package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/InkaNode/kuraka/internal/provision"
	"github.com/InkaNode/kuraka/internal/releases"
	"github.com/InkaNode/kuraka/internal/store"
)

type fakeGitHub struct {
	mu   sync.Mutex
	tags map[string]string
	hits int
}

func (f *fakeGitHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hits++
	repo := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/repos/"), "/releases/latest")
	tag, ok := f.tags[repo]
	if !ok {
		http.NotFound(w, r)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"tag_name": tag})
}

func (f *fakeGitHub) hitCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hits
}

// updatesHarness has one provisioned helpdesk ("kuraka-acme-desk") and one
// connected by URL.
func updatesHarness(t *testing.T) (*harness, *fakeGitHub, string, int64, int64) {
	t.Helper()
	h := newHarness(t, true)
	gh := &fakeGitHub{tags: map[string]string{libredeskRepo: "v2.10.0", webuiRepo: "v2.0.4-kuraka.2"}}
	srv := httptest.NewServer(gh)
	t.Cleanup(srv.Close)
	h.s.releases = &releases.Client{BaseURL: srv.URL}
	dir := t.TempDir()
	h.s.cfg.UpdatesDir = dir
	tenant := h.tenant("Acme")
	ctx := context.Background()
	desk, err := h.st.CreateHelpdesk(ctx, store.Helpdesk{TenantID: tenant, Kind: "libredesk", Name: "desk.acme.test", URL: "http://kuraka-acme-desk:9000"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := h.st.CreateHelpdesk(ctx, store.Helpdesk{TenantID: tenant, Kind: "libredesk", Name: "Other", URL: "http://desk.test"})
	if err != nil {
		t.Fatal(err)
	}
	return h, gh, dir, desk.ID, other.ID
}

type versionReply struct{ Latest, Current, State string }

type deskReply struct {
	ID      int64
	Name    string
	Current string
	State   string
	Message string
}

type libredeskReply struct {
	Latest    string
	Helpdesks []deskReply
}

type updatesReply struct {
	CheckedAt  *time.Time     `json:"checked_at"`
	Error      string         `json:"error"`
	UpdatesDir bool           `json:"updates_dir"`
	WebUI      versionReply   `json:"webui"`
	Libredesk  libredeskReply `json:"libredesk"`
}

func decodeUpdates(t *testing.T, b []byte) updatesReply {
	t.Helper()
	var v updatesReply
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestUpdatesStatusAndCheck(t *testing.T) {
	h, gh, _, desk, _ := updatesHarness(t)
	v := decodeUpdates(t, h.root.do("GET", "/updates", nil, 200))
	if v.CheckedAt != nil || !v.UpdatesDir || v.WebUI.State != "unknown" || len(v.Libredesk.Helpdesks) != 1 {
		t.Fatalf("before check: %+v", v)
	}
	if d := v.Libredesk.Helpdesks[0]; d.ID != desk || d.Current != provision.LibredeskVersion || d.State != "unknown" {
		t.Fatalf("desk before check: %+v", d)
	}
	v = decodeUpdates(t, h.root.do("POST", "/updates/check", nil, 200))
	if v.CheckedAt == nil || v.Error != "" || v.Libredesk.Latest != "v2.10.0" || v.Libredesk.Helpdesks[0].State != "available" ||
		v.WebUI.Latest != "v2.0.4-kuraka.2" || v.WebUI.Current != "" || v.WebUI.State != "available" {
		t.Fatalf("after check: %+v", v)
	}
	// Within 30 seconds the cached result is returned.
	h.root.do("POST", "/updates/check", nil, 200)
	if n := gh.hitCount(); n != 2 {
		t.Fatalf("GitHub hits = %d, want 2", n)
	}
	h.s.updates.mu.Lock()
	h.s.updates.checkedAt = time.Now().Add(-time.Minute)
	h.s.updates.mu.Unlock()
	gh.mu.Lock()
	delete(gh.tags, webuiRepo)
	gh.mu.Unlock()
	v = decodeUpdates(t, h.root.do("POST", "/updates/check", nil, 200))
	if n := gh.hitCount(); n != 4 {
		t.Fatalf("GitHub hits = %d, want 4", n)
	}
	if v.Error == "" || v.WebUI.Latest != "v2.0.4-kuraka.2" {
		t.Fatalf("failed check must keep the previous tag: %+v", v)
	}
	admin := h.admin(h.tenant("Second"), "admin@second.test")
	admin.do("GET", "/updates", nil, 403)
	admin.do("POST", "/updates/check", nil, 403)
}

func TestUpdateWebUI(t *testing.T) {
	h, _, _, _, _ := updatesHarness(t)
	h.root.do("POST", "/updates/webui", nil, 409)
	h.root.do("POST", "/updates/check", nil, 200)
	v := decodeUpdates(t, h.root.do("POST", "/updates/webui", nil, 200))
	if v.WebUI.Current != "v2.0.4-kuraka.2" || v.WebUI.State != "current" {
		t.Fatalf("webui: %+v", v.WebUI)
	}
	h.sw.mu.Lock()
	n := h.sw.appsUpdated
	h.sw.mu.Unlock()
	if n != 1 {
		t.Fatalf("UpdateApps calls = %d", n)
	}
	if applied, err := h.st.GetSetting(context.Background(), webuiSetting); err != nil || applied != "v2.0.4-kuraka.2" {
		t.Fatalf("applied = %q %v", applied, err)
	}
}

func TestUpdateHelpdesk(t *testing.T) {
	h, _, dir, desk, other := updatesHarness(t)
	path := fmt.Sprintf("/helpdesks/%d/update", desk)
	h.root.do("POST", path, nil, 409) // latest unknown
	h.root.do("POST", "/updates/check", nil, 200)
	assertCode(t, h.root.do("POST", fmt.Sprintf("/helpdesks/%d/update", other), nil, 422), "invalid_input")
	h.root.do("POST", "/helpdesks/999/update", nil, 404)

	v := decodeUpdates(t, h.root.do("POST", path, nil, 200))
	if v.Libredesk.Helpdesks[0].State != "updating" {
		t.Fatalf("state: %+v", v.Libredesk.Helpdesks[0])
	}
	request := filepath.Join(dir, "libredesk-kuraka-acme-desk.json")
	data, err := os.ReadFile(request)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]string
	if err := json.Unmarshal(data, &got); err != nil || len(got) != 2 || got["container"] != "kuraka-acme-desk" || got["tag"] != "v2.10.0" {
		t.Fatalf("request file: %s", data)
	}
	assertCode(t, h.root.do("POST", path, nil, 409), "conflict")

	result := filepath.Join(dir, "libredesk-kuraka-acme-desk.result.json")
	if err := os.Remove(request); err != nil {
		t.Fatal(err)
	}
	writeResult := func(body string) {
		t.Helper()
		if err := os.WriteFile(result, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeResult(`{"tag":"v2.10.0","status":"error","message":"pull failed","at":"2026-11-04T10:00:00Z","current":"v2.9.2"}`)
	d := decodeUpdates(t, h.root.do("GET", "/updates", nil, 200)).Libredesk.Helpdesks[0]
	if d.State != "error" || d.Message != "pull failed" || d.Current != "v2.9.2" {
		t.Fatalf("error result: %+v", d)
	}
	// A failed update can be retried.
	h.root.do("POST", path, nil, 200)
	if err := os.Remove(request); err != nil {
		t.Fatal(err)
	}
	writeResult(`{"tag":"v2.10.0","status":"ok","message":"","at":"2026-11-04T10:05:00Z","current":"v2.10.0"}`)
	d = decodeUpdates(t, h.root.do("GET", "/updates", nil, 200)).Libredesk.Helpdesks[0]
	if d.State != "current" || d.Current != "v2.10.0" || d.Message != "" {
		t.Fatalf("ok result: %+v", d)
	}
	h.root.do("POST", path, nil, 409) // nothing to update

	h.s.cfg.UpdatesDir = ""
	assertCode(t, h.root.do("POST", path, nil, 409), "updates_disabled")
	if v := decodeUpdates(t, h.root.do("GET", "/updates", nil, 200)); v.UpdatesDir {
		t.Fatal("updates_dir must be false")
	}
}
