package domain

import "time"

// UsageCounters contains cumulative or incremental, provider-reported usage.
// Every value is required to be non-negative; callers must not estimate fields
// that are absent from the provider response.
type UsageCounters struct {
	InputTokens  int64 `json:"inputTokens"`
	OutputTokens int64 `json:"outputTokens"`
	CacheTokens  int64 `json:"cacheTokens"`
	Requests     int64 `json:"requests"`
}

// UsageSnapshot is one verified cumulative counter reading for a connected
// account. Model is optional and must only be populated when the provider
// returns a real model dimension.
type UsageSnapshot struct {
	AccountID  string        `json:"accountId"`
	ProviderID string        `json:"providerId"`
	Model      string        `json:"model,omitempty"`
	CapturedAt time.Time     `json:"capturedAt"`
	Counters   UsageCounters `json:"counters"`
}

// UsageSnapshotResult describes how a cumulative reading was handled. The
// first reading creates a baseline. A counter rollback replaces that baseline
// without generating usage, preventing billing-period resets from becoming
// negative or inflated deltas.
type UsageSnapshotResult struct {
	BaselineCreated bool          `json:"baselineCreated"`
	CounterReset    bool          `json:"counterReset"`
	Delta           UsageCounters `json:"delta"`
}

// UsageModelReading is an absolute provider-reported aggregate for one model.
// Model may be empty only when the provider exposes totals without a real model
// breakdown.
type UsageModelReading struct {
	Model    string        `json:"model,omitempty"`
	Counters UsageCounters `json:"counters"`
}

// AttributedDailyUsage is the durable daily aggregate behind the global trend.
// Empty Model means the provider did not expose a verifiable model dimension.
type AttributedDailyUsage struct {
	Date         string `json:"date"`
	AccountID    string `json:"accountId"`
	ProviderID   string `json:"providerId"`
	Model        string `json:"model,omitempty"`
	InputTokens  int64  `json:"inputTokens"`
	OutputTokens int64  `json:"outputTokens"`
	CacheTokens  int64  `json:"cacheTokens"`
	Requests     int64  `json:"requests"`
}
