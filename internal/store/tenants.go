package store

import (
	"context"
	"time"
)

type Tenant struct {
	ID        int64
	Name      string
	CreatedAt time.Time
}

func scanTenant(row scanner) (Tenant, error) {
	var tenant Tenant
	var created int64
	err := row.Scan(&tenant.ID, &tenant.Name, &created)
	tenant.CreatedAt = utcTime(created)
	return tenant, err
}

func (s *Store) CreateTenant(ctx context.Context, name string) (Tenant, error) {
	tenant := Tenant{Name: name, CreatedAt: creationTime(time.Time{})}
	result, err := s.db.ExecContext(ctx, `INSERT INTO tenants(name, created_at) VALUES (?, ?)`, name, tenant.CreatedAt.Unix())
	if err != nil {
		return Tenant{}, storeError("create tenant", err)
	}
	tenant.ID, err = result.LastInsertId()
	return tenant, storeError("create tenant", err)
}

func (s *Store) GetTenant(ctx context.Context, id int64) (Tenant, error) {
	tenant, err := scanTenant(s.db.QueryRowContext(ctx, `SELECT id, name, created_at FROM tenants WHERE id = ?`, id))
	return tenant, storeError("get tenant", err)
}

func (s *Store) ListTenants(ctx context.Context) ([]Tenant, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, created_at FROM tenants ORDER BY id`)
	if err != nil {
		return nil, storeError("list tenants", err)
	}
	defer rows.Close()
	tenants := make([]Tenant, 0)
	for rows.Next() {
		tenant, err := scanTenant(rows)
		if err != nil {
			return nil, storeError("list tenants", err)
		}
		tenants = append(tenants, tenant)
	}
	return tenants, storeError("list tenants", rows.Err())
}

func (s *Store) RenameTenant(ctx context.Context, id int64, name string) error {
	return s.execOne(ctx, "rename tenant", `UPDATE tenants SET name = ? WHERE id = ?`, name, id)
}

func (s *Store) DeleteTenant(ctx context.Context, id int64) error {
	return s.execOne(ctx, "delete tenant", `DELETE FROM tenants WHERE id = ?`, id)
}
