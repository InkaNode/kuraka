package api

import (
	"crypto/rand"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/InkaNode/kuraka/internal/authz"
	"github.com/InkaNode/kuraka/internal/libredesk"
	"github.com/InkaNode/kuraka/internal/store"
)

var metaMissing = &apiError{status: 404, code: "meta_missing", message: "Channel no longer exists in the helpdesk"}

func (s *Server) metaRoutes(route func(string, string, bool, bool, endpoint)) {
	route("GET /api/v1/domains/{id}/meta", "meta.list", false, false, s.metaChannels)
	route("POST /api/v1/domains/{id}/meta", "meta.create", false, false, s.createMetaChannel)
	route("GET /api/v1/domains/{id}/meta/{channel}", "meta.get", false, false, s.metaChannel)
	route("PUT /api/v1/domains/{id}/meta/{channel}", "meta.update", false, false, s.updateMetaChannel)
	route("DELETE /api/v1/domains/{id}/meta/{channel}", "meta.delete", false, false, s.deleteMetaChannel)
}

func metaID(v string) bool {
	return v != "" && len(v) <= 32 && strings.IndexFunc(v, func(r rune) bool { return r < '0' || r > '9' }) < 0
}

func metaSecret(v string) bool {
	return len(v) <= 2048 && strings.IndexFunc(v, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) < 0
}

// cleanWhatsApp trims and validates the fields; secrets are required only on create.
func cleanWhatsApp(w *libredesk.WhatsApp, create bool) error {
	for _, p := range []*string{&w.Name, &w.PhoneNumberID, &w.WABAID, &w.AccessToken, &w.AppSecret} {
		*p = strings.TrimSpace(*p)
	}
	switch {
	case w.Name == "" || utf8.RuneCountInString(w.Name) > 128:
		return invalid("name")
	case !metaID(w.PhoneNumberID):
		return invalid("phone_number_id")
	case !metaID(w.WABAID):
		return invalid("waba_id")
	case !metaSecret(w.AccessToken) || create && w.AccessToken == "":
		return invalid("access_token")
	case !metaSecret(w.AppSecret) || create && w.AppSecret == "":
		return invalid("app_secret")
	}
	return nil
}

// metaFailure tells apart details Meta or Libredesk rejected from a helpdesk
// that failed.
func metaFailure(message string, err error) error {
	if errors.Is(err, libredesk.ErrNotFound) {
		return metaMissing
	}
	if ae, ok := errors.AsType[*libredesk.APIError](err); ok && (ae.Status == http.StatusBadRequest || ae.Status == http.StatusUnprocessableEntity) {
		return &apiError{status: 422, code: "whatsapp_rejected", message: "WhatsApp details were rejected", cause: err}
	}
	return helpdeskFailure(message, err)
}

func metaJSON(mc store.MetaChannel) map[string]any {
	return map[string]any{"id": mc.ID, "channel": mc.Channel, "helpdesk_id": mc.HelpdeskID, "name": mc.Name, "created_at": mc.CreatedAt}
}

// metaDetail adds the settings and what Meta's webhook setup needs.
func metaDetail(mc store.MetaChannel, w libredesk.WhatsApp, rootURL string) map[string]any {
	out := metaJSON(mc)
	out["settings"] = map[string]string{"name": w.Name, "phone_number_id": w.PhoneNumberID, "waba_id": w.WABAID}
	out["webhook"] = map[string]string{
		"url":          strings.TrimRight(rootURL, "/") + "/webhooks/whatsapp/" + strconv.FormatInt(mc.InboxID, 10),
		"verify_token": mc.VerifyToken,
	}
	return out
}

func (s *Server) authorizedMetaChannel(r *http.Request) (store.MetaChannel, store.Helpdesk, HelpdeskAPI, error) {
	d, err := s.authorizedDomain(r)
	if err != nil {
		return store.MetaChannel{}, store.Helpdesk{}, nil, err
	}
	id, err := positiveID(r.PathValue("channel"), "channel")
	if err != nil {
		return store.MetaChannel{}, store.Helpdesk{}, nil, err
	}
	mc, err := s.st.GetMetaChannel(r.Context(), id)
	if err != nil {
		return mc, store.Helpdesk{}, nil, err
	}
	if mc.DomainID != d.ID {
		return mc, store.Helpdesk{}, nil, authz.ErrNotFound
	}
	h, client, err := s.tenantHelpdesk(r.Context(), d, mc.HelpdeskID)
	return mc, h, client, err
}

func (s *Server) metaChannels(_ http.ResponseWriter, r *http.Request) (any, error) {
	d, err := s.authorizedDomain(r)
	if err != nil {
		return nil, err
	}
	list, err := s.st.ListMetaChannels(r.Context(), d.ID)
	out := make([]any, 0, len(list))
	for _, mc := range list {
		out = append(out, metaJSON(mc))
	}
	return out, err
}

func (s *Server) createMetaChannel(_ http.ResponseWriter, r *http.Request) (any, error) {
	d, err := s.authorizedDomain(r)
	if err != nil {
		return nil, err
	}
	var in struct {
		HelpdeskID int64  `json:"helpdesk_id"`
		Channel    string `json:"channel"`
		libredesk.WhatsApp
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	// frugal: Messenger and Instagram join here once Libredesk has those channels.
	if in.Channel != "whatsapp" {
		return nil, invalid("channel")
	}
	if err := cleanWhatsApp(&in.WhatsApp, true); err != nil {
		return nil, err
	}
	h, client, err := s.tenantHelpdesk(r.Context(), d, in.HelpdeskID)
	if err != nil {
		return nil, err
	}
	root, err := widgetBase(r.Context(), h, client)
	if err != nil {
		return nil, helpdeskFailure("Helpdesk URL is not available", err)
	}
	token := rand.Text()
	inboxID, err := client.CreateWhatsApp(r.Context(), in.WhatsApp, token)
	if err != nil {
		return nil, metaFailure("WhatsApp creation failed", err)
	}
	mc, err := s.st.CreateMetaChannel(r.Context(), store.MetaChannel{DomainID: d.ID, HelpdeskID: in.HelpdeskID, Channel: in.Channel, InboxID: inboxID, Name: in.Name, VerifyToken: token})
	if err != nil {
		ctx, cancel := cleanupContext(r.Context())
		defer cancel()
		_ = client.DeleteInbox(ctx, inboxID)
		return nil, err
	}
	return reply{201, metaDetail(mc, in.WhatsApp, root)}, nil
}

func (s *Server) metaChannel(_ http.ResponseWriter, r *http.Request) (any, error) {
	mc, h, client, err := s.authorizedMetaChannel(r)
	if err != nil {
		return nil, err
	}
	w, err := client.GetWhatsApp(r.Context(), mc.InboxID)
	if err != nil {
		return nil, metaFailure("WhatsApp could not be read", err)
	}
	root, err := widgetBase(r.Context(), h, client)
	if err != nil {
		return nil, helpdeskFailure("Helpdesk URL is not available", err)
	}
	return metaDetail(mc, w, root), nil
}

func (s *Server) updateMetaChannel(_ http.ResponseWriter, r *http.Request) (any, error) {
	mc, h, client, err := s.authorizedMetaChannel(r)
	if err != nil {
		return nil, err
	}
	var in libredesk.WhatsApp
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	if err := cleanWhatsApp(&in, false); err != nil {
		return nil, err
	}
	root, err := widgetBase(r.Context(), h, client)
	if err != nil {
		return nil, helpdeskFailure("Helpdesk URL is not available", err)
	}
	if err := client.UpdateWhatsApp(r.Context(), mc.InboxID, in, mc.VerifyToken); err != nil {
		return nil, metaFailure("WhatsApp update failed", err)
	}
	if err := s.st.RenameMetaChannel(r.Context(), mc.ID, in.Name); err != nil {
		return nil, err
	}
	mc.Name = in.Name
	return metaDetail(mc, in, root), nil
}

// deleteMetaChannel also forgets a channel already deleted in the helpdesk.
func (s *Server) deleteMetaChannel(_ http.ResponseWriter, r *http.Request) (any, error) {
	mc, _, client, err := s.authorizedMetaChannel(r)
	if err != nil {
		return nil, err
	}
	if err := client.DeleteInbox(r.Context(), mc.InboxID); err != nil && !errors.Is(err, libredesk.ErrNotFound) {
		return nil, helpdeskFailure("WhatsApp deletion failed", err)
	}
	return reply{204, nil}, s.st.DeleteMetaChannel(r.Context(), mc.ID)
}
