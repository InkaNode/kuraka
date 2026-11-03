package store

import (
	"context"
	"time"
)

type Session struct {
	ID         string
	UserID     int64
	MFAPending bool
	CreatedAt  time.Time
	ExpiresAt  time.Time
	IP         string
	UserAgent  string
}

func (s *Store) CreateSession(ctx context.Context, session Session) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions
		(id, user_id, mfa_pending, created_at, expires_at, ip, user_agent)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, session.ID, session.UserID,
		session.MFAPending, creationTime(session.CreatedAt).Unix(), session.ExpiresAt.Unix(), session.IP, session.UserAgent)
	return storeError("create session", err)
}

// GetSession treats the exact expiration second as expired.
func (s *Store) GetSession(ctx context.Context, id string, now time.Time) (Session, error) {
	var session Session
	var created, expires int64
	err := s.db.QueryRowContext(ctx, `SELECT id, user_id, mfa_pending, created_at, expires_at, ip, user_agent
		FROM sessions WHERE id = ? AND expires_at > ?`, id, now.Unix()).Scan(
		&session.ID, &session.UserID, &session.MFAPending, &created, &expires, &session.IP, &session.UserAgent)
	session.CreatedAt, session.ExpiresAt = utcTime(created), utcTime(expires)
	return session, storeError("get session", err)
}

func (s *Store) CompleteMFA(ctx context.Context, id string) error {
	return s.execOne(ctx, "complete MFA", `UPDATE sessions SET mfa_pending = 0 WHERE id = ?`, id)
}

// DeleteSession is idempotent so repeated logout requests can succeed.
func (s *Store) DeleteSession(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	return storeError("delete session", err)
}

func (s *Store) DeleteUserSessions(ctx context.Context, userID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID)
	return storeError("delete user sessions", err)
}

func (s *Store) PurgeExpiredSessions(ctx context.Context, now time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, now.Unix())
	if err != nil {
		return 0, storeError("purge expired sessions", err)
	}
	count, err := result.RowsAffected()
	return count, storeError("purge expired sessions", err)
}
