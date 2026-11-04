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
	"github.com/InkaNode/kuraka/internal/version"
)

type fakeGitHub struct {
	mu      sync.Mutex
	tags    map[string]string
	commits map[string]string
	urls    map[string]string
	hits    int
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
	_ = json.NewEncoder(w).Encode(map[string]string{"tag_name": tag, "target_commitish": f.commits[repo], "html_url": f.urls[repo]})
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
	gh := &fakeGitHub{
		tags:    map[string]string{libredeskRepo: "v2.9.3", webuiRepo: "v2.0.4", stalwartRepo: "v1.0.3", kurakaRepo: "v99.0.0"},
		commits: map[string]string{kurakaRepo: strings.Repeat("a", 40)},
		urls:    map[string]string{kurakaRepo: "https://github.com/InkaNode/kuraka/releases/tag/v99.0.0"},
	}
	srv := httptest.NewServer(gh)
	t.Cleanup(srv.Close)
	h.s.releases = &releases.Client{BaseURL: srv.URL}
	dir := t.TempDir()
	h.s.cfg.UpdatesDir = dir
	h.s.cfg.StalwartVersion = "v1.0.2"
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

type versionReply struct{ Latest, Current, State, Message, Supported string }

type kurakaReply struct {
	Latest, Current, Commit, URL, State, Kind, Message string
	LatestCommit                                       string `json:"latest_commit"`
}

type deskReply struct {
	ID        int64
	Name      string
	Current   string
	State     string
	Message   string
	Supported string
}

type libredeskReply struct {
	Latest    string
	Helpdesks []deskReply
}

type updatesReply struct {
	CheckedAt  *time.Time     `json:"checked_at"`
	Error      string         `json:"error"`
	UpdatesDir bool           `json:"updates_dir"`
	Stalwart   versionReply   `json:"stalwart"`
	Kuraka     kurakaReply    `json:"kuraka"`
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
	if webuiRepo != "stalwartlabs/webui" {
		t.Fatalf("webui repo = %q", webuiRepo)
	}
	h, gh, _, desk, _ := updatesHarness(t)
	v := decodeUpdates(t, h.root.do("GET", "/updates", nil, 200))
	if v.CheckedAt != nil || !v.UpdatesDir || v.WebUI.State != "unknown" || len(v.Libredesk.Helpdesks) != 1 ||
		v.Stalwart.Current != "v1.0.2" || v.Stalwart.State != "unknown" {
		t.Fatalf("before check: %+v", v)
	}
	if d := v.Libredesk.Helpdesks[0]; d.ID != desk || d.Current != provision.LibredeskVersion || d.State != "unknown" {
		t.Fatalf("desk before check: %+v", d)
	}
	v = decodeUpdates(t, h.root.do("POST", "/updates/check", nil, 200))
	if v.CheckedAt == nil || v.Error != "" || v.Libredesk.Latest != "v2.9.3" || v.Libredesk.Helpdesks[0].State != "available" ||
		v.WebUI.Latest != "v2.0.4" || v.WebUI.Current != "" || v.WebUI.State != "available" ||
		v.Stalwart.Latest != "v1.0.3" || v.Stalwart.State != "available" {
		t.Fatalf("after check: %+v", v)
	}
	// Within 30 seconds the cached result is returned.
	h.root.do("POST", "/updates/check", nil, 200)
	if n := gh.hitCount(); n != 4 {
		t.Fatalf("GitHub hits = %d, want 4", n)
	}
	h.s.updates.mu.Lock()
	h.s.updates.checkedAt = time.Now().Add(-time.Minute)
	h.s.updates.mu.Unlock()
	gh.mu.Lock()
	delete(gh.tags, webuiRepo)
	delete(gh.tags, kurakaRepo)
	gh.mu.Unlock()
	v = decodeUpdates(t, h.root.do("POST", "/updates/check", nil, 200))
	if n := gh.hitCount(); n != 8 {
		t.Fatalf("GitHub hits = %d, want 8", n)
	}
	if v.Error == "" || v.WebUI.Latest != "v2.0.4" || v.Kuraka.Latest != "v99.0.0" ||
		v.Kuraka.LatestCommit != strings.Repeat("a", 40) || v.Kuraka.URL != "https://github.com/InkaNode/kuraka/releases/tag/v99.0.0" ||
		!strings.Contains(v.Error, webuiRepo) || !strings.Contains(v.Error, kurakaRepo) {
		t.Fatalf("failed check must keep the previous tag: %+v", v)
	}
	admin := h.admin(h.tenant("Second"), "admin@second.test")
	admin.do("GET", "/updates", nil, 403)
	admin.do("POST", "/updates/check", nil, 403)
	admin.do("POST", "/updates/stalwart", nil, 403)
	admin.do("POST", "/updates/kuraka", nil, 403)
}

func TestAddonUpdatesWaiting(t *testing.T) {
	h, gh, dir, desk, _ := updatesHarness(t)
	gh.mu.Lock()
	gh.tags[stalwartRepo] = "v1.1.0"
	gh.tags[webuiRepo] = "v2.1.0"
	gh.tags[libredeskRepo] = "v2.10.0"
	gh.mu.Unlock()
	v := decodeUpdates(t, h.root.do("POST", "/updates/check", nil, 200))
	if v.Stalwart.State != "waiting" || v.Stalwart.Supported != "1.0" ||
		v.WebUI.State != "waiting" || v.WebUI.Supported != "2.0" ||
		len(v.Libredesk.Helpdesks) != 1 || v.Libredesk.Helpdesks[0].State != "waiting" ||
		v.Libredesk.Helpdesks[0].Supported != "2.9" || v.Kuraka.State != "available" {
		t.Fatalf("waiting: %+v", v)
	}
	for _, tc := range []struct{ path, tag string }{
		{"/updates/stalwart", "v1.1.0"},
		{"/updates/webui", "v2.1.0"},
		{fmt.Sprintf("/helpdesks/%d/update", desk), "v2.10.0"},
	} {
		body := h.root.do("POST", tc.path, nil, 409)
		assertCode(t, body, "conflict")
		got := object(t, body)["error"].(map[string]any)["message"]
		if want := "Waiting for a Kuraka update that supports " + tc.tag; got != want {
			t.Fatalf("%s: %v, want %s", tc.path, got, want)
		}
	}
	for _, name := range []string{"stalwart.json", "libredesk-kuraka-acme-desk.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("blocked update wrote %s: %v", name, err)
		}
	}
	h.sw.mu.Lock()
	n := h.sw.appsUpdated
	h.sw.mu.Unlock()
	if n != 0 {
		t.Fatalf("blocked webui called UpdateApps %d times", n)
	}
	if applied, err := h.st.GetSetting(context.Background(), webuiSetting); err != nil || applied != "" {
		t.Fatalf("blocked webui applied setting: %q %v", applied, err)
	}
	// Disabling an addon does not bypass update compatibility.
	h.root.do("PUT", "/addons/stalwart", map[string]bool{"disabled": true}, 200)
	h.root.do("POST", "/updates/stalwart", nil, 409)
	h.root.do("POST", "/updates/kuraka", nil, 200)
}

func TestAddonCompatibilityStates(t *testing.T) {
	for _, status := range []string{"unknown", "current", "updating", "error"} {
		row := map[string]any{"state": status}
		addonCompatibility(row, "stalwart", "v99.0.0")
		if len(row) != 1 || row["state"] != status {
			t.Fatalf("%s: %v", status, row)
		}
	}
	for _, tc := range []struct{ component, tag string }{
		{"stalwart", "v1.0.99"}, {"webui", "v2.0.99"}, {"libredesk", "v2.9.99"},
	} {
		row := map[string]any{"state": "available"}
		addonCompatibility(row, tc.component, tc.tag)
		if len(row) != 1 || row["state"] != "available" {
			t.Fatalf("supported %s: %v", tc.component, row)
		}
	}
}

func TestUpdateWebUI(t *testing.T) {
	h, _, _, _, _ := updatesHarness(t)
	h.root.do("POST", "/updates/webui", nil, 409)
	h.root.do("POST", "/updates/check", nil, 200)
	v := decodeUpdates(t, h.root.do("POST", "/updates/webui", nil, 200))
	if v.WebUI.Current != "v2.0.4" || v.WebUI.State != "current" {
		t.Fatalf("webui: %+v", v.WebUI)
	}
	h.sw.mu.Lock()
	n := h.sw.appsUpdated
	h.sw.mu.Unlock()
	if n != 1 {
		t.Fatalf("UpdateApps calls = %d", n)
	}
	if applied, err := h.st.GetSetting(context.Background(), webuiSetting); err != nil || applied != "v2.0.4" {
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
	if err := json.Unmarshal(data, &got); err != nil || len(got) != 2 || got["container"] != "kuraka-acme-desk" || got["tag"] != "v2.9.3" {
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
	writeResult(`{"tag":"v2.9.3","status":"error","message":"pull failed","at":"2026-11-04T10:00:00Z","current":"v2.9.2"}`)
	d := decodeUpdates(t, h.root.do("GET", "/updates", nil, 200)).Libredesk.Helpdesks[0]
	if d.State != "error" || d.Message != "pull failed" || d.Current != "v2.9.2" {
		t.Fatalf("error result: %+v", d)
	}
	// A failed update can be retried.
	h.root.do("POST", path, nil, 200)
	if err := os.Remove(request); err != nil {
		t.Fatal(err)
	}
	writeResult(`{"tag":"v2.9.3","status":"ok","message":"","at":"2026-11-04T10:05:00Z","current":"v2.9.3"}`)
	d = decodeUpdates(t, h.root.do("GET", "/updates", nil, 200)).Libredesk.Helpdesks[0]
	if d.State != "current" || d.Current != "v2.9.3" || d.Message != "" {
		t.Fatalf("ok result: %+v", d)
	}
	h.root.do("POST", path, nil, 409) // nothing to update

	h.s.cfg.UpdatesDir = ""
	assertCode(t, h.root.do("POST", path, nil, 409), "updates_disabled")
	if v := decodeUpdates(t, h.root.do("GET", "/updates", nil, 200)); v.UpdatesDir {
		t.Fatal("updates_dir must be false")
	}
}

func TestUpdateStalwart(t *testing.T) {
	h, _, dir, _, _ := updatesHarness(t)
	h.root.do("POST", "/updates/stalwart", nil, 409) // latest unknown
	h.root.do("POST", "/updates/check", nil, 200)

	v := decodeUpdates(t, h.root.do("POST", "/updates/stalwart", nil, 200))
	if v.Stalwart.State != "updating" || v.Stalwart.Current != "v1.0.2" {
		t.Fatalf("state: %+v", v.Stalwart)
	}
	request := filepath.Join(dir, "stalwart.json")
	data, err := os.ReadFile(request)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]string
	if err := json.Unmarshal(data, &got); err != nil || len(got) != 1 || got["tag"] != "v1.0.3" {
		t.Fatalf("request file: %s", data)
	}
	assertCode(t, h.root.do("POST", "/updates/stalwart", nil, 409), "conflict")

	result := filepath.Join(dir, "stalwart.result.json")
	if err := os.Remove(request); err != nil {
		t.Fatal(err)
	}
	writeResult := func(body string) {
		t.Helper()
		if err := os.WriteFile(result, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeResult(`{"tag":"v1.0.3","status":"error","message":"pull failed","at":"2026-11-04T10:00:00Z","current":"v1.0.2"}`)
	s := decodeUpdates(t, h.root.do("GET", "/updates", nil, 200)).Stalwart
	if s.State != "error" || s.Message != "pull failed" || s.Current != "v1.0.2" {
		t.Fatalf("error result: %+v", s)
	}
	// A failed update can be retried.
	h.root.do("POST", "/updates/stalwart", nil, 200)
	if err := os.Remove(request); err != nil {
		t.Fatal(err)
	}
	writeResult(`{"tag":"v1.0.3","status":"ok","message":"","at":"2026-11-04T10:05:00Z","current":"v1.0.3"}`)
	s = decodeUpdates(t, h.root.do("GET", "/updates", nil, 200)).Stalwart
	if s.State != "current" || s.Current != "v1.0.3" || s.Message != "" {
		t.Fatalf("ok result: %+v", s)
	}
	h.root.do("POST", "/updates/stalwart", nil, 409) // nothing to update

	h.s.cfg.UpdatesDir = ""
	assertCode(t, h.root.do("POST", "/updates/stalwart", nil, 409), "updates_disabled")
	if s := decodeUpdates(t, h.root.do("GET", "/updates", nil, 200)).Stalwart; s.Current != "v1.0.2" || s.State != "available" {
		t.Fatalf("without updates dir: %+v", s)
	}
}

func TestKurakaStates(t *testing.T) {
	h, _, dir, _, _ := updatesHarness(t)
	oldCommit := version.Commit
	version.Commit = strings.Repeat("b", 40)
	t.Cleanup(func() { version.Commit = oldCommit })
	current := "v" + version.Version
	latestCommit := strings.Repeat("a", 40)
	for _, tc := range []struct {
		name, tag, commit, result, wantState, wantKind string
		pending, disabled                              bool
	}{
		{name: "unknown", wantState: "unknown"},
		{name: "version", tag: "v99.0.0", commit: latestCommit, wantState: "available", wantKind: "version"},
		{name: "commits", tag: current, commit: latestCommit, wantState: "available", wantKind: "commits"},
		{name: "current", tag: current, commit: version.Commit, wantState: "current"},
		{name: "no latest commit", tag: current, wantState: "current"},
		{name: "error", tag: current, commit: latestCommit, result: "error", wantState: "error"},
		{name: "other tag error", tag: current, commit: latestCommit, result: "other tag", wantState: "available", wantKind: "commits"},
		{name: "other commit error", tag: current, commit: latestCommit, result: "other commit", wantState: "available", wantKind: "commits"},
		{name: "ok does not replace build", tag: "v99.0.0", commit: latestCommit, result: "ok", wantState: "available", wantKind: "version"},
		{name: "malformed result", tag: current, commit: latestCommit, result: "malformed", wantState: "available", wantKind: "commits"},
		{name: "updating before error", tag: current, commit: latestCommit, result: "error", pending: true, wantState: "updating"},
		{name: "updating before unknown", pending: true, wantState: "updating"},
		{name: "disabled ignores files", tag: current, commit: latestCommit, result: "error", pending: true, disabled: true, wantState: "available", wantKind: "commits"},
		{name: "disabled unknown", disabled: true, wantState: "unknown"},
		{name: "disabled current", tag: current, commit: version.Commit, disabled: true, wantState: "current"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h.s.cfg.UpdatesDir = dir
			h.s.updates.mu.Lock()
			h.s.updates.kuraka = releases.Release{Tag: tc.tag, Commit: tc.commit, URL: "https://github.com/InkaNode/kuraka"}
			h.s.updates.mu.Unlock()
			for _, name := range []string{"kuraka.json", "kuraka.result.json"} {
				if err := os.Remove(filepath.Join(dir, name)); err != nil && !os.IsNotExist(err) {
					t.Fatal(err)
				}
			}
			if tc.result != "" {
				result := updateResult{Tag: tc.tag, Commit: tc.commit, Status: "error", Message: strings.Repeat("x", 301), Current: "v99.0.0"}
				switch tc.result {
				case "other tag":
					result.Tag = "v98.0.0"
				case "other commit":
					result.Commit = version.Commit
				case "ok":
					result.Status = "ok"
				}
				data, err := json.Marshal(result)
				if err != nil {
					t.Fatal(err)
				}
				if tc.result == "malformed" {
					data = []byte(`{"status":"error"`)
				}
				if err := os.WriteFile(filepath.Join(dir, "kuraka.result.json"), data, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if tc.pending {
				if err := os.WriteFile(filepath.Join(dir, "kuraka.json"), []byte(`{}`), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if tc.disabled {
				h.s.cfg.UpdatesDir = ""
			}
			body := h.root.do("GET", "/updates", nil, 200)
			got := decodeUpdates(t, body).Kuraka
			if got.State != tc.wantState || got.Kind != tc.wantKind || got.Current != current || got.Commit != version.Commit ||
				got.Latest != tc.tag || got.LatestCommit != tc.commit || got.URL != "https://github.com/InkaNode/kuraka" {
				t.Fatalf("kuraka: %+v", got)
			}
			row := object(t, body)["kuraka"].(map[string]any)
			_, hasKind := row["kind"]
			_, hasMessage := row["message"]
			if hasKind != (tc.wantState == "available") || hasMessage != (tc.wantState == "error") {
				t.Fatalf("conditional fields: %v", row)
			}
			if tc.wantState == "error" && got.Message != strings.Repeat("x", 300) {
				t.Fatalf("message: %q", got.Message)
			}
		})
	}
	version.Commit = ""
	if status, _ := h.s.kurakaState(releases.Release{Tag: current, Commit: latestCommit}); status != "current" {
		t.Fatalf("unknown build commit: %s", status)
	}
}

func TestUpdateKuraka(t *testing.T) {
	h, _, dir, _, _ := updatesHarness(t)
	assertCode(t, h.root.do("POST", "/updates/kuraka", nil, 409), "conflict")
	h.root.do("POST", "/updates/check", nil, 200)
	v := decodeUpdates(t, h.root.do("POST", "/updates/kuraka", nil, 200))
	if v.Kuraka.State != "updating" || v.Kuraka.Current != "v"+version.Version {
		t.Fatalf("state: %+v", v.Kuraka)
	}
	request := filepath.Join(dir, "kuraka.json")
	data, err := os.ReadFile(request)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]string
	if err := json.Unmarshal(data, &got); err != nil || len(got) != 2 || got["tag"] != "v99.0.0" || got["commit"] != strings.Repeat("a", 40) {
		t.Fatalf("request file: %s", data)
	}
	body := h.root.do("POST", "/updates/kuraka", nil, 409)
	assertCode(t, body, "conflict")
	if !strings.Contains(string(body), "Kuraka update already in progress") {
		t.Fatalf("conflict: %s", body)
	}
	if err := os.Remove(request); err != nil {
		t.Fatal(err)
	}
	result, err := json.Marshal(updateResult{Tag: "v99.0.0", Commit: strings.Repeat("a", 40), Status: "error", Message: "pull failed"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "kuraka.result.json"), result, 0o600); err != nil {
		t.Fatal(err)
	}
	// A failed attempt can be retried with the same tag and commit.
	h.root.do("POST", "/updates/kuraka", nil, 200)
	if err := os.Remove(request); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "kuraka.result.json")); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		tag, commit string
		status      int
		message     string
	}{
		{"", "", 409, "No Kuraka update available"},
		{"v" + version.Version, version.Commit, 409, "No Kuraka update available"},
		{"v99.0.0", "", 409, "Latest release has no commit"},
		{"v99.0.0+build", strings.Repeat("a", 40), 422, "release tag"},
		{"v99.0.0-rc.1", strings.Repeat("a", 40), 200, ""},
	} {
		h.s.updates.mu.Lock()
		h.s.updates.kuraka = releases.Release{Tag: tc.tag, Commit: tc.commit}
		h.s.updates.mu.Unlock()
		body := h.root.do("POST", "/updates/kuraka", nil, tc.status)
		if tc.message != "" && !strings.Contains(string(body), tc.message) {
			t.Fatalf("response: %s", body)
		}
		if tc.status != 200 {
			if _, err := os.Stat(request); !os.IsNotExist(err) {
				t.Fatalf("rejected request wrote a file: %v", err)
			}
		}
	}
	h.s.cfg.UpdatesDir = ""
	assertCode(t, h.root.do("POST", "/updates/kuraka", nil, 409), "updates_disabled")
}
