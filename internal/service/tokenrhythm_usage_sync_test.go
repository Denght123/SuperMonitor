package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Denght123/SuperMonitor/internal/domain"
	"github.com/Denght123/SuperMonitor/internal/integration/tokenrhythm"
)

type tokenRhythmUsageFetcherStub struct {
	histories   [][]tokenrhythm.UsagePanel
	today       tokenrhythm.UsagePanel
	historyErr  error
	historyCall int
	ranges      []string
}

func (s *tokenRhythmUsageFetcherStub) FetchUsageHistory(_ context.Context, _ tokenrhythm.Credential, usageRange string) ([]tokenrhythm.UsagePanel, error) {
	s.ranges = append(s.ranges, usageRange)
	index := s.historyCall
	s.historyCall++
	if s.historyErr != nil {
		return nil, s.historyErr
	}
	if len(s.histories) == 0 {
		return nil, nil
	}
	if index >= len(s.histories) {
		index = len(s.histories) - 1
	}
	return s.histories[index], nil
}

func (s *tokenRhythmUsageFetcherStub) FetchTodayUsage(context.Context, tokenrhythm.Credential) (tokenrhythm.UsagePanel, error) {
	return s.today, nil
}

type absoluteDailyUsageWriterStub struct {
	dates  map[string]int
	failed error
}

func (s *absoluteDailyUsageWriterStub) UpsertAbsoluteDailyUsage(_ context.Context, date, _, _ string, _ []domain.UsageModelReading) error {
	if s.failed != nil {
		return s.failed
	}
	if s.dates == nil {
		s.dates = make(map[string]int)
	}
	s.dates[date]++
	return nil
}

func TestSyncTokenRhythmUsageSnapshotsReloadsHistoryAndRepairsOfflineGap(t *testing.T) {
	fetcher := &tokenRhythmUsageFetcherStub{
		histories: [][]tokenrhythm.UsagePanel{
			{{Date: "2026-09-18", InputTokens: 10}},
			{{Date: "2026-09-18", InputTokens: 10}, {Date: "2026-09-19", InputTokens: 20}},
		},
		today: tokenrhythm.UsagePanel{Date: "2026-09-20", InputTokens: 30},
	}
	writer := &absoluteDailyUsageWriterStub{}
	for range 2 {
		if errs := syncTokenRhythmUsageSnapshots(context.Background(), fetcher, writer, "account-1", tokenrhythm.Credential{SessionToken: "sess_test"}); len(errs) != 0 {
			t.Fatalf("unexpected sync errors: %v", errs)
		}
	}
	if fetcher.historyCall != 2 {
		t.Fatalf("history calls = %d, want one call per sync", fetcher.historyCall)
	}
	if len(fetcher.ranges) != 2 || fetcher.ranges[0] != "30d" || fetcher.ranges[1] != "30d" {
		t.Fatalf("history ranges = %v, want [30d 30d]", fetcher.ranges)
	}
	if writer.dates["2026-09-19"] != 1 {
		t.Fatalf("offline gap date was not repaired: writes = %v", writer.dates)
	}
	if writer.dates["2026-09-18"] != 2 || writer.dates["2026-09-20"] != 2 {
		t.Fatalf("rolling snapshots were not refreshed idempotently: writes = %v", writer.dates)
	}
}

func TestSyncTokenRhythmUsageSnapshotsStillWritesTodayWhenHistoryFails(t *testing.T) {
	fetcher := &tokenRhythmUsageFetcherStub{
		historyErr: errors.New("history unavailable"),
		today:      tokenrhythm.UsagePanel{Date: "2026-09-20", InputTokens: 30},
	}
	writer := &absoluteDailyUsageWriterStub{}
	errs := syncTokenRhythmUsageSnapshots(context.Background(), fetcher, writer, "account-1", tokenrhythm.Credential{SessionToken: "sess_test"})
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "30 天") {
		t.Fatalf("sync errors = %v, want history error only", errs)
	}
	if writer.dates["2026-09-20"] != 1 {
		t.Fatalf("today snapshot was not written after history failure: %v", writer.dates)
	}
}
