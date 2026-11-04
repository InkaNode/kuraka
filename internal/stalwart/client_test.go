package stalwart

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

const testKey = "API_test"

type fakeCall struct {
	Name string
	Args json.RawMessage
	ID   string
}

// newFake starts a JMAP server that checks the envelope and lets handle
// produce the methodResponses.
func newFake(t *testing.T, handle func(calls []fakeCall) []any) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/jmap/" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+testKey {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q", got)
		}
		var req struct {
			Using       []string            `json:"using"`
			MethodCalls [][]json.RawMessage `json:"methodCalls"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if !reflect.DeepEqual(req.Using, []string{"urn:ietf:params:jmap:core", "urn:stalwart:jmap"}) {
			t.Errorf("using = %v", req.Using)
		}
		var calls []fakeCall
		for _, mc := range req.MethodCalls {
			var c fakeCall
			json.Unmarshal(mc[0], &c.Name)
			c.Args = mc[1]
			json.Unmarshal(mc[2], &c.ID)
			calls = append(calls, c)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"methodResponses": handle(calls), "sessionState": "s"})
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL, testKey)
}

// assertJSON compares JSON values ignoring key order and whitespace.
func assertJSON(t *testing.T, what string, got json.RawMessage, want string) {
	t.Helper()
	var g, w any
	if err := json.Unmarshal(got, &g); err != nil {
		t.Errorf("%s: bad JSON %s", what, got)
		return
	}
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatalf("%s: bad want JSON: %v", what, err)
	}
	if !reflect.DeepEqual(g, w) {
		t.Errorf("%s =\n%s\nwant\n%s", what, got, want)
	}
}

func assertNames(t *testing.T, calls []fakeCall, names ...string) {
	t.Helper()
	var got []string
	for _, c := range calls {
		got = append(got, c.Name)
	}
	if !reflect.DeepEqual(got, names) {
		t.Errorf("methods = %v, want %v", got, names)
	}
}

func raw(s string) json.RawMessage { return json.RawMessage(s) }

func resp(name, id, args string) []any { return []any{name, raw(args), id} }

const idsRef = `{"#ids":{"resultOf":"q","name":"%s/query","path":"/ids"}}`

func TestPing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/jmap/session" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer "+testKey {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	if err := New(srv.URL+"/", testKey).Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	var he *HTTPError
	if err := New(srv.URL, "bad").Ping(context.Background()); !errors.As(err, &he) || he.Status != 401 {
		t.Fatalf("Ping bad key = %v", err)
	}
}

func TestHTTPErrorProblemJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"type":"about:blank","title":"Forbidden","status":403,"detail":"You are not allowed"}`))
	}))
	defer srv.Close()
	_, err := New(srv.URL, testKey).GetDomain(context.Background(), "d1")
	var he *HTTPError
	if !errors.As(err, &he) {
		t.Fatalf("err = %v, want *HTTPError", err)
	}
	want := HTTPError{Status: 403, Type: "about:blank", Title: "Forbidden", Detail: "You are not allowed"}
	if *he != want {
		t.Fatalf("got %+v, want %+v", *he, want)
	}
}

func TestHTTPErrorNonJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad gateway", http.StatusBadGateway)
	}))
	defer srv.Close()
	_, err := New(srv.URL, testKey).GetDomain(context.Background(), "a")
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != 502 || he.Detail != "bad gateway" {
		t.Fatalf("err = %#v", err)
	}
}

func TestMethodError(t *testing.T) {
	c := newFake(t, func(calls []fakeCall) []any {
		return []any{resp("error", "s", `{"type":"forbidden","description":"no permission"}`)}
	})
	_, err := c.CreateDomain(context.Background(), "a.test", "", nil)
	var me *MethodError
	if !errors.As(err, &me) || me.Type != "forbidden" || me.Description != "no permission" {
		t.Fatalf("err = %v", err)
	}
}

func TestListUnmarshalOrdersNumericKeys(t *testing.T) {
	var l list[string]
	if err := json.Unmarshal([]byte(`{"10":"k","2":"c","0":"a","1":"b"}`), &l); err != nil {
		t.Fatal(err)
	}
	if want := (list[string]{"a", "b", "c", "k"}); !reflect.DeepEqual(l, want) {
		t.Fatalf("got %v, want %v", l, want)
	}
}
