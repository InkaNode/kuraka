package stalwart

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
)

func TestListDomains(t *testing.T) {
	c := newFake(t, func(calls []fakeCall) []any {
		assertNames(t, calls, "x:Domain/query", "x:Domain/get")
		assertJSON(t, "query args", calls[0].Args, `{}`)
		assertJSON(t, "get args", calls[1].Args, fmt.Sprintf(idsRef, "x:Domain"))
		return []any{
			resp("x:Domain/query", "q", `{"ids":["d1","d2"]}`),
			resp("x:Domain/get", "g", `{"list":[
				{"id":"d1","name":"cliente1.test","aliases":{"b.test":true,"a.test":true,"off.test":false},
				 "enable":true,"description":"Cliente 1","dnsZoneFile":"zone"},
				{"id":"d2","name":"other.test","aliases":{},"enable":false,"description":null}
			],"notFound":[]}`),
		}
	})
	got, err := c.ListDomains(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []Domain{
		{ID: "d1", Name: "cliente1.test", Aliases: []string{"a.test", "b.test"}, Enable: true, Description: "Cliente 1", ZoneFile: "zone"},
		{ID: "d2", Name: "other.test", Aliases: []string{}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestGetDomain(t *testing.T) {
	c := newFake(t, func(calls []fakeCall) []any {
		assertNames(t, calls, "x:Domain/get")
		if string(calls[0].Args) == `{"ids":["missing"]}` {
			return []any{resp("x:Domain/get", "g", `{"list":[],"notFound":["missing"]}`)}
		}
		assertJSON(t, "get args", calls[0].Args, `{"ids":["d1"]}`)
		return []any{resp("x:Domain/get", "g", `{"list":[{"id":"d1","name":"a.test","enable":true}],"notFound":[]}`)}
	})
	d, err := c.GetDomain(context.Background(), "d1")
	if err != nil || d.ID != "d1" || d.Name != "a.test" || !d.Enable {
		t.Fatalf("GetDomain = %+v, %v", d, err)
	}
	if _, err := c.GetDomain(context.Background(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing err = %v", err)
	}
}

func TestFindDomainByName(t *testing.T) {
	c := newFake(t, func(calls []fakeCall) []any {
		assertNames(t, calls, "x:Domain/query", "x:Domain/get")
		assertJSON(t, "get args", calls[1].Args, fmt.Sprintf(idsRef, "x:Domain"))
		return []any{
			resp("x:Domain/query", "q", `{"ids":["d1","d2"]}`),
			resp("x:Domain/get", "g", `{"list":[
				{"id":"d1","name":"cliente1.test.br"},
				{"id":"d2","name":"cliente1.test"}
			]}`),
		}
	})
	d, err := c.FindDomainByName(context.Background(), "Cliente1.TEST")
	if err != nil || d.ID != "d2" {
		t.Fatalf("FindDomainByName = %+v, %v", d, err)
	}
	if _, err := c.FindDomainByName(context.Background(), "cliente1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("partial match err = %v", err)
	}
}

func TestFindDomainByNameFilter(t *testing.T) {
	c := newFake(t, func(calls []fakeCall) []any {
		assertJSON(t, "query args", calls[0].Args, `{"filter":{"name":"x.test"}}`)
		return []any{
			resp("x:Domain/query", "q", `{"ids":[]}`),
			resp("x:Domain/get", "g", `{"list":[]}`),
		}
	})
	if _, err := c.FindDomainByName(context.Background(), "x.test"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestCreateDomain(t *testing.T) {
	c := newFake(t, func(calls []fakeCall) []any {
		assertNames(t, calls, "x:Domain/set")
		assertJSON(t, "set args", calls[0].Args, `{"create":{"n":{
			"name":"cliente1.test","description":"Cliente 1",
			"aliases":{"alt.test":true,"alt2.test":true}}}}`)
		return []any{resp("x:Domain/set", "s", `{"created":{"n":{"id":"d9"}}}`)}
	})
	id, err := c.CreateDomain(context.Background(), "cliente1.test", "Cliente 1", []string{"alt.test", "alt2.test"})
	if err != nil || id != "d9" {
		t.Fatalf("CreateDomain = %q, %v", id, err)
	}
}

func TestCreateDomainNotCreated(t *testing.T) {
	c := newFake(t, func(calls []fakeCall) []any {
		assertJSON(t, "set args", calls[0].Args, `{"create":{"n":{"name":"dup.test"}}}`)
		return []any{resp("x:Domain/set", "s", `{"notCreated":{"n":{
			"type":"primaryKeyViolation","description":"exists","properties":["name"]}}}`)}
	})
	_, err := c.CreateDomain(context.Background(), "dup.test", "", nil)
	var se *SetError
	if !errors.As(err, &se) {
		t.Fatalf("err = %v, want *SetError", err)
	}
	if se.Type != "primaryKeyViolation" || se.Description != "exists" || !reflect.DeepEqual(se.Properties, []string{"name"}) {
		t.Fatalf("SetError = %+v", se)
	}
}

func TestSetDomainAliases(t *testing.T) {
	c := newFake(t, func(calls []fakeCall) []any {
		assertNames(t, calls, "x:Domain/set")
		if string(calls[0].Args) == `{"update":{"d2":{"aliases":{}}}}` {
			return []any{resp("x:Domain/set", "s", `{"notUpdated":{"d2":{"type":"notFound"}}}`)}
		}
		assertJSON(t, "set args", calls[0].Args, `{"update":{"d1":{"aliases":{"a.test":true,"b.test":true}}}}`)
		return []any{resp("x:Domain/set", "s", `{"updated":{"d1":null}}`)}
	})
	if err := c.SetDomainAliases(context.Background(), "d1", []string{"a.test", "b.test"}); err != nil {
		t.Fatal(err)
	}
	if err := c.SetDomainAliases(context.Background(), "d2", nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("notUpdated err = %v", err)
	}
}
