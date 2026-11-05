package cloudflare

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testToken = "secret-token-xyz"

type call struct {
	Method string
	Path   string
	Query  string
	Body   map[string]any
}

// newTestClient starts a server whose handler returns (status, body) for each
// request and records every call.
func newTestClient(t *testing.T, h func(r *http.Request) (int, string)) (*Client, *[]call) {
	t.Helper()
	var calls []call
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+testToken {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.Header.Get("User-Agent"); got != "kuraka" {
			t.Errorf("User-Agent = %q", got)
		}
		c := call{Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery}
		if b, _ := io.ReadAll(r.Body); len(b) > 0 {
			if r.Header.Get("Content-Type") != "application/json" {
				t.Errorf("Content-Type = %q", r.Header.Get("Content-Type"))
			}
			if err := json.Unmarshal(b, &c.Body); err != nil {
				t.Errorf("request body: %v", err)
			}
		}
		calls = append(calls, c)
		status, body := h(r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	c := New(testToken)
	c.BaseURL = srv.URL
	return c, &calls
}

func ok(result string) string {
	return `{"success":true,"errors":[],"messages":[],"result":` + result + `}`
}

func TestVerifyToken(t *testing.T) {
	c, calls := newTestClient(t, func(r *http.Request) (int, string) {
		return 200, ok(`{"id":"t1","status":"active"}`)
	})
	if err := c.VerifyToken(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := (*calls)[0]; got.Method != "GET" || got.Path != "/user/tokens/verify" {
		t.Fatalf("call = %+v", got)
	}
}

func TestVerifyTokenInactive(t *testing.T) {
	c, _ := newTestClient(t, func(r *http.Request) (int, string) {
		return 200, ok(`{"id":"t1","status":"disabled"}`)
	})
	if err := c.VerifyToken(context.Background()); err == nil {
		t.Fatal("expected error for disabled token")
	}
}

func TestErrorEnvelope(t *testing.T) {
	c, _ := newTestClient(t, func(r *http.Request) (int, string) {
		return 403, `{"success":false,"errors":[{"code":9109,"message":"Invalid access token"},{"code":10000,"message":"Authentication error"}],"messages":[],"result":null}`
	})
	err := c.VerifyToken(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %T %v", err, err)
	}
	if apiErr.Status != 403 || len(apiErr.Codes) != 2 || apiErr.Codes[0] != 9109 || apiErr.Messages[1] != "Authentication error" {
		t.Fatalf("apiErr = %+v", apiErr)
	}
	if strings.Contains(err.Error(), testToken) {
		t.Fatal("error leaks token")
	}
}

func TestErrorSuccessFalseWith200(t *testing.T) {
	c, _ := newTestClient(t, func(r *http.Request) (int, string) {
		return 200, `{"success":false,"errors":[{"code":1003,"message":"bad"}]}`
	})
	var apiErr *APIError
	if err := c.DeleteRecord(context.Background(), "z", "r"); !errors.As(err, &apiErr) || apiErr.Codes[0] != 1003 {
		t.Fatalf("err = %v", err)
	}
}

func TestErrorNonJSON(t *testing.T) {
	c, _ := newTestClient(t, func(r *http.Request) (int, string) {
		return 502, `<html>bad gateway</html>`
	})
	var apiErr *APIError
	if err := c.VerifyToken(context.Background()); !errors.As(err, &apiErr) || apiErr.Status != 502 {
		t.Fatalf("err = %v", err)
	}
}

func TestFindZoneExact(t *testing.T) {
	c, calls := newTestClient(t, func(r *http.Request) (int, string) {
		return 200, ok(`[{"id":"z1","name":"example.com"}]`)
	})
	z, err := c.FindZone(context.Background(), "Example.com.")
	if err != nil {
		t.Fatal(err)
	}
	if z.ID != "z1" || z.Name != "example.com" {
		t.Fatalf("zone = %+v", z)
	}
	if len(*calls) != 1 || (*calls)[0].Query != "name=example.com" {
		t.Fatalf("calls = %+v", *calls)
	}
}

func TestFindZoneParentFallback(t *testing.T) {
	c, calls := newTestClient(t, func(r *http.Request) (int, string) {
		if r.URL.Query().Get("name") == "example.com" {
			return 200, ok(`[{"id":"z1","name":"example.com"}]`)
		}
		return 200, ok(`[]`)
	})
	z, err := c.FindZone(context.Background(), "a.b.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if z.ID != "z1" {
		t.Fatalf("zone = %+v", z)
	}
	var names []string
	for _, cl := range *calls {
		names = append(names, cl.Query)
	}
	want := "name=a.b.example.com,name=b.example.com,name=example.com"
	if strings.Join(names, ",") != want {
		t.Fatalf("queries = %v", names)
	}
}

func TestCreateZone(t *testing.T) {
	for _, tc := range []struct {
		accounts string
		account  string
	}{
		{`[{"id":"acc1"}]`, "acc1"},
		{`[{"id":"acc1"},{"id":"acc2"}]`, ""},
		{"", ""},
	} {
		c, calls := newTestClient(t, func(r *http.Request) (int, string) {
			if r.URL.Path == "/accounts" {
				if tc.accounts == "" {
					return 403, `{"success":false,"errors":[{"code":9109,"message":"denied"}]}`
				}
				return 200, ok(tc.accounts)
			}
			return 200, ok(`{"id":"z1","name":"example.com","status":"pending","name_servers":["a.ns.cloudflare.com","b.ns.cloudflare.com"]}`)
		})
		z, err := c.CreateZone(context.Background(), "example.com")
		if err != nil {
			t.Fatal(err)
		}
		if z.ID != "z1" || z.Status != "pending" || len(z.NameServers) != 2 {
			t.Fatalf("zone = %+v", z)
		}
		post := (*calls)[1]
		if post.Method != "POST" || post.Path != "/zones" || post.Body["name"] != "example.com" || post.Body["type"] != "full" {
			t.Fatalf("call = %+v", post)
		}
		got, _ := post.Body["account"].(map[string]any)
		if id, _ := got["id"].(string); (got != nil) != (tc.account != "") || id != tc.account {
			t.Fatalf("account = %v", post.Body["account"])
		}
	}
}

func TestFindZoneNotFound(t *testing.T) {
	c, calls := newTestClient(t, func(r *http.Request) (int, string) {
		return 200, ok(`[]`)
	})
	if _, err := c.FindZone(context.Background(), "b.example.com"); !errors.Is(err, ErrZoneNotFound) {
		t.Fatalf("err = %v", err)
	}
	if len(*calls) != 2 {
		t.Fatalf("calls = %d, want 2 (no TLD lookup)", len(*calls))
	}
}
