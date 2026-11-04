package stalwart

import (
	"context"
	"errors"
	"testing"
)

func TestListBlockedIPs(t *testing.T) {
	c := newFake(t, func(calls []fakeCall) []any {
		assertNames(t, calls, objBlockedIP+"/get")
		assertJSON(t, "get args", calls[0].Args, `{}`)
		return []any{resp(objBlockedIP+"/get", "g", `{"list":[{"id":"b1","address":"172.30.0.3","reason":"portScanning","createdAt":"2026-11-04T06:50:51Z","expiresAt":null}],"notFound":[]}`)}
	})
	ips, err := c.ListBlockedIPs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(ips) != 1 || ips[0].ID != "b1" || ips[0].Address != "172.30.0.3" || ips[0].Reason != "portScanning" || ips[0].ExpiresAt != nil {
		t.Fatalf("ips = %+v", ips)
	}
}

func TestUnblockIP(t *testing.T) {
	c := newFake(t, func(calls []fakeCall) []any {
		assertNames(t, calls, objBlockedIP+"/set", "x:AllowedIp/set", "x:Action/set")
		assertJSON(t, "destroy args", calls[0].Args, `{"destroy":["b1"]}`)
		assertJSON(t, "allow args", calls[1].Args, `{"create":{"a":{"address":"172.30.0.0/24"}}}`)
		assertJSON(t, "reload args", calls[2].Args, `{"create":{"r":{"@type":"ReloadBlockedIps"}}}`)
		return []any{
			resp(objBlockedIP+"/set", "d", `{"destroyed":["b1"]}`),
			resp("x:AllowedIp/set", "a", `{"created":{"a":{"id":"x1"}}}`),
			resp("x:Action/set", "r", `{"created":{"r":{"id":"y1"}}}`),
		}
	})
	if err := c.UnblockIP(context.Background(), "b1", "172.30.0.0/24"); err != nil {
		t.Fatal(err)
	}
	external := newFake(t, func(calls []fakeCall) []any {
		assertNames(t, calls, objBlockedIP+"/set", "x:Action/set")
		return []any{resp(objBlockedIP+"/set", "d", `{"destroyed":["b1"]}`), resp("x:Action/set", "r", `{"created":{"r":{"id":"y1"}}}`)}
	})
	if err := external.UnblockIP(context.Background(), "b1", ""); err != nil {
		t.Fatal(err)
	}
}

func TestUnblockIPRefused(t *testing.T) {
	missing := newFake(t, func(calls []fakeCall) []any {
		return []any{resp(objBlockedIP+"/set", "d", `{"notDestroyed":{"b1":{"type":"notFound"}}}`), resp("x:Action/set", "r", `{"created":{"r":{"id":"y1"}}}`)}
	})
	if err := missing.UnblockIP(context.Background(), "b1", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing error = %v", err)
	}
	untrusted := newFake(t, func(calls []fakeCall) []any {
		return []any{
			resp(objBlockedIP+"/set", "d", `{"destroyed":["b1"]}`),
			resp("x:AllowedIp/set", "a", `{"notCreated":{"a":{"type":"invalidProperties"}}}`),
			resp("x:Action/set", "r", `{"created":{"r":{"id":"y1"}}}`),
		}
	})
	if err := untrusted.UnblockIP(context.Background(), "b1", "172.30.0.0/24"); err == nil {
		t.Fatal("want an error")
	}
	unreloaded := newFake(t, func(calls []fakeCall) []any {
		return []any{resp(objBlockedIP+"/set", "d", `{"destroyed":["b1"]}`), resp("x:Action/set", "r", `{"notCreated":{"r":{"type":"forbidden"}}}`)}
	})
	if err := unreloaded.UnblockIP(context.Background(), "b1", ""); err == nil {
		t.Fatal("want an error")
	}
}
