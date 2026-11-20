-- Live chat widgets a domain created in a helpdesk. Libredesk keeps the
-- widget settings; name and website are copies for listing.
CREATE TABLE livechats (
    id INTEGER PRIMARY KEY,
    domain_id INTEGER NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    helpdesk_id INTEGER NOT NULL REFERENCES helpdesks(id) ON DELETE CASCADE,
    inbox_id INTEGER NOT NULL,
    inbox_uuid TEXT NOT NULL,
    name TEXT NOT NULL,
    website_url TEXT NOT NULL,
    created_at INTEGER NOT NULL
);
CREATE INDEX livechats_domain_id ON livechats(domain_id);
CREATE INDEX livechats_helpdesk_id ON livechats(helpdesk_id);
