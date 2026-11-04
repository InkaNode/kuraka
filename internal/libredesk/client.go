// Package libredesk is a typed client for the parts of the Libredesk HTTP API
// Kuraka needs: email inboxes, agents and activation.
package libredesk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ErrNotFound is returned when the requested object does not exist.
var ErrNotFound = errors.New("libredesk: not found")

// ErrConflict is returned when an agent's email already exists.
var ErrConflict = errors.New("libredesk: conflict")

const maxBody = 16 << 20

// Client talks to one Libredesk instance with an agent API key.
type Client struct {
	baseURL string
	key     string
	secret  string
	http    *http.Client
}

// New returns a client for the Libredesk instance at baseURL.
func New(baseURL, key, secret string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		key:     key,
		secret:  secret,
		http:    &http.Client{Timeout: 20 * time.Second},
	}
}

// APIError is a non-2xx response. It never carries credentials.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("libredesk: http %d: %s", e.Status, e.Message)
	}
	return fmt.Sprintf("libredesk: http %d", e.Status)
}

// Inbox is the subset of a Libredesk inbox Kuraka reads.
type Inbox struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Channel string `json:"channel"`
	From    string `json:"from"`
	Enabled bool   `json:"enabled"`
}

// MailServer is one IMAP or SMTP endpoint. TLS is "tls", "starttls" or "none".
type MailServer struct {
	Host       string
	Port       int
	TLS        string
	SkipVerify bool
	Username   string
	Password   string
}

// NewEmailInbox describes an email inbox with password auth.
type NewEmailInbox struct {
	Name string
	From string
	IMAP MailServer
	SMTP MailServer
}

// NewAgent describes an agent with named Libredesk roles.
type NewAgent struct {
	FirstName string
	LastName  string
	Email     string
	Roles     []string
}

func agentBody(a NewAgent) map[string]any {
	return map[string]any{
		"first_name":          a.FirstName,
		"last_name":           a.LastName,
		"email":               a.Email,
		"send_welcome_email":  false,
		"teams":               []string{},
		"roles":               a.Roles,
		"enabled":             true,
		"availability_status": "offline",
	}
}

// CreateAgent creates an agent; Libredesk generates its initial password.
func (c *Client) CreateAgent(ctx context.Context, a NewAgent) (int64, error) {
	var out struct {
		ID int64 `json:"id"`
	}
	if err := c.do(ctx, http.MethodPost, "/api/v1/agents", agentBody(a), &out); err != nil {
		// v2.9.2 reports duplicate email as a GeneralError (HTTP 500).
		if ae, ok := errors.AsType[*APIError](err); ok && (ae.Status == http.StatusConflict || strings.Contains(strings.ToLower(ae.Message), "already exists")) {
			return 0, ErrConflict
		}
		return 0, err
	}
	if out.ID <= 0 {
		return 0, errors.New("libredesk: agent create returned no id")
	}
	return out.ID, nil
}

// SetAgentPassword sets a password while preserving the newly created fields.
func (c *Client) SetAgentPassword(ctx context.Context, id int64, a NewAgent, password string) error {
	body := agentBody(a)
	body["new_password"] = password
	return c.do(ctx, http.MethodPut, "/api/v1/agents/"+strconv.FormatInt(id, 10), body, nil)
}

type envelope struct {
	Status  string          `json:"status"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// Ping checks that the instance is reachable and the key is accepted.
func (c *Client) Ping(ctx context.Context) error {
	return c.do(ctx, http.MethodGet, "/api/v1/inboxes", nil, nil)
}

// SystemAPIKey logs in and creates an API key for the returned System user.
func (c *Client) SystemAPIKey(ctx context.Context, password string) (string, string, error) {
	body, err := json.Marshal(map[string]string{"email": "System", "password": password})
	if err != nil {
		return "", "", errors.New("libredesk: encode login")
	}
	var cookies []*http.Cookie
	csrf := ""
	// Secure cookies must be carried explicitly on the internal HTTP network.
	client := *c.http
	client.Jar = nil
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	post := func(path string, body []byte, out any) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
		if err != nil {
			return errors.New("libredesk: create setup request")
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		for _, cookie := range cookies {
			req.AddCookie(cookie)
		}
		if csrf != "" {
			req.Header.Set("X-CSRFTOKEN", csrf)
		}
		resp, err := client.Do(req)
		if err != nil {
			return errors.New("libredesk: setup request failed")
		}
		defer resp.Body.Close()
		var env envelope
		if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&env); err != nil {
			return errors.New("libredesk: invalid setup response")
		}
		if resp.StatusCode < 200 || resp.StatusCode > 299 || env.Status == "error" {
			return &APIError{Status: resp.StatusCode}
		}
		for _, cookie := range resp.Cookies() {
			if cookie.Name == "libredesk_session" || cookie.Name == "csrf_token" {
				cookies = append(cookies, cookie)
				if cookie.Name == "csrf_token" {
					csrf = cookie.Value
				}
			}
		}
		if err := json.Unmarshal(env.Data, out); err != nil {
			return errors.New("libredesk: invalid setup data")
		}
		return nil
	}
	var user struct {
		ID int64 `json:"id"`
	}
	if err := post("/api/v1/auth/login", body, &user); err != nil {
		return "", "", err
	}
	hasSession := false
	for _, cookie := range cookies {
		hasSession = hasSession || cookie.Name == "libredesk_session" && cookie.Value != ""
	}
	if user.ID <= 0 || csrf == "" || !hasSession {
		return "", "", errors.New("libredesk: incomplete login response")
	}
	var credentials struct {
		Key    string `json:"api_key"`
		Secret string `json:"api_secret"`
	}
	if err := post("/api/v1/agents/"+strconv.FormatInt(user.ID, 10)+"/api-key", nil, &credentials); err != nil {
		return "", "", err
	}
	if credentials.Key == "" || credentials.Secret == "" {
		return "", "", errors.New("libredesk: incomplete API credentials")
	}
	return credentials.Key, credentials.Secret, nil
}

// ListInboxes returns every inbox the key's agent can see.
func (c *Client) ListInboxes(ctx context.Context) ([]Inbox, error) {
	var out []Inbox
	if err := c.do(ctx, http.MethodGet, "/api/v1/inboxes", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CreateEmailInbox creates an enabled email inbox and returns its id.
func (c *Client) CreateEmailInbox(ctx context.Context, in NewEmailInbox) (int64, error) {
	body := map[string]any{
		"name":         in.Name,
		"channel":      "email",
		"enabled":      true,
		"csat_enabled": false,
		"from":         in.From,
		"config": map[string]any{
			"auth_type":              "password",
			"from":                   in.From,
			"reply_to":               "",
			"enable_plus_addressing": false,
			"smtp": []map[string]any{{
				"host":              in.SMTP.Host,
				"port":              in.SMTP.Port,
				"username":          in.SMTP.Username,
				"password":          in.SMTP.Password,
				"auth_protocol":     "plain",
				"tls_type":          in.SMTP.TLS,
				"tls_skip_verify":   in.SMTP.SkipVerify,
				"hello_hostname":    "",
				"max_conns":         5,
				"max_msg_retries":   3,
				"idle_timeout":      "25s",
				"pool_wait_timeout": "60s",
			}},
			"imap": []map[string]any{{
				"host":             in.IMAP.Host,
				"port":             in.IMAP.Port,
				"username":         in.IMAP.Username,
				"password":         in.IMAP.Password,
				"mailbox":          "INBOX",
				"tls_type":         in.IMAP.TLS,
				"tls_skip_verify":  in.IMAP.SkipVerify,
				"read_interval":    "30s",
				"scan_inbox_since": "48h",
			}},
		},
	}
	var out Inbox
	if err := c.do(ctx, http.MethodPost, "/api/v1/inboxes", body, &out); err != nil {
		return 0, err
	}
	if out.ID == 0 {
		return 0, errors.New("libredesk: inbox create returned no id")
	}
	return out.ID, nil
}

// DeleteInbox deletes an inbox. A missing inbox yields ErrNotFound.
func (c *Client) DeleteInbox(ctx context.Context, id int64) error {
	err := c.do(ctx, http.MethodDelete, "/api/v1/inboxes/"+strconv.FormatInt(id, 10), nil, nil)
	if ae, ok := errors.AsType[*APIError](err); ok && ae.Status == http.StatusNotFound {
		return ErrNotFound
	}
	return err
}

// do sends a request and decodes the envelope's data into out (if non-nil).
func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var rd io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "token "+c.key+":"+c.secret)
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		// url.Error carries only the URL, which holds no credentials.
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return err
	}
	var env envelope
	jsonErr := json.Unmarshal(raw, &env)
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		msg := env.Message
		if jsonErr != nil || msg == "" {
			msg = http.StatusText(resp.StatusCode)
		}
		return &APIError{Status: resp.StatusCode, Message: msg}
	}
	// A 2xx can still carry an error envelope, even for calls without output.
	if jsonErr == nil && env.Status == "error" {
		return &APIError{Status: resp.StatusCode, Message: env.Message}
	}
	if out == nil {
		return nil
	}
	if jsonErr != nil {
		return fmt.Errorf("libredesk: decode response: %w", jsonErr)
	}
	if err := json.Unmarshal(env.Data, out); err != nil {
		return fmt.Errorf("libredesk: decode data: %w", err)
	}
	return nil
}
