package libredesk

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

const (
	testKey    = "kkey"
	testSecret = "ssecret"
)

func newTest(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return New(srv.URL+"/", testKey, testSecret)
}

func checkAuth(t *testing.T, r *http.Request) {
	t.Helper()
	if got, want := r.Header.Get("Authorization"), "token kkey:ssecret"; got != want {
		t.Errorf("Authorization = %q, want %q", got, want)
	}
}

func TestPingAndList(t *testing.T) {
	c := newTest(t, func(w http.ResponseWriter, r *http.Request) {
		checkAuth(t, r)
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/inboxes" {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		io.WriteString(w, `{"status":"success","data":[{"id":7,"name":"Support","channel":"email","from":"s@example.com","enabled":true,"config":{}}]}`)
	})
	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	got, err := c.ListInboxes(context.Background())
	if err != nil {
		t.Fatalf("ListInboxes: %v", err)
	}
	want := []Inbox{{ID: 7, Name: "Support", Channel: "email", From: "s@example.com", Enabled: true}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ListInboxes = %+v, want %+v", got, want)
	}
}

func TestCreateEmailInbox(t *testing.T) {
	var body map[string]any
	c := newTest(t, func(w http.ResponseWriter, r *http.Request) {
		checkAuth(t, r)
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/inboxes" {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q", ct)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		io.WriteString(w, `{"status":"success","data":{"id":42,"name":"Support"}}`)
	})
	id, err := c.CreateEmailInbox(context.Background(), NewEmailInbox{
		Name: "Support",
		From: "support@example.com",
		IMAP: MailServer{Host: "mail.example.com", Port: 993, TLS: "tls", Username: "support@example.com", Password: "app-pw"},
		SMTP: MailServer{Host: "mail.example.com", Port: 465, TLS: "tls", SkipVerify: true, Username: "support@example.com", Password: "app-pw"},
	})
	if err != nil {
		t.Fatalf("CreateEmailInbox: %v", err)
	}
	if id != 42 {
		t.Errorf("id = %d, want 42", id)
	}
	var want map[string]any
	if err := json.Unmarshal([]byte(`{
		"name": "Support",
		"channel": "email",
		"enabled": true,
		"csat_enabled": false,
		"from": "support@example.com",
		"config": {
			"auth_type": "password",
			"from": "support@example.com",
			"reply_to": "",
			"enable_plus_addressing": false,
			"smtp": [{
				"host": "mail.example.com", "port": 465,
				"username": "support@example.com", "password": "app-pw",
				"auth_protocol": "plain", "tls_type": "tls", "tls_skip_verify": true,
				"hello_hostname": "", "max_conns": 5, "max_msg_retries": 3,
				"idle_timeout": "25s", "pool_wait_timeout": "60s"
			}],
			"imap": [{
				"host": "mail.example.com", "port": 993,
				"username": "support@example.com", "password": "app-pw",
				"mailbox": "INBOX", "tls_type": "tls", "tls_skip_verify": false,
				"read_interval": "30s", "scan_inbox_since": "48h"
			}]
		}
	}`), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(body, want) {
		g, _ := json.MarshalIndent(body, "", "  ")
		t.Errorf("body mismatch:\n%s", g)
	}
}

func TestErrorEnvelope(t *testing.T) {
	c := newTest(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"status":"error","message":"Invalid inbox config","data":null}`)
	})
	_, err := c.CreateEmailInbox(context.Background(), NewEmailInbox{Name: "x"})
	ae, ok := errors.AsType[*APIError](err)
	if !ok {
		t.Fatalf("err = %v, want *APIError", err)
	}
	if ae.Status != http.StatusBadRequest || ae.Message != "Invalid inbox config" {
		t.Errorf("APIError = %+v", ae)
	}
	if s := err.Error(); strings.Contains(s, testKey) || strings.Contains(s, testSecret) {
		t.Errorf("error leaks credentials: %q", s)
	}
}

func TestNonJSONError(t *testing.T) {
	c := newTest(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		io.WriteString(w, "<html>bad gateway</html>")
	})
	err := c.Ping(context.Background())
	ae, ok := errors.AsType[*APIError](err)
	if !ok || ae.Status != http.StatusBadGateway || ae.Message != "Bad Gateway" {
		t.Fatalf("err = %v", err)
	}
}

func TestDeleteInbox(t *testing.T) {
	c := newTest(t, func(w http.ResponseWriter, r *http.Request) {
		checkAuth(t, r)
		if r.Method != http.MethodDelete {
			t.Errorf("method = %s", r.Method)
		}
		switch r.URL.Path {
		case "/api/v1/inboxes/5":
			io.WriteString(w, `{"status":"success","data":true}`)
		case "/api/v1/inboxes/6":
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"status":"error","message":"Inbox not found"}`)
		default:
			t.Errorf("path = %s", r.URL.Path)
		}
	})
	if err := c.DeleteInbox(context.Background(), 5); err != nil {
		t.Fatalf("DeleteInbox(5): %v", err)
	}
	if err := c.DeleteInbox(context.Background(), 6); !errors.Is(err, ErrNotFound) {
		t.Fatalf("DeleteInbox(6) = %v, want ErrNotFound", err)
	}
}
