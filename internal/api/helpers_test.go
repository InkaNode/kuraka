package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/InkaNode/kuraka/internal/config"
	"github.com/InkaNode/kuraka/internal/stalwart"
	"github.com/InkaNode/kuraka/internal/store"
)

type fakeStalwart struct {
	mu                sync.Mutex
	domains           map[string]stalwart.Domain
	accounts          map[string]stalwart.Account
	next              int
	mutations         int
	fail              error
	afterDomainCreate func(string, string) error
	appPasswords      map[string]string
	appDescriptions   []string
	appCreated        int
	appDeleted        int
	queued            map[string]stalwart.QueuedMessage
}

var _ Stalwart = (*fakeStalwart)(nil)

func (f *fakeStalwart) ListQueue(context.Context) ([]stalwart.QueuedMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]stalwart.QueuedMessage, 0, len(f.queued))
	for _, message := range f.queued {
		out = append(out, message)
	}
	return out, f.fail
}

func (f *fakeStalwart) GetQueued(_ context.Context, id string) (stalwart.QueuedMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return stalwart.QueuedMessage{}, f.fail
	}
	message, ok := f.queued[id]
	if !ok {
		return message, stalwart.ErrNotFound
	}
	return message, nil
}

func (f *fakeStalwart) RetryQueued(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	if _, ok := f.queued[id]; !ok {
		return stalwart.ErrNotFound
	}
	f.mutations++
	return nil
}

func (f *fakeStalwart) CancelQueued(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	if _, ok := f.queued[id]; !ok {
		return stalwart.ErrNotFound
	}
	delete(f.queued, id)
	f.mutations++
	return nil
}

func (f *fakeStalwart) CreateAppPassword(_ context.Context, accountID, desc string) (string, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return "", "", f.fail
	}
	if f.appPasswords == nil {
		f.appPasswords = map[string]string{}
	}
	f.appCreated++
	id := fmt.Sprintf("app%d", f.appCreated)
	f.appPasswords[accountID+"/"+id] = "app-secret-" + id
	f.appDescriptions = append(f.appDescriptions, desc)
	return id, "app-secret-" + id, nil
}
func (f *fakeStalwart) DeleteAppPassword(_ context.Context, accountID, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	delete(f.appPasswords, accountID+"/"+id)
	f.appDeleted++
	return nil
}

func (f *fakeStalwart) Ping(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.fail
}
func (f *fakeStalwart) GetDomain(_ context.Context, id string) (stalwart.Domain, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return stalwart.Domain{}, f.fail
	}
	d, ok := f.domains[id]
	if !ok {
		return d, stalwart.ErrNotFound
	}
	d.Aliases = append([]string{}, d.Aliases...)
	return d, nil
}
func (f *fakeStalwart) FindDomainByName(_ context.Context, name string) (stalwart.Domain, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return stalwart.Domain{}, f.fail
	}
	for _, d := range f.domains {
		if strings.EqualFold(d.Name, name) {
			d.Aliases = append([]string{}, d.Aliases...)
			return d, nil
		}
	}
	return stalwart.Domain{}, stalwart.ErrNotFound
}
func (f *fakeStalwart) CreateDomain(_ context.Context, name, description string, aliases []string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return "", f.fail
	}
	f.next++
	id := fmt.Sprintf("d%d", f.next)
	f.mutations++
	f.domains[id] = stalwart.Domain{ID: id, Name: name, Description: description, Aliases: append([]string{}, aliases...), ZoneFile: name + ". IN MX 10 mail.test.\n" + name + ". IN TXT \"v=spf1 -all\""}
	if f.afterDomainCreate != nil {
		if err := f.afterDomainCreate(name, id); err != nil {
			return "", err
		}
	}
	return id, nil
}
func (f *fakeStalwart) SetDomainAliases(_ context.Context, id string, aliases []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	d, ok := f.domains[id]
	if !ok {
		return stalwart.ErrNotFound
	}
	d.Aliases = append([]string{}, aliases...)
	f.domains[id] = d
	f.mutations++
	return nil
}
func (f *fakeStalwart) ListAccounts(_ context.Context, sid string) ([]stalwart.Account, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return nil, f.fail
	}
	out := []stalwart.Account{}
	for _, a := range f.accounts {
		if a.DomainID == sid {
			a.Aliases = append([]stalwart.Alias{}, a.Aliases...)
			out = append(out, a)
		}
	}
	return out, nil
}
func (f *fakeStalwart) GetAccount(_ context.Context, id string) (stalwart.Account, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return stalwart.Account{}, f.fail
	}
	a, ok := f.accounts[id]
	if !ok {
		return a, stalwart.ErrNotFound
	}
	a.Aliases = append([]stalwart.Alias{}, a.Aliases...)
	return a, nil
}
func (f *fakeStalwart) CreateAccount(_ context.Context, in stalwart.NewAccount) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return "", f.fail
	}
	f.next++
	id := fmt.Sprintf("a%d", f.next)
	f.mutations++
	f.accounts[id] = stalwart.Account{ID: id, Kind: in.Kind, Name: in.Name, DomainID: in.DomainID, Description: in.Description, QuotaBytes: in.QuotaBytes, Aliases: append([]stalwart.Alias{}, in.Aliases...)}
	return id, nil
}
func (f *fakeStalwart) UpdateAccount(_ context.Context, id string, p stalwart.AccountPatch) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	a, ok := f.accounts[id]
	if !ok {
		return stalwart.ErrNotFound
	}
	if p.Description != nil {
		a.Description = *p.Description
	}
	if p.QuotaBytes != nil {
		a.QuotaBytes = *p.QuotaBytes
	}
	if p.Aliases != nil {
		a.Aliases = append([]stalwart.Alias{}, (*p.Aliases)...)
	}
	f.accounts[id] = a
	f.mutations++
	return nil
}
func (f *fakeStalwart) DeleteAccount(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	if _, ok := f.accounts[id]; !ok {
		return stalwart.ErrNotFound
	}
	delete(f.accounts, id)
	f.mutations++
	return nil
}
func (f *fakeStalwart) ListDkim(_ context.Context, id string) ([]stalwart.DkimKey, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return nil, f.fail
	}
	return []stalwart.DkimKey{{DomainID: id, Selector: "s1", Algorithm: "RSA", Stage: "active"}}, nil
}
func (f *fakeStalwart) setFailure(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fail = err
}
func (f *fakeStalwart) mutationCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.mutations
}

type harness struct {
	t    *testing.T
	s    *Server
	st   *store.Store
	sw   *fakeStalwart
	http *httptest.Server
	root *testClient
}
type testClient struct {
	h      *harness
	client *http.Client
}

const rootPassword = "correct password 123!"

func newHarness(t *testing.T, setup bool) *harness {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/k.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	sw := &fakeStalwart{domains: map[string]stalwart.Domain{}, accounts: map[string]stalwart.Account{}}
	cfg := config.Config{PublicURL: "http://kuraka.test", InsecureCookies: true, SecretKeyHex: strings.Repeat("ab", 32)}
	s, err := New(cfg, st, sw, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{t: t, s: s, st: st, sw: sw}
	h.http = httptest.NewServer(s.Handler())
	t.Cleanup(h.http.Close)
	h.root = h.newClient()
	if setup {
		h.root.do("POST", "/setup", map[string]any{"token": s.SetupToken(), "email": "root@example.test", "name": "Root", "password": rootPassword}, 201)
		h.root.login("root@example.test", rootPassword, 200)
	}
	return h
}
func (h *harness) newClient() *testClient {
	jar, err := cookiejar.New(nil)
	if err != nil {
		h.t.Fatal(err)
	}
	return &testClient{h: h, client: &http.Client{Jar: jar}}
}
func apiPath(path string) string {
	if strings.HasPrefix(path, "/healthz") {
		return path
	}
	return "/api/v1" + path
}
func (c *testClient) raw(method, path, body, origin, contentType string) (int, []byte, http.Header) {
	c.h.t.Helper()
	req, err := http.NewRequest(method, c.h.http.URL+apiPath(path), strings.NewReader(body))
	if err != nil {
		c.h.t.Fatal(err)
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		c.h.t.Fatal(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		c.h.t.Fatal(err)
	}
	return resp.StatusCode, data, resp.Header
}
func (c *testClient) do(method, path string, body any, status int) json.RawMessage {
	c.h.t.Helper()
	text := ""
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			c.h.t.Fatal(err)
		}
		text = string(b)
	}
	got, b, _ := c.raw(method, path, text, "http://kuraka.test", "application/json")
	if got != status {
		c.h.t.Fatalf("%s %s: status %d, want %d: %s", method, path, got, status, b)
	}
	return b
}
func object(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	return v
}
func array(t *testing.T, b []byte) []map[string]any {
	t.Helper()
	var v []map[string]any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	return v
}
func (c *testClient) login(email, password string, status int) json.RawMessage {
	return c.do("POST", "/auth/login", map[string]string{"email": email, "password": password}, status)
}
func (h *harness) tenant(name string) int64 {
	h.t.Helper()
	return int64(object(h.t, h.root.do("POST", "/tenants", map[string]string{"name": name}, 201))["id"].(float64))
}
func (h *harness) domain(tenant int64, name string) int64 {
	h.t.Helper()
	return int64(object(h.t, h.root.do("POST", fmt.Sprintf("/tenants/%d/domains", tenant), map[string]any{"name": name, "aliases": []string{}}, 201))["id"].(float64))
}
func (h *harness) admin(tenant int64, email string) *testClient {
	h.t.Helper()
	h.root.do("POST", "/users", map[string]any{"email": email, "name": "Admin", "role": "tenant_admin", "tenant_id": tenant, "password": rootPassword}, 201)
	c := h.newClient()
	c.login(email, rootPassword, 200)
	return c
}
func assertCode(t *testing.T, b []byte, code string) {
	t.Helper()
	v := object(t, b)
	if v["error"].(map[string]any)["code"] != code {
		t.Fatalf("want code %s: %s", code, b)
	}
}
