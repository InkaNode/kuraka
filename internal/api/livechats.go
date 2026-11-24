package api

import (
	"cmp"
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/InkaNode/kuraka/internal/authz"
	"github.com/InkaNode/kuraka/internal/libredesk"
	"github.com/InkaNode/kuraka/internal/store"
)

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

var liveChatMissing = &apiError{status: 404, code: "livechat_missing", message: "Live chat no longer exists in the helpdesk"}

func (s *Server) liveChatRoutes(route func(string, string, bool, bool, endpoint)) {
	route("GET /api/v1/domains/{id}/livechats", "livechat.list", false, false, s.liveChats)
	route("POST /api/v1/domains/{id}/livechats", "livechat.create", false, false, s.createLiveChat)
	route("GET /api/v1/domains/{id}/livechats/{chat}", "livechat.get", false, false, s.liveChat)
	route("PUT /api/v1/domains/{id}/livechats/{chat}", "livechat.update", false, false, s.updateLiveChat)
	route("DELETE /api/v1/domains/{id}/livechats/{chat}", "livechat.delete", false, false, s.deleteLiveChat)
	route("GET /api/v1/domains/{id}/helpdesks/{helpdesk}/teams", "livechat.teams", false, false, s.helpdeskTeams)
	route("POST /api/v1/domains/{id}/helpdesks/{helpdesk}/teams", "livechat.teams.create", false, false, s.createHelpdeskTeam)
}

func httpURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != "" && !strings.ContainsAny(u.Host, " *")
}

// cleanLiveChat trims and validates the fields; a website without a scheme gets
// https:// and allowed domains are reduced to hosts.
func cleanLiveChat(lc *libredesk.LiveChat) error {
	for _, p := range []*string{&lc.Name, &lc.Website, &lc.Color, &lc.LogoURL, &lc.Greeting, &lc.Position, &lc.Theme, &lc.LauncherLogoURL} {
		*p = strings.TrimSpace(*p)
	}
	if lc.Website != "" && !strings.Contains(lc.Website, "://") {
		lc.Website = "https://" + lc.Website
	}
	if lc.Domains != nil {
		clean := make([]string, 0, len(lc.Domains))
		for _, d := range lc.Domains {
			d = strings.ToLower(strings.TrimSpace(d))
			if i := strings.Index(d, "://"); i >= 0 {
				d = d[i+3:]
			}
			d, _, _ = strings.Cut(d, "/")
			if d == "" || slices.Contains(clean, d) {
				continue
			}
			// Libredesk accepts a "*." prefix as the only wildcard.
			if strings.ContainsAny(d, " ,") || strings.Contains(strings.TrimPrefix(d, "*."), "*") {
				return invalid("allowed_domains")
			}
			clean = append(clean, d)
		}
		if len(clean) > 50 {
			return invalid("allowed_domains")
		}
		lc.Domains = clean
	}
	switch {
	case lc.Name == "" || utf8.RuneCountInString(lc.Name) > 128:
		return invalid("name")
	case !httpURL(lc.Website):
		return invalid("website_url")
	case !hexColor.MatchString(lc.Color):
		return invalid("color")
	case lc.LogoURL != "" && !httpURL(lc.LogoURL):
		return invalid("logo_url")
	case utf8.RuneCountInString(lc.Greeting) > 500:
		return invalid("greeting")
	case lc.Position != "left" && lc.Position != "right":
		return invalid("position")
	case !slices.Contains([]string{"", "light", "dark", "system"}, lc.Theme):
		return invalid("theme")
	case lc.LauncherLogoURL != "" && !httpURL(lc.LauncherLogoURL):
		return invalid("launcher_logo_url")
	}
	return cleanLiveChatFlow(lc)
}

// cleanLiveChatFlow validates the flow settings; nil ones are left alone.
func cleanLiveChatFlow(lc *libredesk.LiveChat) error {
	if p := lc.PreChat; p != nil {
		// A waiting chat needs the visitor's email to answer later; Libredesk asks for it only then.
		if o := lc.Offline; o != nil && (o.Mode == "wait" || o.Mode == "both") {
			p.Email.Shown = true
		}
		for _, f := range []*libredesk.FormField{&p.LastName, &p.Email, &p.Phone} {
			f.Required = f.Shown && f.Required
		}
	}
	if m := lc.Menu; m != nil {
		m.AgentButtonText = strings.TrimSpace(m.AgentButtonText)
		if len(m.Items) > 8 || utf8.RuneCountInString(m.AgentButtonText) > 60 {
			return invalid("menu")
		}
		for i := range m.Items {
			item := &m.Items[i]
			item.Label, item.Answer = strings.TrimSpace(item.Label), strings.TrimSpace(item.Answer)
			if item.Label == "" || item.Answer == "" || utf8.RuneCountInString(item.Label) > 60 || utf8.RuneCountInString(item.Answer) > 1000 {
				return invalid("menu")
			}
		}
		if m.Items == nil {
			m.Items = []libredesk.MenuItem{}
		}
	}
	if lc.TeamID != nil && *lc.TeamID < 0 {
		return invalid("team_id")
	}
	if o := lc.Offline; o != nil {
		o.Email = strings.TrimSpace(o.Email)
		switch o.Mode {
		case "", "wait":
			if o.Email != "" && !validEmail(o.Email) {
				return invalid("offline_email")
			}
		case "both", "email":
			if !validEmail(o.Email) {
				return invalid("offline_email")
			}
		default:
			return invalid("offline")
		}
	}
	if m := lc.InactivityClose; m != nil && (*m < 0 || *m > 7*24*60) {
		return invalid("inactivity_close")
	}
	return nil
}

// checkTeam makes sure the chosen team exists; its assignment is left as is,
// as it also applies to the team's emails.
func checkTeam(ctx context.Context, client HelpdeskAPI, lc libredesk.LiveChat) error {
	if lc.TeamID == nil || *lc.TeamID == 0 {
		return nil
	}
	teams, err := client.ListTeams(ctx)
	if err != nil {
		return helpdeskFailure("Teams could not be read", err)
	}
	if !slices.ContainsFunc(teams, func(t libredesk.Team) bool { return t.ID == *lc.TeamID }) {
		return invalid("team_id")
	}
	return nil
}

// teamsClient returns the client of the domain's helpdesk in the path.
func (s *Server) teamsClient(r *http.Request) (HelpdeskAPI, error) {
	d, err := s.authorizedDomain(r)
	if err != nil {
		return nil, err
	}
	id, err := positiveID(r.PathValue("helpdesk"), "helpdesk")
	if err != nil {
		return nil, err
	}
	_, client, err := s.tenantHelpdesk(r.Context(), d, id)
	return client, err
}

// createHelpdeskTeam creates a round robin team, meant for one chat.
func (s *Server) createHelpdeskTeam(_ http.ResponseWriter, r *http.Request) (any, error) {
	client, err := s.teamsClient(r)
	if err != nil {
		return nil, err
	}
	var in struct {
		Name string `json:"name"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	in.Name = strings.TrimSpace(in.Name)
	if n := utf8.RuneCountInString(in.Name); n < 1 || n > 50 || strings.IndexFunc(in.Name, unicode.IsControl) >= 0 {
		return nil, invalid("name")
	}
	id, err := client.CreateTeam(r.Context(), in.Name)
	if err != nil {
		return nil, helpdeskFailure("Team creation failed", err)
	}
	return reply{201, map[string]any{"id": id, "name": in.Name}}, nil
}

func (s *Server) helpdeskTeams(_ http.ResponseWriter, r *http.Request) (any, error) {
	client, err := s.teamsClient(r)
	if err != nil {
		return nil, err
	}
	teams, err := client.ListTeams(r.Context())
	if err != nil {
		return nil, helpdeskFailure("Teams could not be read", err)
	}
	return teams, nil
}

func liveChatJSON(lc store.LiveChat) map[string]any {
	return map[string]any{"id": lc.ID, "helpdesk_id": lc.HelpdeskID, "inbox_id": lc.InboxID, "name": lc.Name, "website_url": lc.WebsiteURL, "created_at": lc.CreatedAt}
}

// liveChatDetail adds the editable settings and what the widget snippet needs.
func liveChatDetail(lc store.LiveChat, settings libredesk.LiveChat, rootURL string) map[string]any {
	out := liveChatJSON(lc)
	out["settings"] = settings
	out["widget"] = map[string]string{"base_url": rootURL, "inbox_uuid": lc.InboxUUID}
	return out
}

// widgetBase is the helpdesk's public address: https://<name> for a helpdesk
// provisioned by Kuraka, else the Libredesk Root URL.
func widgetBase(ctx context.Context, h store.Helpdesk, client HelpdeskAPI) (string, error) {
	if deskContainer(h) != "" {
		return deskURL(h), nil
	}
	return client.RootURL(ctx)
}

// tenantHelpdesk returns a ready helpdesk of the domain's tenant and its client.
func (s *Server) tenantHelpdesk(ctx context.Context, d store.Domain, id int64) (store.Helpdesk, HelpdeskAPI, error) {
	h, err := s.st.GetHelpdesk(ctx, id)
	if err != nil {
		return h, nil, err
	}
	if h.TenantID != d.TenantID {
		return h, nil, authz.ErrNotFound
	}
	if h.Status == "pending" {
		return h, nil, helpdeskNotReady
	}
	client, err := s.helpdeskClient(h)
	return h, client, err
}

func (s *Server) authorizedLiveChat(r *http.Request) (store.LiveChat, store.Helpdesk, HelpdeskAPI, error) {
	d, err := s.authorizedDomain(r)
	if err != nil {
		return store.LiveChat{}, store.Helpdesk{}, nil, err
	}
	id, err := positiveID(r.PathValue("chat"), "chat")
	if err != nil {
		return store.LiveChat{}, store.Helpdesk{}, nil, err
	}
	lc, err := s.st.GetLiveChat(r.Context(), id)
	if err != nil {
		return lc, store.Helpdesk{}, nil, err
	}
	if lc.DomainID != d.ID {
		return lc, store.Helpdesk{}, nil, authz.ErrNotFound
	}
	h, client, err := s.tenantHelpdesk(r.Context(), d, lc.HelpdeskID)
	return lc, h, client, err
}

func liveChatFailure(message string, err error) error {
	if errors.Is(err, libredesk.ErrNotFound) {
		return liveChatMissing
	}
	return helpdeskFailure(message, err)
}

func (s *Server) liveChats(_ http.ResponseWriter, r *http.Request) (any, error) {
	d, err := s.authorizedDomain(r)
	if err != nil {
		return nil, err
	}
	list, err := s.st.ListLiveChats(r.Context(), d.ID)
	out := make([]any, 0, len(list))
	for _, lc := range list {
		out = append(out, liveChatJSON(lc))
	}
	return out, err
}

func (s *Server) createLiveChat(_ http.ResponseWriter, r *http.Request) (any, error) {
	d, err := s.authorizedDomain(r)
	if err != nil {
		return nil, err
	}
	var in struct {
		HelpdeskID int64  `json:"helpdesk_id"`
		Language   string `json:"language"`
		libredesk.LiveChat
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	if err := cleanLiveChat(&in.LiveChat); err != nil {
		return nil, err
	}
	h, client, err := s.tenantHelpdesk(r.Context(), d, in.HelpdeskID)
	if err != nil {
		return nil, err
	}
	if in.LogoURL == "" || in.LauncherLogoURL == "" {
		if b, err := s.resolvedBranding(r.Context(), "desk", &d.TenantID); err == nil {
			in.LogoURL = cmp.Or(in.LogoURL, b.Logo)
			in.LauncherLogoURL = cmp.Or(in.LauncherLogoURL, b.Icon)
		}
	}
	language := "en-US"
	if in.Language == "es" {
		language = "es-ES"
	}
	root, err := widgetBase(r.Context(), h, client)
	if err != nil {
		return nil, helpdeskFailure("Helpdesk URL is not available", err)
	}
	if err := checkTeam(r.Context(), client, in.LiveChat); err != nil {
		return nil, err
	}
	inboxID, uuid, err := client.CreateLiveChat(r.Context(), in.LiveChat, language)
	if err != nil {
		return nil, helpdeskFailure("Live chat creation failed", err)
	}
	lc, err := s.st.CreateLiveChat(r.Context(), store.LiveChat{DomainID: d.ID, HelpdeskID: in.HelpdeskID, InboxID: inboxID, InboxUUID: uuid, Name: in.Name, WebsiteURL: in.Website})
	if err != nil {
		ctx, cancel := cleanupContext(r.Context())
		defer cancel()
		_ = client.DeleteInbox(ctx, inboxID)
		return nil, err
	}
	return reply{201, liveChatDetail(lc, in.LiveChat, root)}, nil
}

func (s *Server) liveChat(_ http.ResponseWriter, r *http.Request) (any, error) {
	lc, h, client, err := s.authorizedLiveChat(r)
	if err != nil {
		return nil, err
	}
	settings, err := client.GetLiveChat(r.Context(), lc.InboxID)
	if err != nil {
		return nil, liveChatFailure("Live chat could not be read", err)
	}
	root, err := widgetBase(r.Context(), h, client)
	if err != nil {
		return nil, helpdeskFailure("Helpdesk URL is not available", err)
	}
	return liveChatDetail(lc, settings, root), nil
}

func (s *Server) updateLiveChat(_ http.ResponseWriter, r *http.Request) (any, error) {
	lc, h, client, err := s.authorizedLiveChat(r)
	if err != nil {
		return nil, err
	}
	var in libredesk.LiveChat
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	if err := cleanLiveChat(&in); err != nil {
		return nil, err
	}
	if in.LauncherLogoURL == "" {
		if b, err := s.resolvedBranding(r.Context(), "desk", &h.TenantID); err == nil {
			in.LauncherLogoURL = b.Icon
		}
	}
	root, err := widgetBase(r.Context(), h, client)
	if err != nil {
		return nil, helpdeskFailure("Helpdesk URL is not available", err)
	}
	if err := checkTeam(r.Context(), client, in); err != nil {
		return nil, err
	}
	if err := client.UpdateLiveChat(r.Context(), lc.InboxID, in); err != nil {
		return nil, liveChatFailure("Live chat update failed", err)
	}
	if err := s.st.RenameLiveChat(r.Context(), lc.ID, in.Name, in.Website); err != nil {
		return nil, err
	}
	lc.Name, lc.WebsiteURL = in.Name, in.Website
	return liveChatDetail(lc, in, root), nil
}

// deleteLiveChat also forgets a chat already deleted in the helpdesk.
func (s *Server) deleteLiveChat(_ http.ResponseWriter, r *http.Request) (any, error) {
	lc, _, client, err := s.authorizedLiveChat(r)
	if err != nil {
		return nil, err
	}
	if err := client.DeleteInbox(r.Context(), lc.InboxID); err != nil && !errors.Is(err, libredesk.ErrNotFound) {
		return nil, helpdeskFailure("Live chat deletion failed", err)
	}
	return reply{204, nil}, s.st.DeleteLiveChat(r.Context(), lc.ID)
}
