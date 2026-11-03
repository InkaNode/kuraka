// Package store provides SQLite persistence for Kuraka.
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

var (
	ErrNotFound = errors.New("store: not found")
	ErrConflict = errors.New("store: constraint conflict")
)

//go:embed migrations/*.sql
var migrations embed.FS

// Store owns a single-connection SQLite pool. Use Open to initialize it.
type Store struct {
	db *sql.DB
}

// Open opens the database and applies all embedded migrations in filename order.
func Open(path string) (*Store, error) {
	dsn := "file:" + path + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, storeError("open database", err)
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY, applied_at INTEGER
	)`)
	if err != nil {
		return storeError("create migration table", err)
	}
	files, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return storeError("list migrations", err)
	}
	// fs.Glob returns filenames in sorted order.
	for _, name := range files {
		base := strings.TrimPrefix(name, "migrations/")
		version, err := strconv.Atoi(strings.SplitN(base, "_", 2)[0])
		if err != nil {
			return fmt.Errorf("migration %s: %w", name, err)
		}
		if err := s.applyMigration(ctx, name, version); err != nil {
			return fmt.Errorf("migration %s: %w", name, err)
		}
	}
	return nil
}

func (s *Store) applyMigration(ctx context.Context, name string, version int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return storeError("begin migration", err)
	}
	defer tx.Rollback()
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations WHERE version = ?`, version).Scan(&exists); err != nil {
		return storeError("check migration", err)
	}
	if exists != 0 {
		return storeError("finish migration check", tx.Commit())
	}
	script, err := migrations.ReadFile(name)
	if err != nil {
		return storeError("read migration", err)
	}
	if _, err := tx.ExecContext(ctx, string(script)); err != nil {
		return storeError("apply migration", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version, applied_at) VALUES (?, ?)`, version, time.Now().Unix()); err != nil {
		return storeError("record migration", err)
	}
	return storeError("commit migration", tx.Commit())
}

func storeError(op string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%s: %w", op, ErrNotFound)
	}
	// SQLite extended result codes retain the primary code in the low byte.
	var coded interface{ Code() int }
	if errors.As(err, &coded) && coded.Code()&0xff == 19 { // SQLITE_CONSTRAINT
		return fmt.Errorf("%s: %w: %w", op, ErrConflict, err)
	}
	return fmt.Errorf("%s: %w", op, err)
}

// execOne reports missing rows for mutations of a single existing resource.
func (s *Store) execOne(ctx context.Context, op, query string, args ...any) error {
	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return storeError(op, err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return storeError(op, err)
	}
	if n == 0 {
		return storeError(op, sql.ErrNoRows)
	}
	return nil
}

type scanner interface {
	Scan(...any) error
}

func utcTime(seconds int64) time.Time { return time.Unix(seconds, 0).UTC() }

// creationTime supplies the current time when omitted and matches DB precision.
func creationTime(t time.Time) time.Time {
	if t.IsZero() {
		t = time.Now()
	}
	return utcTime(t.Unix())
}

func optionalTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.Unix()
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
