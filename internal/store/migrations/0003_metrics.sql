CREATE TABLE metric_samples (
    at INTEGER NOT NULL,
    name TEXT NOT NULL,
    value REAL NOT NULL
);

CREATE INDEX metric_samples_name_at ON metric_samples(name, at);

CREATE TABLE alert_rules (
    id INTEGER PRIMARY KEY,
    metric TEXT NOT NULL,
    threshold REAL NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1,
    firing_since INTEGER NULL,
    created_at INTEGER NOT NULL
);
