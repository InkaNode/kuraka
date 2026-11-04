package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestHealthcheckAddress(t *testing.T) {
	for _, tc := range []struct{ listen, want string }{
		{":8080", "127.0.0.1:8080"},
		{"0.0.0.0:9000", "127.0.0.1:9000"},
		{"[::]:8080", "127.0.0.1:8080"},
		{"", "127.0.0.1:8080"},
		{"  :9000  ", "127.0.0.1:9000"},
	} {
		t.Run(tc.listen, func(t *testing.T) {
			got, err := healthcheckAddress(tc.listen)
			if err != nil || got != tc.want {
				t.Fatalf("healthcheckAddress(%q) = %q, %v; want %q", tc.listen, got, err, tc.want)
			}
		})
	}
	if _, err := healthcheckAddress("invalid"); err == nil {
		t.Fatal("accepted invalid listen address")
	}
}

func TestHealthcheckStatus(t *testing.T) {
	for _, status := range []int{200, 204, 302, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/healthz" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				w.Header().Set("Location", "/healthz")
				w.WriteHeader(status)
			}))
			defer server.Close()
			err := healthcheck(strings.TrimPrefix(server.URL, "http://"))
			if (err == nil) != (status == http.StatusOK) {
				t.Fatalf("HTTP %d: %v", status, err)
			}
		})
	}
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	server.Close()
	if err := healthcheck(strings.TrimPrefix(server.URL, "http://")); err == nil {
		t.Fatal("unreachable server reported healthy")
	}
}

func TestHealthcheckRunWithoutServerConfiguration(t *testing.T) {
	for _, key := range []string{"KURAKA_PUBLIC_URL", "KURAKA_SECRET_KEY", "KURAKA_STALWART_URL", "KURAKA_STALWART_API_KEY", "KURAKA_DB"} {
		t.Setenv(key, "")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	t.Setenv("KURAKA_LISTEN", strings.TrimPrefix(server.URL, "http://"))
	args := os.Args
	os.Args = []string{"kuraka", "healthcheck"}
	defer func() { os.Args = args }()
	if err := run(); err != nil {
		t.Fatal(err)
	}
}
