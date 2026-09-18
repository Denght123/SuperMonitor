CREATE TABLE IF NOT EXISTS connected_accounts (
    id TEXT PRIMARY KEY,
    provider_id TEXT NOT NULL,
    alias TEXT NOT NULL,
    email TEXT NOT NULL DEFAULT '',
    plan TEXT NOT NULL DEFAULT '',
    auth_method TEXT NOT NULL,
    status TEXT NOT NULL,
    source TEXT NOT NULL,
    last_refreshed_at TEXT,
    next_refresh_at TEXT,
    error TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS account_credentials (
    account_id TEXT PRIMARY KEY REFERENCES connected_accounts(id) ON DELETE CASCADE,
    encrypted_payload BLOB NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS account_quota_windows (
    id TEXT PRIMARY KEY,
    account_id TEXT NOT NULL REFERENCES connected_accounts(id) ON DELETE CASCADE,
    label TEXT NOT NULL,
    kind TEXT NOT NULL,
    value REAL NOT NULL,
    total REAL,
    unit TEXT NOT NULL,
    remaining_percent REAL,
    window_seconds INTEGER,
    reset_at TEXT,
    expires_at TEXT,
    status TEXT NOT NULL,
    source TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_account_quota_windows_account
    ON account_quota_windows(account_id);
