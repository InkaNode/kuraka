package api

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"mime"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/InkaNode/kuraka/internal/addons"
	"github.com/InkaNode/kuraka/internal/store"
	"github.com/InkaNode/kuraka/web"
)

var addonFilename = regexp.MustCompile(`^(stalwart|desk)\.(js|css|json)$`)
var addonMissing = regexp.MustCompile(`^[a-z0-9-]{1,60}$`)

// A quoted CSS URL accepts only this subset of the public image URL alphabet.
// Quotes, backslashes and parentheses are deliberately excluded.
var addonLogoURL = regexp.MustCompile(`^https?://[A-Za-z0-9._~:/?#@!$&*+,;=%-]+$`)

func (s *Server) addonRoutes(route func(string, string, bool, bool, endpoint)) {
	route("GET /api/v1/addons", "addon.list", false, true, s.addonsStatus)
	route("PUT /api/v1/addons/{app}", "addon.update", false, true, s.updateAddon)
}

func addonApp(app string) bool { return app == "stalwart" || app == "desk" }

type addonBranding struct {
	Name         string `json:"name"`
	Logo         string `json:"logo"`
	Icon         string `json:"icon"`
	Color        string `json:"color"`
	GradientFrom string `json:"gradient_from"`
	GradientTo   string `json:"gradient_to"`
	Disabled     bool   `json:"disabled"`
}

func (s *Server) addonFile(w http.ResponseWriter, r *http.Request) {
	parts := addonFilename.FindStringSubmatch(r.PathValue("file"))
	if parts == nil {
		s.fail(w, store.ErrNotFound)
		return
	}
	app, ext := parts[1], parts[2]
	disabled, err := s.st.GetSetting(r.Context(), "addon_disabled_"+app)
	if err != nil {
		s.fail(w, err)
		return
	}
	var data []byte
	contentType := "text/javascript; charset=utf-8"
	switch ext {
	case "json":
		b, err := s.resolvedBranding(r.Context(), app, nil)
		if err != nil {
			s.fail(w, err)
			return
		}
		writeAddonHeaders(w)
		writeJSON(w, http.StatusOK, addonBranding{
			Name: b.Name, Logo: b.Logo, Icon: b.Icon, Color: b.Color,
			GradientFrom: b.GradientFrom, GradientTo: b.GradientTo, Disabled: disabled == "true",
		})
		return
	case "css":
		contentType = "text/css"
		if disabled == "true" {
			data = []byte("/* kuraka addon disabled */")
		} else {
			b, err := s.resolvedBranding(r.Context(), app, nil)
			if err != nil {
				s.fail(w, err)
				return
			}
			data = []byte(addonCSS(b))
		}
	default:
		if disabled == "true" {
			data = []byte("/* kuraka addon disabled */")
		}
	}
	if disabled != "true" {
		embedded, err := web.Addon(app + "." + ext)
		if err != nil {
			s.fail(w, err)
			return
		}
		data = append(data, embedded...)
	}
	writeAddonHeaders(w)
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func writeAddonHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.Header().Set("X-Content-Type-Options", "nosniff")
}

func addonCSS(b store.Branding) string {
	var css strings.Builder
	css.WriteString(":root{")
	if brandingColor.MatchString(b.Color) {
		hover, foreground := addonColor(b.Color)
		fmt.Fprintf(&css, "--kuraka-color:%s;--kuraka-color-hover:%s;--kuraka-color-foreground:%s;", b.Color, hover, foreground)
	}
	for _, c := range []struct{ name, value string }{
		{"gradient-from", b.GradientFrom}, {"gradient-to", b.GradientTo},
	} {
		if brandingColor.MatchString(c.value) {
			fmt.Fprintf(&css, "--kuraka-%s:%s;", c.name, c.value)
		}
	}
	if addonLogoURL.MatchString(b.Logo) {
		fmt.Fprintf(&css, `--kuraka-logo:url("%s");`, b.Logo)
	}
	css.WriteString("}\n")
	return css.String()
}

// addonColor uses HSL lightness for hover and WCAG relative luminance for text,
// matching the panel's onColor threshold.
func addonColor(color string) (hover, foreground string) {
	var rgb [3]float64
	luminance := 0.0
	for i, weight := range []float64{0.2126, 0.7152, 0.0722} {
		n, _ := strconv.ParseUint(color[1+2*i:3+2*i], 16, 8)
		c := float64(n) / 255
		rgb[i] = c
		linear := c / 12.92
		if c > 0.04045 {
			linear = math.Pow((c+0.055)/1.055, 2.4)
		}
		luminance += weight * linear
	}
	foreground = "#ffffff"
	if luminance > 0.179 {
		foreground = "#1c1917"
	}
	r, g, b := rgb[0], rgb[1], rgb[2]
	hi, lo := math.Max(r, math.Max(g, b)), math.Min(r, math.Min(g, b))
	d, l := hi-lo, (hi+lo)/2
	h, saturation := 0.0, 0.0
	if d != 0 {
		saturation = d / (1 - math.Abs(2*l-1))
		switch hi {
		case r:
			h = math.Mod((g-b)/d, 6)
		case g:
			h = (b-r)/d + 2
		default:
			h = (r-g)/d + 4
		}
		h *= 60
		if h < 0 {
			h += 360
		}
	}
	return fmt.Sprintf("hsl(%.2f %.2f%% %.2f%%)", h, saturation*100, math.Max(0, l-0.08)*100), foreground
}

type addonReport struct {
	At      time.Time `json:"at"`
	Version string    `json:"version"`
	Missing []string  `json:"missing"`
}

func (s *Server) addonReport(w http.ResponseWriter, r *http.Request) {
	app := r.PathValue("app")
	if !addonApp(app) {
		s.fail(w, invalid("app"))
		return
	}
	contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || contentType != "application/json" {
		s.fail(w, invalid("content type"))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	var in struct {
		Version *string   `json:"version"`
		Missing *[]string `json:"missing"`
	}
	if err := decode(r, &in); err != nil {
		s.fail(w, invalid("report"))
		return
	}
	if in.Version == nil || utf8.RuneCountInString(*in.Version) > 40 ||
		strings.IndexFunc(*in.Version, func(r rune) bool { return !unicode.IsPrint(r) }) >= 0 {
		s.fail(w, invalid("version"))
		return
	}
	if in.Missing == nil || len(*in.Missing) > 20 {
		s.fail(w, invalid("missing"))
		return
	}
	for _, missing := range *in.Missing {
		if !addonMissing.MatchString(missing) {
			s.fail(w, invalid("missing"))
			return
		}
	}
	now := s.now()
	if !s.addonLimits.allow(app+":"+clientIP(r), now) {
		s.fail(w, rateLimited)
		return
	}
	value := ""
	if len(*in.Missing) != 0 {
		data, err := json.Marshal(addonReport{At: now, Version: *in.Version, Missing: *in.Missing})
		if err != nil {
			s.fail(w, err)
			return
		}
		value = string(data)
	}
	if err := s.st.SetSetting(r.Context(), "addon_report_"+app, value); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Reports use a rolling hour per app/IP, independently of login lockouts.
// The key cap bounds anonymous memory use without evicting active limits.
type addonReportLimiter struct {
	mu      sync.Mutex
	entries map[string][]time.Time
	pruned  time.Time
}

func (l *addonReportLimiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := now.Add(-time.Hour)
	if l.entries == nil {
		l.entries = make(map[string][]time.Time)
	}
	if l.pruned.IsZero() || !now.Before(l.pruned.Add(time.Minute)) {
		for k, times := range l.entries {
			if !times[len(times)-1].After(cutoff) {
				delete(l.entries, k)
			}
		}
		l.pruned = now
	}
	times := l.entries[key]
	_, exists := l.entries[key]
	i := 0
	for i < len(times) && !times[i].After(cutoff) {
		i++
	}
	times = times[i:]
	if len(times) >= 10 || !exists && len(l.entries) >= maxFailureKeys {
		return false
	}
	l.entries[key] = append(times, now)
	return true
}

type addonStatus struct {
	Disabled  bool              `json:"disabled"`
	Supported map[string]string `json:"supported"`
	Report    *addonReport      `json:"report"`
}

func (s *Server) addonsBody(ctx context.Context) (map[string]addonStatus, error) {
	out := make(map[string]addonStatus, 2)
	for _, app := range []string{"stalwart", "desk"} {
		disabled, err := s.st.GetSetting(ctx, "addon_disabled_"+app)
		if err != nil {
			return nil, err
		}
		raw, err := s.st.GetSetting(ctx, "addon_report_"+app)
		if err != nil {
			return nil, err
		}
		row := addonStatus{Disabled: disabled == "true", Supported: map[string]string{"libredesk": addons.Supported["libredesk"]}}
		if app == "stalwart" {
			row.Supported = map[string]string{"stalwart": addons.Supported["stalwart"], "webui": addons.Supported["webui"]}
		}
		if raw != "" {
			var report addonReport
			if err := json.Unmarshal([]byte(raw), &report); err != nil {
				return nil, err
			}
			row.Report = &report
		}
		out[app] = row
	}
	return out, nil
}

func (s *Server) addonsStatus(_ http.ResponseWriter, r *http.Request) (any, error) {
	return s.addonsBody(r.Context())
}

func (s *Server) updateAddon(_ http.ResponseWriter, r *http.Request) (any, error) {
	app := r.PathValue("app")
	if !addonApp(app) {
		return nil, invalid("app")
	}
	var in struct {
		Disabled *bool `json:"disabled"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	if in.Disabled == nil {
		return nil, invalid("disabled")
	}
	value := ""
	if *in.Disabled {
		value = "true"
	}
	state(r).detail = app + " disabled=" + strconv.FormatBool(*in.Disabled)
	if err := s.st.SetSetting(r.Context(), "addon_disabled_"+app, value); err != nil {
		return nil, err
	}
	return s.addonsBody(r.Context())
}
