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
// sends every request for alias, or any of its subdomains, to the same
// subdomain of targetURL's host, keeping the path and query string.
func (c *Client) SetRedirect(ctx context.Context, zoneID, alias, targetURL string) error {
	target, err := redirectTarget(targetURL)
	if err != nil {
		return err
	}
	if !validAlias(alias) {
		return ErrInvalidTarget
	}
	u, _ := url.Parse(target)
	var rule redirectRule
	rule.Description = "kuraka alias redirect"
	rule.Expression = "true"
	rule.Action = "redirect"
	rule.ActionParameters.FromValue.StatusCode = 301
	rule.ActionParameters.FromValue.TargetURL.Expression = `concat("https://", wildcard_replace(http.host, "*` + alias + `", "${1}` + u.Host + `"), "` + u.Path + `", http.request.uri.path)`
	rule.ActionParameters.FromValue.PreserveQueryString = true
	body := map[string]any{"rules": []redirectRule{rule}}
	path := "/zones/" + url.PathEscape(zoneID) + "/rulesets/phases/http_request_dynamic_redirect/entrypoint"
	_, err = c.do(ctx, http.MethodPut, path, body, nil)
	return err
}

// validAlias reports whether alias is a plain DNS name safe to embed in a
// rule expression.
func validAlias(alias string) bool {
	if alias == "" || strings.HasPrefix(alias, ".") || strings.HasSuffix(alias, ".") {
		return false
	}
	for _, r := range alias {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '.') {
			return false
		}
	}
	return true
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
