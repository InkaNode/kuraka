package store

import (
	"context"
	"strings"
	"time"
)

type Domain struct {
	ID         int64
	TenantID   int64
	Name       string
	StalwartID string
	CreatedAt  time.Time
}

const domainColumns = `id, tenant_id, name, stalwart_id, created_at`

func scanDomain(row scanner) (Domain, error) {
	var domain Domain
	var created int64
	err := row.Scan(&domain.ID, &domain.TenantID, &domain.Name, &domain.StalwartID, &created)
	domain.CreatedAt = utcTime(created)
	return domain, err
}

func (s *Store) CreateDomain(ctx context.Context, tenantID int64, name, stalwartID string) (Domain, error) {
	domain := Domain{TenantID: tenantID, Name: strings.ToLower(name), StalwartID: stalwartID, CreatedAt: creationTime(time.Time{})}
	result, err := s.db.ExecContext(ctx, `INSERT INTO domains(tenant_id, name, stalwart_id, created_at) VALUES (?, ?, ?, ?)`,
		tenantID, domain.Name, stalwartID, domain.CreatedAt.Unix())
	if err != nil {
		return Domain{}, storeError("create domain", err)
	}
	domain.ID, err = result.LastInsertId()
	return domain, storeError("create domain", err)
}

func (s *Store) GetDomain(ctx context.Context, id int64) (Domain, error) {
	domain, err := scanDomain(s.db.QueryRowContext(ctx, `SELECT `+domainColumns+` FROM domains WHERE id = ?`, id))
	return domain, storeError("get domain", err)
}

func (s *Store) GetDomainByName(ctx context.Context, name string) (Domain, error) {
	domain, err := scanDomain(s.db.QueryRowContext(ctx, `SELECT `+domainColumns+` FROM domains WHERE name = ?`, strings.ToLower(name)))
	return domain, storeError("get domain by name", err)
}

func (s *Store) GetDomainByStalwartID(ctx context.Context, sid string) (Domain, error) {
	domain, err := scanDomain(s.db.QueryRowContext(ctx, `SELECT `+domainColumns+` FROM domains WHERE stalwart_id = ?`, sid))
	return domain, storeError("get domain by Stalwart ID", err)
}

func (s *Store) ListDomains(ctx context.Context, tenantID *int64) ([]Domain, error) {
	query := `SELECT ` + domainColumns + ` FROM domains`
	var args []any
	if tenantID != nil {
		query += ` WHERE tenant_id = ?`
		args = append(args, *tenantID)
	}
	rows, err := s.db.QueryContext(ctx, query+` ORDER BY id`, args...)
	if err != nil {
		return nil, storeError("list domains", err)
	}
	defer rows.Close()
	domains := make([]Domain, 0)
	for rows.Next() {
		domain, err := scanDomain(rows)
		if err != nil {
			return nil, storeError("list domains", err)
		}
		domains = append(domains, domain)
	}
	return domains, storeError("list domains", rows.Err())
}

func (s *Store) DeleteDomain(ctx context.Context, id int64) error {
	return s.execOne(ctx, "delete domain", `DELETE FROM domains WHERE id = ?`, id)
}
