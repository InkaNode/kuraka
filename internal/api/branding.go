package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/InkaNode/kuraka/internal/store"
)

const maxBrandingAsset = 512 << 10

var brandingApps = map[string]bool{"default": true, "kuraka": true, "stalwart": true, "desk": true}
var brandingColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
var brandingAssetID = regexp.MustCompile(`^[0-9a-f]{32}$`)
var rasterTypes = map[string]bool{"image/png": true, "image/jpeg": true, "image/webp": true, "image/gif": true}

func (s *Server) brandingRoutes(route func(string, string, bool, bool, endpoint)) {
	route("GET /api/v1/branding/{app}", "branding.public", true, false, s.publicBranding)
	route("GET /api/v1/branding", "branding.get", false, true, s.branding)
	route("PUT /api/v1/branding/{app}", "branding.update", false, true, s.updateBranding)
	route("POST /api/v1/branding/assets", "branding.asset.create", false, true, s.createBrandingAsset)
}

// resolveBranding takes each field from the first non-empty of tenant[app],
// tenant["default"], global[app] and global["default"]. Stalwart is shared
// by all tenants, so tenant rows never apply to it; Libredesk only takes the
// name, logo and icon.
func resolveBranding(app string, tenant, global map[string]store.Branding) store.Branding {
	layers := []store.Branding{tenant[app], tenant["default"], global[app], global["default"]}
	if app == "stalwart" {
		layers = layers[2:]
	}
	var out store.Branding
	for _, l := range layers {
		dst := []*string{&out.Name, &out.Logo, &out.Icon, &out.Color, &out.GradientFrom, &out.GradientTo, &out.Banner, &out.MaintenanceMessage}
		src := []string{l.Name, l.Logo, l.Icon, l.Color, l.GradientFrom, l.GradientTo, l.Banner, l.MaintenanceMessage}
		for i, p := range dst {
			if *p == "" {
				*p = src[i]
			}
		}
	}
	if app == "desk" {
		out = store.Branding{Name: out.Name, Logo: out.Logo, Icon: out.Icon}
	}
	return out
}

// resolvedBranding returns the app's branding for a tenant (nil: global),
// with images as absolute URLs for browsers and Libredesk.
func (s *Server) resolvedBranding(ctx context.Context, app string, tenantID *int64) (store.Branding, error) {
	global, err := s.st.GetBranding(ctx, nil)
	if err != nil {
		return store.Branding{}, err
	}
	var tenant map[string]store.Branding
	if tenantID != nil {
		if tenant, err = s.st.GetBranding(ctx, tenantID); err != nil {
			return store.Branding{}, err
		}
	}
	b := resolveBranding(app, tenant, global)
	for _, p := range []*string{&b.Logo, &b.Icon, &b.Banner} {
		if *p != "" {
			*p = s.cfg.PublicURL + "/branding/assets/" + *p
		}
	}
	return b, nil
}

// publicBranding is read before sign-in. A signed-in tenant user gets the
// tenant's Kuraka branding; everyone else the global one.
func (s *Server) publicBranding(w http.ResponseWriter, r *http.Request) (any, error) {
	app := r.PathValue("app")
	if app != "kuraka" && app != "stalwart" {
		return nil, store.ErrNotFound
	}
	var tenant *int64
	if app == "kuraka" && s.authenticate(w, r) == nil {
		tenant = state(r).user.TenantID
	}
	return s.resolvedBranding(r.Context(), app, tenant)
}

// brandingAsset serves uploaded images. Ids are random and never reused, so
// browsers may cache them for good.
func (s *Server) brandingAsset(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !brandingAssetID.MatchString(id) {
		writeError(w, 404, "not_found", "Resource not found")
		return
	}
	contentType, data, err := s.st.GetBrandingAsset(r.Context(), id)
	if err != nil {
		s.fail(w, err)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	if contentType == "image/svg+xml" {
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	}
	w.WriteHeader(200)
	_, _ = w.Write(data)
}

// brandingTenant reads the optional ?tenant= scope; nil is global.
func (s *Server) brandingTenant(r *http.Request) (*int64, error) {
	raw := r.URL.Query().Get("tenant")
	if raw == "" {
		return nil, nil
	}
	id, err := positiveID(raw, "tenant")
	if err != nil {
		return nil, err
	}
	if _, err := s.st.GetTenant(r.Context(), id); err != nil {
		return nil, err
	}
	state(r).tenant = &id
	return &id, nil
}

func (s *Server) branding(_ http.ResponseWriter, r *http.Request) (any, error) {
	tenant, err := s.brandingTenant(r)
	if err != nil {
		return nil, err
	}
	scope, err := s.st.GetBranding(r.Context(), tenant)
	if err != nil {
		return nil, err
	}
	out := map[string]any{"scope": scope}
	if tenant != nil {
		global, err := s.st.GetBranding(r.Context(), nil)
		if err != nil {
			return nil, err
		}
		out["global"] = global
	}
	return out, nil
}

func (s *Server) updateBranding(_ http.ResponseWriter, r *http.Request) (any, error) {
	app := r.PathValue("app")
	if !brandingApps[app] {
		return nil, invalid("app")
	}
	tenant, err := s.brandingTenant(r)
	if err != nil {
		return nil, err
	}
	if tenant != nil && app == "stalwart" {
		return nil, invalid("app: Stalwart branding is global only")
	}
	var b store.Branding
	if err := decode(r, &b); err != nil {
		return nil, err
	}
	b.Name = strings.TrimSpace(b.Name)
	if utf8.RuneCountInString(b.Name) > 64 || strings.IndexFunc(b.Name, unicode.IsControl) >= 0 {
		return nil, invalid("name")
	}
	if app != "kuraka" && b.MaintenanceMessage != "" {
		return nil, invalid("maintenance_message: only Kuraka branding applies")
	}
	if strings.IndexFunc(b.MaintenanceMessage, func(r rune) bool { return r != '\n' && unicode.IsControl(r) }) >= 0 {
		return nil, invalid("maintenance_message")
	}
	b.MaintenanceMessage = strings.TrimSpace(b.MaintenanceMessage)
	if utf8.RuneCountInString(b.MaintenanceMessage) > 500 {
		return nil, invalid("maintenance_message")
	}
	for _, c := range []*string{&b.Color, &b.GradientFrom, &b.GradientTo} {
		if *c != "" && !brandingColor.MatchString(*c) {
			return nil, invalid("color")
		}
		*c = strings.ToLower(*c)
	}
	if app == "desk" && (b.Color != "" || b.GradientFrom != "" || b.GradientTo != "" || b.Banner != "") {
		return nil, invalid("desk branding: only name, logo and icon apply")
	}
	for _, id := range []string{b.Logo, b.Icon, b.Banner} {
		if id == "" {
			continue
		}
		if !brandingAssetID.MatchString(id) {
			return nil, invalid("image")
		}
		if _, _, err := s.st.GetBrandingAsset(r.Context(), id); errors.Is(err, store.ErrNotFound) {
			return nil, invalid("image")
		} else if err != nil {
			return nil, err
		}
	}
	if err := s.st.SetBranding(r.Context(), tenant, app, b); err != nil {
		return nil, err
	}
	if tenant == nil && (app == "kuraka" || app == "default") {
		if err := s.writeMaintenancePage(r.Context()); err != nil {
			s.log.Warn("maintenance page render failed", "error", err)
		}
	}
	// Uploads younger than an hour may belong to a form not saved yet.
	if err := s.st.DeleteUnusedBrandingAssets(r.Context(), s.now().Add(-time.Hour)); err != nil {
		s.log.Warn("branding asset cleanup failed", "error", err)
	}
	desks := []deskBranding{}
	if app == "default" || app == "desk" {
		desks = s.pushDeskBranding(r.Context(), tenant)
	}
	return map[string]any{"saved": b, "desks": desks}, nil
}

type deskBranding struct {
	ID    int64  `json:"id"`
	URL   string `json:"url"`
	Error string `json:"error,omitempty"`
}

// pushDeskBranding applies the resolved desk branding to every ready
// helpdesk in scope (nil: all). Failures are reported, never returned.
func (s *Server) pushDeskBranding(ctx context.Context, tenant *int64) []deskBranding {
	out := []deskBranding{}
	rows, err := s.st.ListHelpdesks(ctx, tenant)
	if err != nil {
		s.log.Error("helpdesk branding failed", "error", err)
		return out
	}
	for _, h := range rows {
		if h.Status != "ready" {
			continue
		}
		applied, err := s.applyDeskBranding(ctx, h)
		if !applied && err == nil {
			continue
		}
		row := deskBranding{ID: h.ID, URL: h.URL}
		if err != nil {
			// Helpdesk errors can contain upstream-controlled text.
			s.log.Warn("helpdesk branding failed", "helpdesk_id", h.ID, "error", err)
			row.Error = "Helpdesk branding failed"
		}
		out = append(out, row)
	}
	return out
}

// applyDeskBranding sends the helpdesk's resolved name, logo and favicon.
// Nothing is sent when all three are empty, so Libredesk keeps its own.
func (s *Server) applyDeskBranding(ctx context.Context, h store.Helpdesk) (bool, error) {
	b, err := s.resolvedBranding(ctx, "desk", &h.TenantID)
	if err != nil || b == (store.Branding{}) {
		return false, err
	}
	client, err := s.helpdeskClient(h)
	if err != nil {
		return true, err
	}
	return true, client.ApplyBranding(ctx, b.Name, b.Logo, b.Icon)
}

// createBrandingAsset stores an image sent as a base64 data URL. The
// declared type must match the bytes; SVG is served with a sandbox CSP.
func (s *Server) createBrandingAsset(_ http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		Data string `json:"data"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	rest, ok := strings.CutPrefix(in.Data, "data:")
	meta, payload, found := strings.Cut(rest, ",")
	contentType, isBase64 := strings.CutSuffix(strings.ToLower(meta), ";base64")
	if !ok || !found || !isBase64 {
		return nil, invalid("image: expected a base64 data URL")
	}
	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil || len(data) == 0 {
		return nil, invalid("image")
	}
	if len(data) > maxBrandingAsset {
		return nil, invalid("image: larger than 512 KiB")
	}
	sniffed := http.DetectContentType(data)
	switch {
	case rasterTypes[contentType] && sniffed == contentType:
	case contentType == "image/svg+xml" && bytes.Contains(data, []byte("<svg")) &&
		(strings.HasPrefix(sniffed, "text/xml") || strings.HasPrefix(sniffed, "text/plain")):
	default:
		return nil, invalid("image type")
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, err
	}
	id := hex.EncodeToString(random[:])
	state(r).target = id
	if err := s.st.CreateBrandingAsset(r.Context(), id, contentType, data); err != nil {
		return nil, err
	}
	return reply{201, map[string]string{"id": id}}, nil
}
