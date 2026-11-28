package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/InkaNode/kuraka/internal/meta"
)

func (f *fakeHelpdesk) ForwardWebhook(_ context.Context, path string, body []byte, sig string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !meta.ValidSignature(body, sig, "sec") {
		return fmt.Errorf("bad signature")
	}
	f.forwarded = append(f.forwarded, path)
	return nil
}

// fakeGraph answers the Graph calls Kuraka makes for app 111 with secret sec.
func fakeGraph(t *testing.T) (*httptest.Server, *[]string) {
	var mu sync.Mutex
	var posts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		path := strings.TrimPrefix(r.URL.Path, "/"+meta.Version+"/")
		reply := func(v string) { _, _ = io.WriteString(w, v) }
		if r.Method == http.MethodPost {
			mu.Lock()
			posts = append(posts, path+" "+r.PostForm.Get("access_token"))
			mu.Unlock()
			reply(`{"success":true}`)
			return
		}
		switch path {
		case "111":
			if r.Form.Get("access_token") != "111|sec" {
				w.WriteHeader(400)
				reply(`{"error":{"message":"Invalid OAuth access token"}}`)
				return
			}
			reply(`{"name":"Kuraka"}`)
		case "oauth/access_token":
			reply(`{"access_token":"user-token"}`)
		case "me/accounts":
			reply(`{"data":[{"id":"777","name":"Tienda","access_token":"page-token","instagram_business_account":{"id":"555","username":"tienda"}}]}`)
		case "debug_token":
			reply(`{"data":{"granular_scopes":[{"scope":"whatsapp_business_management","target_ids":["900"]}]}}`)
		case "900/phone_numbers":
			reply(`{"data":[{"id":"123","display_phone_number":"+51 999","verified_name":"Tienda","platform_type":"NOT_APPLICABLE"}]}`)
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &posts
}

func metaState(t *testing.T, b []byte) string {
	t.Helper()
	u, err := url.Parse(object(t, b)["url"].(string))
	if err != nil {
		t.Fatal(err)
	}
	return u.Query().Get("state")
}

func TestMetaConnect(t *testing.T) {
	h, _, hd := integrations(t)
	graph, posts := fakeGraph(t)
	h.s.graphURL = graph.URL
	a := h.tenant("A")
	da := h.domain(a, "a.test")
	ca, cb := h.admin(a, "a@admin.test"), h.admin(h.tenant("B"), "b@admin.test")
	ha := helpdesk(t, ca, a)
	base := fmt.Sprintf("/domains/%d/meta", da)

	app := map[string]any{"app_id": "111", "app_secret": "wrong", "pages_config_id": "222", "whatsapp_config_id": "333"}
	ca.do("PUT", "/meta-app", app, 403)
	ca.do("POST", base+"/login", map[string]any{"kind": "pages"}, 409)
	assertCode(t, h.root.do("PUT", "/meta-app", app, 422), "meta_app_rejected")
	app["app_secret"] = "sec"
	if b := h.root.do("PUT", "/meta-app", app, 200); strings.Contains(string(b), "sec\"") || object(t, b)["webhook_ok"] != false {
		t.Fatalf("saved: %s", b)
	}
	delete(app, "app_secret")
	h.root.do("PUT", "/meta-app", app, 200)
	got := object(t, h.root.do("GET", "/meta-app", nil, 200))
	verify := got["webhook"].(map[string]any)["verify_token"].(string)
	if got["redirect_url"] != "http://kuraka.test/meta/callback" || len(verify) < 20 {
		t.Fatalf("app: %v", got)
	}
	if s := object(t, ca.do("GET", "/meta-app/status", nil, 200)); s["pages"] != true || s["whatsapp"] != true {
		t.Fatalf("status: %v", s)
	}

	// Pages: one login, then Messenger and Instagram from the same Page.
	state := metaState(t, ca.do("POST", base+"/login", map[string]any{"kind": "pages"}, 200))
	cb.do("POST", base+"/oauth", map[string]any{"code": "c", "state": state}, 404)
	res := object(t, ca.do("POST", base+"/oauth", map[string]any{"code": "c", "state": state}, 200))
	assertCode(t, ca.do("POST", base+"/oauth", map[string]any{"code": "c", "state": state}, 422), "meta_expired")
	pages := res["pages"].([]any)
	if len(pages) != 1 || pages[0].(map[string]any)["instagram"].(map[string]any)["username"] != "tienda" {
		t.Fatalf("pages: %v", res)
	}
	pick := map[string]any{"grant": res["grant"], "helpdesk_id": ha, "messenger": []string{"777"}, "instagram": []string{"555"}}
	cb.do("POST", base+"/import", pick, 404)
	out := object(t, ca.do("POST", base+"/import", pick, 200))
	if len(out["created"].([]any)) != 2 || len(out["failed"].([]any)) != 0 {
		t.Fatalf("import: %v", out)
	}
	if p := hd.metaPages[300]; p.PageID != "777" || p.AccessToken != "page-token" || p.AppSecret != "sec" || p.Name != "Tienda" {
		t.Fatalf("messenger: %+v", p)
	}
	if p := hd.metaPages[301]; p.PageID != "555" || p.Name != "@tienda" {
		t.Fatalf("instagram: %+v", p)
	}
	if f := object(t, ca.do("POST", base+"/import", pick, 200))["failed"].([]any); len(f) != 2 || f[0].(map[string]any)["code"] != "meta_exists" {
		t.Fatalf("again: %v", f)
	}

	// WhatsApp: the number is subscribed, registered and connected.
	state = metaState(t, ca.do("POST", base+"/login", map[string]any{"kind": "whatsapp"}, 200))
	res = object(t, ca.do("POST", base+"/oauth", map[string]any{"code": "c", "state": state}, 200))
	out = object(t, ca.do("POST", base+"/import", map[string]any{"grant": res["grant"], "helpdesk_id": ha, "whatsapp": []string{"123"}}, 200))
	if len(out["created"].([]any)) != 1 {
		t.Fatalf("whatsapp: %v", out)
	}
	if w := hd.whatsApps[200]; w.PhoneNumberID != "123" || w.WABAID != "900" || w.AccessToken != "user-token" || w.AppSecret != "sec" {
		t.Fatalf("whatsapp: %+v", w)
	}
	if p := strings.Join(*posts, ","); !strings.Contains(p, "777/subscribed_apps page-token") || !strings.Contains(p, "900/subscribed_apps user-token") || !strings.Contains(p, "123/register") {
		t.Fatalf("graph posts: %s", p)
	}

	// The app webhook reaches the helpdesk of each Page and number.
	get, err := http.Get(h.http.URL + "/webhooks/meta?hub.mode=subscribe&hub.challenge=xyz&hub.verify_token=" + verify)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := io.ReadAll(get.Body); get.StatusCode != 200 || string(b) != "xyz" {
		t.Fatalf("verify: %d %s", get.StatusCode, b)
	}
	post := func(body, secret string) int {
		req, _ := http.NewRequest("POST", h.http.URL+"/webhooks/meta", strings.NewReader(body))
		req.Header.Set("X-Hub-Signature-256", meta.Sign([]byte(body), secret))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	page := `{"object":"page","entry":[{"id":"777","messaging":[]}]}`
	if post(page, "other") != 403 || len(hd.forwarded) != 0 {
		t.Fatal("unsigned event forwarded")
	}
	wa, _ := json.Marshal(map[string]any{"object": "whatsapp_business_account", "entry": []any{map[string]any{"id": "900",
		"changes": []any{map[string]any{"value": map[string]any{"metadata": map[string]any{"phone_number_id": "123"}}}}}}})
	if post(page, "sec") != 200 || post(string(wa), "sec") != 200 || post(`{"object":"page","entry":[{"id":"1"}]}`, "sec") != 200 {
		t.Fatal("event refused")
	}
	if f := strings.Join(hd.forwarded, ","); f != "/webhooks/meta/300,/webhooks/whatsapp/200" {
		t.Fatalf("forwarded: %s", f)
	}
}
