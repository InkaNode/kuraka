package api

import (
	"context"
	"fmt"
	"testing"

	"github.com/InkaNode/kuraka/internal/libredesk"
)

func (f *fakeHelpdesk) CreateWhatsApp(_ context.Context, w libredesk.WhatsApp, token string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.whatsAppErr != nil {
		return 0, f.whatsAppErr
	}
	if f.whatsApps == nil {
		f.whatsApps, f.whatsAppTokens = map[int64]libredesk.WhatsApp{}, map[int64]string{}
	}
	id := int64(len(f.whatsApps) + 200)
	f.whatsApps[id], f.whatsAppTokens[id] = w, token
	return id, nil
}
func (f *fakeHelpdesk) GetWhatsApp(_ context.Context, id int64) (libredesk.WhatsApp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w, ok := f.whatsApps[id]
	if !ok {
		return w, libredesk.ErrNotFound
	}
	w.AccessToken, w.AppSecret = "", ""
	return w, nil
}
func (f *fakeHelpdesk) UpdateWhatsApp(_ context.Context, id int64, w libredesk.WhatsApp, token string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	old, ok := f.whatsApps[id]
	if !ok {
		return libredesk.ErrNotFound
	}
	if w.AccessToken == "" {
		w.AccessToken = old.AccessToken
	}
	if w.AppSecret == "" {
		w.AppSecret = old.AppSecret
	}
	f.whatsApps[id], f.whatsAppTokens[id] = w, token
	return nil
}

func TestMetaChannels(t *testing.T) {
	h, _, hd := integrations(t)
	a, b := h.tenant("A"), h.tenant("B")
	da, db := h.domain(a, "a.test"), h.domain(b, "b.test")
	ca, cb := h.admin(a, "a@admin.test"), h.admin(b, "b@admin.test")
	ha := helpdesk(t, ca, a)
	base := fmt.Sprintf("/domains/%d/meta", da)
	in := map[string]any{"helpdesk_id": ha, "channel": "whatsapp", "name": " Ventas ", "phone_number_id": "123", "waba_id": "456", "access_token": "EAAtoken", "app_secret": "abc"}

	for field, value := range map[string]any{"channel": "telegram", "name": "", "phone_number_id": "12a", "waba_id": "", "access_token": "", "app_secret": "a b"} {
		bad := map[string]any{}
		for k, v := range in {
			bad[k] = v
		}
		bad[field] = value
		ca.do("POST", base, bad, 422)
	}
	cb.do("POST", fmt.Sprintf("/domains/%d/meta", db), in, 404)
	cb.do("POST", base, in, 404)

	hd.whatsAppErr = &libredesk.APIError{Status: 400, Message: "invalid credentials"}
	if e := object(t, ca.do("POST", base, in, 422))["error"].(map[string]any); e["code"] != "whatsapp_rejected" {
		t.Fatalf("rejected: %v", e)
	}
	hd.whatsAppErr = nil

	ch := object(t, ca.do("POST", base, in, 201))
	hook := ch["webhook"].(map[string]any)
	token := hd.whatsAppTokens[200]
	if hook["url"] != "https://desk.example/webhooks/whatsapp/200" || hook["verify_token"] != token || len(token) < 20 || ch["name"] != "Ventas" {
		t.Fatalf("created: %v", ch)
	}
	if w := hd.whatsApps[200]; w.PhoneNumberID != "123" || w.AccessToken != "EAAtoken" || w.AppSecret != "abc" {
		t.Fatalf("libredesk got %+v", w)
	}
	path := fmt.Sprintf("%s/%.0f", base, ch["id"])
	if list := array(t, ca.do("GET", base, nil, 200)); len(list) != 1 {
		t.Fatalf("list: %v", list)
	}
	cb.do("GET", path, nil, 404)
	cb.do("GET", fmt.Sprintf("/domains/%d/meta/%.0f", db, ch["id"]), nil, 404)

	edit := map[string]any{"name": "Soporte", "phone_number_id": "789", "waba_id": "456"}
	cb.do("PUT", path, edit, 404)
	ca.do("PUT", path, edit, 200)
	if w := hd.whatsApps[200]; w.Name != "Soporte" || w.PhoneNumberID != "789" || w.AccessToken != "EAAtoken" || hd.whatsAppTokens[200] != token {
		t.Fatalf("updated: %+v", w)
	}
	got := object(t, ca.do("GET", path, nil, 200))
	if s := got["settings"].(map[string]any); s["phone_number_id"] != "789" || s["access_token"] != nil {
		t.Fatalf("get: %v", got)
	}

	cb.do("DELETE", path, nil, 404)
	ca.do("DELETE", path, nil, 204)
	if len(hd.deleted) != 1 || hd.deleted[0] != 200 || len(array(t, ca.do("GET", base, nil, 200))) != 0 {
		t.Fatalf("deleted: %v", hd.deleted)
	}
}
