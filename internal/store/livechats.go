package store

import (
	"context"
	"time"
)

// LiveChat is a live chat inbox a domain created in a helpdesk.
type LiveChat struct {
	ID         int64
	DomainID   int64
	HelpdeskID int64
	InboxID    int64
	InboxUUID  string
	Name       string
	WebsiteURL string
	CreatedAt  time.Time
}

const liveChatColumns = `id, domain_id, helpdesk_id, inbox_id, inbox_uuid, name, website_url, created_at`

func scanLiveChat(row scanner) (LiveChat, error) {
	var lc LiveChat
	var created int64
	err := row.Scan(&lc.ID, &lc.DomainID, &lc.HelpdeskID, &lc.InboxID, &lc.InboxUUID, &lc.Name, &lc.WebsiteURL, &created)
	lc.CreatedAt = utcTime(created)
	return lc, err
}

func (s *Store) CreateLiveChat(ctx context.Context, lc LiveChat) (LiveChat, error) {
	lc.CreatedAt = creationTime(lc.CreatedAt)
	result, err := s.db.ExecContext(ctx, `INSERT INTO livechats
		(domain_id, helpdesk_id, inbox_id, inbox_uuid, name, website_url, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, lc.DomainID, lc.HelpdeskID, lc.InboxID, lc.InboxUUID,
		lc.Name, lc.WebsiteURL, lc.CreatedAt.Unix())
	if err != nil {
		return LiveChat{}, storeError("create live chat", err)
	}
	lc.ID, err = result.LastInsertId()
	return lc, storeError("create live chat", err)
}

func (s *Store) GetLiveChat(ctx context.Context, id int64) (LiveChat, error) {
	lc, err := scanLiveChat(s.db.QueryRowContext(ctx, `SELECT `+liveChatColumns+` FROM livechats WHERE id = ?`, id))
	return lc, storeError("get live chat", err)
}

func (s *Store) ListLiveChats(ctx context.Context, domainID int64) ([]LiveChat, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+liveChatColumns+` FROM livechats WHERE domain_id = ? ORDER BY id`, domainID)
	if err != nil {
		return nil, storeError("list live chats", err)
	}
	defer rows.Close()
	out := make([]LiveChat, 0)
	for rows.Next() {
		lc, err := scanLiveChat(rows)
		if err != nil {
			return nil, storeError("list live chats", err)
		}
		out = append(out, lc)
	}
	return out, storeError("list live chats", rows.Err())
}

func (s *Store) RenameLiveChat(ctx context.Context, id int64, name, website string) error {
	return s.execOne(ctx, "rename live chat", `UPDATE livechats SET name = ?, website_url = ? WHERE id = ?`, name, website, id)
}

func (s *Store) DeleteLiveChat(ctx context.Context, id int64) error {
	return s.execOne(ctx, "delete live chat", `DELETE FROM livechats WHERE id = ?`, id)
}
