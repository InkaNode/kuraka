package cloudflare

import (
	"context"
	"net/http"
	"reflect"
	"testing"
)

func TestListRecordsPaginated(t *testing.T) {
	c, calls := newTestClient(t, func(r *http.Request) (int, string) {
		if r.URL.Query().Get("page") == "1" {
			return 200, `{"success":true,"errors":[],"result":[
				{"id":"r1","type":"A","name":"mail.example.com","content":"198.51.100.10","ttl":1,"proxied":false},
				{"id":"r2","type":"MX","name":"example.com","content":"mail.example.com","priority":10,"ttl":1,"comment":"managed by Kuraka"}
			],"result_info":{"page":1,"per_page":100,"count":2,"total_count":3,"total_pages":2}}`
		}
		return 200, `{"success":true,"errors":[],"result":[
			{"id":"r3","type":"SRV","name":"_imaps._tcp.example.com","content":"1 993 mail.example.com","ttl":300,"data":{"priority":0,"weight":1,"port":993,"target":"mail.example.com"}}
		],"result_info":{"page":2,"per_page":100,"count":1,"total_count":3,"total_pages":2}}`
	})
	recs, err := c.ListRecords(context.Background(), "z1")
	if err != nil {
		t.Fatal(err)
	}
	want := []Record{
		{ID: "r1", Type: "A", Name: "mail.example.com", Content: "198.51.100.10", TTL: 1},
		{ID: "r2", Type: "MX", Name: "example.com", Content: "mail.example.com", Priority: 10, TTL: 1, Comment: "managed by Kuraka"},
		{ID: "r3", Type: "SRV", Name: "_imaps._tcp.example.com", Content: "1 993 mail.example.com", TTL: 300,
			Data: &SRVData{Priority: 0, Weight: 1, Port: 993, Target: "mail.example.com"}},
	}
	if !reflect.DeepEqual(recs, want) {
		t.Fatalf("records = %+v", recs)
	}
	if len(*calls) != 2 {
		t.Fatalf("calls = %d", len(*calls))
	}
	for i, q := range []string{"page=1&per_page=100", "page=2&per_page=100"} {
		if cl := (*calls)[i]; cl.Path != "/zones/z1/dns_records" || cl.Query != q {
			t.Fatalf("call %d = %+v", i, cl)
		}
	}
}

func TestCreateRecordBodies(t *testing.T) {
	tests := []struct {
		name string
		in   Record
		want map[string]any
	}{
		{"A", Record{Type: "A", Name: "mail.example.com", Content: "198.51.100.10", Comment: "managed by Kuraka"},
			map[string]any{"type": "A", "name": "mail.example.com", "content": "198.51.100.10", "ttl": 1.0, "proxied": false, "comment": "managed by Kuraka"}},
		{"MX", Record{Type: "MX", Name: "example.com", Content: "mail.example.com", Priority: 10, TTL: 3600},
			map[string]any{"type": "MX", "name": "example.com", "content": "mail.example.com", "priority": 10.0, "ttl": 3600.0}},
		{"TXT", Record{Type: "TXT", Name: "example.com", Content: `"v=spf1 mx -all"`, Proxied: true},
			map[string]any{"type": "TXT", "name": "example.com", "content": `"v=spf1 mx -all"`, "ttl": 1.0}},
		{"SRV", Record{Type: "SRV", Name: "_submissions._tcp.example.com", Content: "ignored", Priority: 5,
			Data: &SRVData{Priority: 0, Weight: 1, Port: 465, Target: "mail.example.com"}},
			map[string]any{"type": "SRV", "name": "_submissions._tcp.example.com", "ttl": 1.0,
				"data": map[string]any{"priority": 0.0, "weight": 1.0, "port": 465.0, "target": "mail.example.com"}}},
		{"TLSA", Record{Type: "TLSA", Name: "_25._tcp.mail.example.com", Content: "ignored",
			TLSA: &TLSAData{Usage: 3, Selector: 1, MatchingType: 1, Certificate: "abcd"}},
			map[string]any{"type": "TLSA", "name": "_25._tcp.mail.example.com", "ttl": 1.0,
				"data": map[string]any{"usage": 3.0, "selector": 1.0, "matching_type": 1.0, "certificate": "abcd"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, calls := newTestClient(t, func(r *http.Request) (int, string) {
				return 200, ok(`{"id":"new","type":"` + tt.in.Type + `","name":"` + tt.in.Name + `","ttl":1}`)
			})
			tt.in.ID = "should-not-be-sent"
			got, err := c.CreateRecord(context.Background(), "z1", tt.in)
			if err != nil {
				t.Fatal(err)
			}
			if got.ID != "new" {
				t.Fatalf("record = %+v", got)
			}
			cl := (*calls)[0]
			if cl.Method != "POST" || cl.Path != "/zones/z1/dns_records" {
				t.Fatalf("call = %+v", cl)
			}
			if !reflect.DeepEqual(cl.Body, tt.want) {
				t.Fatalf("body = %v\nwant %v", cl.Body, tt.want)
			}
		})
	}
}

func TestUpdateRecord(t *testing.T) {
	c, calls := newTestClient(t, func(r *http.Request) (int, string) {
		return 200, ok(`{"id":"r9","type":"CNAME","name":"imap.example.com","content":"mail2.example.com","ttl":1,"proxied":false}`)
	})
	got, err := c.UpdateRecord(context.Background(), "z1", "r9", Record{Type: "CNAME", Name: "imap.example.com", Content: "mail2.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Content != "mail2.example.com" || got.ID != "r9" {
		t.Fatalf("record = %+v", got)
	}
	cl := (*calls)[0]
	if cl.Method != "PATCH" || cl.Path != "/zones/z1/dns_records/r9" || cl.Body["content"] != "mail2.example.com" {
		t.Fatalf("call = %+v", cl)
	}
	if _, ok := cl.Body["id"]; ok {
		t.Fatal("id sent in body")
	}
}

func TestDeleteRecord(t *testing.T) {
	c, calls := newTestClient(t, func(r *http.Request) (int, string) {
		return 200, ok(`{"id":"r9"}`)
	})
	if err := c.DeleteRecord(context.Background(), "z1", "r9"); err != nil {
		t.Fatal(err)
	}
	if cl := (*calls)[0]; cl.Method != "DELETE" || cl.Path != "/zones/z1/dns_records/r9" || cl.Body != nil {
		t.Fatalf("call = %+v", cl)
	}
	if err := c.DeleteRecord(context.Background(), "z1", ""); err == nil {
		t.Fatal("expected error for empty id")
	}
}
