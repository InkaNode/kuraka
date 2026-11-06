package stalwart

import (
	"context"
	"testing"
)

func TestUpdateApps(t *testing.T) {
	c := newFake(t, func(calls []fakeCall) []any {
		assertNames(t, calls, "x:Action/set")
		assertJSON(t, "set args", calls[0].Args, `{"create":{"n":{"@type":"UpdateApps"}}}`)
		return []any{resp("x:Action/set", "s", `{"created":{"n":{"id":"a"}}}`)}
	})
	if err := c.UpdateApps(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateAppsRefused(t *testing.T) {
	c := newFake(t, func(calls []fakeCall) []any {
		return []any{resp("x:Action/set", "s", `{"notCreated":{"n":{"type":"forbidden"}}}`)}
	})
	if err := c.UpdateApps(context.Background()); err == nil {
		t.Fatal("want an error")
	}
}

func TestSetWebUIURL(t *testing.T) {
	c := newFake(t, func(calls []fakeCall) []any {
		if calls[0].Name == "x:Application/get" {
			return []any{resp("x:Application/get", "g", `{"list":[{"id":"m","urlPrefix":{"/mail":true},"resourceUrl":"a"},{"id":"w","urlPrefix":{"/admin":true},"resourceUrl":"old"}]}`)}
		}
		assertNames(t, calls, "x:Application/set")
		assertJSON(t, "set args", calls[0].Args, `{"update":{"w":{"resourceUrl":"new"}}}`)
		return []any{resp("x:Application/set", "s", `{"updated":{"w":null}}`)}
	})
	if err := c.SetWebUIURL(context.Background(), "new"); err != nil {
		t.Fatal(err)
	}
}
