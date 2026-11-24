package api

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/InkaNode/kuraka/internal/libredesk"
	"github.com/InkaNode/kuraka/internal/store"
)

func (f *fakeHelpdesk) CreateLiveChat(_ context.Context, lc libredesk.LiveChat, language string) (int64, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.liveChatErr != nil {
		return 0, "", f.liveChatErr
	}
	if f.liveChats == nil {
		f.liveChats = map[int64]libredesk.LiveChat{}
	}
	id := int64(len(f.liveChats) + 100)
	f.liveChats[id] = lc
	f.liveChatLanguage = language
	return id, fmt.Sprintf("uuid-%d", id), nil
}
func (f *fakeHelpdesk) GetLiveChat(_ context.Context, id int64) (libredesk.LiveChat, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	lc, ok := f.liveChats[id]
	if !ok {
		return lc, libredesk.ErrNotFound
	}
	return lc, nil
}
func (f *fakeHelpdesk) UpdateLiveChat(_ context.Context, id int64, lc libredesk.LiveChat) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.liveChats[id]; !ok {
		return libredesk.ErrNotFound
	}
	f.liveChats[id] = lc
	return nil
}
func (f *fakeHelpdesk) RootURL(context.Context) (string, error) { return "https://desk.example", nil }
func (f *fakeHelpdesk) ListTeams(context.Context) ([]libredesk.Team, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.teams, nil
}
func (f *fakeHelpdesk) CreateTeam(_ context.Context, name string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := int64(len(f.teams) + 10)
	f.teams = append(f.teams, libredesk.Team{ID: id, Name: name, AssignmentType: "Round robin"})
	return id, nil
}

func TestLiveChats(t *testing.T) {
	h, _, hd := integrations(t)
	a, b := h.tenant("A"), h.tenant("B")
	da, db := h.domain(a, "a.test"), h.domain(b, "b.test")
	ca, cb := h.admin(a, "a@admin.test"), h.admin(b, "b@admin.test")
	ha := helpdesk(t, ca, a)
	base := fmt.Sprintf("/domains/%d/livechats", da)
	in := map[string]any{"helpdesk_id": ha, "language": "es", "name": " Ventas ", "website_url": "www.a.test", "color": "#1a2b3c", "logo_url": "", "greeting": "Hola", "position": "left"}

	item := map[string]string{"label": "Pedidos", "answer": "Revisa tu correo."}
	for field, value := range map[string]any{"name": "", "website_url": "ftp://a.test", "color": "red", "logo_url": "javascript:x", "position": "top", "allowed_domains": []string{"a*.test"}, "theme": "blue", "launcher_logo_url": "javascript:x",
		"menu":             map[string]any{"enabled": true, "items": slices.Repeat([]any{item}, 9)},
		"offline":          map[string]string{"mode": "both", "email": "not-an-email"},
		"inactivity_close": -1,
		"team_id":          7,
	} {
		bad := map[string]any{}
		for k, v := range in {
			bad[k] = v
		}
		bad[field] = value
		ca.do("POST", base, bad, 422)
	}
	cb.do("POST", fmt.Sprintf("/domains/%d/livechats", db), in, 404)
	cb.do("POST", base, in, 404)

	chat := object(t, ca.do("POST", base, in, 201))
	widget := chat["widget"].(map[string]any)
	if widget["base_url"] != "https://desk.example" || widget["inbox_uuid"] != "uuid-100" || chat["name"] != "Ventas" || chat["website_url"] != "https://www.a.test" {
		t.Fatalf("created: %v", chat)
	}
	if hd.liveChatLanguage != "es-ES" || hd.liveChats[100].Position != "left" || hd.liveChats[100].Color != "#1a2b3c" {
		t.Fatalf("libredesk got %+v %q", hd.liveChats[100], hd.liveChatLanguage)
	}
	path := fmt.Sprintf("%s/%.0f", base, chat["id"])
	if list := array(t, ca.do("GET", base, nil, 200)); len(list) != 1 {
		t.Fatalf("list: %v", list)
	}
	cb.do("GET", path, nil, 404)
	cb.do("GET", fmt.Sprintf("/domains/%d/livechats/%.0f", db, chat["id"]), nil, 404)

	edit := map[string]any{"name": "Soporte", "website_url": "https://a.test", "color": "#000000", "logo_url": "https://a.test/logo.png", "greeting": "Buenas", "position": "right", "allowed_domains": []string{" https://Shop.test/x ", "*.b.test", "", "shop.test"}}
	cb.do("PUT", path, edit, 404)
	got := object(t, ca.do("PUT", path, edit, 200))
	if got["name"] != "Soporte" || hd.liveChats[100].Greeting != "Buenas" || !slices.Equal(hd.liveChats[100].Domains, []string{"shop.test", "*.b.test"}) {
		t.Fatalf("updated: %v", got)
	}
	settings := object(t, ca.do("GET", path, nil, 200))["settings"].(map[string]any)
	if settings["position"] != "right" || settings["logo_url"] != "https://a.test/logo.png" {
		t.Fatalf("settings: %v", settings)
	}

	hd.teams = []libredesk.Team{{ID: 3, Name: "Ventas", AssignmentType: "Manual", Members: 2}}
	if teams := array(t, ca.do("GET", fmt.Sprintf("/domains/%d/helpdesks/%d/teams", da, ha), nil, 200)); len(teams) != 1 {
		t.Fatalf("teams: %v", teams)
	}
	cb.do("GET", fmt.Sprintf("/domains/%d/helpdesks/%d/teams", db, ha), nil, 404)
	teamsPath := fmt.Sprintf("/domains/%d/helpdesks/%d/teams", da, ha)
	cb.do("POST", fmt.Sprintf("/domains/%d/helpdesks/%d/teams", db, ha), map[string]string{"name": "Chat"}, 404)
	ca.do("POST", teamsPath, map[string]string{"name": " "}, 422)
	ca.do("POST", teamsPath, map[string]string{"name": strings.Repeat("a", 51)}, 422)
	if team := object(t, ca.do("POST", teamsPath, map[string]string{"name": " Chat Soporte "}, 201)); team["id"] != 11.0 || hd.teams[1].Name != "Chat Soporte" {
		t.Fatalf("created team: %v %+v", team, hd.teams)
	}
	edit["team_id"] = 3
	edit["menu"] = map[string]any{"enabled": true, "items": []any{map[string]string{"label": " Pedidos ", "answer": "Revisa tu correo."}}, "agent_button_text": ""}
	edit["offline"] = map[string]string{"mode": "email", "email": "hola@a.test"}
	edit["prechat_form"] = map[string]any{"enabled": true, "email": map[string]bool{"shown": false, "required": true}}
	edit["inactivity_close"] = 45
	ca.do("PUT", path, edit, 200)
	saved := hd.liveChats[100]
	if *saved.TeamID != 3 || hd.teams[0].AssignmentType != "Manual" || saved.Menu.Items[0].Label != "Pedidos" || saved.Offline.Mode != "email" ||
		saved.PreChat.Email.Required || *saved.InactivityClose != 45 {
		t.Fatalf("flow settings: %+v", saved)
	}

	delete(hd.liveChats, 100)
	ca.do("GET", path, nil, 404)
	hd.deleteErr = libredesk.ErrNotFound
	cb.do("DELETE", path, nil, 404)
	ca.do("DELETE", path, nil, 204)
	if len(array(t, ca.do("GET", base, nil, 200))) != 0 || len(hd.deleted) != 1 {
		t.Fatal("delete did not forget the chat")
	}

	hd.deleteErr = nil
	hd.liveChatErr = errors.New("down")
	ca.do("POST", base, in, 502)
	if len(array(t, ca.do("GET", base, nil, 200))) != 0 {
		t.Fatal("failed create persisted a chat")
	}
	deniedAudit(t, cb, "livechat.create")

	hd.liveChatErr = nil
	sealed, err := h.s.box.Seal([]byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	desk, err := h.st.CreateHelpdesk(context.Background(), store.Helpdesk{TenantID: a, Kind: "libredesk", Name: "desk.a.test", URL: "http://kuraka-a-desk:9000", Status: "ready", APISecretEnc: sealed})
	if err != nil {
		t.Fatal(err)
	}
	in["helpdesk_id"] = desk.ID
	provisioned := object(t, ca.do("POST", base, in, 201))
	if base := provisioned["widget"].(map[string]any)["base_url"]; base != "https://desk.a.test" {
		t.Fatalf("provisioned base_url: %v", base)
	}
}

func TestCleanLiveChatFlowWaitShowsEmail(t *testing.T) {
	for mode, shown := range map[string]bool{"wait": true, "both": true, "email": false, "": false} {
		lc := libredesk.LiveChat{PreChat: &libredesk.PreChatForm{Enabled: true}, Offline: &libredesk.Offline{Mode: mode, Email: "a@b.test"}}
		if err := cleanLiveChatFlow(&lc); err != nil || lc.PreChat.Email.Shown != shown {
			t.Fatalf("mode %q: err %v, email shown %v", mode, err, lc.PreChat.Email.Shown)
		}
	}
}
