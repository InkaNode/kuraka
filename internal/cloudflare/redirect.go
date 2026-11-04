package cloudflare

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

// ErrInvalidTarget is returned by SetRedirect for unusable target URLs.
var ErrInvalidTarget = errors.New("cloudflare: redirect target must be an absolute https URL without quotes")

type redirectRule struct {
	Description      string `json:"description"`
	Expression       string `json:"expression"`
	Action           string `json:"action"`
	ActionParameters struct {
		FromValue struct {
			StatusCode int `json:"status_code"`
			TargetURL  struct {
				Expression string `json:"expression"`
			} `json:"target_url"`
			PreserveQueryString bool `json:"preserve_query_string"`
		} `json:"from_value"`
	} `json:"action_parameters"`
}

// SetRedirect replaces the zone's Single Redirect rules with one rule that
// sends every request to targetURL with a 301, keeping path and query.
func (c *Client) SetRedirect(ctx context.Context, zoneID, targetURL string) error {
	target, err := redirectTarget(targetURL)
	if err != nil {
		return err
	}
	var rule redirectRule
	rule.Description = "kuraka alias redirect"
	rule.Expression = "true"
	rule.Action = "redirect"
	rule.ActionParameters.FromValue.StatusCode = 301
	rule.ActionParameters.FromValue.TargetURL.Expression = `concat("` + target + `", http.request.uri.path)`
	rule.ActionParameters.FromValue.PreserveQueryString = true
	body := map[string]any{"rules": []redirectRule{rule}}
	path := "/zones/" + url.PathEscape(zoneID) + "/rulesets/phases/http_request_dynamic_redirect/entrypoint"
	_, err = c.do(ctx, http.MethodPut, path, body, nil)
	return err
}

// redirectTarget validates targetURL and returns it without a trailing slash.
// Quotes, backslashes, query, fragment, whitespace and control characters are
// rejected so the value is always a plain string literal inside the rule
// expression and needs no escaping.
func redirectTarget(targetURL string) (string, error) {
	if strings.ContainsAny(targetURL, "\"'`\\?#") {
		return "", ErrInvalidTarget
	}
	for _, r := range targetURL {
		if r <= ' ' || r == 0x7f {
			return "", ErrInvalidTarget
		}
	}
	u, err := url.Parse(targetURL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return "", ErrInvalidTarget
	}
	return strings.TrimRight(targetURL, "/"), nil
}
