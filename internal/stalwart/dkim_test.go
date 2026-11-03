package stalwart

import (
	"context"
	"fmt"
	"reflect"
	"testing"
)

func TestListDkim(t *testing.T) {
	c := newFake(t, func(calls []fakeCall) []any {
		assertNames(t, calls, "x:DkimSignature/query", "x:DkimSignature/get")
		assertJSON(t, "query args", calls[0].Args, `{}`)
		assertJSON(t, "get args", calls[1].Args, fmt.Sprintf(idsRef, "x:DkimSignature"))
		return []any{
			resp("x:DkimSignature/query", "q", `{"ids":["k1","k2","k3"]}`),
			resp("x:DkimSignature/get", "g", `{"list":[
				{"id":"k1","@type":"Dkim1Ed25519Sha256","domainId":"d1","selector":"v1-ed25519-20261103","publicKey":"ED","stage":"active"},
				{"id":"k2","@type":"Dkim1RsaSha256","domainId":"d2","selector":"v1-rsa-20261103","publicKey":"X","stage":"active"},
				{"id":"k3","@type":"Dkim1RsaSha256","domainId":"d1","selector":"v1-rsa-20261103","publicKey":"RSA","stage":{"@type":"Pending"}}
			]}`),
		}
	})
	got, err := c.ListDkim(context.Background(), "d1")
	if err != nil {
		t.Fatal(err)
	}
	want := []DkimKey{
		{ID: "k1", DomainID: "d1", Selector: "v1-ed25519-20261103", Algorithm: "Dkim1Ed25519Sha256", PublicKey: "ED", Stage: "active"},
		{ID: "k3", DomainID: "d1", Selector: "v1-rsa-20261103", Algorithm: "Dkim1RsaSha256", PublicKey: "RSA", Stage: "Pending"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}
