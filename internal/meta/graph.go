// Package meta talks to Meta's Graph API with the installation's own Meta
// app: the Facebook login, the assets it grants, and the app webhooks.
package meta

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Version is the Graph API version, the same Libredesk uses.
const Version = "v26.0"

// App is the installation's Meta app.
type App struct {
	ID, Secret string
	// BaseURL is the Graph API origin; tests point it at a fake.
	BaseURL string
	HTTP    *http.Client
}

// APIError is a non-2xx Graph answer. Message is Meta's text, never a secret.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string { return fmt.Sprintf("meta: %d %s", e.Status, e.Message) }

// Page is a Facebook Page the user granted, with its Instagram account.
type Page struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	AccessToken string `json:"access_token"`
	Instagram   *struct {
		ID       string `json:"id"`
		Username string `json:"username"`
	} `json:"instagram_business_account"`
}

// Phone is a WhatsApp Business number the user granted.
type Phone struct {
	ID           string `json:"id"`
	WABAID       string `json:"-"`
	Display      string `json:"display_phone_number"`
	VerifiedName string `json:"verified_name"`
	PlatformType string `json:"platform_type"`
}

func (a App) base() string {
	if a.BaseURL != "" {
		return strings.TrimRight(a.BaseURL, "/")
	}
	return "https://graph.facebook.com"
}

func (a App) appToken() string { return a.ID + "|" + a.Secret }

// LoginURL is Meta's login dialog for the login configuration configID.
// WhatsApp configurations open the Embedded Signup.
func (a App) LoginURL(configID, redirect, state string, whatsApp bool) string {
	q := url.Values{"client_id": {a.ID}, "config_id": {configID}, "redirect_uri": {redirect}, "state": {state},
		"response_type": {"code"}, "override_default_response_type": {"true"}}
	if whatsApp {
		q.Set("extras", `{"setup":{},"featureType":"","sessionInfoVersion":"3"}`)
	}
	return "https://www.facebook.com/" + Version + "/dialog/oauth?" + q.Encode()
}

func (a App) call(ctx context.Context, method, path string, q url.Values, out any) error {
	var body io.Reader
	u := a.base() + "/" + Version + "/" + path
	if method == http.MethodGet {
		u += "?" + q.Encode()
	} else {
		body = strings.NewReader(q.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	client := a.HTTP
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		// The URL can hold tokens; keep only the cause.
		var ue *url.Error
		if errors.As(err, &ue) {
			return ue.Err
		}
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var e struct {
			Error struct{ Message string } `json:"error"`
		}
		_ = json.Unmarshal(raw, &e)
		return &APIError{Status: resp.StatusCode, Message: e.Error.Message}
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// Check confirms the app ID and secret and returns the app's name.
func (a App) Check(ctx context.Context) (string, error) {
	var out struct{ Name string }
	err := a.call(ctx, http.MethodGet, url.PathEscape(a.ID), url.Values{"fields": {"name"}, "access_token": {a.appToken()}}, &out)
	return out.Name, err
}

// Exchange turns the login code into an access token. Page logins get a
// long-lived token, so the Page tokens read with it do not expire.
func (a App) Exchange(ctx context.Context, code, redirect string) (string, error) {
	var out struct {
		AccessToken string `json:"access_token"`
	}
	err := a.call(ctx, http.MethodGet, "oauth/access_token", url.Values{"client_id": {a.ID}, "client_secret": {a.Secret},
		"redirect_uri": {redirect}, "code": {code}}, &out)
	if err != nil {
		return "", err
	}
	token := out.AccessToken
	// Business tokens are already long-lived and may refuse the exchange.
	if a.call(ctx, http.MethodGet, "oauth/access_token", url.Values{"grant_type": {"fb_exchange_token"}, "client_id": {a.ID},
		"client_secret": {a.Secret}, "fb_exchange_token": {token}}, &out) == nil && out.AccessToken != "" {
		token = out.AccessToken
	}
	return token, nil
}

// Pages lists the Pages token can manage, with their Page tokens.
func (a App) Pages(ctx context.Context, token string) ([]Page, error) {
	var out struct{ Data []Page }
	// frugal: first 100 Pages only; follow paging.next if someone manages more.
	err := a.call(ctx, http.MethodGet, "me/accounts", url.Values{"fields": {"id,name,access_token,instagram_business_account{id,username}"},
		"limit": {"100"}, "access_token": {token}}, &out)
	return out.Data, err
}

// Phones lists the WhatsApp numbers of every WhatsApp Business account token
// was granted.
func (a App) Phones(ctx context.Context, token string) ([]Phone, error) {
	var dbg struct {
		Data struct {
			Scopes []struct {
				Scope     string   `json:"scope"`
				TargetIDs []string `json:"target_ids"`
			} `json:"granular_scopes"`
		}
	}
	if err := a.call(ctx, http.MethodGet, "debug_token", url.Values{"input_token": {token}, "access_token": {a.appToken()}}, &dbg); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []Phone
	for _, s := range dbg.Data.Scopes {
		if s.Scope != "whatsapp_business_management" && s.Scope != "whatsapp_business_messaging" {
			continue
		}
		for _, waba := range s.TargetIDs {
			if seen[waba] {
				continue
			}
			seen[waba] = true
			var list struct{ Data []Phone }
			if err := a.call(ctx, http.MethodGet, url.PathEscape(waba)+"/phone_numbers", url.Values{
				"fields": {"id,display_phone_number,verified_name,platform_type"}, "access_token": {token}}, &list); err != nil {
				return nil, err
			}
			for _, p := range list.Data {
				p.WABAID = waba
				out = append(out, p)
			}
		}
	}
	return out, nil
}

// SubscribePage sends the Page's messages, and its Instagram account's, to
// the app webhook.
func (a App) SubscribePage(ctx context.Context, pageID, pageToken string) error {
	return a.call(ctx, http.MethodPost, url.PathEscape(pageID)+"/subscribed_apps", url.Values{"subscribed_fields": {"messages"}, "access_token": {pageToken}}, nil)
}

// SubscribeWABA sends the WhatsApp account's messages to the app webhook.
func (a App) SubscribeWABA(ctx context.Context, wabaID, token string) error {
	return a.call(ctx, http.MethodPost, url.PathEscape(wabaID)+"/subscribed_apps", url.Values{"access_token": {token}}, nil)
}

// Register turns on the Cloud API for a number new to it, setting pin as its
// two-step verification PIN.
func (a App) Register(ctx context.Context, phoneID, token, pin string) error {
	return a.call(ctx, http.MethodPost, url.PathEscape(phoneID)+"/register", url.Values{"messaging_product": {"whatsapp"}, "pin": {pin}, "access_token": {token}}, nil)
}

// SubscribeApp points the app's Page, Instagram and WhatsApp webhooks at
// callback. Meta checks callback with verifyToken right away.
func (a App) SubscribeApp(ctx context.Context, callback, verifyToken string) error {
	for _, object := range []string{"page", "instagram", "whatsapp_business_account"} {
		if err := a.call(ctx, http.MethodPost, url.PathEscape(a.ID)+"/subscriptions", url.Values{"object": {object}, "callback_url": {callback},
			"fields": {"messages"}, "verify_token": {verifyToken}, "access_token": {a.appToken()}}, nil); err != nil {
			return fmt.Errorf("%s: %w", object, err)
		}
	}
	return nil
}

// ValidSignature checks Meta's X-Hub-Signature-256 header for body.
func ValidSignature(body []byte, header, secret string) bool {
	sig, ok := strings.CutPrefix(header, "sha256=")
	want, err := hex.DecodeString(sig)
	if !ok || err != nil || secret == "" {
		return false
	}
	return hmac.Equal(want, digest(body, secret))
}

func digest(body []byte, secret string) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return mac.Sum(nil)
}

// Sign returns the X-Hub-Signature-256 value Meta sends for body.
func Sign(body []byte, secret string) string {
	return "sha256=" + hex.EncodeToString(digest(body, secret))
}
