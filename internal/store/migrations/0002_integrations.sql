CREATE TABLE dns_providers (
    id INTEGER PRIMARY KEY,
    tenant_id INTEGER NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    kind TEXT NOT NULL CHECK(kind = 'cloudflare'),
    name TEXT NOT NULL,
    token_enc TEXT NOT NULL,
    created_at INTEGER NOT NULL
);

ALTER TABLE domains ADD COLUMN dns_provider_id INTEGER NULL REFERENCES dns_providers(id) ON DELETE SET NULL;

CREATE TABLE helpdesks (
    id INTEGER PRIMARY KEY,
    tenant_id INTEGER NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    kind TEXT NOT NULL CHECK(kind = 'libredesk'),
    name TEXT NOT NULL,
    url TEXT NOT NULL,
    api_key TEXT NOT NULL,
    api_secret_enc TEXT NOT NULL,
    created_at INTEGER NOT NULL
);

CREATE TABLE helpdesk_links (
    id INTEGER PRIMARY KEY,
    domain_id INTEGER NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    account_id TEXT NOT NULL,
    helpdesk_id INTEGER NOT NULL REFERENCES helpdesks(id) ON DELETE CASCADE,
    inbox_id INTEGER NOT NULL,
    app_password_id TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    UNIQUE(account_id, helpdesk_id)
);

CREATE TABLE migration_jobs (
    id INTEGER PRIMARY KEY,
    domain_id INTEGER NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    account_id TEXT NOT NULL,
    source_host TEXT NOT NULL,
    source_port INTEGER NOT NULL,
    source_tls TEXT NOT NULL,
    source_user TEXT NOT NULL,
    source_secret_enc TEXT NULL,
    status TEXT NOT NULL CHECK(status IN ('queued', 'running', 'done', 'failed', 'cancelled')),
    cancel_requested INTEGER NOT NULL DEFAULT 0,
    folders_total INTEGER NOT NULL DEFAULT 0,
    folders_done INTEGER NOT NULL DEFAULT 0,
    messages_total INTEGER NOT NULL DEFAULT 0,
    messages_copied INTEGER NOT NULL DEFAULT 0,
    messages_skipped INTEGER NOT NULL DEFAULT 0,
    error TEXT NOT NULL DEFAULT '',
    created_by INTEGER NULL REFERENCES users(id) ON DELETE SET NULL,
    created_at INTEGER NOT NULL,
    started_at INTEGER NULL,
    finished_at INTEGER NULL
);

CREATE INDEX dns_providers_tenant_id ON dns_providers(tenant_id);
CREATE INDEX domains_dns_provider_id ON domains(dns_provider_id);
CREATE INDEX helpdesks_tenant_id ON helpdesks(tenant_id);
CREATE INDEX helpdesk_links_domain_id ON helpdesk_links(domain_id);
CREATE INDEX helpdesk_links_helpdesk_id ON helpdesk_links(helpdesk_id);
CREATE INDEX migration_jobs_domain_id ON migration_jobs(domain_id);
CREATE INDEX migration_jobs_status ON migration_jobs(status);
