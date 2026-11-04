package releases

import (
	"context"
	"encoding/json"
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

func TestLatestRelease(t *testing.T) {
	sha := strings.Repeat("a", 40)
	for _, tc := range []struct {
		name, tag, commit, url string
		want                   Release
		wantError              bool
	}{
		{"release", "v1.2.3", sha, "https://github.com/owner/app/releases/tag/v1.2.3", Release{"v1.2.3", sha, "https://github.com/owner/app/releases/tag/v1.2.3"}, false},
		{"branch", "v1.2.3", "main", "https://github.com/owner/app", Release{Tag: "v1.2.3", URL: "https://github.com/owner/app"}, false},
		{"uppercase", "v1.2.3", strings.Repeat("A", 40), "https://example.com/", Release{Tag: "v1.2.3"}, false},
		{"short", "v1.2.3", sha[:39], "http://github.com/owner/app", Release{Tag: "v1.2.3"}, false},
		{"long", "v1.2.3", sha + "a", "https://github.com.evil.test/", Release{Tag: "v1.2.3"}, false},
		{"nonhex", "v1.2.3", strings.Repeat("g", 40), "javascript:alert(1)", Release{Tag: "v1.2.3"}, false},
		{"empty metadata", "v1.2.3", "", "", Release{Tag: "v1.2.3"}, false},
		{"bad tag", "../etc", sha, "https://github.com/owner/app", Release{}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/repos/owner/app/releases/latest" {
					t.Errorf("path = %q", r.URL.Path)
				}
				_ = json.NewEncoder(w).Encode(map[string]string{"tag_name": tc.tag, "target_commitish": tc.commit, "html_url": tc.url})
			}))
			defer srv.Close()
			c := &Client{BaseURL: srv.URL}
			got, err := c.LatestRelease(context.Background(), "owner/app")
			if (err != nil) != tc.wantError || got != tc.want {
				t.Fatalf("got %+v, %v; want %+v, error=%t", got, err, tc.want, tc.wantError)
			}
		})
	}
}
