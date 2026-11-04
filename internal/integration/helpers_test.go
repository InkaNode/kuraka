//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/smtp"
	"net/textproto"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/InkaNode/kuraka/internal/api"
	"github.com/InkaNode/kuraka/internal/config"
	"github.com/InkaNode/kuraka/internal/stalwart"
	"github.com/InkaNode/kuraka/internal/store"
)

const publicURL = "http://kuraka.test"

func randomHex(t *testing.T, size int) string {
	t.Helper()
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}

func realStalwart(t *testing.T) *stalwart.Client {
	t.Helper()
	url := os.Getenv("KURAKA_IT_STALWART_URL")
	if url == "" {
		t.Skip("set KURAKA_IT_STALWART_URL to run against real Stalwart")
	}
	key := os.Getenv("KURAKA_IT_STALWART_API_KEY")
	if key == "" {
		t.Fatal("KURAKA_IT_STALWART_API_KEY is required when the Stalwart URL is set")
	}
	sw := stalwart.New(url, key)
	if err := sw.Ping(t.Context()); err != nil {
		t.Fatalf("Stalwart readiness: %v", err)
	}
	return sw
}

func cleanupAccount(t *testing.T, sw *stalwart.Client, id string) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := sw.DeleteAccount(ctx, id); err != nil && !errors.Is(err, stalwart.ErrNotFound) {
			t.Errorf("clean up account %s: %v", id, err)
		}
	})
}

func waitForDkim(t *testing.T, sw *stalwart.Client, domainID string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	var lastErr error
	for {
		keys, err := sw.ListDkim(ctx, domainID)
		lastErr = err
		if err == nil && len(keys) > 0 {
			for _, key := range keys {
				if key.DomainID != domainID || key.Selector == "" || key.PublicKey == "" {
					t.Fatal("DKIM response has missing or mismatched key fields")
				}
			}
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("no DKIM keys within 10 seconds (last error: %v)", lastErr)
		case <-ticker.C:
		}
	}
}

type apiClient struct {
	t      *testing.T
	base   string
	client *http.Client
}

func newAPIClient(t *testing.T, base string) *apiClient {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &apiClient{t: t, base: base, client: &http.Client{Jar: jar, Timeout: 30 * time.Second}}
}

func newAPI(t *testing.T, sw *stalwart.Client) *apiClient {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "kuraka.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	})
	cfg := config.Config{PublicURL: publicURL, InsecureCookies: true, SecretKeyHex: randomHex(t, 32)}
	s, err := api.New(cfg, st, sw, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(s.Handler())
	t.Cleanup(httpServer.Close)
	root := newAPIClient(t, httpServer.URL)
	password := randomHex(t, 16)
	email := "root-" + randomHex(t, 6) + "@ci.test"
	token := s.SetupToken()
	if token == "" {
		t.Fatal("fresh API has no setup token")
	}
	root.do("POST", "/setup", map[string]any{"token": token, "email": email, "name": "CI Root", "password": password}, 201)
	if s.SetupToken() != "" {
		t.Fatal("setup token remains available after setup")
	}
	root.login(email, password)
	return root
}

func (c *apiClient) do(method, path string, body any, wantStatus int) json.RawMessage {
	c.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			c.t.Fatal(err)
		}
	}
	req, err := http.NewRequestWithContext(c.t.Context(), method, c.base+"/api/v1"+path, &buf)
	if err != nil {
		c.t.Fatal(err)
	}
	req.Header.Set("Origin", publicURL)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		c.t.Fatal(err)
	}
	if resp.StatusCode != wantStatus {
		// Successful create responses can contain passwords; keep bodies out of logs.
		c.t.Fatalf("%s %s: status %d, want %d", method, path, resp.StatusCode, wantStatus)
	}
	return data
}

func (c *apiClient) login(email, password string) {
	c.t.Helper()
	c.do("POST", "/auth/login", map[string]string{"email": email, "password": password}, 200)
	c.do("GET", "/auth/me", nil, 200)
}

func decode[T any](t *testing.T, data []byte) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

type resource struct {
	ID                int64  `json:"id"`
	GeneratedPassword string `json:"generated_password"`
}

type mailbox struct {
	ID      string           `json:"id"`
	Aliases []map[string]any `json:"aliases"`
}

func domainPath(id int64) string { return fmt.Sprintf("/domains/%d", id) }

func assertErrorCode(t *testing.T, data []byte, want string) {
	t.Helper()
	type errorEnvelope struct {
		Error struct{ Code string } `json:"error"`
	}
	envelope := decode[errorEnvelope](t, data)
	if envelope.Error.Code != want {
		t.Fatalf("error code = %q, want %q", envelope.Error.Code, want)
	}
}

type auditEntry struct {
	UserID   int64  `json:"user_id"`
	TenantID int64  `json:"tenant_id"`
	Action   string `json:"action"`
	Target   string `json:"target"`
	Result   string `json:"result"`
}

func assertRecipient(t *testing.T, addr, recipient string, accepted bool) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	c, err := smtp.NewClient(conn, "mail.ci.test")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.Hello("kuraka.test"); err != nil {
		t.Fatalf("SMTP greeting: %v", err)
	}
	// A null reverse path avoids depending on public sender DNS in this local test.
	if err := c.Mail(""); err != nil {
		t.Fatalf("SMTP MAIL FROM: %v", err)
	}
	err = c.Rcpt(recipient)
	if accepted {
		if err != nil {
			t.Fatalf("RCPT %s should be accepted: %v", recipient, err)
		}
		return
	}
	var smtpErr *textproto.Error
	if !errors.As(err, &smtpErr) || smtpErr.Code < 500 || smtpErr.Code >= 600 {
		t.Fatalf("RCPT %s should receive a permanent rejection, got %v", recipient, err)
	}
}
