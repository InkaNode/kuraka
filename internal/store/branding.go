package store

import (
	"context"
	"encoding/json"
	"time"
)

// Branding is one app's branding in one scope. Empty fields inherit; Logo,
// Icon and Banner are branding asset ids.
type Branding struct {
	Name         string `json:"name,omitempty"`
	Logo         string `json:"logo,omitempty"`
	Icon         string `json:"icon,omitempty"`
	Color        string `json:"color,omitempty"`
	GradientFrom string `json:"gradient_from,omitempty"`
	GradientTo   string `json:"gradient_to,omitempty"`
	Banner       string `json:"banner,omitempty"`
}

// brandingScope is the unique index key: 0 for global, never a tenant id.
func brandingScope(tenantID *int64) int64 {
	if tenantID == nil {
		return 0
	}
	return *tenantID
}

// GetBranding returns the rows of one scope (nil: global), keyed by app.
func (s *Store) GetBranding(ctx context.Context, tenantID *int64) (map[string]Branding, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT app, data FROM branding WHERE IFNULL(tenant_id, 0) = ?`, brandingScope(tenantID))
	if err != nil {
		return nil, storeError("get branding", err)
	}
	defer rows.Close()
	out := map[string]Branding{}
	for rows.Next() {
		var app, data string
		if err := rows.Scan(&app, &data); err != nil {
			return nil, storeError("get branding", err)
		}
		var b Branding
		if err := json.Unmarshal([]byte(data), &b); err != nil {
			return nil, storeError("get branding", err)
		}
		out[app] = b
	}
	return out, storeError("get branding", rows.Err())
}

// SetBranding replaces one app's branding in one scope; an empty Branding
// deletes the row.
func (s *Store) SetBranding(ctx context.Context, tenantID *int64, app string, b Branding) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return storeError("set branding", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM branding WHERE IFNULL(tenant_id, 0) = ? AND app = ?`, brandingScope(tenantID), app); err != nil {
		return storeError("set branding", err)
	}
	if b != (Branding{}) {
		data, err := json.Marshal(b)
		if err != nil {
			return storeError("set branding", err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO branding(tenant_id, app, data) VALUES (?, ?, ?)`, tenantID, app, string(data)); err != nil {
			return storeError("set branding", err)
		}
	}
	return storeError("set branding", tx.Commit())
}

func (s *Store) CreateBrandingAsset(ctx context.Context, id, contentType string, data []byte) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO branding_assets(id, content_type, data, created_at) VALUES (?, ?, ?, ?)`,
		id, contentType, data, time.Now().Unix())
	return storeError("create branding asset", err)
}

// GetBrandingAsset returns ErrNotFound for unknown ids.
func (s *Store) GetBrandingAsset(ctx context.Context, id string) (string, []byte, error) {
	var contentType string
	var data []byte
	err := s.db.QueryRowContext(ctx, `SELECT content_type, data FROM branding_assets WHERE id = ?`, id).Scan(&contentType, &data)
	return contentType, data, storeError("get branding asset", err)
}

// DeleteUnusedBrandingAssets removes assets no branding row references.
// Assets created at or after olderThan are kept: they may be about to be saved.
func (s *Store) DeleteUnusedBrandingAssets(ctx context.Context, olderThan time.Time) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM branding_assets WHERE created_at < ? AND NOT EXISTS (
		SELECT 1 FROM branding WHERE branding_assets.id IN
			(json_extract(data, '$.logo'), json_extract(data, '$.icon'), json_extract(data, '$.banner')))`, olderThan.Unix())
	return storeError("delete unused branding assets", err)
}
