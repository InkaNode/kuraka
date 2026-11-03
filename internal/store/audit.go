package store

import (
	"context"
	"database/sql"
	"time"
)

type AuditEntry struct {
	ID       int64
	At       time.Time
	UserID   *int64
	TenantID *int64
	Action   string
	Target   string
	Result   string
	Detail   string
	IP       string
}

func (s *Store) AddAudit(ctx context.Context, entry AuditEntry) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO audit_log
		(at, user_id, tenant_id, action, target, result, detail, ip)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, creationTime(entry.At).Unix(),
		entry.UserID, entry.TenantID, entry.Action, entry.Target, entry.Result, entry.Detail, entry.IP)
	return storeError("add audit", err)
}

// ListAudit returns entries by descending ID. beforeID is an exclusive cursor.
func (s *Store) ListAudit(ctx context.Context, tenantID *int64, beforeID int64, limit int) ([]AuditEntry, error) {
	limit = max(1, min(limit, 200))
	query := `SELECT id, at, user_id, tenant_id, action, target, result, detail, ip FROM audit_log WHERE 1 = 1`
	var args []any
	if tenantID != nil {
		query += ` AND tenant_id = ?`
		args = append(args, *tenantID)
	}
	if beforeID != 0 {
		query += ` AND id < ?`
		args = append(args, beforeID)
	}
	query += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, storeError("list audit", err)
	}
	defer rows.Close()
	entries := make([]AuditEntry, 0)
	for rows.Next() {
		var entry AuditEntry
		var at int64
		var user, tenant sql.NullInt64
		if err := rows.Scan(&entry.ID, &at, &user, &tenant, &entry.Action, &entry.Target, &entry.Result, &entry.Detail, &entry.IP); err != nil {
			return nil, storeError("list audit", err)
		}
		entry.At = utcTime(at)
		if user.Valid {
			entry.UserID = &user.Int64
		}
		if tenant.Valid {
			entry.TenantID = &tenant.Int64
		}
		entries = append(entries, entry)
	}
	return entries, storeError("list audit", rows.Err())
}
