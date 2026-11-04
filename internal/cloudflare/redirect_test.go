package cloudflare

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"testing"
)

func TestSetRedirect(t *testing.T) {
	c, calls := newTestClient(t, func(r *http.Request) (int, string) {
		return 200, ok(`{"id":"rs1","phase":"http_request_dynamic_redirect","rules":[]}`)
	})
	if err := c.SetRedirect(context.Background(), "z1", "https://primary.example/"); err != nil {
		t.Fatal(err)
	}
	cl := (*calls)[0]
	if cl.Method != "PUT" || cl.Path != "/zones/z1/rulesets/phases/http_request_dynamic_redirect/entrypoint" {
		t.Fatalf("call = %+v", cl)
	}
	want := map[string]any{"rules": []any{map[string]any{
		"description": "kuraka alias redirect",
		"expression":  "true",
		"action":      "redirect",
		"action_parameters": map[string]any{"from_value": map[string]any{
			"status_code":           301.0,
			"target_url":            map[string]any{"expression": `concat("https://primary.example", http.request.uri.path)`},
			"preserve_query_string": true,
		}},
	}}}
	if !reflect.DeepEqual(cl.Body, want) {
		t.Fatalf("body = %#v", cl.Body)
	}
}

func TestSetRedirectInvalidTargets(t *testing.T) {
	c, calls := newTestClient(t, func(r *http.Request) (int, string) {
		return 200, ok(`{}`)
	})
	for _, target := range []string{
		"",
		"http://primary.example",
		"primary.example",
		"/relative",
		"https://",
		`https://primary.example/"), true or ("`,
		"https://primary.example/'x",
		`https://primary.example\x`,
		"https://primary.example/a b",
		"https://primary.example/\n",
		"https://user:pw@primary.example",
		"https://primary.example/?q=1",
		"https://primary.example/#frag",
	} {
		if err := c.SetRedirect(context.Background(), "z1", target); !errors.Is(err, ErrInvalidTarget) {
			t.Errorf("target %q: err = %v", target, err)
		}
	}
	if len(*calls) != 0 {
		t.Fatalf("invalid targets reached the API: %+v", *calls)
	}
}
