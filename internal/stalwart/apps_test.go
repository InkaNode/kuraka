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
