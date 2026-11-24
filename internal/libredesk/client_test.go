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

func TestRenameEmailInbox(t *testing.T) {
	var put map[string]any
	c := newTest(t, func(w http.ResponseWriter, r *http.Request) {
		checkAuth(t, r)
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/inboxes/5":
			io.WriteString(w, `{"status":"success","data":{"id":5,"name":"Old","channel":"email","from":"Old <a@x.test>","enabled":true,"csat_enabled":true,"from_name_template":"t","config":{"auth_type":"password","imap":[{"host":"mail","password":"masked"}],"smtp":[{"host":"mail","password":"masked"}],"oauth":{"access_token":"masked"}}}}`)
		case "PUT /api/v1/inboxes/5":
			if err := json.NewDecoder(r.Body).Decode(&put); err != nil {
				t.Error(err)
			}
			io.WriteString(w, `{"status":"success","data":{}}`)
		case "GET /api/v1/inboxes/6":
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"status":"error","message":"Inbox not found"}`)
		default:
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
	})
	if err := c.RenameEmailInbox(context.Background(), 5, "New", "New <a@x.test>"); err != nil {
		t.Fatal(err)
	}
	config := put["config"].(map[string]any)
	imap, smtp := config["imap"].([]any)[0].(map[string]any), config["smtp"].([]any)[0].(map[string]any)
	if put["name"] != "New" || put["from"] != "New <a@x.test>" || put["csat_enabled"] != true || put["from_name_template"] != "t" ||
		imap["password"] != "" || imap["host"] != "mail" || smtp["password"] != "" || config["oauth"].(map[string]any)["access_token"] != "" {
		t.Fatalf("put = %v", put)
	}
	if err := c.RenameEmailInbox(context.Background(), 6, "New", "New <a@x.test>"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing inbox = %v", err)
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

func TestApplyBranding(t *testing.T) {
	var put map[string]any
	c := newTest(t, func(w http.ResponseWriter, r *http.Request) {
		checkAuth(t, r)
		if r.URL.Path != "/api/v1/settings/general" {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		switch r.Method {
		case http.MethodGet:
			io.WriteString(w, `{"status":"success","data":{"app.site_name":"Libredesk","app.logo_url":"","app.favicon_url":"old.ico","app.root_url":"https://desk.example.com","app.lang":"en","app.max_file_upload_size":20,"app.update":{},"app.version":"v2.9.2","app.restart_required":false}}`)
		case http.MethodPut:
			if err := json.NewDecoder(r.Body).Decode(&put); err != nil {
				t.Errorf("decode body: %v", err)
			}
			io.WriteString(w, `{"status":"success","data":true}`)
		default:
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
	})
	if err := c.ApplyBranding(context.Background(), "Acme Desk", "https://kuraka.test/branding/assets/a", "", "https://desk.acme.test"); err != nil {
		t.Fatalf("ApplyBranding: %v", err)
	}
	want := map[string]any{
		"app.site_name":            "Acme Desk",
		"app.logo_url":             "https://kuraka.test/branding/assets/a",
		"app.favicon_url":          "old.ico",
		"app.root_url":             "https://desk.acme.test",
		"app.lang":                 "en",
		"app.max_file_upload_size": float64(20),
	}
	if !reflect.DeepEqual(put, want) {
		t.Errorf("PUT body = %v, want %v", put, want)
	}
}

func TestLiveChat(t *testing.T) {
	for in, want := range map[string]string{"https://www.Shop.test/contact": "*.shop.test", "http://a.test:8080": "*.a.test:8080"} {
		if got := TrustedDomain(in); got != want {
			t.Errorf("TrustedDomain(%q) = %q, want %q", in, got, want)
		}
	}
	lc := LiveChat{Name: "Ventas", Website: "https://www.shop.test", Color: "#112233", LogoURL: "https://shop.test/l.png", LauncherLogoURL: "https://shop.test/i.png", Greeting: "Hola", Position: "left"}
	var created, updated map[string]any
	c := newTest(t, func(w http.ResponseWriter, r *http.Request) {
		checkAuth(t, r)
		switch r.Method + " " + r.URL.Path {
		case "POST /api/v1/inboxes":
			json.NewDecoder(r.Body).Decode(&created)
			io.WriteString(w, `{"status":"success","data":{"id":9,"uuid":"u-9"}}`)
		case "GET /api/v1/inboxes/9":
			io.WriteString(w, `{"status":"success","data":{"id":9,"name":"Old","channel":"livechat","secret":"••••","config":{"brand_name":"Old","home_apps":[{"url":"https://x.test"}],"launcher":{"position":"right","icon_scale":80},"branding":{"light":{"colors":{"primary":"#000000"},"logo_url":""},"dark":{"colors":{"primary":"#ffffff"}}}}}}`)
		case "PUT /api/v1/inboxes/9":
			json.NewDecoder(r.Body).Decode(&updated)
			io.WriteString(w, `{"status":"success","data":{}}`)
		case "GET /api/v1/inboxes/8":
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"status":"error","message":"not found"}`)
		case "GET /api/v1/settings/general":
			io.WriteString(w, `{"status":"success","data":{"app.root_url":"https://desk.test/"}}`)
		default:
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
	})
	ctx := context.Background()
	id, uuid, err := c.CreateLiveChat(ctx, lc, "es-ES")
	if err != nil || id != 9 || uuid != "u-9" {
		t.Fatalf("CreateLiveChat = %d %q %v", id, uuid, err)
	}
	cfg := created["config"].(map[string]any)
	dark := cfg["branding"].(map[string]any)["dark"].(map[string]any)
	fields := cfg["prechat_form"].(map[string]any)["fields"].([]any)
	if len(fields) != 3 || fields[0].(map[string]any)["label"] != "Nombre" || fields[1].(map[string]any)["key"] != "email" || fields[1].(map[string]any)["required"] != false ||
		cfg["menu"].(map[string]any)["enabled"] != true || cfg["offline"].(map[string]any)["mode"] != "wait" || cfg["hide_agent_names"] != true ||
		cfg["inactivity_close"] != "30m" || cfg["team_id"] != nil || cfg["visitors"].(map[string]any)["prevent_reply_to_closed_conversation"] != true {
		t.Fatalf("create flow: %v", cfg)
	}
	if created["channel"] != "livechat" || cfg["language"] != "es-ES" || cfg["introduction_message"] != "¿Cómo podemos ayudarte?" || cfg["theme"] != "light" ||
		cfg["branding"].(map[string]any)["light"].(map[string]any)["launcher"].(map[string]any)["logo_url"] != "https://shop.test/i.png" ||
		cfg["launcher"].(map[string]any)["position"] != "left" || dark["colors"].(map[string]any)["primary"] != "#112233" ||
		!reflect.DeepEqual(cfg["trusted_domains"], []any{"*.shop.test"}) {
		t.Fatalf("create body: %v", created)
	}
	lc.Theme = "dark"
	if err := c.UpdateLiveChat(ctx, 9, lc); err != nil {
		t.Fatal(err)
	}
	cfg = updated["config"].(map[string]any)
	launcher := cfg["launcher"].(map[string]any)
	if updated["name"] != "Ventas" || cfg["theme"] != "dark" ||
		cfg["branding"].(map[string]any)["dark"].(map[string]any)["launcher"].(map[string]any)["logo_url"] != "https://shop.test/i.png" || updated["secret"] != "••••" || launcher["position"] != "left" || launcher["icon_scale"] != 80.0 ||
		cfg["home_apps"] == nil || cfg["greeting_message"] != "Hola" || cfg["trusted_domains"] != nil {
		t.Fatalf("update body: %v", updated)
	}
	if cfg["menu"] != nil || cfg["prechat_form"] != nil {
		t.Fatalf("nil flow settings changed: %v", cfg)
	}
	got, err := c.GetLiveChat(ctx, 9)
	if err != nil || got.Name != "Old" || got.Position != "right" || got.Color != "#000000" || got.Domains == nil ||
		got.Menu.Enabled || got.Menu.Items == nil || got.Offline.Mode != "" || *got.HideAgentNames || *got.InactivityClose != 0 || *got.TeamID != 0 ||
		got.PreChat.Enabled || !got.PreChat.Email.Shown {
		t.Fatalf("GetLiveChat = %+v %v", got, err)
	}
	if _, err := c.GetLiveChat(ctx, 8); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	if root, err := c.RootURL(ctx); err != nil || root != "https://desk.test" {
		t.Fatalf("RootURL = %q %v", root, err)
	}
}

func TestLiveChatFlow(t *testing.T) {
	var updated, attribute, team map[string]any
	c := newTest(t, func(w http.ResponseWriter, r *http.Request) {
		checkAuth(t, r)
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/inboxes/9":
			io.WriteString(w, `{"status":"success","data":{"id":9,"name":"Chat","channel":"livechat","config":{"language":"en-US","menu":{"enabled":true,"items":[{"label":"A","answer":"B"}]},"inactivity_close":"2h","team_id":3,"hide_agent_names":true,"offline":{"mode":"both","email":"a@b.test"},
				"prechat_form":{"enabled":true,"title":"Hi","fields":[{"key":"company","custom_attribute_id":5},{"key":"email","enabled":true,"required":true},{"key":"name","enabled":true,"required":true}],"visitors":{"enabled":true,"fields":[{"key":"phone","enabled":false}]}}}}}`)
		case "PUT /api/v1/inboxes/9":
			json.NewDecoder(r.Body).Decode(&updated)
			io.WriteString(w, `{"status":"success","data":{}}`)
		case "GET /api/v1/custom-attributes":
			if r.URL.Query().Get("applies_to") != "contact" {
				t.Errorf("custom attributes query: %s", r.URL.RawQuery)
			}
			io.WriteString(w, `{"status":"success","data":[{"id":5,"key":"company"}]}`)
		case "POST /api/v1/custom-attributes":
			json.NewDecoder(r.Body).Decode(&attribute)
			io.WriteString(w, `{"status":"success","data":{"id":6}}`)
		case "GET /api/v1/teams":
			io.WriteString(w, `{"status":"success","data":[{"id":3,"name":"Ventas","conversation_assignment_type":"Manual"},{"id":4,"name":"Soporte","conversation_assignment_type":"Round robin"}]}`)
		case "GET /api/v1/agents":
			io.WriteString(w, `{"status":"success","data":[{"id":1,"enabled":true},{"id":2,"enabled":false}]}`)
		case "GET /api/v1/agents/1":
			io.WriteString(w, `{"status":"success","data":{"id":1,"teams":[{"id":3}]}}`)
		case "POST /api/v1/teams":
			json.NewDecoder(r.Body).Decode(&team)
			io.WriteString(w, `{"status":"success","data":{"id":7,"name":"Chat Web"}}`)
		default:
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
	})
	ctx := context.Background()
	got, err := c.GetLiveChat(ctx, 9)
	if err != nil || !got.Menu.Enabled || got.Menu.Items[0].Answer != "B" || *got.InactivityClose != 120 || *got.TeamID != 3 || !*got.HideAgentNames ||
		got.Offline.Email != "a@b.test" || !got.PreChat.Enabled || got.PreChat.Phone.Shown || !got.PreChat.Email.Shown {
		t.Fatalf("GetLiveChat = %+v %v", got, err)
	}
	off := 0
	got.PreChat = &PreChatForm{Enabled: true, LastName: FormField{Shown: true}, Email: FormField{Shown: true, Required: true}}
	got.Offline, got.InactivityClose = &Offline{}, &off
	if err := c.UpdateLiveChat(ctx, 9, got); err != nil {
		t.Fatal(err)
	}
	if attribute["key"] != "last_name" || attribute["applies_to"] != "contact" || attribute["name"] != "Last name" {
		t.Fatalf("attribute: %v", attribute)
	}
	cfg := updated["config"].(map[string]any)
	form := cfg["prechat_form"].(map[string]any)
	keys := func(fields any) (out []string) {
		for _, f := range fields.([]any) {
			out = append(out, f.(map[string]any)["key"].(string))
		}
		return out
	}
	visitors := form["visitors"].(map[string]any)["fields"].([]any)
	last := form["fields"].([]any)[1].(map[string]any)
	if form["title"] != "Hi" || !reflect.DeepEqual(keys(form["fields"]), []string{"name", "last_name", "email", "phone", "company"}) ||
		!reflect.DeepEqual(keys(visitors), []string{"name", "last_name", "email", "phone"}) || last["custom_attribute_id"] != 6.0 || last["order"] != 2.0 ||
		visitors[2].(map[string]any)["required"] != true || cfg["offline"] != nil || cfg["inactivity_close"] != "" || cfg["menu"].(map[string]any)["items"] == nil {
		t.Fatalf("update flow: %v", cfg)
	}

	teams, err := c.ListTeams(ctx)
	if err != nil || len(teams) != 2 || teams[0].Members != 1 || teams[1].Members != 0 || teams[0].AssignmentType != "Manual" {
		t.Fatalf("ListTeams = %+v %v", teams, err)
	}
	if id, err := c.CreateTeam(ctx, "Chat Web"); err != nil || id != 7 || team["name"] != "Chat Web" || team["conversation_assignment_type"] != "Round robin" ||
		team["business_hours_id"] != nil || team["max_auto_assigned_conversations"] != 0.0 {
		t.Fatalf("CreateTeam = %d %v %v", id, team, err)
	}
}
