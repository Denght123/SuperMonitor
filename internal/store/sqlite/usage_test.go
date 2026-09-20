package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Denght123/SuperMonitor/internal/domain"
)

func TestRecordUsageSnapshotCreatesBaselineAndIdempotentDelta(t *testing.T) {
	store := openUsageTestStore(t)
	ctx := context.Background()
	saveUsageTestAccount(t, store, "usage-delta", "tokenrhythm")

	capturedAt := time.Date(2026, 9, 20, 3, 0, 0, 0, time.UTC)
	snapshot := domain.UsageSnapshot{
		AccountID: "usage-delta", ProviderID: "tokenrhythm", CapturedAt: capturedAt,
		Counters: domain.UsageCounters{InputTokens: 100, OutputTokens: 40, CacheTokens: 10, Requests: 5},
	}
	result, err := store.RecordUsageSnapshot(ctx, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if !result.BaselineCreated || result.CounterReset || result.Delta != (domain.UsageCounters{}) {
		t.Fatalf("unexpected baseline result: %+v", result)
	}
	if usage, err := store.DailyUsage(ctx); err != nil || len(usage) != 0 {
		t.Fatalf("baseline must not create daily usage: usage=%+v err=%v", usage, err)
	}

	snapshot.CapturedAt = capturedAt.Add(time.Minute)
	snapshot.Counters = domain.UsageCounters{InputTokens: 145, OutputTokens: 58, CacheTokens: 16, Requests: 8}
	result, err = store.RecordUsageSnapshot(ctx, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	wantDelta := domain.UsageCounters{InputTokens: 45, OutputTokens: 18, CacheTokens: 6, Requests: 3}
	if result.BaselineCreated || result.CounterReset || result.Delta != wantDelta {
		t.Fatalf("unexpected delta result: got %+v want %+v", result, wantDelta)
	}

	duplicate, err := store.RecordUsageSnapshot(ctx, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if duplicate != (domain.UsageSnapshotResult{}) {
		t.Fatalf("duplicate snapshot must be a no-op: %+v", duplicate)
	}
	attributed, err := store.AttributedDailyUsage(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(attributed) != 1 || attributed[0].InputTokens != 45 || attributed[0].OutputTokens != 18 || attributed[0].CacheTokens != 6 || attributed[0].Requests != 3 {
		t.Fatalf("unexpected attributed usage after duplicate: %+v", attributed)
	}
	models, err := store.ModelUsage(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 0 {
		t.Fatalf("unattributed provider data must not invent a model: %+v", models)
	}
}

func TestRecordUsageSnapshotCounterRollbackResetsBaseline(t *testing.T) {
	store := openUsageTestStore(t)
	ctx := context.Background()
	saveUsageTestAccount(t, store, "usage-reset", "tokenrhythm")

	base := time.Date(2026, 9, 20, 4, 0, 0, 0, time.UTC)
	record := func(at time.Time, input, output, cache, requests int64) domain.UsageSnapshotResult {
		t.Helper()
		result, err := store.RecordUsageSnapshot(ctx, domain.UsageSnapshot{
			AccountID: "usage-reset", ProviderID: "tokenrhythm", CapturedAt: at,
			Counters: domain.UsageCounters{InputTokens: input, OutputTokens: output, CacheTokens: cache, Requests: requests},
		})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	record(base, 100, 50, 20, 10)
	record(base.Add(time.Minute), 125, 60, 24, 12)
	reset := record(base.Add(2*time.Minute), 5, 3, 1, 1)
	if !reset.CounterReset || reset.Delta != (domain.UsageCounters{}) {
		t.Fatalf("rollback must reset without a delta: %+v", reset)
	}
	afterReset := record(base.Add(3*time.Minute), 11, 7, 2, 2)
	if afterReset.Delta != (domain.UsageCounters{InputTokens: 6, OutputTokens: 4, CacheTokens: 1, Requests: 1}) {
		t.Fatalf("unexpected post-reset delta: %+v", afterReset)
	}

	usage, err := store.DailyUsage(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(usage) != 1 || usage[0].InputTokens != 31 || usage[0].OutputTokens != 14 || usage[0].CacheTokens != 5 || usage[0].Requests != 3 {
		t.Fatalf("reset produced missing or inflated usage: %+v", usage)
	}

	_, err = store.RecordUsageSnapshot(ctx, domain.UsageSnapshot{
		AccountID: "usage-reset", ProviderID: "tokenrhythm", CapturedAt: base,
		Counters: domain.UsageCounters{InputTokens: 11, OutputTokens: 7, CacheTokens: 2, Requests: 2},
	})
	if !errors.Is(err, ErrUsageSnapshotOutOfOrder) {
		t.Fatalf("expected out-of-order error, got %v", err)
	}
}

func TestUpsertAbsoluteDailyUsageIsIdempotentAndRemovesMissingModels(t *testing.T) {
	store := openUsageTestStore(t)
	ctx := context.Background()
	saveUsageTestAccount(t, store, "usage-absolute", "tokenrhythm")

	baseline, err := store.RecordUsageSnapshot(ctx, domain.UsageSnapshot{
		AccountID: "usage-absolute", ProviderID: "tokenrhythm",
		CapturedAt: time.Date(2026, 9, 20, 5, 0, 0, 0, time.UTC),
		Counters:   domain.UsageCounters{InputTokens: 10, OutputTokens: 2, Requests: 1},
	})
	if err != nil || !baseline.BaselineCreated {
		t.Fatalf("failed to create fallback baseline: result=%+v err=%v", baseline, err)
	}

	readings := []domain.UsageModelReading{
		{Model: "glm-4.5", Counters: domain.UsageCounters{InputTokens: 100, OutputTokens: 20, CacheTokens: 5, Requests: 3}},
		{Model: "deepseek-v3", Counters: domain.UsageCounters{InputTokens: 70, OutputTokens: 10, CacheTokens: 2, Requests: 2}},
	}
	if err := store.UpsertAbsoluteDailyUsage(ctx, "2026-09-20", "usage-absolute", "tokenrhythm", readings); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertAbsoluteDailyUsage(ctx, "2026-09-20", "usage-absolute", "tokenrhythm", readings); err != nil {
		t.Fatal(err)
	}
	attributed, err := store.AttributedDailyUsage(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(attributed) != 2 {
		t.Fatalf("idempotent absolute upsert created duplicate rows: %+v", attributed)
	}

	replacement := []domain.UsageModelReading{
		{Model: "glm-4.5", Counters: domain.UsageCounters{InputTokens: 135, OutputTokens: 28, CacheTokens: 9, Requests: 4}},
	}
	if err := store.UpsertAbsoluteDailyUsage(ctx, "2026-09-20", "usage-absolute", "tokenrhythm", replacement); err != nil {
		t.Fatal(err)
	}
	attributed, err = store.AttributedDailyUsage(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(attributed) != 1 || attributed[0].Model != "glm-4.5" || attributed[0].InputTokens != 135 {
		t.Fatalf("absolute replacement left a stale model or wrong values: %+v", attributed)
	}
	models, err := store.ModelUsage(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].Model != "glm-4.5" || models[0].Tokens != 172 || models[0].Color == "" {
		t.Fatalf("unexpected model aggregation: %+v", models)
	}
	usage, err := store.DailyUsage(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(usage) != 1 || usage[0].InputTokens != 135 || usage[0].OutputTokens != 28 || usage[0].CacheTokens != 9 || usage[0].Requests != 4 {
		t.Fatalf("unexpected global daily aggregation: %+v", usage)
	}

	fallback, err := store.RecordUsageSnapshot(ctx, domain.UsageSnapshot{
		AccountID: "usage-absolute", ProviderID: "tokenrhythm",
		CapturedAt: time.Date(2026, 9, 20, 5, 1, 0, 0, time.UTC),
		Counters:   domain.UsageCounters{InputTokens: 20, OutputTokens: 4, Requests: 2},
	})
	if err != nil || !fallback.BaselineCreated {
		t.Fatalf("absolute upsert must reset the cumulative fallback baseline: result=%+v err=%v", fallback, err)
	}
}

func TestDeleteConnectedAccountCascadesUsageHistory(t *testing.T) {
	store := openUsageTestStore(t)
	ctx := context.Background()
	saveUsageTestAccount(t, store, "usage-delete", "tokenrhythm")
	if err := store.UpsertAbsoluteDailyUsage(ctx, "2026-09-20", "usage-delete", "tokenrhythm", []domain.UsageModelReading{
		{Model: "model-a", Counters: domain.UsageCounters{InputTokens: 12, OutputTokens: 3, Requests: 1}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordUsageSnapshot(ctx, domain.UsageSnapshot{
		AccountID: "usage-delete", ProviderID: "tokenrhythm", CapturedAt: time.Now().UTC(),
		Counters: domain.UsageCounters{InputTokens: 12, OutputTokens: 3, Requests: 1},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkUsageBackfillCompleted(ctx, "usage-delete", "tokenrhythm", "tokenrhythm-30d-v1", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DeleteConnectedAccount(ctx, "usage-delete"); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"usage_daily_attributed", "usage_cumulative_snapshots", "usage_backfill_states"} {
		var count int
		if err := store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table+" WHERE account_id=?", "usage-delete").Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("expected %s usage rows to be deleted, got %d", table, count)
		}
	}
}

func TestUsageBackfillStateIsVersionedAndIdempotent(t *testing.T) {
	store := openUsageTestStore(t)
	ctx := context.Background()
	saveUsageTestAccount(t, store, "usage-backfill", "tokenrhythm")
	completed, err := store.UsageBackfillCompleted(ctx, "usage-backfill", "tokenrhythm", "tokenrhythm-30d-v1")
	if err != nil || completed {
		t.Fatalf("unexpected initial state: completed=%v err=%v", completed, err)
	}
	completedAt := time.Date(2026, 9, 20, 6, 0, 0, 0, time.UTC)
	if err := store.MarkUsageBackfillCompleted(ctx, "usage-backfill", "tokenrhythm", "tokenrhythm-30d-v1", completedAt); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkUsageBackfillCompleted(ctx, "usage-backfill", "tokenrhythm", "tokenrhythm-30d-v1", completedAt.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	completed, err = store.UsageBackfillCompleted(ctx, "usage-backfill", "tokenrhythm", "tokenrhythm-30d-v1")
	if err != nil || !completed {
		t.Fatalf("expected completed v1 state: completed=%v err=%v", completed, err)
	}
	completed, err = store.UsageBackfillCompleted(ctx, "usage-backfill", "tokenrhythm", "tokenrhythm-30d-v2")
	if err != nil || completed {
		t.Fatalf("v2 must remain independently pending: completed=%v err=%v", completed, err)
	}
}

func openUsageTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.EnsureProviderCatalog(context.Background()); err != nil {
		t.Fatal(err)
	}
	return store
}

func saveUsageTestAccount(t *testing.T, store *Store, accountID, providerID string) {
	t.Helper()
	if err := store.SaveConnectedAccount(context.Background(), domain.ConnectedAccount{
		ID: accountID, ProviderID: providerID, Alias: accountID, AuthMethod: "test",
		Status: "healthy", Source: "verified test fixture",
	}, []byte("encrypted-test-credential")); err != nil {
		t.Fatal(err)
	}
}
