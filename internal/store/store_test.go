package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

var testContext = context.Background()

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "kuraka.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { mustOK(t, s.Close()) })
	return s
}

func mustOK(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func mustErrorIs(t *testing.T, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want errors.Is(_, %v)", err, want)
	}
}

func mustEqual[T any](t *testing.T, got, want T) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func ptr[T any](value T) *T { return &value }

func makeTenant(t *testing.T, s *Store, name string) Tenant {
	t.Helper()
	tenant, err := s.CreateTenant(testContext, name)
	mustOK(t, err)
	return tenant
}

func makeAdmin(t *testing.T, s *Store, email string, tenantID *int64) User {
	t.Helper()
	role := "superadmin"
	if tenantID != nil {
		role = "tenant_admin"
	}
	user, err := s.CreateUser(testContext, User{TenantID: tenantID, Email: email, Role: role, PasswordHash: "password-hash"})
	mustOK(t, err)
	return user
}

func TestOpenMigrationsAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "database with spaces.db")
	s, err := Open(path)
	mustOK(t, err)
	t.Cleanup(func() { _ = s.Close() })
	mustEqual(t, s.db.Stats().MaxOpenConnections, 1)
	var foreignKeys, timeout int
	var mode string
	mustOK(t, s.db.QueryRow(`PRAGMA foreign_keys`).Scan(&foreignKeys))
	mustOK(t, s.db.QueryRow(`PRAGMA busy_timeout`).Scan(&timeout))
	mustOK(t, s.db.QueryRow(`PRAGMA journal_mode`).Scan(&mode))
	mustEqual(t, foreignKeys, 1)
	mustEqual(t, timeout, 5000)
	mustEqual(t, mode, "wal")
	var count int
	var applied int64
	mustOK(t, s.db.QueryRow(`SELECT count(*), applied_at FROM schema_migrations WHERE version = 1`).Scan(&count, &applied))
	mustEqual(t, count, 1)
	if applied <= 0 {
		t.Fatalf("invalid migration timestamp: %d", applied)
	}
	for _, index := range []string{"domains_tenant_id", "users_tenant_id", "sessions_user_id", "sessions_expires_at", "audit_log_tenant_id_id"} {
		mustOK(t, s.db.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE type = 'index' AND name = ?`, index).Scan(&count))
		mustEqual(t, count, 1)
	}
	tenant := makeTenant(t, s, "persisted")
	mustOK(t, s.Close())
	s, err = Open(path)
	mustOK(t, err)
	got, err := s.GetTenant(testContext, tenant.ID)
	mustOK(t, err)
	mustEqual(t, got, tenant)
	var appliedAgain int64
	mustOK(t, s.db.QueryRow(`SELECT count(*), applied_at FROM schema_migrations`).Scan(&count, &appliedAgain))
	mustEqual(t, count, 1)
	mustEqual(t, appliedAgain, applied)
	// Closing the Store closes the underlying pool.
	mustOK(t, s.Close())
	if err := s.db.Ping(); err == nil {
		t.Fatal("database is still open")
	}
	if _, err := Open(filepath.Join(t.TempDir(), "missing", "database.db")); err == nil {
		t.Fatal("Open succeeded with a missing parent directory")
	}
}

func TestTenants(t *testing.T) {
	s := newTestStore(t)
	empty, err := s.ListTenants(testContext)
	mustOK(t, err)
	mustEqual(t, len(empty), 0)
	tenant := makeTenant(t, s, "InkaNode")
	if tenant.ID <= 0 || tenant.CreatedAt.IsZero() || tenant.CreatedAt.Location() != time.UTC || tenant.CreatedAt.Nanosecond() != 0 {
		t.Fatalf("invalid tenant: %+v", tenant)
	}
	got, err := s.GetTenant(testContext, tenant.ID)
	mustOK(t, err)
	mustEqual(t, got, tenant)
	_, err = s.CreateTenant(testContext, "INKANODE")
	mustErrorIs(t, err, ErrConflict)
	second := makeTenant(t, s, "Second")
	list, err := s.ListTenants(testContext)
	mustOK(t, err)
	mustEqual(t, list, []Tenant{tenant, second})
	mustErrorIs(t, s.RenameTenant(testContext, second.ID, "inkanode"), ErrConflict)
	mustOK(t, s.RenameTenant(testContext, tenant.ID, "Renamed"))
	got, err = s.GetTenant(testContext, tenant.ID)
	mustOK(t, err)
	tenant.Name = "Renamed"
	mustEqual(t, got, tenant)
	mustOK(t, s.DeleteTenant(testContext, tenant.ID))
	_, err = s.GetTenant(testContext, tenant.ID)
	mustErrorIs(t, err, ErrNotFound)
	mustErrorIs(t, s.DeleteTenant(testContext, tenant.ID), ErrNotFound)
	mustErrorIs(t, s.RenameTenant(testContext, tenant.ID, "missing"), ErrNotFound)
	third := makeTenant(t, s, "Third")
	if third.ID <= second.ID {
		t.Fatal("tenant IDs were reused")
	}
}

func TestMigrationRollback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "failed-migration.db")
	db, err := sql.Open("sqlite", "file:"+path)
	mustOK(t, err)
	t.Cleanup(func() { _ = db.Close() })
	// Force the second CREATE TABLE in migration 1 to fail, after the first
	// CREATE TABLE has run inside the transaction.
	_, err = db.Exec(`CREATE TABLE users (id INTEGER PRIMARY KEY)`)
	mustOK(t, err)
	failed, err := Open(path)
	if err == nil {
		_ = failed.Close()
		t.Fatal("migration unexpectedly succeeded")
	}
	var count int
	mustOK(t, db.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE type = 'table' AND name = 'tenants'`).Scan(&count))
	mustEqual(t, count, 0)
	mustOK(t, db.QueryRow(`SELECT count(*) FROM schema_migrations`).Scan(&count))
	mustEqual(t, count, 0)
	_, err = db.Exec(`DROP TABLE users`)
	mustOK(t, err)
	s, err := Open(path)
	mustOK(t, err)
	mustOK(t, s.Close())
}

func TestUsers(t *testing.T) {
	s := newTestStore(t)
	tenant := makeTenant(t, s, "Tenant")
	otherTenant := makeTenant(t, s, "Other")
	count, err := s.CountSuperadmins(testContext)
	mustOK(t, err)
	mustEqual(t, count, 0)
	superadmin := makeAdmin(t, s, "  ROOT@EXAMPLE.COM \t", nil)
	mustEqual(t, superadmin.Email, "root@example.com")
	mustEqual(t, superadmin.TenantID, (*int64)(nil))
	mustEqual(t, superadmin.LastLoginAt, (*time.Time)(nil))
	mustEqual(t, superadmin.TOTPSecretEnc, "")
	mustEqual(t, superadmin.TOTPEnabled, false)
	mustEqual(t, superadmin.Disabled, false)
	got, err := s.GetUserByEmail(testContext, " ROOT@Example.com ")
	mustOK(t, err)
	mustEqual(t, got, superadmin)
	_, err = s.CreateUser(testContext, User{Email: "ROOT@example.com", Role: "superadmin", PasswordHash: "hash"})
	mustErrorIs(t, err, ErrConflict)
	stamp := time.Date(2026, 10, 3, 11, 20, 1, 123, time.FixedZone("Lima", -5*3600))
	admin, err := s.CreateUser(testContext, User{
		TenantID: &tenant.ID, Email: " Admin@EXAMPLE.COM ", Name: "Admin", Role: "tenant_admin",
		PasswordHash: "hash", TOTPSecretEnc: "encrypted", TOTPEnabled: true, Disabled: true,
		CreatedAt: stamp, LastLoginAt: &stamp,
	})
	mustOK(t, err)
	mustEqual(t, admin.Email, "admin@example.com")
	mustEqual(t, admin.CreatedAt, stamp.UTC().Truncate(time.Second))
	mustEqual(t, *admin.LastLoginAt, admin.CreatedAt)
	got, err = s.GetUser(testContext, admin.ID)
	mustOK(t, err)
	mustEqual(t, got, admin)
	other := makeAdmin(t, s, "other@example.com", &otherTenant.ID)
	all, err := s.ListUsers(testContext, nil)
	mustOK(t, err)
	mustEqual(t, all, []User{superadmin, admin, other})
	filtered, err := s.ListUsers(testContext, &tenant.ID)
	mustOK(t, err)
	mustEqual(t, filtered, []User{admin})
	missingTenant := int64(99999)
	filtered, err = s.ListUsers(testContext, &missingTenant)
	mustOK(t, err)
	mustEqual(t, len(filtered), 0)
	count, err = s.CountSuperadmins(testContext)
	mustOK(t, err)
	mustEqual(t, count, 1)
	mustErrorIs(t, s.DeleteTenant(testContext, tenant.ID), ErrConflict)
	_, err = s.GetUser(testContext, 99999)
	mustErrorIs(t, err, ErrNotFound)
	_, err = s.GetUserByEmail(testContext, "missing@example.com")
	mustErrorIs(t, err, ErrNotFound)
	mustOK(t, s.TouchLogin(testContext, superadmin.ID, stamp))
	got, err = s.GetUser(testContext, superadmin.ID)
	mustOK(t, err)
	mustEqual(t, *got.LastLoginAt, stamp.UTC().Truncate(time.Second))
	mustErrorIs(t, s.TouchLogin(testContext, 99999, stamp), ErrNotFound)
}

func TestUserConstraintsAndPatches(t *testing.T) {
	s := newTestStore(t)
	tenant := makeTenant(t, s, "Tenant")
	for _, tc := range []struct {
		name     string
		role     string
		tenantID *int64
	}{
		{"superadmin with tenant", "superadmin", &tenant.ID},
		{"tenant admin without tenant", "tenant_admin", nil},
		{"unknown role", "viewer", &tenant.ID},
		{"missing foreign tenant", "tenant_admin", ptr(int64(99999))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.CreateUser(testContext, User{TenantID: tc.tenantID, Email: "bad@example.com", Role: tc.role, PasswordHash: "hash"})
			mustErrorIs(t, err, ErrConflict)
		})
	}
	admin := makeAdmin(t, s, "admin@example.com", &tenant.ID)
	mustOK(t, s.UpdateUser(testContext, admin.ID, UserPatch{}))
	mustErrorIs(t, s.UpdateUser(testContext, 99999, UserPatch{}), ErrNotFound)
	mustErrorIs(t, s.UpdateUser(testContext, 99999, UserPatch{Name: ptr("missing")}), ErrNotFound)
	mustOK(t, s.UpdateUser(testContext, admin.ID, UserPatch{
		Name: ptr("Changed"), PasswordHash: ptr("new hash"), TOTPSecretEnc: ptr("new secret"), TOTPEnabled: ptr(true), Disabled: ptr(true),
	}))
	got, err := s.GetUser(testContext, admin.ID)
	mustOK(t, err)
	want := admin
	want.Name, want.PasswordHash, want.TOTPSecretEnc, want.TOTPEnabled, want.Disabled = "Changed", "new hash", "new secret", true, true
	mustEqual(t, got, want)
	// Patch failures must preserve every field, including fields preceding the
	// invalid role in the generated UPDATE.
	mustErrorIs(t, s.UpdateUser(testContext, admin.ID, UserPatch{Name: ptr("lost"), Role: ptr("viewer")}), ErrConflict)
	mustErrorIs(t, s.UpdateUser(testContext, admin.ID, UserPatch{Role: ptr("superadmin")}), ErrConflict)
	noTenant := (*int64)(nil)
	mustErrorIs(t, s.UpdateUser(testContext, admin.ID, UserPatch{TenantID: &noTenant}), ErrConflict)
	got, err = s.GetUser(testContext, admin.ID)
	mustOK(t, err)
	mustEqual(t, got, want)
	// Clear tenant and promote in one statement to satisfy the CHECK atomically.
	mustOK(t, s.UpdateUser(testContext, admin.ID, UserPatch{Role: ptr("superadmin"), TenantID: &noTenant}))
	want.Role, want.TenantID = "superadmin", nil
	got, err = s.GetUser(testContext, admin.ID)
	mustOK(t, err)
	mustEqual(t, got, want)
	count, err := s.CountSuperadmins(testContext)
	mustOK(t, err)
	mustEqual(t, count, 1)
	// Set tenant, demote, and explicitly clear strings and bools.
	newTenant := makeTenant(t, s, "New tenant")
	tenantID := &newTenant.ID
	mustOK(t, s.UpdateUser(testContext, admin.ID, UserPatch{
		Role: ptr("tenant_admin"), TenantID: &tenantID, Name: ptr(""), PasswordHash: ptr(""),
		TOTPSecretEnc: ptr(""), TOTPEnabled: ptr(false), Disabled: ptr(false),
	}))
	want.Role, want.TenantID = "tenant_admin", tenantID
	want.Name, want.PasswordHash, want.TOTPSecretEnc, want.TOTPEnabled, want.Disabled = "", "", "", false, false
	got, err = s.GetUser(testContext, admin.ID)
	mustOK(t, err)
	mustEqual(t, got, want)
	var secret sql.NullString
	mustOK(t, s.db.QueryRow(`SELECT totp_secret_enc FROM users WHERE id = ?`, admin.ID).Scan(&secret))
	mustEqual(t, secret.Valid, false)
	mustOK(t, s.DeleteTenant(testContext, tenant.ID))
}

func TestDomains(t *testing.T) {
	s := newTestStore(t)
	tenant := makeTenant(t, s, "Tenant")
	otherTenant := makeTenant(t, s, "Other")
	empty, err := s.ListDomains(testContext, nil)
	mustOK(t, err)
	mustEqual(t, len(empty), 0)
	domain, err := s.CreateDomain(testContext, tenant.ID, "Example.COM", "sid-1")
	mustOK(t, err)
	mustEqual(t, domain.Name, "example.com")
	if domain.ID <= 0 || domain.CreatedAt.IsZero() || domain.CreatedAt.Location() != time.UTC || domain.CreatedAt.Nanosecond() != 0 {
		t.Fatalf("invalid domain: %+v", domain)
	}
	for _, lookup := range []func() (Domain, error){
		func() (Domain, error) { return s.GetDomain(testContext, domain.ID) },
		func() (Domain, error) { return s.GetDomainByName(testContext, "EXAMPLE.COM") },
		func() (Domain, error) { return s.GetDomainByStalwartID(testContext, "sid-1") },
	} {
		got, err := lookup()
		mustOK(t, err)
		mustEqual(t, got, domain)
	}
	_, err = s.CreateDomain(testContext, tenant.ID, "EXAMPLE.COM", "sid-2")
	mustErrorIs(t, err, ErrConflict)
	_, err = s.CreateDomain(testContext, tenant.ID, "different.com", "sid-1")
	mustErrorIs(t, err, ErrConflict)
	_, err = s.CreateDomain(testContext, 99999, "missing.com", "sid-3")
	mustErrorIs(t, err, ErrConflict)
	other, err := s.CreateDomain(testContext, otherTenant.ID, "other.com", "sid-2")
	mustOK(t, err)
	all, err := s.ListDomains(testContext, nil)
	mustOK(t, err)
	mustEqual(t, all, []Domain{domain, other})
	filtered, err := s.ListDomains(testContext, &tenant.ID)
	mustOK(t, err)
	mustEqual(t, filtered, []Domain{domain})
	filtered, err = s.ListDomains(testContext, ptr(int64(99999)))
	mustOK(t, err)
	mustEqual(t, len(filtered), 0)
	mustErrorIs(t, s.DeleteTenant(testContext, tenant.ID), ErrConflict)
	mustOK(t, s.DeleteDomain(testContext, domain.ID))
	mustOK(t, s.DeleteTenant(testContext, tenant.ID))
	mustErrorIs(t, s.DeleteDomain(testContext, domain.ID), ErrNotFound)
	_, err = s.GetDomain(testContext, domain.ID)
	mustErrorIs(t, err, ErrNotFound)
	_, err = s.GetDomainByName(testContext, domain.Name)
	mustErrorIs(t, err, ErrNotFound)
	_, err = s.GetDomainByStalwartID(testContext, domain.StalwartID)
	mustErrorIs(t, err, ErrNotFound)
}

func TestSessions(t *testing.T) {
	s := newTestStore(t)
	user := makeAdmin(t, s, "root@example.com", nil)
	other := makeAdmin(t, s, "other@example.com", nil)
	now := time.Date(2026, 11, 3, 12, 0, 0, 1234, time.FixedZone("Lima", -5*3600))
	session := Session{ID: "session-token-hash", UserID: user.ID, MFAPending: true,
		CreatedAt: now, ExpiresAt: now.Add(time.Hour), IP: "127.0.0.1", UserAgent: "test browser"}
	mustOK(t, s.CreateSession(testContext, session))
	got, err := s.GetSession(testContext, session.ID, now)
	mustOK(t, err)
	session.CreatedAt = session.CreatedAt.UTC().Truncate(time.Second)
	session.ExpiresAt = session.ExpiresAt.UTC().Truncate(time.Second)
	mustEqual(t, got, session)
	mustErrorIs(t, s.CreateSession(testContext, session), ErrConflict)
	bad := session
	bad.ID, bad.UserID = "missing-user", 99999
	mustErrorIs(t, s.CreateSession(testContext, bad), ErrConflict)
	mustOK(t, s.CompleteMFA(testContext, session.ID))
	mustOK(t, s.CompleteMFA(testContext, session.ID))
	got, err = s.GetSession(testContext, session.ID, now)
	mustOK(t, err)
	session.MFAPending = false
	mustEqual(t, got, session)
	mustErrorIs(t, s.CompleteMFA(testContext, "missing"), ErrNotFound)
	_, err = s.GetSession(testContext, "missing", now)
	mustErrorIs(t, err, ErrNotFound)
	_, err = s.GetSession(testContext, session.ID, session.ExpiresAt)
	mustErrorIs(t, err, ErrNotFound)
	_, err = s.GetSession(testContext, session.ID, session.ExpiresAt.Add(time.Second))
	mustErrorIs(t, err, ErrNotFound)
	mustOK(t, s.DeleteSession(testContext, session.ID))
	mustOK(t, s.DeleteSession(testContext, session.ID))
	_, err = s.GetSession(testContext, session.ID, now)
	mustErrorIs(t, err, ErrNotFound)
	for _, entry := range []Session{
		{ID: "user-1", UserID: user.ID, ExpiresAt: now.Add(time.Hour)},
		{ID: "user-2", UserID: user.ID, ExpiresAt: now.Add(time.Hour)},
		{ID: "other", UserID: other.ID, ExpiresAt: now.Add(time.Hour)},
		{ID: "expired", UserID: other.ID, ExpiresAt: now.Add(-time.Second)},
		{ID: "boundary", UserID: other.ID, ExpiresAt: now},
	} {
		mustOK(t, s.CreateSession(testContext, entry))
	}
	mustOK(t, s.DeleteUserSessions(testContext, user.ID))
	mustOK(t, s.DeleteUserSessions(testContext, 99999))
	for _, id := range []string{"user-1", "user-2"} {
		_, err = s.GetSession(testContext, id, now)
		mustErrorIs(t, err, ErrNotFound)
	}
	got, err = s.GetSession(testContext, "other", now)
	mustOK(t, err)
	if got.CreatedAt.IsZero() || got.CreatedAt.Location() != time.UTC {
		t.Fatalf("invalid default session creation time: %v", got.CreatedAt)
	}
	count, err := s.PurgeExpiredSessions(testContext, now)
	mustOK(t, err)
	mustEqual(t, count, int64(2))
	count, err = s.PurgeExpiredSessions(testContext, now)
	mustOK(t, err)
	mustEqual(t, count, int64(0))
	var remaining int
	mustOK(t, s.db.QueryRow(`SELECT count(*) FROM sessions`).Scan(&remaining))
	mustEqual(t, remaining, 1)
	// The schema cascades session deletion when an owning user is removed.
	_, err = s.db.Exec(`DELETE FROM users WHERE id = ?`, other.ID)
	mustOK(t, err)
	_, err = s.GetSession(testContext, "other", now)
	mustErrorIs(t, err, ErrNotFound)
}

func TestAudit(t *testing.T) {
	s := newTestStore(t)
	tenant := makeTenant(t, s, "Tenant")
	otherTenant := makeTenant(t, s, "Other")
	user := makeAdmin(t, s, "admin@example.com", &tenant.ID)
	empty, err := s.ListAudit(testContext, nil, 0, 20)
	mustOK(t, err)
	mustEqual(t, len(empty), 0)
	stamp := time.Date(2026, 11, 3, 12, 30, 0, 999, time.FixedZone("Lima", -5*3600))
	entry := AuditEntry{At: stamp, UserID: &user.ID, TenantID: &tenant.ID,
		Action: "domain.create", Target: "example.com", Result: "ok", Detail: "created", IP: "127.0.0.1"}
	mustOK(t, s.AddAudit(testContext, entry))
	entry.ID, entry.At = 1, stamp.UTC().Truncate(time.Second)
	rows, err := s.ListAudit(testContext, &tenant.ID, 0, 10)
	mustOK(t, err)
	mustEqual(t, rows, []AuditEntry{entry})
	// Audit identifiers deliberately have no FKs so historical records survive
	// deletion and can record attempts concerning absent principals or tenants.
	mustOK(t, s.AddAudit(testContext, AuditEntry{Action: "login", Result: "denied"}))
	mustOK(t, s.AddAudit(testContext, AuditEntry{TenantID: &otherTenant.ID, Action: "domain.delete", Result: "error"}))
	mustOK(t, s.AddAudit(testContext, AuditEntry{TenantID: &tenant.ID, Action: "domain.update", Result: "ok"}))
	mustOK(t, s.AddAudit(testContext, AuditEntry{UserID: ptr(int64(99999)), TenantID: ptr(int64(99999)), Action: "missing", Result: "denied"}))
	mustErrorIs(t, s.AddAudit(testContext, AuditEntry{Action: "bad", Result: "invalid"}), ErrConflict)
	rows, err = s.ListAudit(testContext, nil, 0, 10)
	mustOK(t, err)
	mustEqual(t, len(rows), 5)
	for i, row := range rows {
		mustEqual(t, row.ID, int64(5-i))
		if row.At.IsZero() || row.At.Location() != time.UTC || row.At.Nanosecond() != 0 {
			t.Fatalf("invalid audit timestamp: %v", row.At)
		}
	}
	mustEqual(t, rows[3].UserID, (*int64)(nil))
	mustEqual(t, rows[3].TenantID, (*int64)(nil))
	filtered, err := s.ListAudit(testContext, &tenant.ID, 0, 10)
	mustOK(t, err)
	mustEqual(t, len(filtered), 2)
	mustEqual(t, filtered[0].ID, int64(4))
	mustEqual(t, filtered[1], entry)
	next, err := s.ListAudit(testContext, &tenant.ID, filtered[0].ID, 10)
	mustOK(t, err)
	mustEqual(t, next, []AuditEntry{entry})
	page, err := s.ListAudit(testContext, nil, 4, 2)
	mustOK(t, err)
	mustEqual(t, len(page), 2)
	mustEqual(t, page[0].ID, int64(3))
	mustEqual(t, page[1].ID, int64(2))
	empty, err = s.ListAudit(testContext, &tenant.ID, 1, 10)
	mustOK(t, err)
	mustEqual(t, len(empty), 0)
}

func TestAuditLimitClamping(t *testing.T) {
	s := newTestStore(t)
	for i := 0; i < 205; i++ {
		mustOK(t, s.AddAudit(testContext, AuditEntry{Action: "test", Result: "ok"}))
	}
	for _, tc := range []struct{ limit, want int }{
		{-10, 1}, {0, 1}, {1, 1}, {12, 12}, {200, 200}, {201, 200},
	} {
		entries, err := s.ListAudit(testContext, nil, 0, tc.limit)
		mustOK(t, err)
		mustEqual(t, len(entries), tc.want)
		mustEqual(t, entries[0].ID, int64(205))
	}
}

func TestContextCancellation(t *testing.T) {
	s := newTestStore(t)
	ctx, cancel := context.WithCancel(testContext)
	cancel()
	_, err := s.CreateTenant(ctx, "cancelled")
	mustErrorIs(t, err, context.Canceled)
	_, err = s.ListTenants(ctx)
	mustErrorIs(t, err, context.Canceled)
	_, err = s.GetUser(ctx, 1)
	mustErrorIs(t, err, context.Canceled)
}
