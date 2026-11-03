package store

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

type User struct {
	ID            int64
	TenantID      *int64
	Email         string
	Name          string
	Role          string
	PasswordHash  string
	TOTPSecretEnc string
	TOTPEnabled   bool
	Disabled      bool
	CreatedAt     time.Time
	LastLoginAt   *time.Time
}

// UserPatch changes only non-nil fields. A non-nil TenantID pointing to nil
// explicitly clears the tenant, allowing an atomic promotion to superadmin.
type UserPatch struct {
	Name          *string
	Role          *string
	TenantID      **int64
	PasswordHash  *string
	TOTPSecretEnc *string
	TOTPEnabled   *bool
	Disabled      *bool
}

const userColumns = `id, tenant_id, email, name, role, password_hash,
	totp_secret_enc, totp_enabled, disabled, created_at, last_login_at`

func scanUser(row scanner) (User, error) {
	var user User
	var tenantID, login sql.NullInt64
	var secret sql.NullString
	var created int64
	err := row.Scan(&user.ID, &tenantID, &user.Email, &user.Name, &user.Role,
		&user.PasswordHash, &secret, &user.TOTPEnabled, &user.Disabled, &created, &login)
	if tenantID.Valid {
		user.TenantID = &tenantID.Int64
	}
	if login.Valid {
		t := utcTime(login.Int64)
		user.LastLoginAt = &t
	}
	user.TOTPSecretEnc = secret.String
	user.CreatedAt = utcTime(created)
	return user, err
}

func normalizeEmail(email string) string { return strings.ToLower(strings.TrimSpace(email)) }

func (s *Store) CreateUser(ctx context.Context, user User) (User, error) {
	user.Email = normalizeEmail(user.Email)
	user.CreatedAt = creationTime(user.CreatedAt)
	if user.LastLoginAt != nil {
		login := utcTime(user.LastLoginAt.Unix())
		user.LastLoginAt = &login
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO users
		(tenant_id, email, name, role, password_hash, totp_secret_enc,
		 totp_enabled, disabled, created_at, last_login_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, user.TenantID, user.Email,
		user.Name, user.Role, user.PasswordHash, nullableString(user.TOTPSecretEnc),
		user.TOTPEnabled, user.Disabled, user.CreatedAt.Unix(), optionalTime(user.LastLoginAt))
	if err != nil {
		return User{}, storeError("create user", err)
	}
	user.ID, err = result.LastInsertId()
	return user, storeError("create user", err)
}

func (s *Store) GetUser(ctx context.Context, id int64) (User, error) {
	user, err := scanUser(s.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE id = ?`, id))
	return user, storeError("get user", err)
}

func (s *Store) GetUserByEmail(ctx context.Context, email string) (User, error) {
	user, err := scanUser(s.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE email = ?`, normalizeEmail(email)))
	return user, storeError("get user by email", err)
}

func (s *Store) ListUsers(ctx context.Context, tenantID *int64) ([]User, error) {
	query := `SELECT ` + userColumns + ` FROM users`
	var args []any
	if tenantID != nil {
		query += ` WHERE tenant_id = ?`
		args = append(args, *tenantID)
	}
	rows, err := s.db.QueryContext(ctx, query+` ORDER BY id`, args...)
	if err != nil {
		return nil, storeError("list users", err)
	}
	defer rows.Close()
	users := make([]User, 0)
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, storeError("list users", err)
		}
		users = append(users, user)
	}
	return users, storeError("list users", rows.Err())
}

func (s *Store) UpdateUser(ctx context.Context, id int64, patch UserPatch) error {
	var fields []string
	var args []any
	add := func(column string, value any) {
		fields = append(fields, column+" = ?")
		args = append(args, value)
	}
	if patch.Name != nil {
		add("name", *patch.Name)
	}
	if patch.Role != nil {
		add("role", *patch.Role)
	}
	if patch.TenantID != nil {
		add("tenant_id", *patch.TenantID)
	}
	if patch.PasswordHash != nil {
		add("password_hash", *patch.PasswordHash)
	}
	if patch.TOTPSecretEnc != nil {
		add("totp_secret_enc", nullableString(*patch.TOTPSecretEnc))
	}
	if patch.TOTPEnabled != nil {
		add("totp_enabled", *patch.TOTPEnabled)
	}
	if patch.Disabled != nil {
		add("disabled", *patch.Disabled)
	}
	if len(fields) == 0 {
		var exists int
		return storeError("update user", s.db.QueryRowContext(ctx, `SELECT 1 FROM users WHERE id = ?`, id).Scan(&exists))
	}
	args = append(args, id)
	return s.execOne(ctx, "update user", `UPDATE users SET `+strings.Join(fields, ", ")+` WHERE id = ?`, args...)
}

func (s *Store) CountSuperadmins(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM users WHERE role = 'superadmin'`).Scan(&count)
	return count, storeError("count superadmins", err)
}

func (s *Store) TouchLogin(ctx context.Context, id int64, t time.Time) error {
	return s.execOne(ctx, "touch login", `UPDATE users SET last_login_at = ? WHERE id = ?`, t.Unix(), id)
}
