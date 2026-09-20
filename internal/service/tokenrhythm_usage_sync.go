package service

import (
	"context"
	"fmt"

	"github.com/Denght123/SuperMonitor/internal/domain"
	"github.com/Denght123/SuperMonitor/internal/integration/tokenrhythm"
)

type tokenRhythmUsageFetcher interface {
	FetchUsageHistory(context.Context, tokenrhythm.Credential, string) ([]tokenrhythm.UsagePanel, error)
	FetchTodayUsage(context.Context, tokenrhythm.Credential) (tokenrhythm.UsagePanel, error)
}

type absoluteDailyUsageWriter interface {
	UpsertAbsoluteDailyUsage(context.Context, string, string, string, []domain.UsageModelReading) error
}

// syncTokenRhythmUsageSnapshots deliberately reloads the rolling 30-day
// window on every account refresh. UpsertAbsoluteDailyUsage replaces absolute
// account/day snapshots atomically, making repeated imports idempotent while
// also repairing dates missed while SuperMonitor was offline.
func syncTokenRhythmUsageSnapshots(
	ctx context.Context,
	fetcher tokenRhythmUsageFetcher,
	store absoluteDailyUsageWriter,
	accountID string,
	credential tokenrhythm.Credential,
) []error {
	var syncErrors []error
	history, err := fetcher.FetchUsageHistory(ctx, credential, "30d")
	if err != nil {
		syncErrors = append(syncErrors, fmt.Errorf("读取 30 天官方用量: %w", err))
	} else {
		for _, day := range history {
			if err := store.UpsertAbsoluteDailyUsage(ctx, day.Date, accountID, "tokenrhythm", tokenRhythmUsageReadings(day)); err != nil {
				syncErrors = append(syncErrors, fmt.Errorf("保存 %s 用量: %w", day.Date, err))
				break
			}
		}
	}

	// The aggregate endpoint is cheaper and can include requests that have not
	// yet appeared in the paginated detail list. Writing it last makes today's
	// snapshot as fresh as possible without double-counting.
	today, err := fetcher.FetchTodayUsage(ctx, credential)
	if err != nil {
		syncErrors = append(syncErrors, fmt.Errorf("读取今日官方用量: %w", err))
	} else if err := store.UpsertAbsoluteDailyUsage(ctx, today.Date, accountID, "tokenrhythm", tokenRhythmUsageReadings(today)); err != nil {
		syncErrors = append(syncErrors, fmt.Errorf("保存今日用量: %w", err))
	}
	return syncErrors
}
