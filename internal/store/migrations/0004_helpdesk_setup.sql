ALTER TABLE helpdesks ADD COLUMN status TEXT NOT NULL DEFAULT 'ready' CHECK(status IN ('pending','ready'));
ALTER TABLE helpdesks ADD COLUMN setup_secret_enc TEXT NOT NULL DEFAULT '';
