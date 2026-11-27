-- Meta channels (WhatsApp; later Messenger and Instagram) a domain connected
-- to a helpdesk. Libredesk keeps the credentials; Kuraka keeps the webhook
-- verify token it generated, as Libredesk only returns it masked.
CREATE TABLE meta_channels (
    id INTEGER PRIMARY KEY,
    domain_id INTEGER NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    helpdesk_id INTEGER NOT NULL REFERENCES helpdesks(id) ON DELETE CASCADE,
    channel TEXT NOT NULL,
    inbox_id INTEGER NOT NULL,
    name TEXT NOT NULL,
    verify_token TEXT NOT NULL,
    created_at INTEGER NOT NULL
);
CREATE INDEX meta_channels_domain_id ON meta_channels(domain_id);
CREATE INDEX meta_channels_helpdesk_id ON meta_channels(helpdesk_id);
