package stalwart

import (
	"context"
	"errors"
	"testing"
)

func TestCreateAppPassword(t *testing.T) {
	c := newFake(t, func(calls []fakeCall) []any {
		assertNames(t, calls, "x:AppPassword/set")
		assertJSON(t, "set args", calls[0].Args, `{"accountId":"h","create":{"n":{"description":"libredesk","permissions":{"@type":"Disable","permissions":{"sysDomainDestroy":true,"sysNetworkListenerCreate":true,"sysNetworkListenerDestroy":true,"sysNetworkListenerUpdate":true,"sysRoleCreate":true,"sysRoleDestroy":true,"sysRoleUpdate":true}}}}}`)
		return []any{resp("x:AppPassword/set", "s", `{"accountId":"h","created":{"n":{"id":"b","secret":"app_secret"}}}`)}
	})
	id, secret, err := c.CreateAppPassword(context.Background(), "h", "libredesk")
	if err != nil || id != "b" || secret != "app_secret" {
		t.Fatalf("got %q %q %v", id, secret, err)
	}
}

func TestCreateAppPasswordWithoutSecret(t *testing.T) {
	c := newFake(t, func(calls []fakeCall) []any {
		return []any{resp("x:AppPassword/set", "s", `{"created":{"n":{"id":"b"}}}`)}
	})
	if _, _, err := c.CreateAppPassword(context.Background(), "h", "x"); err == nil {
		t.Fatal("expected an error when no secret is returned")
	}
}

func TestDeleteAppPassword(t *testing.T) {
	c := newFake(t, func(calls []fakeCall) []any {
		assertNames(t, calls, "x:AppPassword/set")
		assertJSON(t, "set args", calls[0].Args, `{"accountId":"h","destroy":["b"]}`)
		return []any{resp("x:AppPassword/set", "s", `{"notDestroyed":{"b":{"type":"notFound"}}}`)}
	})
	err := c.DeleteAppPassword(context.Background(), "h", "b")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}
