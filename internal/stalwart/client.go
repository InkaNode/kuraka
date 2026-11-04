// Package stalwart is a typed JMAP client for the Stalwart management API.
// It exposes only the methods Kuraka needs; there is no generic passthrough.
package stalwart

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"
)

// ErrNotFound is returned when the requested object does not exist.
var ErrNotFound = errors.New("stalwart: not found")

var using = []string{"urn:ietf:params:jmap:core", "urn:stalwart:jmap"}

const maxBody = 16 << 20

// Client talks to the Stalwart JMAP endpoint with an API key.
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// New returns a client for the Stalwart instance at baseURL.
func New(baseURL, apiKey string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		http:    &http.Client{Timeout: 15 * time.Second},
	}
}

// HTTPError is a non-2xx HTTP response, usually problem+json.
type HTTPError struct {
	Status int
	Type   string
	Title  string
	Detail string
}

func (e *HTTPError) Error() string {
	msg := fmt.Sprintf("stalwart: http %d", e.Status)
	if e.Title != "" {
		msg += ": " + e.Title
	}
	if e.Detail != "" {
		msg += ": " + e.Detail
	}
	return msg
}

// MethodError is a JMAP method-level error response.
type MethodError struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

func (e *MethodError) Error() string {
	if e.Description != "" {
		return "stalwart: method error " + e.Type + ": " + e.Description
	}
	return "stalwart: method error " + e.Type
}

// SetError is a per-object failure from a /set call.
type SetError struct {
	Type        string   `json:"type"`
	Description string   `json:"description"`
	Properties  []string `json:"properties"`
}

func (e *SetError) Error() string {
	msg := "stalwart: set error " + e.Type
	if e.Description != "" {
		msg += ": " + e.Description
	}
	if len(e.Properties) > 0 {
		msg += " (" + strings.Join(e.Properties, ", ") + ")"
	}
	return msg
}

// Is reports a notFound set error as ErrNotFound.
func (e *SetError) Is(target error) bool {
	return target == ErrNotFound && e.Type == "notFound"
}

type invocation struct {
	name string
	args any
	id   string
}

func (i invocation) MarshalJSON() ([]byte, error) {
	return json.Marshal([]any{i.name, i.args, i.id})
}

type response struct {
	name string
	args json.RawMessage
	id   string
}

func (r *response) UnmarshalJSON(b []byte) error {
	var raw []json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	if len(raw) != 3 {
		return errors.New("stalwart: malformed method response")
	}
	if err := json.Unmarshal(raw[0], &r.name); err != nil {
		return err
	}
	r.args = raw[1]
	return json.Unmarshal(raw[2], &r.id)
}

// Ping checks that the JMAP session endpoint accepts the API key.
func (c *Client) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/jmap/session", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("stalwart: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if resp.StatusCode != http.StatusOK {
		return newHTTPError(resp.StatusCode, data)
	}
	return nil
}

func (c *Client) call(ctx context.Context, calls []invocation) ([]response, error) {
	body, err := json.Marshal(struct {
		Using       []string     `json:"using"`
		MethodCalls []invocation `json:"methodCalls"`
	}{using, calls})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/jmap/", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("stalwart: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, fmt.Errorf("stalwart: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, newHTTPError(resp.StatusCode, data)
	}
	var env struct {
		MethodResponses []response `json:"methodResponses"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("stalwart: decode response: %w", err)
	}
	for _, r := range env.MethodResponses {
		if r.name == "error" {
			me := &MethodError{}
			if err := json.Unmarshal(r.args, me); err != nil {
				return nil, fmt.Errorf("stalwart: decode method error: %w", err)
			}
			return nil, me
		}
	}
	return env.MethodResponses, nil
}

func newHTTPError(status int, body []byte) *HTTPError {
	e := &HTTPError{Status: status}
	var p struct {
		Type   string `json:"type"`
		Title  string `json:"title"`
		Detail string `json:"detail"`
	}
	if json.Unmarshal(body, &p) == nil {
		e.Type, e.Title, e.Detail = p.Type, p.Title, p.Detail
		return e
	}
	detail := strings.TrimSpace(string(body))
	if len(detail) > 512 {
		detail = detail[:512]
	}
	e.Detail = detail
	return e
}

// result decodes the arguments of the response with the given call id.
func result(resps []response, id string, v any) error {
	for _, r := range resps {
		if r.id == id {
			if err := json.Unmarshal(r.args, v); err != nil {
				return fmt.Errorf("stalwart: decode %s: %w", r.name, err)
			}
			return nil
		}
	}
	return fmt.Errorf("stalwart: missing response %q", id)
}

type getResult[T any] struct {
	List     []T      `json:"list"`
	NotFound []string `json:"notFound"`
}

type setResult struct {
	Created map[string]struct {
		ID     string `json:"id"`
		Secret string `json:"secret"`
	} `json:"created"`
	NotCreated   map[string]*SetError `json:"notCreated"`
	NotUpdated   map[string]*SetError `json:"notUpdated"`
	NotDestroyed map[string]*SetError `json:"notDestroyed"`
}

// queryGet runs object/query and an object/get of its ids in one request.
func queryGet[T any](ctx context.Context, c *Client, object string, filter any) ([]T, error) {
	q := map[string]any{}
	if filter != nil {
		q["filter"] = filter
	}
	get := map[string]any{"#ids": map[string]any{
		"resultOf": "q",
		"name":     object + "/query",
		"path":     "/ids",
	}}
	resps, err := c.call(ctx, []invocation{
		{object + "/query", q, "q"},
		{object + "/get", get, "g"},
	})
	if err != nil {
		return nil, err
	}
	var g getResult[T]
	if err := result(resps, "g", &g); err != nil {
		return nil, err
	}
	return g.List, nil
}

func getOne[T any](ctx context.Context, c *Client, object, id string) (T, error) {
	var zero T
	resps, err := c.call(ctx, []invocation{
		{object + "/get", map[string]any{"ids": []string{id}}, "g"},
	})
	if err != nil {
		return zero, err
	}
	var g getResult[T]
	if err := result(resps, "g", &g); err != nil {
		return zero, err
	}
	if len(g.List) == 0 || slices.Contains(g.NotFound, id) {
		return zero, ErrNotFound
	}
	return g.List[0], nil
}

func (c *Client) set(ctx context.Context, object string, args map[string]any) (setResult, error) {
	var r setResult
	resps, err := c.call(ctx, []invocation{{object + "/set", args, "s"}})
	if err != nil {
		return r, err
	}
	err = result(resps, "s", &r)
	return r, err
}

func (c *Client) create(ctx context.Context, object string, obj map[string]any) (string, error) {
	r, err := c.set(ctx, object, map[string]any{"create": map[string]any{"n": obj}})
	if err != nil {
		return "", err
	}
	if e := r.NotCreated["n"]; e != nil {
		return "", e
	}
	if cr, ok := r.Created["n"]; ok && cr.ID != "" {
		return cr.ID, nil
	}
	return "", errors.New("stalwart: create returned no id")
}

func (c *Client) update(ctx context.Context, object, id string, patch map[string]any) error {
	r, err := c.set(ctx, object, map[string]any{"update": map[string]any{id: patch}})
	if err != nil {
		return err
	}
	if e := r.NotUpdated[id]; e != nil {
		return e
	}
	return nil
}

func (c *Client) destroy(ctx context.Context, object, id string) error {
	r, err := c.set(ctx, object, map[string]any{"destroy": []string{id}})
	if err != nil {
		return err
	}
	if e := r.NotDestroyed[id]; e != nil {
		return e
	}
	return nil
}
