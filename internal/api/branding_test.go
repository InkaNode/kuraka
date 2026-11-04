package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/InkaNode/kuraka/internal/store"
)

var testPNG = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")

func dataURL(mime string, data []byte) string {
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
}

func uploadAsset(t *testing.T, c *testClient, mime string, data []byte) string {
	t.Helper()
	return object(t, c.do("POST", "/branding/assets", map[string]string{"data": dataURL(mime, data)}, 201))["id"].(string)
}

func getAsset(t *testing.T, h *harness, id string) (int, http.Header, []byte) {
	t.Helper()
	resp, err := http.Get(h.http.URL + "/branding/assets/" + id)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, resp.Header, data
}

func TestResolveBranding(t *testing.T) {
	global := map[string]store.Branding{
		"default":  {Name: "Inka", Logo: "glogo", Color: "#111111", Banner: "gbanner"},
		"kuraka":   {Color: "#222222"},
		"stalwart": {Name: "Inka Mail"},
	}
	tenant := map[string]store.Branding{
		"default":  {Name: "Acme", Icon: "ticon"},
		"kuraka":   {GradientFrom: "#333333"},
		"stalwart": {Name: "ignored"},
	}
	for _, c := range []struct {
		app  string
		want store.Branding
	}{
		{"kuraka", store.Branding{Name: "Acme", Logo: "glogo", Icon: "ticon", Color: "#222222", GradientFrom: "#333333", Banner: "gbanner"}},
		{"stalwart", store.Branding{Name: "Inka Mail", Logo: "glogo", Color: "#111111", Banner: "gbanner"}},
		{"desk", store.Branding{Name: "Acme", Logo: "glogo", Icon: "ticon"}},
	} {
		if got := resolveBranding(c.app, tenant, global); got != c.want {
			t.Errorf("%s: got %+v, want %+v", c.app, got, c.want)
		}
	}
	if got := resolveBranding("kuraka", nil, nil); got != (store.Branding{}) {
		t.Errorf("empty: %+v", got)
	}
}

func TestBrandingValidation(t *testing.T) {
	h := newHarness(t, true)
	tenant := h.tenant("Acme")
	for _, c := range []struct {
		path string
		body map[string]string
	}{
		{"/branding/kuraka", map[string]string{"color": "red"}},
		{"/branding/kuraka", map[string]string{"gradient_to": "#12345"}},
		{"/branding/kuraka", map[string]string{"name": strings.Repeat("é", 65)}},
		{"/branding/kuraka", map[string]string{"name": "a\nb"}},
		{"/branding/kuraka", map[string]string{"title": "x"}},
		{"/branding/other", map[string]string{"name": "x"}},
		{fmt.Sprintf("/branding/stalwart?tenant=%d", tenant), map[string]string{"name": "x"}},
		{"/branding/kuraka", map[string]string{"logo": strings.Repeat("0", 32)}},
		{"/branding/kuraka", map[string]string{"icon": "../x"}},
		{"/branding/desk", map[string]string{"color": "#000000"}},
	} {
		assertCode(t, h.root.do("PUT", c.path, c.body, 422), "invalid_input")
	}
	h.root.do("PUT", "/branding/kuraka?tenant=999", map[string]string{"name": "x"}, 404)
	h.admin(tenant, "admin@acme.test").do("PUT", "/branding/kuraka", map[string]string{"name": "x"}, 403)
	id := uploadAsset(t, h.root, "image/png", testPNG)
	out := object(t, h.root.do("PUT", "/branding/kuraka", map[string]string{"name": "  Inka  ", "color": "#ABCDEF", "logo": id}, 200))
	saved := out["saved"].(map[string]any)
	if len(saved) != 3 || saved["name"] != "Inka" || saved["color"] != "#abcdef" || saved["logo"] != id || len(out["desks"].([]any)) != 0 {
		t.Fatalf("saved: %v", out)
	}
}

func TestBrandingAssetUpload(t *testing.T) {
	h := newHarness(t, true)
	id := uploadAsset(t, h.root, "image/png", testPNG)
	if !brandingAssetID.MatchString(id) {
		t.Fatalf("asset id %q", id)
	}
	big := append(append([]byte{}, testPNG...), make([]byte, maxBrandingAsset)...)
	for _, data := range []string{
		dataURL("image/jpeg", testPNG),
		dataURL("image/png", big),
		dataURL("image/svg+xml", []byte("<html><script>alert(1)</script></html>")),
		dataURL("text/html", []byte("<svg></svg>")),
		"image/png;base64," + base64.StdEncoding.EncodeToString(testPNG),
		"data:image/png;base64,***",
	} {
		assertCode(t, h.root.do("POST", "/branding/assets", map[string]string{"data": data}, 422), "invalid_input")
	}
	status, headers, data := getAsset(t, h, id)
	if status != 200 || headers.Get("Content-Type") != "image/png" || headers.Get("Cache-Control") != "public, max-age=31536000, immutable" || !bytes.Equal(data, testPNG) {
		t.Fatalf("png: %d %v", status, headers)
	}
	svg := uploadAsset(t, h.root, "image/svg+xml", []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`))
	status, headers, _ = getAsset(t, h, svg)
	if status != 200 || headers.Get("Content-Type") != "image/svg+xml" || headers.Get("Content-Security-Policy") != "default-src 'none'; style-src 'unsafe-inline'; sandbox" {
		t.Fatalf("svg: %d %v", status, headers)
	}
	for _, missing := range []string{strings.Repeat("0", 32), "not-an-id"} {
		if status, _, _ := getAsset(t, h, missing); status != 404 {
			t.Fatalf("%s: status %d", missing, status)
		}
	}
}

func TestPublicBranding(t *testing.T) {
	h := newHarness(t, true)
	tenant := h.tenant("Acme")
	id := uploadAsset(t, h.root, "image/png", testPNG)
	logo := "http://kuraka.test/branding/assets/" + id
	h.root.do("PUT", "/branding/default", map[string]string{"name": "Inka", "logo": id}, 200)
	h.root.do("PUT", "/branding/stalwart", map[string]string{"name": "Inka Mail"}, 200)
	h.root.do("PUT", fmt.Sprintf("/branding/kuraka?tenant=%d", tenant), map[string]string{"name": "Acme", "color": "#123456"}, 200)
	anon := h.newClient()
	if got := object(t, anon.do("GET", "/branding/kuraka", nil, 200)); len(got) != 2 || got["name"] != "Inka" || got["logo"] != logo {
		t.Fatalf("anonymous: %v", got)
	}
	if got := object(t, h.root.do("GET", "/branding/kuraka", nil, 200)); len(got) != 2 || got["name"] != "Inka" {
		t.Fatalf("superadmin: %v", got)
	}
	if got := object(t, h.admin(tenant, "admin@acme.test").do("GET", "/branding/kuraka", nil, 200)); len(got) != 3 || got["name"] != "Acme" || got["color"] != "#123456" || got["logo"] != logo {
		t.Fatalf("tenant: %v", got)
	}
	if got := object(t, anon.do("GET", "/branding/stalwart", nil, 200)); len(got) != 2 || got["name"] != "Inka Mail" || got["logo"] != logo {
		t.Fatalf("stalwart: %v", got)
	}
	anon.do("GET", "/branding/desk", nil, 404)
	anon.do("GET", "/branding", nil, 401)
	view := object(t, h.root.do("GET", fmt.Sprintf("/branding?tenant=%d", tenant), nil, 200))
	if view["scope"].(map[string]any)["kuraka"].(map[string]any)["name"] != "Acme" || view["global"].(map[string]any)["default"].(map[string]any)["logo"] != id {
		t.Fatalf("admin view: %v", view)
	}
	if view := object(t, h.root.do("GET", "/branding", nil, 200)); view["global"] != nil || view["scope"].(map[string]any)["stalwart"] == nil {
		t.Fatalf("global view: %v", view)
	}
	h.root.do("GET", "/branding?tenant=999", nil, 404)
}

func TestDeskBrandingPush(t *testing.T) {
	h, _, hd := integrations(t)
	tenant := h.tenant("Acme")
	helpdesk(t, h.root, tenant)
	helpdesk(t, h.root, h.tenant("Other"))
	enc, err := h.s.box.Seal([]byte("setup-password"))
	if err != nil {
		t.Fatal(err)
	}
	pending, err := h.st.CreateHelpdesk(context.Background(), store.Helpdesk{TenantID: tenant, Kind: "libredesk", Name: "pending.test", URL: "http://pending.test", Status: "pending", SetupSecretEnc: enc})
	if err != nil {
		t.Fatal(err)
	}
	id := uploadAsset(t, h.root, "image/png", testPNG)
	logo := "http://kuraka.test/branding/assets/" + id
	h.root.do("PUT", "/branding/kuraka", map[string]string{"name": "Kuraka only"}, 200)
	if len(hd.branding) != 0 {
		t.Fatalf("kuraka branding pushed: %v", hd.branding)
	}
	out := object(t, h.root.do("PUT", "/branding/default", map[string]string{"name": "Inka", "logo": id, "color": "#000000"}, 200))
	if desks := out["desks"].([]any); len(desks) != 2 || desks[0].(map[string]any)["url"] != "http://desk.test" || desks[0].(map[string]any)["error"] != nil {
		t.Fatalf("desks: %v", out)
	}
	if len(hd.branding) != 2 || hd.branding[0] != [3]string{"Inka", logo, ""} {
		t.Fatalf("pushed: %v", hd.branding)
	}
	hd.branding, hd.brandingErr = nil, errors.New("down")
	out = object(t, h.root.do("PUT", fmt.Sprintf("/branding/desk?tenant=%d", tenant), map[string]string{"name": "Acme Desk", "icon": id}, 200))
	if desks := out["desks"].([]any); len(desks) != 1 || desks[0].(map[string]any)["error"] != "Helpdesk branding failed" {
		t.Fatalf("failed push: %v", out)
	}
	if len(hd.branding) != 1 || hd.branding[0] != [3]string{"Acme Desk", logo, logo} {
		t.Fatalf("tenant push: %v", hd.branding)
	}
	hd.branding, hd.brandingErr = nil, nil
	h.root.do("POST", fmt.Sprintf("/helpdesks/%d/activate", pending.ID), nil, 200)
	if len(hd.branding) != 1 || hd.branding[0] != [3]string{"Acme Desk", logo, logo} {
		t.Fatalf("activation push: %v", hd.branding)
	}
}
