package releases

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLatest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/vnd.github+json" || r.Header.Get("User-Agent") == "" {
			t.Errorf("headers = %v", r.Header)
		}
		switch r.URL.Path {
		case "/repos/owner/app/releases/latest":
			_, _ = w.Write([]byte(`{"tag_name":"v1.2.3","name":"x"}`))
		case "/repos/owner/bad/releases/latest":
			_, _ = w.Write([]byte(`{"tag_name":"../etc"}`))
		case "/repos/owner/big/releases/latest":
			_, _ = w.Write([]byte(`{"body":"` + strings.Repeat("a", maxBody) + `","tag_name":"v1"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL + "/"}
	tag, err := c.Latest(context.Background(), "owner/app")
	if err != nil || tag != "v1.2.3" {
		t.Fatalf("got %q %v", tag, err)
	}
	for _, repo := range []string{"owner/missing", "owner/bad", "owner/big"} {
		if tag, err := c.Latest(context.Background(), repo); err == nil {
			t.Fatalf("%s: want error, got %q", repo, tag)
		}
	}
}
