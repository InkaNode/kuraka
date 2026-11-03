package stalwart

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
)

const accountsGet = `{"list":[
	{"id":"a1","@type":"User","name":"juan","domainId":"d1","emailAddress":"juan@cliente1.test",
	 "description":"Juan","credentials":{"0":{"@type":"Password","secret":"$hash"}},
	 "aliases":{"1":{"name":"ventas","domainId":"d2","description":"Sales","enable":false},
	            "0":{"name":"contacto","domainId":"d1","description":null,"enable":true}},
	 "quotas":{"maxDiskQuota":1073741824,"maxEmails":0},"usedDiskQuota":2048},
	{"id":"a2","@type":"Group","name":"info","domainId":"d1","emailAddress":"info@cliente1.test",
	 "description":null,"aliases":{},"quotas":{}}
],"notFound":[]}`

func TestListAccounts(t *testing.T) {
	c := newFake(t, func(calls []fakeCall) []any {
		assertNames(t, calls, "x:Account/query", "x:Account/get")
		assertJSON(t, "query args", calls[0].Args, `{"filter":{"domainId":"d1"}}`)
		assertJSON(t, "get args", calls[1].Args, fmt.Sprintf(idsRef, "x:Account"))
		return []any{
			resp("x:Account/query", "q", `{"ids":["a1","a2"]}`),
			resp("x:Account/get", "g", accountsGet),
		}
	})
	got, err := c.ListAccounts(context.Background(), "d1")
	if err != nil {
		t.Fatal(err)
	}
	want := []Account{
		{
			ID: "a1", Kind: KindUser, Name: "juan", DomainID: "d1", Email: "juan@cliente1.test", Description: "Juan",
			Aliases: []Alias{
				{Name: "contacto", DomainID: "d1", Enable: true},
				{Name: "ventas", DomainID: "d2", Description: "Sales"},
			},
			QuotaBytes: 1 << 30, UsedBytes: 2048,
		},
		{ID: "a2", Kind: KindGroup, Name: "info", DomainID: "d1", Email: "info@cliente1.test"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestGetAccount(t *testing.T) {
	c := newFake(t, func(calls []fakeCall) []any {
		assertNames(t, calls, "x:Account/get")
		if string(calls[0].Args) == `{"ids":["nope"]}` {
			return []any{resp("x:Account/get", "g", `{"list":[],"notFound":["nope"]}`)}
		}
		assertJSON(t, "get args", calls[0].Args, `{"ids":["a1"]}`)
		return []any{resp("x:Account/get", "g", accountsGet)}
	})
	a, err := c.GetAccount(context.Background(), "a1")
	if err != nil || a.ID != "a1" || a.Kind != KindUser || len(a.Aliases) != 2 || a.Aliases[0].Name != "contacto" {
		t.Fatalf("GetAccount = %+v, %v", a, err)
	}
	if _, err := c.GetAccount(context.Background(), "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing err = %v", err)
	}
}

func TestCreateAccountUser(t *testing.T) {
	c := newFake(t, func(calls []fakeCall) []any {
		assertNames(t, calls, "x:Account/set")
		assertJSON(t, "set args", calls[0].Args, `{"create":{"n":{
			"@type":"User","name":"juan","domainId":"d1","description":"Juan",
			"credentials":{"0":{"@type":"Password","secret":"s3cret-password"}},
			"quotas":{"maxDiskQuota":1048576},
			"aliases":{
				"0":{"name":"contacto","domainId":"d1","description":null,"enable":true},
				"1":{"name":"ventas","domainId":"d2","description":"Sales","enable":true}}}}}`)
		return []any{resp("x:Account/set", "s", `{"created":{"n":{"id":"a7"}}}`)}
	})
	id, err := c.CreateAccount(context.Background(), NewAccount{
		Kind: KindUser, Name: "juan", DomainID: "d1", Description: "Juan", Password: "s3cret-password",
		QuotaBytes: 1 << 20,
		Aliases: []Alias{
			{Name: "contacto", DomainID: "d1", Enable: true},
			{Name: "ventas", DomainID: "d2", Description: "Sales", Enable: true},
		},
	})
	if err != nil || id != "a7" {
		t.Fatalf("CreateAccount = %q, %v", id, err)
	}
}

func TestCreateAccountGroup(t *testing.T) {
	c := newFake(t, func(calls []fakeCall) []any {
		assertJSON(t, "set args", calls[0].Args, `{"create":{"n":{"@type":"Group","name":"info","domainId":"d1"}}}`)
		return []any{resp("x:Account/set", "s", `{"created":{"n":{"id":"a8"}}}`)}
	})
	id, err := c.CreateAccount(context.Background(), NewAccount{Kind: KindGroup, Name: "info", DomainID: "d1", Password: "ignored"})
	if err != nil || id != "a8" {
		t.Fatalf("CreateAccount = %q, %v", id, err)
	}
}

func TestCreateAccountValidation(t *testing.T) {
	c := New("http://127.0.0.1:0", testKey)
	if _, err := c.CreateAccount(context.Background(), NewAccount{Name: "x", DomainID: "d1"}); err == nil {
		t.Fatal("User without password accepted")
	}
	if _, err := c.CreateAccount(context.Background(), NewAccount{Kind: "Admin", Name: "x", DomainID: "d1"}); err == nil {
		t.Fatal("unknown kind accepted")
	}
}

func TestCreateAccountNotCreated(t *testing.T) {
	c := newFake(t, func(calls []fakeCall) []any {
		return []any{resp("x:Account/set", "s", `{"notCreated":{"n":{"type":"invalidProperties","properties":["name"]}}}`)}
	})
	_, err := c.CreateAccount(context.Background(), NewAccount{Name: "bad name", DomainID: "d1", Password: "p"})
	var se *SetError
	if !errors.As(err, &se) || se.Type != "invalidProperties" || !reflect.DeepEqual(se.Properties, []string{"name"}) {
		t.Fatalf("err = %v", err)
	}
}

func TestUpdateAccount(t *testing.T) {
	c := newFake(t, func(calls []fakeCall) []any {
		assertNames(t, calls, "x:Account/set")
		assertJSON(t, "set args", calls[0].Args, `{"update":{"a1":{
			"description":"New name",
			"credentials":{"0":{"@type":"Password","secret":"new-password"}},
			"quotas":{},
			"aliases":{"0":{"name":"b","domainId":"d1","description":null,"enable":true}}}}}`)
		return []any{resp("x:Account/set", "s", `{"updated":{"a1":null}}`)}
	})
	desc, pw, quota := "New name", "new-password", uint64(0)
	aliases := []Alias{{Name: "b", DomainID: "d1", Enable: true}}
	err := c.UpdateAccount(context.Background(), "a1", AccountPatch{
		Description: &desc, Password: &pw, QuotaBytes: &quota, Aliases: &aliases,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestUpdateAccountQuotaAndEmptyAliases(t *testing.T) {
	c := newFake(t, func(calls []fakeCall) []any {
		assertJSON(t, "set args", calls[0].Args, `{"update":{"a1":{"quotas":{"maxDiskQuota":5000},"aliases":{}}}}`)
		return []any{resp("x:Account/set", "s", `{"notUpdated":{"a1":{"type":"forbidden","description":"nope"}}}`)}
	})
	quota := uint64(5000)
	aliases := []Alias{}
	err := c.UpdateAccount(context.Background(), "a1", AccountPatch{QuotaBytes: &quota, Aliases: &aliases})
	var se *SetError
	if !errors.As(err, &se) || se.Type != "forbidden" {
		t.Fatalf("err = %v", err)
	}
}

func TestUpdateAccountNoop(t *testing.T) {
	c := New("http://127.0.0.1:0", testKey)
	if err := c.UpdateAccount(context.Background(), "a1", AccountPatch{}); err != nil {
		t.Fatalf("empty patch: %v", err)
	}
}

func TestDeleteAccount(t *testing.T) {
	c := newFake(t, func(calls []fakeCall) []any {
		assertNames(t, calls, "x:Account/set")
		if string(calls[0].Args) == `{"destroy":["gone"]}` {
			return []any{resp("x:Account/set", "s", `{"notDestroyed":{"gone":{"type":"notFound"}}}`)}
		}
		assertJSON(t, "set args", calls[0].Args, `{"destroy":["a1"]}`)
		return []any{resp("x:Account/set", "s", `{"destroyed":["a1"]}`)}
	})
	if err := c.DeleteAccount(context.Background(), "a1"); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteAccount(context.Background(), "gone"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("gone err = %v", err)
	}
}
