-- The Page, Instagram account or phone number id of a Meta channel, so Kuraka
-- can pass the app's webhook events to the helpdesk that owns it.
ALTER TABLE meta_channels ADD COLUMN external_id TEXT NOT NULL DEFAULT '';
CREATE INDEX meta_channels_external_id ON meta_channels(channel, external_id);
