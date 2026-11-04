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
		case "/api/v1/inboxes/7":
			io.WriteString(w, `{"status":"error","message":"Inbox in use"}`)
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
	if _, ok := errors.AsType[*APIError](c.DeleteInbox(context.Background(), 7)); !ok {
		t.Fatal("DeleteInbox(7) ignored an error envelope with status 200")
	}
}

func TestCreateAgentAndSetPassword(t *testing.T) {
	calls := 0
	c := newTest(t, func(w http.ResponseWriter, r *http.Request) {
		checkAuth(t, r)
		calls++
		if r.Header.Get("Content-Type") != "application/json" {
			t.Error("missing JSON content type")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		want := map[string]any{
			"first_name": "Ada", "last_name": "Lovelace", "email": "ada@example.test",
			"roles": []any{"Admin"}, "teams": []any{}, "enabled": true,
			"send_welcome_email": false, "availability_status": "offline",
		}
		if calls == 1 {
			if r.Method != http.MethodPost || r.URL.Path != "/api/v1/agents" {
				t.Errorf("create: %s %s", r.Method, r.URL.Path)
			}
		} else {
			if r.Method != http.MethodPut || r.URL.Path != "/api/v1/agents/42" {
				t.Errorf("password: %s %s", r.Method, r.URL.Path)
			}
			want["new_password"] = "Aa9!0123456789abcdef01234567"
		}
		if !reflect.DeepEqual(body, want) {
			t.Error("invalid agent request body")
		}
		io.WriteString(w, `{"status":"success","data":{"id":42}}`)
	})
	a := NewAgent{FirstName: "Ada", LastName: "Lovelace", Email: "ada@example.test", Roles: []string{"Admin"}}
	id, err := c.CreateAgent(context.Background(), a)
	if err != nil || id != 42 {
		t.Fatalf("CreateAgent: id=%d err=%v", id, err)
	}
	if err := c.SetAgentPassword(context.Background(), id, a, "Aa9!0123456789abcdef01234567"); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("calls=%d", calls)
	}
}

func TestSyncAgentPassword(t *testing.T) {
	calls := 0
	c := newTest(t, func(w http.ResponseWriter, r *http.Request) {
		checkAuth(t, r)
		calls++
		if r.URL.Path != "/api/v1/agents/42" {
			t.Errorf("path: %s", r.URL.Path)
		}
		if calls == 1 {
			if r.Method != http.MethodGet {
				t.Errorf("read: %s", r.Method)
			}
			io.WriteString(w, `{"status":"success","data":{"id":42,"first_name":"Ada","last_name":"Lovelace","email":"ada@example.test",
				"roles":["Agent","Billing"],"enabled":false,"availability_status":"away","teams":[{"id":3,"name":"Support","emoji":"x"},{"id":4,"name":"Sales"}]}}`)
			return
		}
		if r.Method != http.MethodPut {
			t.Errorf("update: %s", r.Method)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		want := map[string]any{
			"first_name": "Ada", "last_name": "Lovelace", "email": "ada@example.test",
			"roles": []any{"Agent", "Billing"}, "teams": []any{"Support", "Sales"}, "enabled": false,
			"availability_status": "away", "new_password": "Strong Pass 123!",
		}
		if !reflect.DeepEqual(body, want) {
			t.Errorf("invalid agent request body: %v", body)
		}
		io.WriteString(w, `{"status":"success","data":{}}`)
	})
	if err := c.SyncAgentPassword(context.Background(), 42, "Strong Pass 123!"); err != nil || calls != 2 {
		t.Fatalf("SyncAgentPassword: calls=%d err=%v", calls, err)
	}
	c = newTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Error("updated an agent it could not read")
		}
		w.WriteHeader(404)
		io.WriteString(w, `{"status":"error","message":"Not found"}`)
	})
	if err := c.SyncAgentPassword(context.Background(), 42, "Strong Pass 123!"); err == nil {
		t.Fatal("missing agent ignored")
	}
}

func TestAgentErrors(t *testing.T) {
	for _, tc := range []struct {
		name     string
		status   int
		body     string
		conflict bool
	}{
		{"conflict", 409, `{"status":"error","message":"Conflict"}`, true},
		{"duplicate email", 500, `{"status":"error","message":"User with same email already exists"}`, true},
		{"upstream failure", 500, `{"status":"error","message":"Something went wrong"}`, false},
		{"error envelope", 200, `{"status":"error","message":"Invalid role"}`, false},
		{"missing id", 200, `{"data":{}}`, false},
		{"negative id", 200, `{"data":{"id":-1}}`, false},
		{"invalid JSON", 200, `invalid`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newTest(t, func(w http.ResponseWriter, r *http.Request) {
				checkAuth(t, r)
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.body)
			})
			_, err := c.CreateAgent(context.Background(), NewAgent{})
			if err == nil || errors.Is(err, ErrConflict) != tc.conflict {
				t.Fatalf("CreateAgent: %v", err)
			}
			if tc.status != 200 || strings.Contains(tc.body, `"status":"error"`) {
				if err := c.SetAgentPassword(context.Background(), 42, NewAgent{}, "password"); err == nil {
					t.Fatal("password update ignored upstream error")
				}
			}
		})
	}
}

func TestSystemAPIKey(t *testing.T) {
	calls := 0
	c := newTest(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "POST" || r.Header.Get("Content-Type") != "application/json" || r.Header.Get("Authorization") != "" {
			t.Error("invalid setup request")
		}
		switch r.URL.Path {
		case "/api/v1/auth/login":
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body["email"] != "System" || body["password"] != "private-password" {
				t.Error("invalid login body")
			}
			if len(r.Cookies()) != 0 || r.Header.Get("X-CSRFTOKEN") != "" {
				t.Error("login must not need cookies or CSRF")
			}
			http.SetCookie(w, &http.Cookie{Name: "libredesk_session", Value: "session", Secure: true, HttpOnly: true, Path: "/"})
			http.SetCookie(w, &http.Cookie{Name: "csrf_token", Value: "csrf", Secure: true, Path: "/"})
			io.WriteString(w, `{"status":"success","data":{"id":37}}`)
		case "/api/v1/agents/37/api-key":
			for name, want := range map[string]string{"libredesk_session": "session", "csrf_token": "csrf"} {
				cookie, err := r.Cookie(name)
				if err != nil || cookie.Value != want {
					t.Errorf("missing cookie %s over plain HTTP", name)
				}
			}
			if r.Header.Get("X-CSRFTOKEN") != "csrf" {
				t.Error("missing CSRF header")
			}
			io.WriteString(w, `{"status":"success","data":{"api_key":"new-key","api_secret":"new-secret"}}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(404)
		}
	})
	key, secret, err := c.SystemAPIKey(context.Background(), "private-password")
	if err != nil || key != "new-key" || secret != "new-secret" || calls != 2 {
		t.Fatalf("API key flow failed: %v, calls=%d", err, calls)
	}
}

func TestSystemAPIKeyErrors(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     int
		login, key string
		cookies    bool
	}{
		{"login failure", 401, `{"status":"error","message":"private-password private-secret"}`, "", false},
		{"login error envelope", 200, `{"status":"error","message":"private-password"}`, "", false},
		{"bad JSON", 200, `private-password`, "", false},
		{"missing id", 200, `{"data":{"id":0}}`, "", true},
		{"missing cookies", 200, `{"data":{"id":37}}`, "", false},
		{"key failure", 200, `{"data":{"id":37}}`, `{"status":"error","message":"private-secret"}`, true},
		{"missing credentials", 200, `{"data":{"id":37}}`, `{"data":{"api_key":"key"}}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newTest(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v1/auth/login" {
					if tc.cookies {
						http.SetCookie(w, &http.Cookie{Name: "libredesk_session", Value: "session", Secure: true})
						http.SetCookie(w, &http.Cookie{Name: "csrf_token", Value: "csrf", Secure: true})
					}
					w.WriteHeader(tc.status)
					io.WriteString(w, tc.login)
				} else {
					io.WriteString(w, tc.key)
				}
			})
			_, _, err := c.SystemAPIKey(context.Background(), "private-password")
			if err == nil || strings.Contains(err.Error(), "private-") {
				t.Fatalf("unsafe or missing error: %v", err)
			}
		})
	}
}
