CREATE TABLE tenants (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE COLLATE NOCASE,
    created_at INTEGER NOT NULL
);

CREATE TABLE users (
    id INTEGER PRIMARY KEY,
    tenant_id INTEGER NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    email TEXT NOT NULL UNIQUE COLLATE NOCASE,
    name TEXT NOT NULL DEFAULT '',
    role TEXT NOT NULL CHECK(role IN ('superadmin', 'tenant_admin')),
    password_hash TEXT NOT NULL,
    totp_secret_enc TEXT NULL,
    totp_enabled INTEGER NOT NULL DEFAULT 0,
    disabled INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    last_login_at INTEGER NULL,
    CHECK((role = 'superadmin') = (tenant_id IS NULL))
);

CREATE TABLE domains (
    id INTEGER PRIMARY KEY,
    tenant_id INTEGER NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    name TEXT NOT NULL UNIQUE COLLATE NOCASE,
    stalwart_id TEXT NOT NULL UNIQUE,
    created_at INTEGER NOT NULL
);

CREATE TABLE sessions (
    id TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    mfa_pending INTEGER NOT NULL,
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    ip TEXT NOT NULL DEFAULT '',
    user_agent TEXT NOT NULL DEFAULT ''
);

CREATE TABLE audit_log (
    id INTEGER PRIMARY KEY,
    at INTEGER NOT NULL,
    user_id INTEGER NULL,
    tenant_id INTEGER NULL,
    action TEXT NOT NULL,
    target TEXT NOT NULL DEFAULT '',
    result TEXT NOT NULL CHECK(result IN ('ok', 'denied', 'error')),
    detail TEXT NOT NULL DEFAULT '',
    ip TEXT NOT NULL DEFAULT ''
);

CREATE INDEX domains_tenant_id ON domains(tenant_id);
CREATE INDEX users_tenant_id ON users(tenant_id);
CREATE INDEX sessions_user_id ON sessions(user_id);
CREATE INDEX sessions_expires_at ON sessions(expires_at);
CREATE INDEX audit_log_tenant_id_id ON audit_log(tenant_id, id);
