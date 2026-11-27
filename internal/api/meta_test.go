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

func (f *fakeHelpdesk) CreateMetaPage(_ context.Context, channel string, p libredesk.MetaPage, token string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.metaPages == nil {
		f.metaPages = map[int64]libredesk.MetaPage{}
	}
	if f.whatsAppTokens == nil {
		f.whatsAppTokens = map[int64]string{}
	}
	id := int64(len(f.metaPages) + 300)
	f.metaPages[id], f.whatsAppTokens[id] = p, token
	return id, nil
}
func (f *fakeHelpdesk) GetMetaPage(_ context.Context, id int64, _ string) (libredesk.MetaPage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.metaPages[id]
	if !ok {
		return p, libredesk.ErrNotFound
	}
	p.AccessToken, p.AppSecret = "", ""
	return p, nil
}
func (f *fakeHelpdesk) UpdateMetaPage(_ context.Context, id int64, _ string, p libredesk.MetaPage, token string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	old, ok := f.metaPages[id]
	if !ok {
		return libredesk.ErrNotFound
	}
	if p.AccessToken == "" {
		p.AccessToken = old.AccessToken
	}
	if p.AppSecret == "" {
		p.AppSecret = old.AppSecret
	}
	f.metaPages[id], f.whatsAppTokens[id] = p, token
	return nil
}

func TestMetaPageChannels(t *testing.T) {
	h, _, hd := integrations(t)
	a := h.tenant("A")
	da := h.domain(a, "a.test")
	ca := h.admin(a, "a@admin.test")
	ha := helpdesk(t, ca, a)
	base := fmt.Sprintf("/domains/%d/meta", da)
	in := map[string]any{"helpdesk_id": ha, "channel": "messenger", "name": "Página", "page_id": "777", "access_token": "EAApage", "app_secret": "abc"}

	bad := map[string]any{}
	for k, v := range in {
		bad[k] = v
	}
	bad["page_id"] = "x1"
	ca.do("POST", base, bad, 422)

	ch := object(t, ca.do("POST", base, in, 201))
	hook := ch["webhook"].(map[string]any)
	if hook["url"] != "https://desk.example/webhooks/meta/300" || hook["verify_token"] != hd.whatsAppTokens[300] {
		t.Fatalf("created: %v", ch)
	}
	path := fmt.Sprintf("%s/%.0f", base, ch["id"])
	ca.do("PUT", path, map[string]any{"name": "Ventas", "page_id": "888"}, 200)
	if p := hd.metaPages[300]; p.Name != "Ventas" || p.PageID != "888" || p.AccessToken != "EAApage" {
		t.Fatalf("updated: %+v", p)
	}
	if s := object(t, ca.do("GET", path, nil, 200))["settings"].(map[string]any); s["page_id"] != "888" || s["access_token"] != nil {
		t.Fatalf("get: %v", s)
	}
	in["channel"] = "instagram"
	if hook := object(t, ca.do("POST", base, in, 201))["webhook"].(map[string]any); hook["url"] != "https://desk.example/webhooks/meta/301" {
		t.Fatalf("instagram: %v", hook)
	}
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
