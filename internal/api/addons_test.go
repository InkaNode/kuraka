package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/InkaNode/kuraka/internal/store"
	"github.com/InkaNode/kuraka/web"
)

func addonRequest(t *testing.T, h *harness, method, path, body string, status int) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "https://mail.example.test")
	w := httptest.NewRecorder()
	h.s.Handler().ServeHTTP(w, r)
	if w.Code != status {
		t.Fatalf("%s %s: status %d, want %d: %s", method, path, w.Code, status, w.Body.String())
	}
	return w
}

func TestAddonFiles(t *testing.T) {
	h := newHarness(t, true)
	for _, app := range []string{"stalwart", "desk"} {
		for _, ext := range []string{"js", "css"} {
			name := app + "." + ext
			data, err := web.Addon(name)
			if err != nil {
				t.Fatal(err)
			}
			w := addonRequest(t, h, "GET", "/_kuraka/addon/"+name, "", 200)
			want := data
			contentType := "text/javascript; charset=utf-8"
			if ext == "css" {
				want = append([]byte(":root{}\n"), data...)
				contentType = "text/css"
			}
			if !bytes.Equal(w.Body.Bytes(), want) || w.Header().Get("Content-Type") != contentType ||
				w.Header().Get("Cache-Control") != "public, max-age=300" ||
				w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("Access-Control-Allow-Origin") != "" {
				t.Fatalf("%s: %v %s", name, w.Header(), w.Body.String())
			}
		}
	}
	for _, name := range []string{"other.js", "stalwart.txt", "desk.CSS", "stalwart.js.json", "desk.json.js", "stalwart.js%00"} {
		addonRequest(t, h, "GET", "/_kuraka/addon/"+name, "", 404)
	}
	for _, name := range []string{"desk.json", "../static/index.html", "stalwart.js/", "other.css"} {
		if _, err := web.Addon(name); err == nil {
			t.Fatalf("Addon accepted %q", name)
		}
	}
}

func TestAddonBranding(t *testing.T) {
	h := newHarness(t, true)
	id := uploadAsset(t, h.root, "image/png", testPNG)
	h.root.do("PUT", "/branding/default", map[string]string{
		"name": "Shared", "logo": id, "icon": id, "color": "#123456",
		"gradient_from": "#abcdef", "gradient_to": "#ffffff",
	}, 200)
	h.root.do("PUT", "/branding/stalwart", map[string]string{"name": "Mail"}, 200)
	h.root.do("PUT", "/branding/desk", map[string]string{"name": "Desk"}, 200)
	tenant := h.tenant("Acme")
	if err := h.st.SetBranding(context.Background(), &tenant, "desk", store.Branding{Name: "Tenant"}); err != nil {
		t.Fatal(err)
	}
	logo := "http://kuraka.test/branding/assets/" + id
	for _, app := range []string{"stalwart", "desk"} {
		w := addonRequest(t, h, "GET", "/_kuraka/addon/"+app+".json?tenant=1", "", 200)
		got := object(t, w.Body.Bytes())
		wantName := "Mail"
		if app == "desk" {
			wantName = "Desk"
		}
		if len(got) != 7 || got["name"] != wantName || got["logo"] != logo || got["icon"] != logo ||
			got["color"] != "#123456" || got["disabled"] != false ||
			w.Header().Get("Cache-Control") != "public, max-age=300" || w.Header().Get("Content-Type") != "application/json" {
			t.Fatalf("%s branding: %v %v", app, got, w.Header())
		}
		if app == "desk" && (got["gradient_from"] != "" || got["gradient_to"] != "") {
			t.Fatalf("desk colors: %v", got)
		}
	}
	w := addonRequest(t, h, "GET", "/_kuraka/addon/stalwart.css", "", 200)
	for _, want := range []string{":root{", "--kuraka-color:#123456;", "--kuraka-color-hover:hsl(",
		"--kuraka-color-foreground:#ffffff;", "--kuraka-color-hsl:210.00 65.38% 20.39%;", "--kuraka-color-foreground-hsl:0 0% 100%;", "--kuraka-gradient-from:#abcdef;", "--kuraka-gradient-to:#ffffff;",
		`--kuraka-logo:url("` + logo + `");`} {
		if !strings.Contains(w.Body.String(), want) {
			t.Errorf("CSS missing %q", want)
		}
	}
}

func TestAddonCSSValidation(t *testing.T) {
	for _, logo := range []string{
		"javascript:alert(1)", "https://example.test/a\";color:red;", "https://example.test/a'", "https://example.test/a\\b",
		"https://example.test/a(b)", "https://example.test/a\nb", "https://example.test/a b", "//example.test/a",
	} {
		if got := addonCSS(store.Branding{Logo: logo}); got != ":root{}\n" {
			t.Errorf("unsafe logo %q: %s", logo, got)
		}
	}
	logo := "https://example.test/a%22%29?x=1&y=two#fragment"
	if got := addonCSS(store.Branding{Logo: logo}); got != `:root{--kuraka-logo:url("`+logo+`");}`+"\n" {
		t.Fatalf("safe encoded URL: %s", got)
	}
	if got := addonCSS(store.Branding{Color: "red", GradientFrom: "#12345", GradientTo: "#123456;display:none"}); got != ":root{}\n" {
		t.Fatalf("invalid colors: %s", got)
	}
	for _, tc := range []struct{ color, hover, foreground string }{
		{"#ffffff", "hsl(0.00 0.00% 92.00%)", "#1c1917"},
		{"#000000", "hsl(0.00 0.00% 0.00%)", "#ffffff"},
		{"#ff0000", "hsl(0.00 100.00% 42.00%)", "#1c1917"},
		{"#00ff00", "hsl(120.00 100.00% 42.00%)", "#1c1917"},
		{"#0000ff", "hsl(240.00 100.00% 42.00%)", "#ffffff"},
		{"#757575", "hsl(0.00 0.00% 37.88%)", "#ffffff"},
		{"#767676", "hsl(0.00 0.00% 38.27%)", "#1c1917"},
	} {
		if _, hover, foreground := addonColor(tc.color); hover != tc.hover || foreground != tc.foreground {
			t.Errorf("%s: %s %s", tc.color, hover, foreground)
		}
	}
}

func TestAddonDisabledAndAdmin(t *testing.T) {
	h := newHarness(t, true)
	initial := object(t, h.root.do("GET", "/addons", nil, 200))
	if len(initial) != 2 {
		t.Fatal(initial)
	}
	for _, app := range []string{"stalwart", "desk"} {
		row := initial[app].(map[string]any)
		supported := row["supported"].(map[string]any)
		if len(row) != 3 || row["disabled"] != false || row["report"] != nil {
			t.Fatal(row)
		}
		if app == "stalwart" && (len(supported) != 2 || supported["stalwart"] != "1.0" || supported["webui"] != "2.0") ||
			app == "desk" && (len(supported) != 1 || supported["libredesk"] != "2.9") {
			t.Fatal(supported)
		}
		for _, disabled := range []bool{true, false} {
			body := h.root.do("PUT", "/addons/"+app, map[string]bool{"disabled": disabled}, 200)
			view := object(t, body)
			if len(view) != 2 || view[app].(map[string]any)["disabled"] != disabled {
				t.Fatal(view)
			}
			stored, err := h.st.GetSetting(context.Background(), "addon_disabled_"+app)
			if err != nil || disabled && stored != "true" || !disabled && stored != "" {
				t.Fatalf("setting: %q %v", stored, err)
			}
			for _, ext := range []string{"js", "css", "json"} {
				w := addonRequest(t, h, "GET", "/_kuraka/addon/"+app+"."+ext, "", 200)
				if ext == "json" {
					if got := object(t, w.Body.Bytes()); len(got) != 7 || got["disabled"] != disabled {
						t.Fatal(got)
					}
				} else if disabled && w.Body.String() != "/* kuraka addon disabled */" {
					t.Fatalf("disabled %s: %s", ext, w.Body.String())
				}
			}
		}
	}
	h.newClient().do("GET", "/addons", nil, 401)
	h.newClient().do("PUT", "/addons/stalwart", map[string]bool{"disabled": true}, 401)
	admin := h.admin(h.tenant("A"), "admin@a.test")
	admin.do("GET", "/addons", nil, 403)
	admin.do("PUT", "/addons/stalwart", map[string]bool{"disabled": true}, 403)
	h.root.do("PUT", "/addons/unknown", map[string]bool{"disabled": true}, 422)
	for _, body := range []string{`{}`, `null`, `{"disabled":null}`, `{"disabled":"true"}`, `{"disabled":true,"extra":1}`, `{"disabled":true} {}`} {
		status, data, _ := h.root.raw("PUT", "/addons/desk", body, "http://kuraka.test", "application/json")
		if status != 422 {
			t.Fatalf("%s: %d %s", body, status, data)
		}
	}
	entries, err := h.st.ListAudit(context.Background(), nil, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range entries {
		if entry.Action == "addon.update" && entry.Detail == "desk disabled=true" && entry.Result == "ok" {
			found = true
		}
	}
	if !found {
		t.Fatal("missing addon update audit detail")
	}
}

func TestAddonReports(t *testing.T) {
	h := newHarness(t, true)
	now := time.Now().UTC()
	h.s.now = func() time.Time { return now }
	body := `{"version":"v1.0.3","missing":["login-logo","nav-brand"]}`
	w := addonRequest(t, h, "POST", "/_kuraka/addon/stalwart/report", body, 204)
	if w.Body.Len() != 0 {
		t.Fatal(w.Body.String())
	}
	view := object(t, h.root.do("GET", "/addons", nil, 200))
	report := view["stalwart"].(map[string]any)["report"].(map[string]any)
	if len(report) != 3 || report["at"] != now.Format(time.RFC3339Nano) || report["version"] != "v1.0.3" ||
		len(report["missing"].([]any)) != 2 || report["missing"].([]any)[0] != "login-logo" {
		t.Fatal(report)
	}
	if raw, err := h.st.GetSetting(context.Background(), "addon_report_stalwart"); err != nil || !json.Valid([]byte(raw)) {
		t.Fatalf("stored report: %q %v", raw, err)
	}
	addonRequest(t, h, "POST", "/_kuraka/addon/stalwart/report", `{"version":"v1.0.3","missing":[]}`, 204)
	if raw, err := h.st.GetSetting(context.Background(), "addon_report_stalwart"); err != nil || raw != "" {
		t.Fatalf("report not cleared: %q %v", raw, err)
	}
	view = object(t, h.root.do("GET", "/addons", nil, 200))
	if view["stalwart"].(map[string]any)["report"] != nil {
		t.Fatal(view)
	}
	for _, app := range []string{"stalwart", "desk"} {
		// Exactly 40 printable characters and 20 valid missing keys are accepted.
		valid, err := json.Marshal(map[string]any{"version": strings.Repeat("x", 40), "missing": strings.Split(strings.Repeat("a,", 19)+"a", ",")})
		if err != nil {
			t.Fatal(err)
		}
		addonRequest(t, h, "POST", "/_kuraka/addon/"+app+"/report", string(valid), 204)
	}
}

func TestAddonReportValidation(t *testing.T) {
	h := newHarness(t, true)
	for _, body := range []string{
		`{}`, `null`, `[]`, `{"version":"x"}`, `{"missing":[]}`, `{"version":null,"missing":[]}`,
		`{"version":"x","missing":null}`, `{"version":1,"missing":[]}`, `{"version":"x","missing":"a"}`,
		`{"version":"x","missing":[null]}`, `{"version":"x","missing":["A"]}`, `{"version":"x","missing":[""]}`,
		`{"version":"x","missing":["a_b"]}`, `{"version":"x","missing":[],"extra":true}`, `{"version":"x","missing":[]} {}`,
		`{"version":"a\nb","missing":[]}`, `{"version":"a\u007fb","missing":[]}`,
		`{"version":"` + strings.Repeat("x", 41) + `","missing":[]}`,
		`{"version":"x","missing":["` + strings.Repeat("a", 61) + `"]}`,
		`{"version":"x","missing":[` + strings.Repeat(`"a",`, 20) + `"a"]}`,
		`{"version":"x","missing":[]}` + strings.Repeat(" ", 4<<10),
	} {
		w := addonRequest(t, h, "POST", "/_kuraka/addon/stalwart/report", body, 422)
		assertCode(t, w.Body.Bytes(), "invalid_input")
	}
	addonRequest(t, h, "POST", "/_kuraka/addon/other/report", `{"version":"x","missing":[]}`, 422)
	for _, contentType := range []string{"", "text/plain", "application/json; bad"} {
		r := httptest.NewRequest("POST", "/_kuraka/addon/stalwart/report", strings.NewReader(`{"version":"x","missing":[]}`))
		r.Header.Set("Content-Type", contentType)
		w := httptest.NewRecorder()
		h.s.Handler().ServeHTTP(w, r)
		if w.Code != 422 {
			t.Fatalf("content type %q: %d", contentType, w.Code)
		}
	}
	if raw, err := h.st.GetSetting(context.Background(), "addon_report_stalwart"); err != nil || raw != "" {
		t.Fatalf("invalid report persisted: %q %v", raw, err)
	}
}

func TestAddonReportRateLimit(t *testing.T) {
	h := newHarness(t, true)
	now := time.Now()
	h.s.now = func() time.Time { return now }
	body := `{"version":"x","missing":["logo"]}`
	for i := 0; i < 10; i++ {
		addonRequest(t, h, "POST", "/_kuraka/addon/stalwart/report", body, 204)
	}
	w := addonRequest(t, h, "POST", "/_kuraka/addon/stalwart/report", body, 429)
	assertCode(t, w.Body.Bytes(), "rate_limited")
	addonRequest(t, h, "POST", "/_kuraka/addon/desk/report", body, 204)
	now = now.Add(time.Hour - time.Second)
	addonRequest(t, h, "POST", "/_kuraka/addon/stalwart/report", body, 429)
	now = now.Add(time.Second)
	addonRequest(t, h, "POST", "/_kuraka/addon/stalwart/report", body, 204)
}

func TestAddonReportLimiterConcurrentIPs(t *testing.T) {
	var limiter addonReportLimiter
	now := time.Now()
	var allowed atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if limiter.allow("stalwart:192.0.2.1", now) {
				allowed.Add(1)
			}
		}()
	}
	wg.Wait()
	if allowed.Load() != 10 || !limiter.allow("stalwart:192.0.2.2", now) || !limiter.allow("desk:192.0.2.1", now) {
		t.Fatalf("allowed %d; app/IP limits must be independent", allowed.Load())
	}
	if !limiter.allow("stalwart:192.0.2.1", now.Add(time.Hour)) {
		t.Fatal("limit did not expire")
	}
	if len(limiter.entries) != 1 {
		t.Fatalf("expired IPs retained: %d", len(limiter.entries))
	}
}
