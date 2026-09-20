package service

import (
	"testing"

	"github.com/Denght123/SuperMonitor/internal/integration/tokenrhythm"
)

func TestTokenRhythmUsageReadingsPreserveModelAndUnattributedTotals(t *testing.T) {
	readings := tokenRhythmUsageReadings(tokenrhythm.UsagePanel{
		InputTokens: 100, OutputTokens: 40, CacheReadTokens: 20, CacheWriteTokens: 5, Calls: 6,
		Models: []tokenrhythm.ModelUsage{
			{Model: "model-a", InputTokens: 70, OutputTokens: 30, CacheReadTokens: 12, CacheWriteTokens: 3, Calls: 4},
		},
	})
	if len(readings) != 2 {
		t.Fatalf("readings = %+v, want model plus unattributed remainder", readings)
	}
	if readings[0].Model != "model-a" || readings[0].Counters.InputTokens != 70 || readings[0].Counters.CacheTokens != 15 {
		t.Fatalf("unexpected attributed reading: %+v", readings[0])
	}
	if readings[1].Model != "" || readings[1].Counters.InputTokens != 30 || readings[1].Counters.OutputTokens != 10 || readings[1].Counters.CacheTokens != 10 || readings[1].Counters.Requests != 2 {
		t.Fatalf("unexpected unattributed remainder: %+v", readings[1])
	}
}

func TestTokenRhythmUsageReadingsAvoidDoubleCountingCompleteModels(t *testing.T) {
	readings := tokenRhythmUsageReadings(tokenrhythm.UsagePanel{
		InputTokens: 10, OutputTokens: 5, Calls: 1,
		Models: []tokenrhythm.ModelUsage{{Model: "model-a", InputTokens: 10, OutputTokens: 5, Calls: 1}},
	})
	if len(readings) != 1 || readings[0].Model != "model-a" {
		t.Fatalf("complete model totals must not add an aggregate row: %+v", readings)
	}
}
