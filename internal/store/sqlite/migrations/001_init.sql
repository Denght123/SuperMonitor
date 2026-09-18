PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS providers (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    region TEXT NOT NULL,
    tier TEXT NOT NULL,
    status TEXT NOT NULL,
    auth_methods TEXT NOT NULL,
    capabilities TEXT NOT NULL,
    last_checked_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS accounts (
    id TEXT PRIMARY KEY,
    provider_id TEXT NOT NULL REFERENCES providers(id),
    alias TEXT NOT NULL,
    services TEXT NOT NULL,
    primary_metric TEXT NOT NULL,
    secondary_metric TEXT NOT NULL,
    status TEXT NOT NULL,
    source TEXT NOT NULL,
    last_refreshed_at TEXT NOT NULL,
    next_refresh_at TEXT NOT NULL,
    error TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS usage_daily (
    usage_date TEXT PRIMARY KEY,
    input_tokens INTEGER NOT NULL,
    output_tokens INTEGER NOT NULL,
    cache_tokens INTEGER NOT NULL,
    requests INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS model_usage (
    model TEXT PRIMARY KEY,
    tokens INTEGER NOT NULL,
    color TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS quota_signals (
    id TEXT PRIMARY KEY,
    provider TEXT NOT NULL,
    label TEXT NOT NULL,
    kind TEXT NOT NULL,
    value REAL NOT NULL,
    total REAL,
    unit TEXT NOT NULL,
    remaining_percent REAL,
    reset_at TEXT,
    expires_at TEXT,
    status TEXT NOT NULL,
    source TEXT NOT NULL,
    confidence TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS alerts (
    id TEXT PRIMARY KEY,
    severity TEXT NOT NULL,
    title TEXT NOT NULL,
    message TEXT NOT NULL,
    provider TEXT NOT NULL,
    created_at TEXT NOT NULL,
    recovery TEXT NOT NULL
);
