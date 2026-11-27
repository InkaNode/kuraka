package store

import (
	"context"
	"time"
)

// MetaChannel is a Meta inbox (WhatsApp) a domain connected to a helpdesk.
type MetaChannel struct {
	ID          int64
	DomainID    int64
	HelpdeskID  int64
	Channel     string
	InboxID     int64
	Name        string
	VerifyToken string
	CreatedAt   time.Time
}

const metaChannelColumns = `id, domain_id, helpdesk_id, channel, inbox_id, name, verify_token, created_at`

func scanMetaChannel(row scanner) (MetaChannel, error) {
	var mc MetaChannel
	var created int64
	err := row.Scan(&mc.ID, &mc.DomainID, &mc.HelpdeskID, &mc.Channel, &mc.InboxID, &mc.Name, &mc.VerifyToken, &created)
	mc.CreatedAt = utcTime(created)
	return mc, err
}

func (s *Store) CreateMetaChannel(ctx context.Context, mc MetaChannel) (MetaChannel, error) {
	mc.CreatedAt = creationTime(mc.CreatedAt)
	result, err := s.db.ExecContext(ctx, `INSERT INTO meta_channels
		(domain_id, helpdesk_id, channel, inbox_id, name, verify_token, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, mc.DomainID, mc.HelpdeskID, mc.Channel, mc.InboxID,
		mc.Name, mc.VerifyToken, mc.CreatedAt.Unix())
	if err != nil {
		return MetaChannel{}, storeError("create meta channel", err)
	}
	mc.ID, err = result.LastInsertId()
	return mc, storeError("create meta channel", err)
}

func (s *Store) GetMetaChannel(ctx context.Context, id int64) (MetaChannel, error) {
	mc, err := scanMetaChannel(s.db.QueryRowContext(ctx, `SELECT `+metaChannelColumns+` FROM meta_channels WHERE id = ?`, id))
	return mc, storeError("get meta channel", err)
}

func (s *Store) ListMetaChannels(ctx context.Context, domainID int64) ([]MetaChannel, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+metaChannelColumns+` FROM meta_channels WHERE domain_id = ? ORDER BY id`, domainID)
	if err != nil {
		return nil, storeError("list meta channels", err)
	}
	defer rows.Close()
	out := make([]MetaChannel, 0)
	for rows.Next() {
		mc, err := scanMetaChannel(rows)
		if err != nil {
			return nil, storeError("list meta channels", err)
		}
		out = append(out, mc)
	}
	return out, storeError("list meta channels", rows.Err())
}

func (s *Store) RenameMetaChannel(ctx context.Context, id int64, name string) error {
	return s.execOne(ctx, "rename meta channel", `UPDATE meta_channels SET name = ? WHERE id = ?`, name, id)
}

func (s *Store) DeleteMetaChannel(ctx context.Context, id int64) error {
	return s.execOne(ctx, "delete meta channel", `DELETE FROM meta_channels WHERE id = ?`, id)
}
