package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/InkaNode/kuraka/internal/addons"
	"github.com/InkaNode/kuraka/internal/provision"
	"github.com/InkaNode/kuraka/internal/releases"
	"github.com/InkaNode/kuraka/internal/store"
	"github.com/InkaNode/kuraka/internal/version"
)

const (
	libredeskRepo = "abhinavxd/libredesk"
	webuiRepo     = "stalwartlabs/webui"
	stalwartRepo  = "stalwartlabs/stalwart"
	kurakaRepo    = "InkaNode/kuraka"
	webuiSetting  = "webui_applied"
)

var deskHost = regexp.MustCompile(`^kuraka-[a-z0-9-]+-desk$`)
var stableTag = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)
var kurakaTag = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$`)

// updateState caches the latest release tags read from GitHub.
type updateState struct {
	checkMu   sync.Mutex // serializes checks
	requestMu sync.Mutex // serializes update requests written to the host
	mu        sync.Mutex // guards the fields below
	checkedAt time.Time
	libredesk string
	webui     string
	stalwart  string
	kuraka    releases.Release
	err       string
}

func (s *Server) updateRoutes(route func(string, string, bool, bool, endpoint)) {
	route("GET /api/v1/updates", "update.list", false, true, s.updatesStatus)
	route("POST /api/v1/updates/check", "update.check", false, true, s.checkUpdatesNow)
	route("POST /api/v1/updates/webui", "update.webui", false, true, s.updateWebUI)
	route("POST /api/v1/updates/stalwart", "update.stalwart", false, true, s.updateStalwart)
	route("POST /api/v1/updates/kuraka", "update.kuraka", false, true, s.updateKuraka)
	route("POST /api/v1/helpdesks/{id}/update", "helpdesk.update", false, true, s.updateHelpdesk)
}

// StartUpdateChecks reads GitHub releases a minute after start and then
// daily. The returned channel is closed when the loop has stopped.
func (s *Server) StartUpdateChecks(ctx context.Context) <-chan struct{} {
	if err := s.writeMaintenancePage(ctx); err != nil {
		s.log.Warn("maintenance page render failed", "error", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		timer := time.NewTimer(time.Minute)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
			}
			s.checkUpdates(ctx)
			timer.Reset(24 * time.Hour)
		}
	}()
	return done
}

// checkUpdates refreshes the cached tags, at most once every 30 seconds. A
// failed lookup keeps the previous tag.
func (s *Server) checkUpdates(ctx context.Context) {
	u := &s.updates
	u.checkMu.Lock()
	defer u.checkMu.Unlock()
	u.mu.Lock()
	recent := !u.checkedAt.IsZero() && s.now().Sub(u.checkedAt) < 30*time.Second
	u.mu.Unlock()
	if recent {
		return
	}
	libredesk, libredeskErr := s.releases.Latest(ctx, libredeskRepo)
	webui, webuiErr := s.releases.Latest(ctx, webuiRepo)
	stalwart, stalwartErr := s.releases.Latest(ctx, stalwartRepo)
	kuraka, kurakaErr := s.releases.LatestRelease(ctx, kurakaRepo)
	if ctx.Err() != nil {
		return
	}
	err := errors.Join(libredeskErr, webuiErr, stalwartErr, kurakaErr)
	if err != nil {
		s.log.Warn("update check failed", "error", err)
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	u.checkedAt, u.err = s.now(), ""
	if err != nil {
		u.err = err.Error()
	}
	if libredeskErr == nil {
		u.libredesk = libredesk
	}
	if webuiErr == nil {
		u.webui = webui
	}
	if stalwartErr == nil {
		u.stalwart = stalwart
	}
	if kurakaErr == nil {
		u.kuraka = kuraka
	}
}

func (s *Server) latestTags() (libredesk, webui, stalwart string) {
	s.updates.mu.Lock()
	defer s.updates.mu.Unlock()
	return s.updates.libredesk, s.updates.webui, s.updates.stalwart
}

func versionState(current, latest string) string {
	switch {
	case latest == "":
		return "unknown"
	case current == latest:
		return "current"
	}
	return "available"
}

func addonCompatibility(row map[string]any, component, latest string) {
	if row["state"] == "available" && !addons.Allows(component, latest) {
		row["state"] = "waiting"
		row["supported"] = addons.Supported[component]
	}
}

// deskContainer returns the container of a helpdesk provisioned by Kuraka,
// or "" for helpdesks connected by URL.
func deskContainer(h store.Helpdesk) string {
	u, err := url.Parse(h.URL)
	if err != nil || !deskHost.MatchString(u.Hostname()) {
		return ""
	}
	return u.Hostname()
}

// updateResult is written by deploy/apply-updates.sh. Current is the tag the
// compose project runs after the attempt.
type updateResult struct {
	Tag     string `json:"tag"`
	Commit  string `json:"commit"`
	Status  string `json:"status"`
	Message string `json:"message"`
	At      string `json:"at"`
	Current string `json:"current"`
}

// requestVersion reads the pending request <name>.json or last result
// <name>.result.json the host timer left in the updates directory. installed
// is the version shown when there is no result.
func (s *Server) requestVersion(name, installed, latest string) (current, status, message string) {
	current = installed
	if s.cfg.UpdatesDir == "" {
		return current, versionState(current, latest), ""
	}
	base := filepath.Join(s.cfg.UpdatesDir, name)
	var result updateResult
	if f, err := os.Open(base + ".result.json"); err == nil {
		err = json.NewDecoder(io.LimitReader(f, 64<<10)).Decode(&result)
		f.Close()
		if err != nil || !stableTag.MatchString(result.Tag) {
			result = updateResult{}
		}
	}
	switch {
	case stableTag.MatchString(result.Current):
		current = result.Current
	case result.Status == "ok":
		current = result.Tag
	}
	if _, err := os.Stat(base + ".json"); err == nil {
		return current, "updating", ""
	}
	if result.Status == "error" && result.Tag == latest {
		message = result.Message
		if len(message) > 300 {
			message = message[:300]
		}
		return current, "error", message
	}
	return current, versionState(current, latest), ""
}

func (s *Server) kurakaState(latest releases.Release) (status, message string) {
	if s.cfg.UpdatesDir != "" {
		base := filepath.Join(s.cfg.UpdatesDir, "kuraka")
		if _, err := os.Stat(base + ".json"); err == nil {
			return "updating", ""
		}
		var result updateResult
		if f, err := os.Open(base + ".result.json"); err == nil {
			err = json.NewDecoder(io.LimitReader(f, 64<<10)).Decode(&result)
			f.Close()
			if err == nil && result.Status == "error" && result.Tag == latest.Tag && result.Commit == latest.Commit {
				message = result.Message
				if len(message) > 300 {
					message = message[:300]
				}
				return "error", message
			}
		}
	}
	status = versionState("v"+version.Version, latest.Tag)
	if status == "current" && version.Commit != "" && latest.Commit != "" && version.Commit != latest.Commit {
		status = "available"
	}
	return status, ""
}

func (s *Server) updatesBody(ctx context.Context) (any, error) {
	s.updates.mu.Lock()
	checkedAt, libredesk, webui, stalwart, checkErr := s.updates.checkedAt, s.updates.libredesk, s.updates.webui, s.updates.stalwart, s.updates.err
	kuraka := s.updates.kuraka
	s.updates.mu.Unlock()
	applied, err := s.st.GetSetting(ctx, webuiSetting)
	if err != nil {
		return nil, err
	}
	rows, err := s.st.ListHelpdesks(ctx, nil)
	if err != nil {
		return nil, err
	}
	desks := make([]any, 0, len(rows))
	for _, h := range rows {
		container := deskContainer(h)
		if container == "" {
			continue
		}
		current, status, message := s.requestVersion("libredesk-"+container, provision.LibredeskVersion, libredesk)
		row := map[string]any{"id": h.ID, "name": h.Name, "current": current, "state": status}
		addonCompatibility(row, "libredesk", libredesk)
		if message != "" {
			row["message"] = message
		}
		desks = append(desks, row)
	}
	current, status, message := s.requestVersion("stalwart", s.cfg.StalwartVersion, stalwart)
	server := map[string]any{"latest": stalwart, "current": current, "state": status}
	addonCompatibility(server, "stalwart", stalwart)
	if message != "" {
		server["message"] = message
	}
	status, message = s.kurakaState(kuraka)
	self := map[string]any{
		"current":       "v" + version.Version,
		"commit":        version.Commit,
		"latest":        kuraka.Tag,
		"latest_commit": kuraka.Commit,
		"url":           kuraka.URL,
		"state":         status,
	}
	if status == "available" {
		self["kind"] = "version"
		if kuraka.Tag == "v"+version.Version {
			self["kind"] = "commits"
		}
	}
	if status == "error" {
		self["message"] = message
	}
	web := map[string]any{"latest": webui, "current": applied, "state": versionState(applied, webui)}
	addonCompatibility(web, "webui", webui)
	out := map[string]any{
		"checked_at":  nil,
		"updates_dir": s.cfg.UpdatesDir != "",
		"stalwart":    server,
		"kuraka":      self,
		"webui":       web,
		"libredesk":   map[string]any{"latest": libredesk, "helpdesks": desks},
	}
	if !checkedAt.IsZero() {
		out["checked_at"] = checkedAt
	}
	if checkErr != "" {
		out["error"] = checkErr
	}
	return out, nil
}

func (s *Server) updatesStatus(_ http.ResponseWriter, r *http.Request) (any, error) {
	return s.updatesBody(r.Context())
}

func (s *Server) checkUpdatesNow(_ http.ResponseWriter, r *http.Request) (any, error) {
	s.checkUpdates(r.Context())
	return s.updatesBody(r.Context())
}

// updateWebUI makes Stalwart download webui.zip again: its resource URL
// always points to the latest official webui release.
func (s *Server) updateWebUI(_ http.ResponseWriter, r *http.Request) (any, error) {
	_, latest, _ := s.latestTags()
	if latest == "" {
		return nil, conflict("Latest release unknown; check for updates first")
	}
	state(r).detail = latest
	if !addons.Allows("webui", latest) {
		return nil, conflict("Waiting for a Kuraka update that supports " + latest)
	}
	if err := s.sw.UpdateApps(r.Context()); err != nil {
		return nil, upstream(err)
	}
	if err := s.st.SetSetting(r.Context(), webuiSetting, latest); err != nil {
		return nil, err
	}
	return s.updatesBody(r.Context())
}

// updateHelpdesk leaves a request for deploy/apply-updates.sh, which runs as
// root on the host. Kuraka only writes the container name and the tag; the
// host validates both and finds the compose directory from Docker labels.
func (s *Server) updateHelpdesk(_ http.ResponseWriter, r *http.Request) (any, error) {
	id, err := pathID(r)
	if err != nil {
		return nil, err
	}
	h, err := s.st.GetHelpdesk(r.Context(), id)
	if err != nil {
		return nil, err
	}
	state(r).tenant = &h.TenantID
	container := deskContainer(h)
	if container == "" {
		return nil, invalid("helpdesk: not provisioned by Kuraka")
	}
	if s.cfg.UpdatesDir == "" {
		return nil, &apiError{status: 409, code: "updates_disabled", message: "KURAKA_UPDATES_DIR is not set"}
	}
	latest, _, _ := s.latestTags()
	s.updates.requestMu.Lock()
	defer s.updates.requestMu.Unlock()
	// A failed attempt can be retried; it is reported as "error".
	switch _, status, _ := s.requestVersion("libredesk-"+container, provision.LibredeskVersion, latest); status {
	case "updating":
		return nil, conflict("Helpdesk update already in progress")
	case "available", "error":
	default:
		return nil, conflict("No helpdesk update available")
	}
	state(r).detail = latest
	if !addons.Allows("libredesk", latest) {
		return nil, conflict("Waiting for a Kuraka update that supports " + latest)
	}
	if !stableTag.MatchString(latest) {
		return nil, invalid("release tag")
	}
	data, err := json.Marshal(map[string]string{"container": container, "tag": latest})
	if err != nil {
		return nil, err
	}
	if err := writeFileAtomic(filepath.Join(s.cfg.UpdatesDir, "libredesk-"+container+".json"), data); err != nil {
		return nil, err
	}
	return s.updatesBody(r.Context())
}

// updateStalwart leaves a request for deploy/apply-updates.sh. Kuraka only
// writes the tag; the host validates it and updates its own compose project.
func (s *Server) updateStalwart(_ http.ResponseWriter, r *http.Request) (any, error) {
	if s.cfg.UpdatesDir == "" {
		return nil, &apiError{status: 409, code: "updates_disabled", message: "KURAKA_UPDATES_DIR is not set"}
	}
	_, _, latest := s.latestTags()
	s.updates.requestMu.Lock()
	defer s.updates.requestMu.Unlock()
	switch _, status, _ := s.requestVersion("stalwart", s.cfg.StalwartVersion, latest); status {
	case "updating":
		return nil, conflict("Stalwart update already in progress")
	case "available", "error":
	default:
		return nil, conflict("No Stalwart update available")
	}
	state(r).detail = latest
	if !addons.Allows("stalwart", latest) {
		return nil, conflict("Waiting for a Kuraka update that supports " + latest)
	}
	if !stableTag.MatchString(latest) {
		return nil, invalid("release tag")
	}
	data, err := json.Marshal(map[string]string{"tag": latest})
	if err != nil {
		return nil, err
	}
	if err := writeFileAtomic(filepath.Join(s.cfg.UpdatesDir, "stalwart.json"), data); err != nil {
		return nil, err
	}
	return s.updatesBody(r.Context())
}

// updateKuraka leaves the release tag and immutable commit for the host timer.
func (s *Server) updateKuraka(_ http.ResponseWriter, r *http.Request) (any, error) {
	if s.cfg.UpdatesDir == "" {
		return nil, &apiError{status: 409, code: "updates_disabled", message: "KURAKA_UPDATES_DIR is not set"}
	}
	s.updates.requestMu.Lock()
	defer s.updates.requestMu.Unlock()
	s.updates.mu.Lock()
	latest := s.updates.kuraka
	s.updates.mu.Unlock()
	switch status, _ := s.kurakaState(latest); status {
	case "updating":
		return nil, conflict("Kuraka update already in progress")
	case "available", "error":
	default:
		return nil, conflict("No Kuraka update available")
	}
	if latest.Commit == "" {
		return nil, conflict("Latest release has no commit")
	}
	if !kurakaTag.MatchString(latest.Tag) {
		return nil, invalid("release tag")
	}
	state(r).detail = latest.Tag + " " + latest.Commit[:7]
	data, err := json.Marshal(map[string]string{"tag": latest.Tag, "commit": latest.Commit})
	if err != nil {
		return nil, err
	}
	if err := writeFileAtomic(filepath.Join(s.cfg.UpdatesDir, "kuraka.json"), data); err != nil {
		return nil, err
	}
	return s.updatesBody(r.Context())
}

// writeFileAtomic keeps readers from seeing a partial file.
func writeFileAtomic(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
