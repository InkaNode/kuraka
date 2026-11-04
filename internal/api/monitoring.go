package api

import (
	"errors"
	"math"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/InkaNode/kuraka/internal/authz"
	"github.com/InkaNode/kuraka/internal/stalwart"
)

func (s *Server) queueDomains(r *http.Request) (map[string]bool, error) {
	p := state(r).principal
	if p.IsSuperadmin() {
		return nil, nil
	}
	if p.Role != authz.RoleTenantAdmin || p.TenantID == nil {
		return nil, authz.ErrForbidden
	}
	domains, err := s.st.ListDomains(r.Context(), authz.TenantScope(p))
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, d := range domains {
		out[strings.ToLower(d.Name)] = true
	}
	return out, nil
}

// A nil domain set is reserved for the superadmin's global scope.
func queueVisible(message stalwart.QueuedMessage, domains map[string]bool) bool {
	if domains == nil {
		return true
	}
	_, domain, ok := strings.Cut(message.ReturnPath, "@")
	return ok && domains[strings.ToLower(domain)]
}

func (s *Server) queue(_ http.ResponseWriter, r *http.Request) (any, error) {
	domains, err := s.queueDomains(r)
	if err != nil {
		return nil, err
	}
	messages, err := s.sw.ListQueue(r.Context())
	if err != nil {
		return nil, upstream(err)
	}
	out := make([]any, 0, len(messages))
	for _, message := range messages {
		if !queueVisible(message, domains) {
			continue
		}
		recipients := make([]map[string]any, 0, len(message.Recipients))
		addresses := make([]string, 0, len(message.Recipients))
		for address := range message.Recipients {
			addresses = append(addresses, address)
		}
		sort.Strings(addresses)
		for _, address := range addresses {
			recipient := message.Recipients[address]
			recipients = append(recipients, map[string]any{"address": address, "status": recipient.Status.Type, "errorMessage": recipient.Status.ErrorMessage, "retryCount": recipient.RetryCount})
		}
		out = append(out, map[string]any{"id": message.ID, "createdAt": message.CreatedAt, "nextRetry": message.NextRetry, "returnPath": message.ReturnPath, "size": message.Size, "recipients": recipients})
	}
	return out, nil
}

func (s *Server) queued(r *http.Request) (stalwart.QueuedMessage, error) {
	domains, err := s.queueDomains(r)
	if err != nil {
		return stalwart.QueuedMessage{}, err
	}
	message, err := s.sw.GetQueued(r.Context(), r.PathValue("id"))
	if err != nil {
		return message, upstream(err)
	}
	if !queueVisible(message, domains) {
		return stalwart.QueuedMessage{}, authz.ErrNotFound
	}
	return message, nil
}

func (s *Server) retryQueued(_ http.ResponseWriter, r *http.Request) (any, error) {
	message, err := s.queued(r)
	if err != nil {
		return nil, err
	}
	if err := s.sw.RetryQueued(r.Context(), message.ID); err != nil {
		return nil, queueError(err)
	}
	return reply{http.StatusNoContent, nil}, nil
}

func (s *Server) cancelQueued(_ http.ResponseWriter, r *http.Request) (any, error) {
	message, err := s.queued(r)
	if err != nil {
		return nil, err
	}
	if err := s.sw.CancelQueued(r.Context(), message.ID); err != nil {
		return nil, queueError(err)
	}
	return reply{http.StatusNoContent, nil}, nil
}

func (s *Server) metrics(_ http.ResponseWriter, r *http.Request) (any, error) {
	latest, err := s.st.Latest(r.Context())
	if err != nil {
		return nil, err
	}
	series := map[string][][2]float64{}
	for _, name := range stalwart.MetricNames {
		points, err := s.st.Series(r.Context(), name, s.now().Add(-24*time.Hour).Unix())
		if err != nil {
			return nil, err
		}
		values := make([][2]float64, 0, len(points))
		for _, p := range points {
			values = append(values, [2]float64{float64(p.At), p.Value})
		}
		series[name] = values
	}
	return map[string]any{"enabled": s.cfg.StalwartMetricsSecret != "", "latest": latest, "series": series}, nil
}

func (s *Server) alerts(_ http.ResponseWriter, r *http.Request) (any, error) {
	return s.st.ListAlertRules(r.Context())
}

func (s *Server) createAlert(_ http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		Metric    string   `json:"metric"`
		Threshold *float64 `json:"threshold"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	if !slices.Contains(stalwart.MetricNames, in.Metric) {
		return nil, invalid("metric")
	}
	if in.Threshold == nil || math.IsNaN(*in.Threshold) || math.IsInf(*in.Threshold, 0) {
		return nil, invalid("threshold")
	}
	rule, err := s.st.CreateAlertRule(r.Context(), in.Metric, *in.Threshold)
	return reply{http.StatusCreated, rule}, err
}

func (s *Server) deleteAlert(_ http.ResponseWriter, r *http.Request) (any, error) {
	id, err := pathID(r)
	if err != nil {
		return nil, err
	}
	if err := s.st.DeleteAlertRule(r.Context(), id); err != nil {
		return nil, err
	}
	return reply{http.StatusNoContent, nil}, nil
}

// Stalwart refuses changes with "forbidden" while a delivery attempt holds the message.
func queueError(err error) error {
	var se *stalwart.SetError
	if errors.As(err, &se) && se.Type == "forbidden" {
		return &apiError{status: 409, code: "queue_busy", message: "Message is being delivered; try again shortly"}
	}
	return upstream(err)
}
