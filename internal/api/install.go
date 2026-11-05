package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/InkaNode/kuraka/internal/store"
)

func writeInstallRequest(dir, name, host, password string) error {
	path := filepath.Join(dir, "install-"+name+".json")
	if _, err := os.Stat(path); err == nil {
		return conflict("Helpdesk install already in progress")
	} else if !os.IsNotExist(err) {
		return err
	}
	data, err := json.Marshal(map[string]string{"name": name, "host": host, "system_password": password})
	if err != nil {
		return err
	}
	return writeFileAtomic(path, data)
}

func (s *Server) installStatus(_ http.ResponseWriter, r *http.Request) (any, error) {
	id, err := pathID(r)
	if err != nil {
		return nil, err
	}
	h, err := s.st.GetHelpdesk(r.Context(), id)
	if err != nil {
		return nil, err
	}
	state(r).tenant = &h.TenantID
	status, message, err := s.installState(h)
	if err != nil {
		return nil, err
	}
	return map[string]string{"status": status, "message": message}, nil
}

func (s *Server) installState(h store.Helpdesk) (status, message string, err error) {
	container := deskContainer(h)
	if s.cfg.UpdatesDir == "" || container == "" {
		return "none", "", nil
	}
	name := strings.TrimSuffix(strings.TrimPrefix(container, "kuraka-"), "-desk")
	base := filepath.Join(s.cfg.UpdatesDir, "install-"+name)
	if _, err := os.Stat(base + ".json"); err == nil {
		return "installing", "", nil
	} else if !os.IsNotExist(err) {
		return "", "", err
	}
	f, err := os.Open(base + ".result.json")
	if os.IsNotExist(err) {
		return "none", "", nil
	}
	if err != nil {
		return "", "", err
	}
	defer f.Close()
	var result updateResult
	if err := json.NewDecoder(io.LimitReader(f, 64<<10)).Decode(&result); err != nil {
		return "", "", err
	}
	if result.Status == "ok" || result.Status == "error" {
		return result.Status, result.Message, nil
	}
	return "none", "", nil
}

// StartHelpdeskActivation returns a channel closed when the loop has stopped.
func (s *Server) StartHelpdeskActivation(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	if s.cfg.UpdatesDir == "" {
		close(done)
		return done
	}
	go func() {
		defer close(done)
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.activatePending(ctx)
			}
		}
	}()
	return done
}

func (s *Server) activatePending(ctx context.Context) {
	rows, err := s.st.ListHelpdesks(ctx, nil)
	if err != nil {
		s.log.Warn("helpdesk activation list failed", "error", err)
		return
	}
	for _, h := range rows {
		if h.Status != "pending" {
			continue
		}
		status, _, err := s.installState(h)
		if err != nil {
			s.log.Warn("helpdesk install state failed", "helpdesk_id", h.ID)
			continue
		}
		if status != "ok" {
			continue
		}
		s.helpdeskMu.Lock()
		current, err := s.st.GetHelpdesk(ctx, h.ID)
		if err == nil && current.Status == "pending" {
			_, err = s.activate(ctx, current)
		}
		s.helpdeskMu.Unlock()
		if err != nil && err != helpdeskNotReady {
			s.log.Warn("helpdesk activation failed", "helpdesk_id", h.ID)
		}
	}
}
