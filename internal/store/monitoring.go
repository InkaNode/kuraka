package store

import (
	"context"
	"time"
)

// Point is a Unix timestamp and its sampled value.
type Point struct {
	At    int64
	Value float64
}

type AlertRule struct {
	ID          int64   `json:"id"`
	Metric      string  `json:"metric"`
	Threshold   float64 `json:"threshold"`
	Enabled     bool    `json:"enabled"`
	FiringSince *int64  `json:"firing_since"`
	CreatedAt   int64   `json:"created_at"`
}

func (s *Store) InsertSamples(ctx context.Context, at int64, samples map[string]float64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return storeError("insert samples", err)
	}
	defer tx.Rollback()
	for name, value := range samples {
		if _, err := tx.ExecContext(ctx, `INSERT INTO metric_samples(at, name, value) VALUES (?, ?, ?)`, at, name, value); err != nil {
			return storeError("insert samples", err)
		}
	}
	return storeError("insert samples", tx.Commit())
}

func (s *Store) PruneSamples(ctx context.Context, before int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM metric_samples WHERE at < ?`, before)
	return storeError("prune samples", err)
}

func (s *Store) Series(ctx context.Context, name string, since int64) ([]Point, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT at, value FROM metric_samples WHERE name = ? AND at >= ? ORDER BY at`, name, since)
	if err != nil {
		return nil, storeError("metric series", err)
	}
	defer rows.Close()
	out := make([]Point, 0)
	for rows.Next() {
		var p Point
		if err := rows.Scan(&p.At, &p.Value); err != nil {
			return nil, storeError("metric series", err)
		}
		out = append(out, p)
	}
	return out, storeError("metric series", rows.Err())
}

func (s *Store) Latest(ctx context.Context) (map[string]float64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT s.name, s.value FROM metric_samples s
		JOIN (SELECT name, MAX(at) AS at FROM metric_samples GROUP BY name) latest
		ON s.name = latest.name AND s.at = latest.at`)
	if err != nil {
		return nil, storeError("latest metrics", err)
	}
	defer rows.Close()
	out := map[string]float64{}
	for rows.Next() {
		var name string
		var value float64
		if err := rows.Scan(&name, &value); err != nil {
			return nil, storeError("latest metrics", err)
		}
		out[name] = value
	}
	return out, storeError("latest metrics", rows.Err())
}

func (s *Store) ListAlertRules(ctx context.Context) ([]AlertRule, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, metric, threshold, enabled, firing_since, created_at FROM alert_rules ORDER BY id`)
	if err != nil {
		return nil, storeError("list alert rules", err)
	}
	defer rows.Close()
	out := make([]AlertRule, 0)
	for rows.Next() {
		var rule AlertRule
		if err := rows.Scan(&rule.ID, &rule.Metric, &rule.Threshold, &rule.Enabled, &rule.FiringSince, &rule.CreatedAt); err != nil {
			return nil, storeError("list alert rules", err)
		}
		out = append(out, rule)
	}
	return out, storeError("list alert rules", rows.Err())
}

func (s *Store) CreateAlertRule(ctx context.Context, metric string, threshold float64) (AlertRule, error) {
	rule := AlertRule{Metric: metric, Threshold: threshold, Enabled: true, CreatedAt: time.Now().Unix()}
	result, err := s.db.ExecContext(ctx, `INSERT INTO alert_rules(metric, threshold, created_at) VALUES (?, ?, ?)`, metric, threshold, rule.CreatedAt)
	if err != nil {
		return rule, storeError("create alert rule", err)
	}
	rule.ID, err = result.LastInsertId()
	return rule, storeError("create alert rule", err)
}

func (s *Store) DeleteAlertRule(ctx context.Context, id int64) error {
	return s.execOne(ctx, "delete alert rule", `DELETE FROM alert_rules WHERE id = ?`, id)
}

func (s *Store) SetAlertFiring(ctx context.Context, id int64, since *int64) error {
	return s.execOne(ctx, "set alert firing", `UPDATE alert_rules SET firing_since = ? WHERE id = ?`, since, id)
}
