package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestServeUI(t *testing.T) {
	h := newHarness(t, false)
	h.s.WithUI(fstest.MapFS{
		"index.html":    {Data: []byte("<!doctype html>index")},
		"assets/app.js": {Data: []byte("console.log(1)")},
	})
	handler := h.s.Handler()
	cases := []struct {
		method, path string
		status       int
		body         string
	}{
		{"GET", "/", 200, "index"},
		{"GET", "/domains/5", 200, "index"},
		{"GET", "/assets/app.js", 200, "console.log"},
		{"GET", "/assets/missing.js", 404, ""},
		{"GET", "/api/v1/nope", 404, "not_found"},
		{"GET", "/../../etc/passwd", 307, ""}, // ServeMux cleans the path and redirects
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(c.method, c.path, nil))
		if rec.Code != c.status || !strings.Contains(rec.Body.String(), c.body) {
			t.Errorf("%s %s: got %d %q", c.method, c.path, rec.Code, rec.Body.String())
		}
		if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'self'") {
			t.Errorf("%s: missing CSP, got %q", c.path, csp)
		}
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/domains", strings.NewReader("{}")))
	if rec.Code == http.StatusOK {
		t.Errorf("POST to UI route should not serve the app")
	}
}
