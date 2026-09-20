-- Provider usage endpoints generally expose monotonically increasing counters.
-- Keep the latest verified counter as a baseline, then persist only positive
-- deltas. Both tables are account-scoped so deleting a connected account also
-- removes its usage history.
CREATE TABLE IF NOT EXISTS usage_cumulative_snapshots (
    account_id TEXT NOT NULL REFERENCES connected_accounts(id) ON DELETE CASCADE,
    provider_id TEXT NOT NULL,
    model TEXT NOT NULL DEFAULT '',
    input_tokens INTEGER NOT NULL CHECK (input_tokens >= 0),
    output_tokens INTEGER NOT NULL CHECK (output_tokens >= 0),
    cache_tokens INTEGER NOT NULL CHECK (cache_tokens >= 0),
    requests INTEGER NOT NULL CHECK (requests >= 0),
    captured_at TEXT NOT NULL,
    PRIMARY KEY (account_id, provider_id, model)
);

CREATE TABLE IF NOT EXISTS usage_daily_attributed (
    usage_date TEXT NOT NULL,
    account_id TEXT NOT NULL REFERENCES connected_accounts(id) ON DELETE CASCADE,
    provider_id TEXT NOT NULL,
    model TEXT NOT NULL DEFAULT '',
    input_tokens INTEGER NOT NULL CHECK (input_tokens >= 0),
    output_tokens INTEGER NOT NULL CHECK (output_tokens >= 0),
    cache_tokens INTEGER NOT NULL CHECK (cache_tokens >= 0),
    requests INTEGER NOT NULL CHECK (requests >= 0),
    PRIMARY KEY (usage_date, account_id, provider_id, model)
);

CREATE INDEX IF NOT EXISTS idx_usage_daily_attributed_date
    ON usage_daily_attributed(usage_date);

CREATE INDEX IF NOT EXISTS idx_usage_daily_attributed_model
    ON usage_daily_attributed(model, usage_date);

CREATE TABLE IF NOT EXISTS usage_backfill_states (
    account_id TEXT NOT NULL REFERENCES connected_accounts(id) ON DELETE CASCADE,
    provider_id TEXT NOT NULL,
    source_key TEXT NOT NULL,
    completed_at TEXT NOT NULL,
    PRIMARY KEY (account_id, provider_id, source_key)
);
