package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// MigrationJob copies one mailbox from an external IMAP server. The source
// secret is wiped whenever the job ends.
type MigrationJob struct {
	ID              int64
	DomainID        int64
	AccountID       string
	SourceHost      string
	SourcePort      int
	SourceTLS       string
	SourceUser      string
	SourceSecretEnc string
	Status          string
	FoldersTotal    int
	FoldersDone     int
	MessagesTotal   int
	MessagesCopied  int
	MessagesSkipped int
	Error           string
	CreatedBy       *int64
	CreatedAt       time.Time
	StartedAt       *time.Time
	FinishedAt      *time.Time
}

// MigrationProgress holds the counters a running job reports.
type MigrationProgress struct {
	FoldersTotal    int
	FoldersDone     int
	MessagesTotal   int
	MessagesCopied  int
	MessagesSkipped int
}

const migrationJobColumns = `id, domain_id, account_id, source_host, source_port, source_tls,
	source_user, source_secret_enc, status, folders_total, folders_done, messages_total,
	messages_copied, messages_skipped, error, created_by, created_at, started_at, finished_at`

func scanMigrationJob(row scanner) (MigrationJob, error) {
	var job MigrationJob
	var secret sql.NullString
	var createdBy, started, finished sql.NullInt64
	var created int64
	err := row.Scan(&job.ID, &job.DomainID, &job.AccountID, &job.SourceHost, &job.SourcePort,
		&job.SourceTLS, &job.SourceUser, &secret, &job.Status, &job.FoldersTotal, &job.FoldersDone,
		&job.MessagesTotal, &job.MessagesCopied, &job.MessagesSkipped, &job.Error, &createdBy,
		&created, &started, &finished)
	job.SourceSecretEnc = secret.String
	if createdBy.Valid {
		job.CreatedBy = &createdBy.Int64
	}
	job.CreatedAt = utcTime(created)
	if started.Valid {
		t := utcTime(started.Int64)
		job.StartedAt = &t
	}
	if finished.Valid {
		t := utcTime(finished.Int64)
		job.FinishedAt = &t
	}
	return job, err
}

// CreateMigrationJob inserts a queued job with zeroed progress.
func (s *Store) CreateMigrationJob(ctx context.Context, job MigrationJob) (MigrationJob, error) {
	job.Status = "queued"
	job.FoldersTotal, job.FoldersDone = 0, 0
	job.MessagesTotal, job.MessagesCopied, job.MessagesSkipped = 0, 0, 0
	job.Error = ""
	job.StartedAt, job.FinishedAt = nil, nil
	job.CreatedAt = creationTime(job.CreatedAt)
	result, err := s.db.ExecContext(ctx, `INSERT INTO migration_jobs
		(domain_id, account_id, source_host, source_port, source_tls, source_user,
		 source_secret_enc, status, created_by, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, job.DomainID, job.AccountID, job.SourceHost,
		job.SourcePort, job.SourceTLS, job.SourceUser, nullableString(job.SourceSecretEnc),
		job.Status, job.CreatedBy, job.CreatedAt.Unix())
	if err != nil {
		return MigrationJob{}, storeError("create migration job", err)
	}
	job.ID, err = result.LastInsertId()
	return job, storeError("create migration job", err)
}

func (s *Store) GetMigrationJob(ctx context.Context, id int64) (MigrationJob, error) {
	job, err := scanMigrationJob(s.db.QueryRowContext(ctx, `SELECT `+migrationJobColumns+` FROM migration_jobs WHERE id = ?`, id))
	return job, storeError("get migration job", err)
}

// ListMigrationJobs returns a domain's jobs, newest first.
func (s *Store) ListMigrationJobs(ctx context.Context, domainID int64, limit int) ([]MigrationJob, error) {
	limit = max(1, min(limit, 200))
	rows, err := s.db.QueryContext(ctx, `SELECT `+migrationJobColumns+` FROM migration_jobs
		WHERE domain_id = ? ORDER BY id DESC LIMIT ?`, domainID, limit)
	if err != nil {
		return nil, storeError("list migration jobs", err)
	}
	defer rows.Close()
	jobs := make([]MigrationJob, 0)
	for rows.Next() {
		job, err := scanMigrationJob(rows)
		if err != nil {
			return nil, storeError("list migration jobs", err)
		}
		jobs = append(jobs, job)
	}
	return jobs, storeError("list migration jobs", rows.Err())
}

// ClaimNextMigrationJob atomically moves the oldest queued job to running.
// It returns ErrNotFound when nothing is queued.
func (s *Store) ClaimNextMigrationJob(ctx context.Context, now time.Time) (MigrationJob, error) {
	job, err := scanMigrationJob(s.db.QueryRowContext(ctx, `UPDATE migration_jobs
		SET status = 'running', started_at = ?
		WHERE id = (SELECT id FROM migration_jobs WHERE status = 'queued' ORDER BY created_at, id LIMIT 1)
		RETURNING `+migrationJobColumns, now.Unix()))
	return job, storeError("claim migration job", err)
}

func (s *Store) UpdateMigrationProgress(ctx context.Context, id int64, progress MigrationProgress) error {
	return s.execOne(ctx, "update migration progress", `UPDATE migration_jobs
		SET folders_total = ?, folders_done = ?, messages_total = ?, messages_copied = ?, messages_skipped = ?
		WHERE id = ?`, progress.FoldersTotal, progress.FoldersDone, progress.MessagesTotal,
		progress.MessagesCopied, progress.MessagesSkipped, id)
}

// FinishMigrationJob records a final status and wipes the source secret.
func (s *Store) FinishMigrationJob(ctx context.Context, id int64, status, errText string, now time.Time) error {
	switch status {
	case "done", "failed", "cancelled":
	default:
		return fmt.Errorf("finish migration job: invalid final status %q", status)
	}
	return s.execOne(ctx, "finish migration job", `UPDATE migration_jobs
		SET status = ?, error = ?, finished_at = ?, source_secret_enc = ''
		WHERE id = ?`, status, errText, now.Unix(), id)
}

// RequestCancelMigration cancels a queued job immediately and flags a running
// one for its worker. A job that already ended returns ErrConflict.
func (s *Store) RequestCancelMigration(ctx context.Context, id int64) error {
	now := time.Now().Unix()
	result, err := s.db.ExecContext(ctx, `UPDATE migration_jobs SET
		cancel_requested = 1,
		finished_at = CASE WHEN status = 'queued' THEN ? ELSE finished_at END,
		source_secret_enc = CASE WHEN status = 'queued' THEN '' ELSE source_secret_enc END,
		status = CASE WHEN status = 'queued' THEN 'cancelled' ELSE status END
		WHERE id = ? AND status IN ('queued', 'running')`, now, id)
	if err != nil {
		return storeError("cancel migration", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return storeError("cancel migration", err)
	}
	if n != 0 {
		return nil
	}
	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT 1 FROM migration_jobs WHERE id = ?`, id).Scan(&exists); err != nil {
		return storeError("cancel migration", err)
	}
	return fmt.Errorf("cancel migration: job already ended: %w", ErrConflict)
}

func (s *Store) IsMigrationCancelRequested(ctx context.Context, id int64) (bool, error) {
	var requested bool
	err := s.db.QueryRowContext(ctx, `SELECT cancel_requested FROM migration_jobs WHERE id = ?`, id).Scan(&requested)
	return requested, storeError("check migration cancel", err)
}

// FailInterruptedMigrations marks jobs left running by a previous process as
// failed and wipes their secrets. It returns the number of jobs changed.
func (s *Store) FailInterruptedMigrations(ctx context.Context, now time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE migration_jobs
		SET status = 'failed', error = 'interrupted', finished_at = ?, source_secret_enc = ''
		WHERE status = 'running'`, now.Unix())
	if err != nil {
		return 0, storeError("fail interrupted migrations", err)
	}
	n, err := result.RowsAffected()
	return n, storeError("fail interrupted migrations", err)
}
