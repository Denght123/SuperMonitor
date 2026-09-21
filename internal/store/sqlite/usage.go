package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"hash/fnv"
	"strings"
	"time"

	"github.com/Denght123/SuperMonitor/internal/domain"
)

var ErrUsageSnapshotOutOfOrder = errors.New("usage snapshot is not newer than the stored baseline")

const maxImportedUsageCounter int64 = 1_000_000_000_000_000

// RecordUsageSnapshot stores a verified cumulative provider reading and adds
// only its positive delta to the daily aggregate. The first observation is a
// baseline and intentionally contributes no usage. If any cumulative counter
// rolls back, the whole observation becomes the new baseline; this avoids
// inventing usage across billing-period resets or provider-side counter resets.
func (s *Store) RecordUsageSnapshot(ctx context.Context, snapshot domain.UsageSnapshot) (domain.UsageSnapshotResult, error) {
	snapshot.AccountID = strings.TrimSpace(snapshot.AccountID)
	snapshot.ProviderID = strings.TrimSpace(snapshot.ProviderID)
	snapshot.Model = strings.TrimSpace(snapshot.Model)
	snapshot.CapturedAt = snapshot.CapturedAt.UTC()
	if err := validateUsageSnapshot(snapshot); err != nil {
		return domain.UsageSnapshotResult{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.UsageSnapshotResult{}, err
	}
	defer tx.Rollback()

	var connectedProviderID string
	if err := tx.QueryRowContext(ctx, "SELECT provider_id FROM connected_accounts WHERE id=?", snapshot.AccountID).Scan(&connectedProviderID); err != nil {
		return domain.UsageSnapshotResult{}, err
	}
	if connectedProviderID != snapshot.ProviderID {
		return domain.UsageSnapshotResult{}, fmt.Errorf("usage snapshot provider %q does not match account provider %q", snapshot.ProviderID, connectedProviderID)
	}

	var previous domain.UsageCounters
	var previousCapturedAtText string
	err = tx.QueryRowContext(ctx, `SELECT input_tokens, output_tokens, cache_tokens, requests, captured_at
		FROM usage_cumulative_snapshots WHERE account_id=? AND provider_id=? AND model=?`,
		snapshot.AccountID, snapshot.ProviderID, snapshot.Model,
	).Scan(&previous.InputTokens, &previous.OutputTokens, &previous.CacheTokens, &previous.Requests, &previousCapturedAtText)
	if errors.Is(err, sql.ErrNoRows) {
		if err := insertUsageBaseline(ctx, tx, snapshot); err != nil {
			return domain.UsageSnapshotResult{}, err
		}
		if err := tx.Commit(); err != nil {
			return domain.UsageSnapshotResult{}, err
		}
		return domain.UsageSnapshotResult{BaselineCreated: true}, nil
	}
	if err != nil {
		return domain.UsageSnapshotResult{}, err
	}

	previousCapturedAt, err := time.Parse(time.RFC3339Nano, previousCapturedAtText)
	if err != nil {
		return domain.UsageSnapshotResult{}, fmt.Errorf("parse stored usage snapshot timestamp: %w", err)
	}
	if snapshot.CapturedAt.Before(previousCapturedAt) {
		return domain.UsageSnapshotResult{}, fmt.Errorf("%w: captured at %s, baseline is %s", ErrUsageSnapshotOutOfOrder, snapshot.CapturedAt.Format(time.RFC3339Nano), previousCapturedAt.Format(time.RFC3339Nano))
	}
	if snapshot.CapturedAt.Equal(previousCapturedAt) {
		if snapshot.Counters == previous {
			if err := tx.Commit(); err != nil {
				return domain.UsageSnapshotResult{}, err
			}
			return domain.UsageSnapshotResult{}, nil
		}
		return domain.UsageSnapshotResult{}, fmt.Errorf("%w: counters changed at the same capture time %s", ErrUsageSnapshotOutOfOrder, snapshot.CapturedAt.Format(time.RFC3339Nano))
	}

	result := domain.UsageSnapshotResult{}
	if usageCountersRolledBack(previous, snapshot.Counters) {
		result.CounterReset = true
	} else {
		result.Delta = subtractUsageCounters(snapshot.Counters, previous)
	}
	if err := updateUsageBaseline(ctx, tx, snapshot); err != nil {
		return domain.UsageSnapshotResult{}, err
	}
	if !result.CounterReset && !usageCountersEmpty(result.Delta) {
		if err := addDailyUsageDelta(ctx, tx, snapshot, result.Delta); err != nil {
			return domain.UsageSnapshotResult{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return domain.UsageSnapshotResult{}, err
	}
	return result, nil
}

// AttributedDailyUsage exposes the durable account/provider/model dimensions.
// Consumers should treat an empty model as explicitly unattributed rather than
// assigning an inferred model name.
func (s *Store) AttributedDailyUsage(ctx context.Context) ([]domain.AttributedDailyUsage, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT usage_date, account_id, provider_id, model,
		input_tokens, output_tokens, cache_tokens, requests
		FROM usage_daily_attributed ORDER BY usage_date, provider_id, account_id, model`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []domain.AttributedDailyUsage
	for rows.Next() {
		var item domain.AttributedDailyUsage
		if err := rows.Scan(&item.Date, &item.AccountID, &item.ProviderID, &item.Model,
			&item.InputTokens, &item.OutputTokens, &item.CacheTokens, &item.Requests); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

// UsageBackfillCompleted reports whether one bounded provider-history import
// has completed for an account. Source keys are versioned by the adapter so a
// future parser can intentionally run a corrected backfill once.
func (s *Store) UsageBackfillCompleted(ctx context.Context, accountID, providerID, sourceKey string) (bool, error) {
	accountID = strings.TrimSpace(accountID)
	providerID = strings.TrimSpace(providerID)
	sourceKey = strings.TrimSpace(sourceKey)
	if accountID == "" || providerID == "" || sourceKey == "" {
		return false, fmt.Errorf("usage backfill account, provider, and source key are required")
	}
	var exists int
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM usage_backfill_states WHERE account_id=? AND provider_id=? AND source_key=?
	)`, accountID, providerID, sourceKey).Scan(&exists)
	return exists == 1, err
}

// MarkUsageBackfillCompleted records completion only after every daily bucket
// has been stored successfully. It validates account ownership so a provider
// cannot mark another adapter's history as complete.
func (s *Store) MarkUsageBackfillCompleted(ctx context.Context, accountID, providerID, sourceKey string, completedAt time.Time) error {
	accountID = strings.TrimSpace(accountID)
	providerID = strings.TrimSpace(providerID)
	sourceKey = strings.TrimSpace(sourceKey)
	if accountID == "" || providerID == "" || sourceKey == "" {
		return fmt.Errorf("usage backfill account, provider, and source key are required")
	}
	if completedAt.IsZero() {
		return fmt.Errorf("usage backfill completion time is required")
	}
	var connectedProviderID string
	if err := s.db.QueryRowContext(ctx, "SELECT provider_id FROM connected_accounts WHERE id=?", accountID).Scan(&connectedProviderID); err != nil {
		return err
	}
	if connectedProviderID != providerID {
		return fmt.Errorf("usage backfill provider %q does not match account provider %q", providerID, connectedProviderID)
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO usage_backfill_states
		(account_id, provider_id, source_key, completed_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(account_id, provider_id, source_key) DO UPDATE SET completed_at=excluded.completed_at`,
		accountID, providerID, sourceKey, completedAt.UTC().Format(time.RFC3339Nano))
	return err
}

// UpsertAbsoluteDailyUsage atomically replaces one provider-reported daily
// aggregate. Missing models are removed, so repeated reads are idempotent and
// a provider correction cannot leave stale model rows behind. Any cumulative
// baselines for the account are discarded: if a caller later falls back to
// cumulative snapshots, its first read will establish a fresh baseline rather
// than double-counting the absolute aggregate.
func (s *Store) UpsertAbsoluteDailyUsage(ctx context.Context, date, accountID, providerID string, readings []domain.UsageModelReading) error {
	date = strings.TrimSpace(date)
	accountID = strings.TrimSpace(accountID)
	providerID = strings.TrimSpace(providerID)
	parsedDate, err := time.Parse("2006-01-02", date)
	if err != nil || parsedDate.Format("2006-01-02") != date {
		return fmt.Errorf("absolute usage date must use YYYY-MM-DD")
	}
	if accountID == "" {
		return fmt.Errorf("absolute usage account ID is required")
	}
	if providerID == "" {
		return fmt.Errorf("absolute usage provider ID is required")
	}
	normalized := make([]domain.UsageModelReading, len(readings))
	models := make(map[string]struct{}, len(readings))
	for index, reading := range readings {
		reading.Model = strings.TrimSpace(reading.Model)
		if err := validateUsageCounters(reading.Counters); err != nil {
			return fmt.Errorf("absolute usage reading %d: %w", index, err)
		}
		if _, exists := models[reading.Model]; exists {
			return fmt.Errorf("absolute usage contains duplicate model %q", reading.Model)
		}
		models[reading.Model] = struct{}{}
		normalized[index] = reading
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var connectedProviderID string
	if err := tx.QueryRowContext(ctx, "SELECT provider_id FROM connected_accounts WHERE id=?", accountID).Scan(&connectedProviderID); err != nil {
		return err
	}
	if connectedProviderID != providerID {
		return fmt.Errorf("absolute usage provider %q does not match account provider %q", providerID, connectedProviderID)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM usage_daily_attributed
		WHERE usage_date=? AND account_id=? AND provider_id=?`, date, accountID, providerID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM usage_cumulative_snapshots
		WHERE account_id=? AND provider_id=?`, accountID, providerID); err != nil {
		return err
	}
	for _, reading := range normalized {
		if _, err := tx.ExecContext(ctx, `INSERT INTO usage_daily_attributed
			(usage_date, account_id, provider_id, model, input_tokens, output_tokens, cache_tokens, requests)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, date, accountID, providerID, reading.Model,
			reading.Counters.InputTokens, reading.Counters.OutputTokens, reading.Counters.CacheTokens,
			reading.Counters.Requests); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ReplaceCodexUsageSources updates source-scoped Codex rollout aggregates and
// rebuilds the account's attributed daily usage atomically. A stable source ID
// plus content hash makes repeated folder imports idempotent while allowing a
// still-growing rollout file to replace its older aggregate.
func (s *Store) ReplaceCodexUsageSources(ctx context.Context, accountID string, sources []domain.CodexUsageImportSource) (domain.CodexUsageImportResult, error) {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return domain.CodexUsageImportResult{}, fmt.Errorf("Codex usage account ID is required")
	}
	if len(sources) == 0 || len(sources) > 500 {
		return domain.CodexUsageImportResult{}, fmt.Errorf("Codex usage import must contain 1 to 500 sources")
	}
	for index := range sources {
		if err := validateCodexUsageSource(&sources[index]); err != nil {
			return domain.CodexUsageImportResult{}, fmt.Errorf("Codex usage source %d: %w", index, err)
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.CodexUsageImportResult{}, err
	}
	defer tx.Rollback()
	var providerID string
	if err := tx.QueryRowContext(ctx, "SELECT provider_id FROM connected_accounts WHERE id=?", accountID).Scan(&providerID); err != nil {
		return domain.CodexUsageImportResult{}, err
	}
	if providerID != "codex" {
		return domain.CodexUsageImportResult{}, fmt.Errorf("usage logs can only be imported into a Codex account")
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	result := domain.CodexUsageImportResult{}
	for _, source := range sources {
		var existingHash string
		err := tx.QueryRowContext(ctx, `SELECT content_hash FROM codex_usage_import_sources
			WHERE account_id=? AND source_id=?`, accountID, source.SourceID).Scan(&existingHash)
		if err == nil && existingHash == source.ContentHash {
			result.UnchangedSources++
			continue
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return domain.CodexUsageImportResult{}, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO codex_usage_import_sources
			(account_id, source_id, content_hash, imported_at) VALUES (?, ?, ?, ?)
			ON CONFLICT(account_id, source_id) DO UPDATE SET
			content_hash=excluded.content_hash, imported_at=excluded.imported_at`,
			accountID, source.SourceID, source.ContentHash, now); err != nil {
			return domain.CodexUsageImportResult{}, err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM codex_usage_import_entries
			WHERE account_id=? AND source_id=?`, accountID, source.SourceID); err != nil {
			return domain.CodexUsageImportResult{}, err
		}
		for _, entry := range source.Entries {
			if _, err := tx.ExecContext(ctx, `INSERT INTO codex_usage_import_entries
				(account_id, source_id, usage_date, model, input_tokens, output_tokens, cache_tokens, requests)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, accountID, source.SourceID, entry.Date, entry.Model,
				entry.Counters.InputTokens, entry.Counters.OutputTokens, entry.Counters.CacheTokens,
				entry.Counters.Requests); err != nil {
				return domain.CodexUsageImportResult{}, err
			}
			result.ImportedEntries++
			result.ImportedTokens += entry.Counters.InputTokens + entry.Counters.OutputTokens + entry.Counters.CacheTokens
		}
		result.ImportedSources++
	}

	if result.ImportedSources > 0 {
		if _, err := tx.ExecContext(ctx, `DELETE FROM usage_daily_attributed
			WHERE account_id=? AND provider_id='codex'`, accountID); err != nil {
			return domain.CodexUsageImportResult{}, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO usage_daily_attributed
			(usage_date, account_id, provider_id, model, input_tokens, output_tokens, cache_tokens, requests)
			SELECT usage_date, account_id, 'codex', model,
				SUM(input_tokens), SUM(output_tokens), SUM(cache_tokens), SUM(requests)
			FROM codex_usage_import_entries WHERE account_id=?
			GROUP BY usage_date, account_id, model`, accountID); err != nil {
			return domain.CodexUsageImportResult{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return domain.CodexUsageImportResult{}, err
	}
	return result, nil
}

func validateCodexUsageSource(source *domain.CodexUsageImportSource) error {
	source.SourceID = strings.TrimSpace(source.SourceID)
	source.ContentHash = strings.TrimSpace(source.ContentHash)
	if source.SourceID == "" || len(source.SourceID) > 160 {
		return fmt.Errorf("source ID is missing or too long")
	}
	if source.ContentHash == "" || len(source.ContentHash) > 160 {
		return fmt.Errorf("content hash is missing or too long")
	}
	if len(source.Entries) == 0 || len(source.Entries) > 1000 {
		return fmt.Errorf("source must contain 1 to 1000 daily model entries")
	}
	seen := make(map[string]struct{}, len(source.Entries))
	for index := range source.Entries {
		entry := &source.Entries[index]
		entry.Date = strings.TrimSpace(entry.Date)
		entry.Model = strings.TrimSpace(entry.Model)
		parsed, err := time.Parse("2006-01-02", entry.Date)
		if err != nil || parsed.Format("2006-01-02") != entry.Date {
			return fmt.Errorf("entry %d has an invalid date", index)
		}
		if entry.Model == "" || len(entry.Model) > 200 {
			return fmt.Errorf("entry %d has a missing or invalid model", index)
		}
		if err := validateUsageCounters(entry.Counters); err != nil {
			return fmt.Errorf("entry %d: %w", index, err)
		}
		if entry.Counters == (domain.UsageCounters{}) {
			return fmt.Errorf("entry %d contains no usage", index)
		}
		if entry.Counters.InputTokens > maxImportedUsageCounter || entry.Counters.OutputTokens > maxImportedUsageCounter ||
			entry.Counters.CacheTokens > maxImportedUsageCounter || entry.Counters.Requests > maxImportedUsageCounter {
			return fmt.Errorf("entry %d exceeds the supported counter range", index)
		}
		key := entry.Date + "\x00" + entry.Model
		if _, exists := seen[key]; exists {
			return fmt.Errorf("entry %d duplicates date and model", index)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func validateUsageSnapshot(snapshot domain.UsageSnapshot) error {
	if snapshot.AccountID == "" {
		return fmt.Errorf("usage snapshot account ID is required")
	}
	if snapshot.ProviderID == "" {
		return fmt.Errorf("usage snapshot provider ID is required")
	}
	if snapshot.CapturedAt.IsZero() {
		return fmt.Errorf("usage snapshot capture time is required")
	}
	return validateUsageCounters(snapshot.Counters)
}

func validateUsageCounters(counters domain.UsageCounters) error {
	if counters.InputTokens < 0 || counters.OutputTokens < 0 || counters.CacheTokens < 0 || counters.Requests < 0 {
		return fmt.Errorf("usage counters must be non-negative")
	}
	return nil
}

func insertUsageBaseline(ctx context.Context, tx *sql.Tx, snapshot domain.UsageSnapshot) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO usage_cumulative_snapshots
		(account_id, provider_id, model, input_tokens, output_tokens, cache_tokens, requests, captured_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, snapshot.AccountID, snapshot.ProviderID, snapshot.Model,
		snapshot.Counters.InputTokens, snapshot.Counters.OutputTokens, snapshot.Counters.CacheTokens,
		snapshot.Counters.Requests, snapshot.CapturedAt.Format(time.RFC3339Nano))
	return err
}

func updateUsageBaseline(ctx context.Context, tx *sql.Tx, snapshot domain.UsageSnapshot) error {
	_, err := tx.ExecContext(ctx, `UPDATE usage_cumulative_snapshots
		SET input_tokens=?, output_tokens=?, cache_tokens=?, requests=?, captured_at=?
		WHERE account_id=? AND provider_id=? AND model=?`, snapshot.Counters.InputTokens,
		snapshot.Counters.OutputTokens, snapshot.Counters.CacheTokens, snapshot.Counters.Requests,
		snapshot.CapturedAt.Format(time.RFC3339Nano), snapshot.AccountID, snapshot.ProviderID, snapshot.Model)
	return err
}

func addDailyUsageDelta(ctx context.Context, tx *sql.Tx, snapshot domain.UsageSnapshot, delta domain.UsageCounters) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO usage_daily_attributed
		(usage_date, account_id, provider_id, model, input_tokens, output_tokens, cache_tokens, requests)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(usage_date, account_id, provider_id, model) DO UPDATE SET
		input_tokens=usage_daily_attributed.input_tokens+excluded.input_tokens,
		output_tokens=usage_daily_attributed.output_tokens+excluded.output_tokens,
		cache_tokens=usage_daily_attributed.cache_tokens+excluded.cache_tokens,
		requests=usage_daily_attributed.requests+excluded.requests`, snapshot.CapturedAt.Format("2006-01-02"),
		snapshot.AccountID, snapshot.ProviderID, snapshot.Model, delta.InputTokens, delta.OutputTokens,
		delta.CacheTokens, delta.Requests)
	return err
}

func usageCountersRolledBack(previous, current domain.UsageCounters) bool {
	return current.InputTokens < previous.InputTokens ||
		current.OutputTokens < previous.OutputTokens ||
		current.CacheTokens < previous.CacheTokens ||
		current.Requests < previous.Requests
}

func subtractUsageCounters(current, previous domain.UsageCounters) domain.UsageCounters {
	return domain.UsageCounters{
		InputTokens:  current.InputTokens - previous.InputTokens,
		OutputTokens: current.OutputTokens - previous.OutputTokens,
		CacheTokens:  current.CacheTokens - previous.CacheTokens,
		Requests:     current.Requests - previous.Requests,
	}
}

func usageCountersEmpty(counters domain.UsageCounters) bool {
	return counters == (domain.UsageCounters{})
}

func usageModelColor(model string) string {
	palette := [...]string{"#2563eb", "#7c3aed", "#0891b2", "#059669", "#ca8a04", "#ea580c", "#db2777", "#4f46e5"}
	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(model))
	return palette[int(hasher.Sum32())%len(palette)]
}
