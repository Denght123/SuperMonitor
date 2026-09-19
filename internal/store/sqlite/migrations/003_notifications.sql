CREATE TABLE IF NOT EXISTS notification_channels (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL,
    name TEXT NOT NULL,
    target TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1,
    encrypted_payload BLOB NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS notification_events (
    dedupe_key TEXT PRIMARY KEY,
    account_id TEXT NOT NULL REFERENCES connected_accounts(id) ON DELETE CASCADE,
    alert_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS notification_deliveries (
    dedupe_key TEXT NOT NULL REFERENCES notification_events(dedupe_key) ON DELETE CASCADE,
    channel_id TEXT NOT NULL REFERENCES notification_channels(id) ON DELETE CASCADE,
    event_type TEXT NOT NULL,
    sent_at TEXT NOT NULL,
    PRIMARY KEY (dedupe_key, channel_id)
);

CREATE TABLE IF NOT EXISTS notification_states (
    state_key TEXT PRIMARY KEY,
    account_id TEXT NOT NULL REFERENCES connected_accounts(id) ON DELETE CASCADE,
    state_value TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_notification_deliveries_sent_at
    ON notification_deliveries(sent_at);

CREATE INDEX IF NOT EXISTS idx_notification_events_account
    ON notification_events(account_id);

CREATE INDEX IF NOT EXISTS idx_notification_states_account
    ON notification_states(account_id);
