// Package cloudflare is a minimal client for the Cloudflare API v4 covering
// the zone, DNS record and Single Redirect operations Kuraka needs.
package cloudflare

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultBaseURL is the Cloudflare API v4 endpoint.
const DefaultBaseURL = "https://api.cloudflare.com/client/v4"

const maxBody = 16 << 20

// ErrZoneNotFound is returned when no zone matches a name or its parents.
var ErrZoneNotFound = errors.New("cloudflare: zone not found")

// Client talks to the Cloudflare API with an API token.
type Client struct {
	// BaseURL may be overridden (e.g. in tests); no trailing slash.
	BaseURL string
	token   string
	http    *http.Client
}

// New returns a client authenticated with token.
func New(token string) *Client {
	return &Client{
		BaseURL: DefaultBaseURL,
		token:   token,
		http:    &http.Client{Timeout: 20 * time.Second},
	}
}

// APIError is a failed Cloudflare response.
type APIError struct {
	Status   int
	Codes    []int
	Messages []string
}

func (e *APIError) Error() string {
	msg := fmt.Sprintf("cloudflare: http %d", e.Status)
	for i, m := range e.Messages {
		if i < len(e.Codes) {
			msg += fmt.Sprintf(": [%d] %s", e.Codes[i], m)
		} else {
			msg += ": " + m
		}
	}
	return msg
}

type envelope struct {
	Success bool `json:"success"`
	Errors  []struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"errors"`
	Result     json.RawMessage `json:"result"`
	ResultInfo *struct {
		Page       int `json:"page"`
		TotalPages int `json:"total_pages"`
	} `json:"result_info"`
}

// do sends a request and decodes the envelope; out receives result when non-nil.
func (c *Client) do(ctx context.Context, method, path string, body, out any) (*envelope, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("cloudflare: encode request: %w", err)
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+path, rd)
	if err != nil {
		return nil, fmt.Errorf("cloudflare: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("User-Agent", "kuraka")
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cloudflare: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, fmt.Errorf("cloudflare: read response: %w", err)
	}
	var env envelope
	jerr := json.Unmarshal(raw, &env)
	if jerr != nil || !env.Success || resp.StatusCode/100 != 2 {
		apiErr := &APIError{Status: resp.StatusCode}
		if jerr == nil {
			for _, e := range env.Errors {
				apiErr.Codes = append(apiErr.Codes, e.Code)
				apiErr.Messages = append(apiErr.Messages, e.Message)
			}
		}
		if len(apiErr.Messages) == 0 {
			if jerr != nil && resp.StatusCode/100 == 2 {
				apiErr.Messages = []string{"invalid response body"}
			} else {
				apiErr.Messages = []string{http.StatusText(resp.StatusCode)}
			}
		}
		return nil, apiErr
	}
	if out != nil && len(env.Result) > 0 {
		if err := json.Unmarshal(env.Result, out); err != nil {
			return nil, fmt.Errorf("cloudflare: decode result: %w", err)
		}
	}
	return &env, nil
}

// VerifyToken checks that the token is valid and active.
func (c *Client) VerifyToken(ctx context.Context) error {
	var res struct {
		Status string `json:"status"`
	}
	if _, err := c.do(ctx, http.MethodGet, "/user/tokens/verify", nil, &res); err != nil {
		return err
	}
	if res.Status != "active" {
		return fmt.Errorf("cloudflare: token status %q", res.Status)
	}
	return nil
}

// Zone is a Cloudflare zone.
type Zone struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// FindZone returns the zone for name, trying name itself and then each
// parent (a.b.example.com, b.example.com, example.com).
func (c *Client) FindZone(ctx context.Context, name string) (Zone, error) {
	name = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(name), "."))
	for name != "" && strings.Contains(name, ".") {
		var zones []Zone
		if _, err := c.do(ctx, http.MethodGet, "/zones?name="+url.QueryEscape(name), nil, &zones); err != nil {
			return Zone{}, err
		}
		for _, z := range zones {
			if strings.EqualFold(z.Name, name) {
				return z, nil
			}
		}
		_, name, _ = strings.Cut(name, ".")
	}
	return Zone{}, ErrZoneNotFound
}
