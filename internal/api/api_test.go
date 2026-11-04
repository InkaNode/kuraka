package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/InkaNode/kuraka/internal/auth"
	"github.com/InkaNode/kuraka/internal/authz"
	"github.com/InkaNode/kuraka/internal/stalwart"
	"github.com/InkaNode/kuraka/internal/store"
)

func TestSetupAndLogin(t *testing.T) {
	h := newHarness(t, false)
	token := h.s.SetupToken()
	if token == "" {
		t.Fatal("missing setup token")
	}
	payload := map[string]any{"token": "wrong", "email": "root@example.test", "name": "Root", "password": rootPassword}
	h.root.do("POST", "/setup", payload, 403)
	payload["token"] = token
	h.root.do("POST", "/setup", payload, 201)
	if h.s.SetupToken() != "" {
		t.Fatal("setup token retained")
	}
	h.root.do("POST", "/setup", payload, 409)
	good := object(t, h.root.login("ROOT@example.test", rootPassword, 200))
	if good["mfa_required"] != false {
		t.Fatal(good)
	}
	h.root.do("GET", "/auth/me", nil, 200)
	wrong := h.newClient().login("root@example.test", "bad password", 401)
	unknown := h.newClient().login("missing@example.test", "bad password", 401)
	if !bytes.Equal(wrong, unknown) {
		t.Fatalf("credential enumeration: %s / %s", wrong, unknown)
	}
	assertCode(t, wrong, "invalid_credentials")
	h.root.do("POST", "/auth/logout", nil, 204)
	h.root.do("GET", "/auth/me", nil, 401)
	entries, err := h.st.ListAudit(context.Background(), nil, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	logins := 0
	for _, a := range entries {
		if a.Action == "auth.login" {
			logins++
		}
	}
	if logins != 3 {
		t.Fatalf("login audits: %d", logins)
	}
}

func TestLoginLimiterAndDisabledUser(t *testing.T) {
	h := newHarness(t, true)
	now := time.Now()
	h.s.now = func() time.Time { return now }
	c := h.newClient()
	for i := 0; i < 4; i++ {
		c.login("missing@example.test", "bad", 401)
	}
	c.login("MISSING@example.test", "bad", 429)
	now = now.Add(14 * time.Minute)
	c.login("missing@example.test", "bad", 429)
	now = now.Add(2 * time.Minute)
	c.login("missing@example.test", "bad", 401)
	// A different email has its own bucket.
	c.login("different@example.test", "bad", 401)
	tenant := h.tenant("disabled")
	user := h.admin(tenant, "disabled@example.test")
	u, err := h.st.GetUserByEmail(context.Background(), "disabled@example.test")
	if err != nil {
		t.Fatal(err)
	}
	disabled := true
	if err := h.st.UpdateUser(context.Background(), u.ID, store.UserPatch{Disabled: &disabled}); err != nil {
		t.Fatal(err)
	}
	user.do("GET", "/domains", nil, 401)
	unknown := c.login("another@example.test", "bad", 401)
	response := c.login(u.Email, "bad", 401)
	if !bytes.Equal(unknown, response) {
		t.Fatal("disabled user response differs")
	}
}

func TestTOTPFlow(t *testing.T) {
	h := newHarness(t, true)
	now := time.Now().UTC()
	h.s.now = func() time.Time { return now }
	result := object(t, h.root.do("POST", "/auth/totp/enroll", map[string]any{}, 200))
	secret := result["secret"].(string)
	if !strings.Contains(result["otpauth_url"].(string), "issuer=Kuraka") {
		t.Fatal(result)
	}
	u, err := h.st.GetUserByEmail(context.Background(), "root@example.test")
	if err != nil {
		t.Fatal(err)
	}
	if u.TOTPEnabled || strings.Contains(u.TOTPSecretEnc, secret) {
		t.Fatal("unencrypted or prematurely enabled TOTP")
	}
	code, err := auth.TOTPCode(secret, now)
	if err != nil {
		t.Fatal(err)
	}
	h.root.do("POST", "/auth/totp/confirm", map[string]string{"code": code}, 200)
	h.root.do("POST", "/auth/totp/enroll", nil, 409)
	c := h.newClient()
	login := object(t, c.login(u.Email, rootPassword, 200))
	if login["mfa_required"] != true || login["user"] != nil {
		t.Fatal(login)
	}
	c.do("GET", "/domains", nil, 401)
	c.do("GET", "/auth/me", nil, 401)
	c.do("GET", "/healthz", nil, 401)
	c.login(u.Email, rootPassword, 401)
	c.do("POST", "/auth/totp", map[string]string{"code": "abcdef"}, 401)
	c.do("POST", "/auth/totp", map[string]string{"code": code}, 200)
	c.do("GET", "/domains", nil, 200)
	pending := h.newClient()
	pending.login(u.Email, rootPassword, 200)
	pending.do("POST", "/auth/logout", nil, 204)
	h.root.do("POST", "/auth/totp/disable", map[string]string{"password": rootPassword, "code": code}, 200)
	c.do("GET", "/auth/me", nil, 401)
	u, err = h.st.GetUser(context.Background(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if u.TOTPEnabled || u.TOTPSecretEnc != "" {
		t.Fatal("TOTP not cleared")
	}
	h.newClient().login(u.Email, rootPassword, 200)
}

func TestTOTPLimiter(t *testing.T) {
	h := newHarness(t, true)
	result := object(t, h.root.do("POST", "/auth/totp/enroll", nil, 200))
	secret := result["secret"].(string)
	code, err := auth.TOTPCode(secret, h.s.now())
	if err != nil {
		t.Fatal(err)
	}
	h.root.do("POST", "/auth/totp/confirm", map[string]string{"code": code}, 200)
	c := h.newClient()
	c.login("root@example.test", rootPassword, 200)
	for i := 0; i < 4; i++ {
		c.do("POST", "/auth/totp", map[string]string{"code": "bad"}, 401)
	}
	c.do("POST", "/auth/totp", map[string]string{"code": "bad"}, 429)
	c.do("POST", "/auth/totp", map[string]string{"code": code}, 429)
}

func TestCSRFAndStrictJSON(t *testing.T) {
	h := newHarness(t, true)
	for _, tc := range []struct{ origin, contentType string }{
		{"", "application/json"},
		{"http://evil.test", "application/json"},
		{"http://kuraka.test", "text/plain"},
		{"http://kuraka.test.evil", "application/json"},
	} {
		status, b, headers := h.root.raw("POST", "/auth/login", `{"email":"root@example.test","password":"bad"}`, tc.origin, tc.contentType)
		if status != 403 {
			t.Fatalf("CSRF accepted: %+v: %d", tc, status)
		}
		assertCode(t, b, "csrf")
		for key, value := range map[string]string{"X-Content-Type-Options": "nosniff", "X-Frame-Options": "DENY", "Referrer-Policy": "same-origin", "Cache-Control": "no-store"} {
			if headers.Get(key) != value {
				t.Fatalf("missing security header %s", key)
			}
		}
	}
	// Referer fallback requires the slash boundary after the exact origin.
	for _, tc := range []struct {
		referer string
		status  int
	}{
		{"http://kuraka.test/login", 200},
		{"http://kuraka.test.evil/login", 403},
	} {
		req, err := http.NewRequest("POST", h.http.URL+apiPath("/auth/login"), strings.NewReader(fmt.Sprintf(`{"email":"root@example.test","password":%q}`, rootPassword)))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Referer", tc.referer)
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		resp, err := h.root.client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != tc.status {
			t.Fatalf("referer %s: %d", tc.referer, resp.StatusCode)
		}
	}
	status, b, _ := h.root.raw("POST", "/tenants", `{"name":"x","unknown":true}`, "http://kuraka.test", "application/json")
	if status != 422 {
		t.Fatal(status)
	}
	assertCode(t, b, "invalid_input")
	status, _, _ = h.root.raw("POST", "/tenants", `{"name":"x"} {}`, "http://kuraka.test", "application/json")
	if status != 422 {
		t.Fatal(status)
	}
	status, _, _ = h.root.raw("POST", "/tenants", `{"name":"`+strings.Repeat("a", 1<<20)+`"}`, "http://kuraka.test", "application/json")
	if status != 422 {
		t.Fatalf("oversized body: %d", status)
	}
}

func TestAdminDomainsAndImport(t *testing.T) {
	h := newHarness(t, true)
	tenant := h.tenant("A")
	c := h.admin(tenant, "a@example.test")
	c.do("GET", "/tenants", nil, 403)
	c.do("POST", "/tenants", map[string]string{"name": "forbidden"}, 403)
	h.root.do("PATCH", fmt.Sprintf("/tenants/%d", tenant), map[string]string{"name": "renamed"}, 200)
	h.root.do("GET", "/users?tenant_id="+fmt.Sprint(tenant), nil, 200)
	path := fmt.Sprintf("/tenants/%d/domains", tenant)
	result := object(t, h.root.do("POST", path, map[string]any{"name": "EXAMPLE.TEST", "aliases": []string{"ALIAS.TEST"}}, 201))
	id := int64(result["id"].(float64))
	if result["name"] != "example.test" {
		t.Fatal(result)
	}
	h.root.do("POST", path, map[string]any{"name": "example.test"}, 409)
	h.root.do("POST", path, map[string]any{"name": "other.test", "aliases": []string{"alias.test"}}, 409)
	h.root.do("POST", path, map[string]any{"name": "bad_name.test"}, 422)
	domainPath := fmt.Sprintf("/domains/%d", id)
	details := object(t, c.do("GET", domainPath, nil, 200))
	if len(details["dns_records"].([]any)) != 2 || len(details["dkim"].([]any)) != 1 {
		t.Fatal(details)
	}
	aliasRecords := details["alias_dns_records"].(map[string]any)["alias.test"].([]any)
	if aliasRecords[0].(map[string]any)["name"] != "alias.test" {
		t.Fatal(aliasRecords)
	}
	c.do("PUT", domainPath+"/aliases", map[string]any{"aliases": []string{"alias.test", "new-alias.test"}}, 200)
	c.do("PUT", domainPath+"/aliases", map[string]any{}, 422)
	_, err := h.sw.CreateDomain(context.Background(), "import.test", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	h.root.do("POST", path, map[string]string{"name": "import.test"}, 409)
	imported := object(t, h.root.do("POST", path+"/import", map[string]string{"name": "import.test"}, 201))
	h.root.do("POST", path+"/import", map[string]string{"name": "import.test"}, 409)
	h.root.do("DELETE", fmt.Sprintf("/tenants/%d", tenant), nil, 409)
	mutations := h.sw.mutationCount()
	h.root.do("DELETE", fmt.Sprintf("/domains/%.0f", imported["id"]), nil, 204)
	if h.sw.mutationCount() != mutations {
		t.Fatal("unlink changed Stalwart")
	}
	if _, err := h.sw.FindDomainByName(context.Background(), "import.test"); err != nil {
		t.Fatal(err)
	}
	empty := h.tenant("empty")
	h.root.do("DELETE", fmt.Sprintf("/tenants/%d", empty), nil, 204)
	usersOnly := h.tenant("users-only")
	h.admin(usersOnly, "user-only@example.test")
	h.root.do("DELETE", fmt.Sprintf("/tenants/%d", usersOnly), nil, 409)
}

func TestCrossTenantIsolationAndAudit(t *testing.T) {
	h := newHarness(t, true)
	a, b := h.tenant("A"), h.tenant("B")
	da, db := h.domain(a, "a.test"), h.domain(b, "b.test")
	ca := h.admin(a, "a@example.test")
	cb := h.admin(b, "b@example.test")
	pa, pb := fmt.Sprintf("/domains/%d", da), fmt.Sprintf("/domains/%d", db)
	ma := object(t, ca.do("POST", pa+"/mailboxes", map[string]any{"name": "mine"}, 201))["id"].(string)
	mb := object(t, cb.do("POST", pb+"/mailboxes", map[string]any{"name": "theirs"}, 201))["id"].(string)
	before := h.sw.mutationCount()
	attempts := 0
	ca.do("GET", pb, nil, 404)
	attempts++
	ca.do("PUT", pb+"/aliases", map[string]any{"aliases": []string{"stolen.test"}}, 404)
	attempts++
	for _, path := range []string{pb + "/mailboxes/" + mb, pa + "/mailboxes/" + mb} {
		ca.do("GET", path, nil, 404)
		ca.do("PATCH", path, map[string]any{"display_name": "pwned"}, 404)
		ca.do("DELETE", path, nil, 404)
		attempts += 3
	}
	foreign := map[string]any{"aliases": []map[string]any{{"name": "ceo", "domain_id": db}}}
	assertCode(t, ca.do("PATCH", pa+"/mailboxes/"+ma, foreign, 422), "foreign_alias")
	attempts++
	foreign["name"] = "another"
	assertCode(t, ca.do("POST", pa+"/mailboxes", foreign, 422), "foreign_alias")
	attempts++
	// Even a superadmin cannot attach aliases from a different tenant.
	delete(foreign, "name")
	assertCode(t, h.root.do("PATCH", pa+"/mailboxes/"+ma, foreign, 422), "foreign_alias")
	if h.sw.mutationCount() != before {
		t.Fatal("denied request mutated upstream")
	}
	rows := array(t, ca.do("GET", pa+"/mailboxes", nil, 200))
	if len(rows) != 1 || rows[0]["id"] != ma {
		t.Fatal(rows)
	}
	listed := array(t, ca.do("GET", "/domains", nil, 200))
	if len(listed) != 1 || int64(listed[0]["id"].(float64)) != da {
		t.Fatal(listed)
	}
	entries, err := h.st.ListAudit(context.Background(), &a, 0, 200)
	if err != nil {
		t.Fatal(err)
	}
	user, err := h.st.GetUserByEmail(context.Background(), "a@example.test")
	if err != nil {
		t.Fatal(err)
	}
	denied := 0
	for _, entry := range entries {
		if entry.UserID != nil && *entry.UserID == user.ID && entry.Result == "denied" {
			denied++
		}
	}
	if denied != attempts {
		t.Fatalf("denied audits = %d, want %d", denied, attempts)
	}
	for _, entry := range array(t, ca.do("GET", "/audit?limit=200", nil, 200)) {
		if int64(entry["tenant_id"].(float64)) != a {
			t.Fatal("foreign audit exposed")
		}
	}
}

func TestMailboxLifecycleAndPasswords(t *testing.T) {
	h := newHarness(t, true)
	tenant := h.tenant("A")
	domain := h.domain(tenant, "a.test")
	aliasDomain := h.domain(tenant, "alias.test")
	c := h.admin(tenant, "a@example.test")
	path := fmt.Sprintf("/domains/%d/mailboxes", domain)
	created := object(t, c.do("POST", path, map[string]any{"name": "UPPER", "display_name": "Person", "quota_mb": 42, "aliases": []map[string]any{{"name": "second", "domain_id": aliasDomain}}}, 201))
	generated := created["generated_password"].(string)
	if len(generated) != 20 || auth.ValidatePassword(generated) != nil {
		t.Fatal("invalid generated mailbox password")
	}
	if created["name"] != "upper" || created["quota_mb"] != float64(42) {
		t.Fatal(created)
	}
	aliases := created["aliases"].([]any)
	if aliases[0].(map[string]any)["domain_id"] != float64(aliasDomain) || aliases[0].(map[string]any)["email"] != "second@alias.test" {
		t.Fatal(aliases)
	}
	id := created["id"].(string)
	read := object(t, c.do("GET", path+"/"+id, nil, 200))
	if _, ok := read["generated_password"]; ok {
		t.Fatal("password returned again")
	}
	c.do("PATCH", path+"/"+id, map[string]any{"display_name": "New", "quota_mb": 0, "aliases": []any{}, "password": rootPassword}, 200)
	reset := object(t, c.do("PATCH", path+"/"+id, map[string]any{"password": ""}, 200))["generated_password"].(string)
	if len(reset) != 20 || auth.ValidatePassword(reset) != nil {
		t.Fatal("invalid generated mailbox reset password")
	}
	if object(t, c.do("GET", path+"/"+id, nil, 200))["generated_password"] != nil {
		t.Fatal("reset password returned again")
	}
	c.do("PATCH", path+"/"+id, map[string]any{"password": "short"}, 422)
	c.do("POST", path, map[string]any{"name": "bad name"}, 422)
	c.do("POST", path, map[string]any{"name": "bad", "password": "short"}, 422)
	c.do("POST", path, map[string]any{"name": "overflow", "quota_mb": uint64(^uint64(0))}, 422)
	shared := object(t, c.do("POST", path, map[string]any{"name": "shared", "shared": true}, 201))
	if shared["kind"] != "shared" || shared["generated_password"] != nil {
		t.Fatal(shared)
	}
	c.do("DELETE", path+"/"+id, nil, 204)
	c.do("GET", path+"/"+id, nil, 404)
}

func TestUsersLastSuperadminAndGeneratedPasswords(t *testing.T) {
	h := newHarness(t, true)
	tenant := h.tenant("A")
	root, err := h.st.GetUserByEmail(context.Background(), "root@example.test")
	if err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/users/%d", root.ID)
	h.root.do("PATCH", path, map[string]any{"disabled": true}, 409)
	h.root.do("PATCH", path, map[string]any{"role": "tenant_admin", "tenant_id": tenant}, 409)
	for _, body := range []map[string]any{
		{"email": "bad@example.test", "name": "Bad", "role": "superadmin", "tenant_id": tenant},
		{"email": "bad@example.test", "name": "Bad", "role": "tenant_admin"},
		{"email": "bad@example.test", "name": "Bad", "role": "unknown"},
	} {
		h.root.do("POST", "/users", body, 422)
	}
	created := object(t, h.root.do("POST", "/users", map[string]any{"email": "generated@example.test", "name": "Generated", "role": "tenant_admin", "tenant_id": tenant}, 201))
	generated := created["generated_password"].(string)
	if len(generated) != 20 || auth.ValidatePassword(generated) != nil {
		t.Fatal("invalid generated user password")
	}
	c := h.newClient()
	c.login("generated@example.test", generated, 200)
	for _, u := range array(t, h.root.do("GET", "/users", nil, 200)) {
		if u["generated_password"] != nil || u["password_hash"] != nil || u["totp_secret_enc"] != nil {
			t.Fatal("user secrets leaked")
		}
	}
	userPath := fmt.Sprintf("/users/%.0f", created["id"])
	reset := object(t, h.root.do("PATCH", userPath, map[string]any{"reset_password": true}, 200))["generated_password"].(string)
	if reset == generated || auth.ValidatePassword(reset) != nil {
		t.Fatal("invalid reset password")
	}
	c.do("GET", "/auth/me", nil, 401)
	c.login("generated@example.test", reset, 200)
	h.root.do("PATCH", userPath, map[string]any{"role": "superadmin", "tenant_id": nil}, 200)
	c.do("GET", "/auth/me", nil, 401)
	h.root.do("PATCH", path, map[string]any{"disabled": true}, 200)
	h.root.do("GET", "/auth/me", nil, 401)
	c.login("generated@example.test", reset, 200)
	c.do("PATCH", userPath, map[string]any{"disabled": true}, 409)
}

func TestPasswordRotationAndSessionStorage(t *testing.T) {
	h := newHarness(t, true)
	other := h.newClient()
	other.login("root@example.test", rootPassword, 200)
	req, err := http.NewRequest("GET", h.http.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	cookies := h.root.client.Jar.Cookies(req.URL)
	if len(cookies) != 1 {
		t.Fatal(cookies)
	}
	token := cookies[0].Value
	if _, err := h.st.GetSession(context.Background(), token, h.s.now()); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("raw session token stored")
	}
	session, err := h.st.GetSession(context.Background(), auth.HashToken(token), h.s.now())
	if err != nil {
		t.Fatal(err)
	}
	if session.ExpiresAt.Sub(session.CreatedAt) != 12*time.Hour {
		t.Fatal("wrong session lifetime")
	}
	h.root.do("POST", "/auth/password", map[string]string{"current": "wrong", "new": rootPassword}, 401)
	h.root.do("POST", "/auth/password", map[string]string{"current": rootPassword, "new": "short"}, 422)
	next := "a different password 456!"
	h.root.do("POST", "/auth/password", map[string]string{"current": rootPassword, "new": next}, 200)
	other.do("GET", "/auth/me", nil, 401)
	h.root.do("GET", "/auth/me", nil, 200)
	h.newClient().login("root@example.test", rootPassword, 401)
	h.newClient().login("root@example.test", next, 200)
}

func TestHealthAndUpstreamErrors(t *testing.T) {
	h := newHarness(t, true)
	tenant := h.tenant("A")
	domain := h.domain(tenant, "a.test")
	h.newClient().do("GET", "/healthz", nil, 200)
	h.sw.setFailure(errors.New("sensitive upstream failure"))
	status := object(t, h.newClient().do("GET", "/healthz", nil, 503))
	if status["stalwart"] != "unreachable" {
		t.Fatal(status)
	}
	b := h.root.do("GET", fmt.Sprintf("/domains/%d", domain), nil, 502)
	assertCode(t, b, "upstream_error")
	if bytes.Contains(b, []byte("sensitive")) {
		t.Fatal("upstream details leaked")
	}
}

func TestDomainLinkFailureIsAuditedAsError(t *testing.T) {
	h := newHarness(t, true)
	tenant := h.tenant("A")
	var sid string
	h.sw.afterDomainCreate = func(name, id string) error {
		sid = id
		_, err := h.st.CreateDomain(context.Background(), tenant, name, "concurrent-id")
		return err
	}
	h.root.do("POST", fmt.Sprintf("/tenants/%d/domains", tenant), map[string]any{"name": "orphan.test"}, 409)
	entries, err := h.st.ListAudit(context.Background(), nil, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Action != "domain.create" || entries[0].Result != "error" || entries[0].Target != sid || !strings.Contains(entries[0].Detail, sid) {
		t.Fatal(entries)
	}
	remote, err := h.sw.GetDomain(context.Background(), sid)
	if err != nil || remote.Name != "orphan.test" {
		t.Fatalf("created upstream domain lost: %+v, %v", remote, err)
	}
}

func TestClientIPAndCookieFlags(t *testing.T) {
	for _, tc := range []struct{ remote, forwarded, want string }{
		{"192.168.1.2:99", "203.0.113.4, 198.51.100.7", "198.51.100.7"},
		{"[::1]:99", "198.51.100.7", "198.51.100.7"},
		{"8.8.8.8:99", "198.51.100.7", "8.8.8.8"},
		{"127.0.0.1:99", "", "127.0.0.1"},
	} {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = tc.remote
		req.Header.Set("X-Forwarded-For", tc.forwarded)
		if got := clientIP(req); got != tc.want {
			t.Fatalf("IP = %s, want %s", got, tc.want)
		}
	}
	h := newHarness(t, true)
	w := httptest.NewRecorder()
	h.s.cfg.InsecureCookies = false
	h.s.cookie(w, "test", time.Now().Add(12*time.Hour), 43200)
	cookie := w.Result().Cookies()[0]
	if !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" || cookie.MaxAge != 43200 {
		t.Fatal(cookie)
	}
}

func TestConcurrentLastSuperadmin(t *testing.T) {
	h := newHarness(t, true)
	second := object(t, h.root.do("POST", "/users", map[string]any{"email": "second@example.test", "name": "Second", "role": "superadmin", "password": rootPassword}, 201))
	c := h.newClient()
	c.login("second@example.test", rootPassword, 200)
	root, err := h.st.GetUserByEmail(context.Background(), "root@example.test")
	if err != nil {
		t.Fatal(err)
	}
	statuses := make(chan int, 2)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for _, candidate := range []struct {
		client *testClient
		id     int64
	}{
		{h.root, root.ID},
		{c, int64(second["id"].(float64))},
	} {
		wg.Add(1)
		go func(client *testClient, id int64) {
			defer wg.Done()
			<-start
			code, _, _ := client.raw("PATCH", fmt.Sprintf("/users/%d", id), `{"disabled":true}`, "http://kuraka.test", "application/json")
			statuses <- code
		}(candidate.client, candidate.id)
	}
	close(start)
	wg.Wait()
	close(statuses)
	ok, conflicts := 0, 0
	for code := range statuses {
		if code == 200 {
			ok++
		}
		if code == 409 {
			conflicts++
		}
	}
	if ok != 1 || conflicts != 1 {
		t.Fatalf("concurrent disable outcomes: ok=%d conflicts=%d", ok, conflicts)
	}
	users, err := h.st.ListUsers(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	enabled := 0
	for _, u := range users {
		if u.Role == authz.RoleSuperadmin && !u.Disabled {
			enabled++
		}
	}
	if enabled != 1 {
		t.Fatal("last superadmin lost")
	}
}

func TestClassifyUpstreamForbidden(t *testing.T) {
	if status, code, _ := classify(upstream(&stalwart.SetError{Type: "forbidden"})); status != 403 || code != "upstream_forbidden" {
		t.Fatal(status, code)
	}
	if status, code, _ := classify(upstream(&stalwart.SetError{Type: "invalidProperties"})); status != 502 || code != "upstream_error" {
		t.Fatal(status, code)
	}
}
