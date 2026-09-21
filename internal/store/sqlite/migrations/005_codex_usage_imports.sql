-- Codex's quota endpoint exposes percentage windows but no model/token
-- breakdown. These tables store only browser-extracted aggregates from local
-- rollout JSONL files; prompts and responses are never uploaded or persisted.
CREATE TABLE IF NOT EXISTS codex_usage_import_sources (
    account_id TEXT NOT NULL REFERENCES connected_accounts(id) ON DELETE CASCADE,
    source_id TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    imported_at TEXT NOT NULL,
    PRIMARY KEY (account_id, source_id)
);

CREATE TABLE IF NOT EXISTS codex_usage_import_entries (
    account_id TEXT NOT NULL,
    source_id TEXT NOT NULL,
    usage_date TEXT NOT NULL,
    model TEXT NOT NULL DEFAULT '',
    input_tokens INTEGER NOT NULL CHECK (input_tokens >= 0),
    output_tokens INTEGER NOT NULL CHECK (output_tokens >= 0),
    cache_tokens INTEGER NOT NULL CHECK (cache_tokens >= 0),
    requests INTEGER NOT NULL CHECK (requests >= 0),
    PRIMARY KEY (account_id, source_id, usage_date, model),
    FOREIGN KEY (account_id, source_id)
        REFERENCES codex_usage_import_sources(account_id, source_id)
        ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_codex_usage_import_entries_account_date
    ON codex_usage_import_entries(account_id, usage_date);
