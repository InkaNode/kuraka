package jobs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/InkaNode/kuraka/internal/config"
	"github.com/InkaNode/kuraka/internal/stalwart"
	"github.com/InkaNode/kuraka/internal/store"
)

// StartMetrics returns a channel closed when sampling has stopped.
func StartMetrics(ctx context.Context, st *store.Store, sw *stalwart.Client, cfg config.Config, log *slog.Logger) <-chan struct{} {
	done := make(chan struct{})
	if cfg.StalwartMetricsSecret == "" {
		close(done)
		return done
	}
	if log == nil {
		log = slog.Default()
	}
	go func() {
		defer close(done)
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		succeeded, unreachable := false, false
		for ctx.Err() == nil {
			samples, err := sw.Metrics(ctx, stalwart.MetricsUser, cfg.StalwartMetricsSecret)
			if ctx.Err() != nil {
				return
			}
			at := time.Now().Unix()
			if err != nil {
				log.Error("metrics fetch failed", "error", err)
				if succeeded && !unreachable {
					unreachable = true
					notifyAlert(ctx, client, st, sw, cfg, log, store.AlertRule{Metric: "stalwart_unreachable"}, 1, "firing", at)
				}
			} else {
				succeeded = true
				if unreachable {
					unreachable = false
					notifyAlert(ctx, client, st, sw, cfg, log, store.AlertRule{Metric: "stalwart_unreachable"}, 0, "resolved", at)
				}
				values := map[string]float64{}
				for _, name := range stalwart.MetricNames {
					if value, ok := samples[name]; ok {
						values[name] = value
					}
				}
				if err := st.InsertSamples(ctx, at, values); err != nil {
					log.Error("metrics storage failed", "error", err)
				} else {
					if err := st.PruneSamples(ctx, at-int64(7*24*time.Hour/time.Second)); err != nil {
						log.Error("metrics pruning failed", "error", err)
					}
					evaluateAlerts(ctx, st, sw, client, cfg, log, values, at)
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return done
}

func evaluateAlerts(ctx context.Context, st *store.Store, sw *stalwart.Client, client *http.Client, cfg config.Config, log *slog.Logger, values map[string]float64, at int64) {
	rules, err := st.ListAlertRules(ctx)
	if err != nil {
		log.Error("alert rules lookup failed", "error", err)
		return
	}
	for _, rule := range rules {
		value, ok := values[rule.Metric]
		if !rule.Enabled || !ok {
			continue
		}
		if strings.HasSuffix(rule.Metric, "_total") {
			points, err := st.Series(ctx, rule.Metric, at-300)
			if err != nil {
				log.Error("alert series lookup failed", "rule_id", rule.ID, "error", err)
				continue
			}
			if len(points) == 0 {
				continue
			}
			oldest := points[0].Value
			reset := false
			for i := 1; i < len(points); i++ {
				if points[i].Value < points[i-1].Value {
					reset = true
					break
				}
			}
			// On a counter reset anywhere in the window, use the latest value.
			if !reset {
				value = max(0, value-oldest)
			}
		}
		firing := value > rule.Threshold
		if firing == (rule.FiringSince != nil) {
			continue
		}
		var since *int64
		state := "resolved"
		if firing {
			since, state = &at, "firing"
		}
		if err := st.SetAlertFiring(ctx, rule.ID, since); err != nil {
			log.Error("alert state update failed", "rule_id", rule.ID, "error", err)
			continue
		}
		notifyAlert(ctx, client, st, sw, cfg, log, rule, value, state, at)
	}
}

func notifyAlert(ctx context.Context, client *http.Client, st *store.Store, sw *stalwart.Client, cfg config.Config, log *slog.Logger, rule store.AlertRule, value float64, state string, at int64) {
	log.Info("alert transition", "rule_id", rule.ID, "metric", rule.Metric, "value", value, "state", state)
	if cfg.AlertEmailFrom != "" {
		// Deferred so the webhook is not delayed by the mail round trips.
		defer sendAlertMail(ctx, st, sw, cfg, log, rule, value, state, at)
	}
	webhook := cfg.AlertWebhookURL
	if webhook == "" {
		return
	}
	body, err := json.Marshal(map[string]any{"rule_id": rule.ID, "metric": rule.Metric, "threshold": rule.Threshold, "value": value, "state": state, "at": at})
	if err == nil {
		var req *http.Request
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, webhook, bytes.NewReader(body))
		if err == nil {
			req.Header.Set("Content-Type", "application/json")
			var resp *http.Response
			resp, err = client.Do(req)
			if err == nil {
				_, readErr := io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
				resp.Body.Close()
				if resp.StatusCode < 200 || resp.StatusCode > 299 {
					err = fmt.Errorf("http %d", resp.StatusCode)
				} else {
					err = readErr
				}
			}
		}
	}
	if err != nil {
		log.Error("alert webhook failed", "rule_id", rule.ID, "error", err)
	}
}
