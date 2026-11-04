package store

import (
	"context"
	"time"
)

// Helpdesk is a Libredesk instance. It always belongs to exactly one tenant.
type Helpdesk struct {
	ID             int64
	TenantID       int64
	Kind           string
	Name           string
	URL            string
	APIKey         string
	APISecretEnc   string
	Status         string
	SetupSecretEnc string
	CreatedAt      time.Time
}

// HelpdeskLink connects a mailbox to a helpdesk inbox through an app password.
type HelpdeskLink struct {
	ID            int64
	DomainID      int64
	AccountID     string
	HelpdeskID    int64
	InboxID       int64
	AppPasswordID string
	CreatedAt     time.Time
}

const helpdeskColumns = `id, tenant_id, kind, name, url, api_key, api_secret_enc, status, setup_secret_enc, created_at`

func scanHelpdesk(row scanner) (Helpdesk, error) {
	var helpdesk Helpdesk
	var created int64
	err := row.Scan(&helpdesk.ID, &helpdesk.TenantID, &helpdesk.Kind, &helpdesk.Name,
		&helpdesk.URL, &helpdesk.APIKey, &helpdesk.APISecretEnc, &helpdesk.Status, &helpdesk.SetupSecretEnc, &created)
	helpdesk.CreatedAt = utcTime(created)
	return helpdesk, err
}

func (s *Store) CreateHelpdesk(ctx context.Context, helpdesk Helpdesk) (Helpdesk, error) {
	if helpdesk.Status == "" {
		helpdesk.Status = "ready"
	}
	helpdesk.CreatedAt = creationTime(helpdesk.CreatedAt)
	result, err := s.db.ExecContext(ctx, `INSERT INTO helpdesks
		(tenant_id, kind, name, url, api_key, api_secret_enc, status, setup_secret_enc, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, helpdesk.TenantID, helpdesk.Kind, helpdesk.Name,
		helpdesk.URL, helpdesk.APIKey, helpdesk.APISecretEnc, helpdesk.Status, helpdesk.SetupSecretEnc, helpdesk.CreatedAt.Unix())
	if err != nil {
		return Helpdesk{}, storeError("create helpdesk", err)
	}
	helpdesk.ID, err = result.LastInsertId()
	return helpdesk, storeError("create helpdesk", err)
}

func (s *Store) ActivateHelpdesk(ctx context.Context, id int64, apiKey, apiSecretEnc string) error {
	return s.execOne(ctx, "activate helpdesk", `UPDATE helpdesks SET api_key = ?, api_secret_enc = ?,
		status = 'ready', setup_secret_enc = '' WHERE id = ? AND status = 'pending'`, apiKey, apiSecretEnc, id)
}

func (s *Store) GetHelpdesk(ctx context.Context, id int64) (Helpdesk, error) {
	helpdesk, err := scanHelpdesk(s.db.QueryRowContext(ctx, `SELECT `+helpdeskColumns+` FROM helpdesks WHERE id = ?`, id))
	return helpdesk, storeError("get helpdesk", err)
}

func (s *Store) ListHelpdesks(ctx context.Context, tenantID *int64) ([]Helpdesk, error) {
	query := `SELECT ` + helpdeskColumns + ` FROM helpdesks`
	var args []any
	if tenantID != nil {
		query += ` WHERE tenant_id = ?`
		args = append(args, *tenantID)
	}
	rows, err := s.db.QueryContext(ctx, query+` ORDER BY id`, args...)
	if err != nil {
		return nil, storeError("list helpdesks", err)
	}
	defer rows.Close()
	helpdesks := make([]Helpdesk, 0)
	for rows.Next() {
		helpdesk, err := scanHelpdesk(rows)
		if err != nil {
			return nil, storeError("list helpdesks", err)
		}
		helpdesks = append(helpdesks, helpdesk)
	}
	return helpdesks, storeError("list helpdesks", rows.Err())
}

// DeleteHelpdesk removes the helpdesk and, by cascade, its links.
func (s *Store) DeleteHelpdesk(ctx context.Context, id int64) error {
	return s.execOne(ctx, "delete helpdesk", `DELETE FROM helpdesks WHERE id = ?`, id)
}

const helpdeskLinkColumns = `id, domain_id, account_id, helpdesk_id, inbox_id, app_password_id, created_at`

func scanHelpdeskLink(row scanner) (HelpdeskLink, error) {
	var link HelpdeskLink
	var created int64
	err := row.Scan(&link.ID, &link.DomainID, &link.AccountID, &link.HelpdeskID,
		&link.InboxID, &link.AppPasswordID, &created)
	link.CreatedAt = utcTime(created)
	return link, err
}

// CreateHelpdeskLink returns ErrConflict when the account is already linked to the helpdesk.
func (s *Store) CreateHelpdeskLink(ctx context.Context, link HelpdeskLink) (HelpdeskLink, error) {
	link.CreatedAt = creationTime(link.CreatedAt)
	result, err := s.db.ExecContext(ctx, `INSERT INTO helpdesk_links
		(domain_id, account_id, helpdesk_id, inbox_id, app_password_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`, link.DomainID, link.AccountID, link.HelpdeskID,
		link.InboxID, link.AppPasswordID, link.CreatedAt.Unix())
	if err != nil {
		return HelpdeskLink{}, storeError("create helpdesk link", err)
	}
	link.ID, err = result.LastInsertId()
	return link, storeError("create helpdesk link", err)
}

func (s *Store) GetHelpdeskLink(ctx context.Context, id int64) (HelpdeskLink, error) {
	link, err := scanHelpdeskLink(s.db.QueryRowContext(ctx, `SELECT `+helpdeskLinkColumns+` FROM helpdesk_links WHERE id = ?`, id))
	return link, storeError("get helpdesk link", err)
}

// ListHelpdeskLinks lists a domain's links, narrowed to one account when accountID is set.
func (s *Store) ListHelpdeskLinks(ctx context.Context, domainID int64, accountID string) ([]HelpdeskLink, error) {
	query := `SELECT ` + helpdeskLinkColumns + ` FROM helpdesk_links WHERE domain_id = ?`
	args := []any{domainID}
	if accountID != "" {
		query += ` AND account_id = ?`
		args = append(args, accountID)
	}
	rows, err := s.db.QueryContext(ctx, query+` ORDER BY id`, args...)
	if err != nil {
		return nil, storeError("list helpdesk links", err)
	}
	defer rows.Close()
	links := make([]HelpdeskLink, 0)
	for rows.Next() {
		link, err := scanHelpdeskLink(rows)
		if err != nil {
			return nil, storeError("list helpdesk links", err)
		}
		links = append(links, link)
	}
	return links, storeError("list helpdesk links", rows.Err())
}

func (s *Store) DeleteHelpdeskLink(ctx context.Context, id int64) error {
	return s.execOne(ctx, "delete helpdesk link", `DELETE FROM helpdesk_links WHERE id = ?`, id)
}
