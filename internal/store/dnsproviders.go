package store

import (
	"context"
	"database/sql"
	"time"
)

// DNSProvider belongs to a tenant, or is global when TenantID is nil.
type DNSProvider struct {
	ID        int64
	TenantID  *int64
	Kind      string
	Name      string
	TokenEnc  string
	CreatedAt time.Time
}

const dnsProviderColumns = `id, tenant_id, kind, name, token_enc, created_at`

func scanDNSProvider(row scanner) (DNSProvider, error) {
	var provider DNSProvider
	var tenantID sql.NullInt64
	var created int64
	err := row.Scan(&provider.ID, &tenantID, &provider.Kind, &provider.Name, &provider.TokenEnc, &created)
	if tenantID.Valid {
		provider.TenantID = &tenantID.Int64
	}
	provider.CreatedAt = utcTime(created)
	return provider, err
}

func (s *Store) CreateDNSProvider(ctx context.Context, provider DNSProvider) (DNSProvider, error) {
	provider.CreatedAt = creationTime(provider.CreatedAt)
	result, err := s.db.ExecContext(ctx, `INSERT INTO dns_providers(tenant_id, kind, name, token_enc, created_at)
		VALUES (?, ?, ?, ?, ?)`, provider.TenantID, provider.Kind, provider.Name, provider.TokenEnc, provider.CreatedAt.Unix())
	if err != nil {
		return DNSProvider{}, storeError("create DNS provider", err)
	}
	provider.ID, err = result.LastInsertId()
	return provider, storeError("create DNS provider", err)
}

func (s *Store) GetDNSProvider(ctx context.Context, id int64) (DNSProvider, error) {
	provider, err := scanDNSProvider(s.db.QueryRowContext(ctx, `SELECT `+dnsProviderColumns+` FROM dns_providers WHERE id = ?`, id))
	return provider, storeError("get DNS provider", err)
}

// ListDNSProviders lists every provider when tenantID is nil. Otherwise it
// lists the tenant's providers, plus global ones when includeGlobal is set.
func (s *Store) ListDNSProviders(ctx context.Context, tenantID *int64, includeGlobal bool) ([]DNSProvider, error) {
	query := `SELECT ` + dnsProviderColumns + ` FROM dns_providers`
	var args []any
	if tenantID != nil {
		query += ` WHERE tenant_id = ?`
		if includeGlobal {
			query += ` OR tenant_id IS NULL`
		}
		args = append(args, *tenantID)
	}
	rows, err := s.db.QueryContext(ctx, query+` ORDER BY id`, args...)
	if err != nil {
		return nil, storeError("list DNS providers", err)
	}
	defer rows.Close()
	providers := make([]DNSProvider, 0)
	for rows.Next() {
		provider, err := scanDNSProvider(rows)
		if err != nil {
			return nil, storeError("list DNS providers", err)
		}
		providers = append(providers, provider)
	}
	return providers, storeError("list DNS providers", rows.Err())
}

// DeleteDNSProvider removes the provider; domains using it are left without one.
func (s *Store) DeleteDNSProvider(ctx context.Context, id int64) error {
	return s.execOne(ctx, "delete DNS provider", `DELETE FROM dns_providers WHERE id = ?`, id)
}
