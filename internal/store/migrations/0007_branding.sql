-- Branding per app, global (tenant_id NULL) or per tenant. data is a JSON
-- object with the store.Branding fields; images are branding_assets ids.
CREATE TABLE branding (
    tenant_id INTEGER NULL REFERENCES tenants(id) ON DELETE CASCADE,
    app TEXT NOT NULL CHECK(app IN ('default', 'kuraka', 'stalwart', 'desk')),
    data TEXT NOT NULL
);

CREATE UNIQUE INDEX branding_scope_app ON branding(IFNULL(tenant_id, 0), app);

CREATE TABLE branding_assets (
    id TEXT PRIMARY KEY,
    content_type TEXT NOT NULL,
    data BLOB NOT NULL,
    created_at INTEGER NOT NULL
);
