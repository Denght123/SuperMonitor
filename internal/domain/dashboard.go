package domain

import "time"

type Overview struct {
	GeneratedAt  time.Time        `json:"generatedAt"`
	Environment  string           `json:"environment"`
	Connection   string           `json:"connection"`
	KPIs         []KPI            `json:"kpis"`
	TokenTrend   []DailyUsage     `json:"tokenTrend"`
	ModelUsage   []ModelUsage     `json:"modelUsage"`
	QuotaSignals []QuotaSignal    `json:"quotaSignals"`
	Accounts     []AccountSummary `json:"accounts"`
	Alerts       []Alert          `json:"alerts"`
}

type KPI struct {
	ID    string  `json:"id"`
	Label string  `json:"label"`
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
	Delta float64 `json:"delta"`
	Tone  string  `json:"tone"`
}

type DailyUsage struct {
	Date         string `json:"date"`
	InputTokens  int64  `json:"inputTokens"`
	OutputTokens int64  `json:"outputTokens"`
	CacheTokens  int64  `json:"cacheTokens"`
	Requests     int64  `json:"requests"`
}

type ModelUsage struct {
	Model  string `json:"model"`
	Tokens int64  `json:"tokens"`
	Color  string `json:"color"`
}

type QuotaSignal struct {
	ID               string     `json:"id"`
	Provider         string     `json:"provider"`
	Label            string     `json:"label"`
	Kind             string     `json:"kind"`
	Value            float64    `json:"value"`
	Total            *float64   `json:"total,omitempty"`
	Unit             string     `json:"unit"`
	RemainingPercent *float64   `json:"remainingPercent,omitempty"`
	ResetAt          *time.Time `json:"resetAt,omitempty"`
	ExpiresAt        *time.Time `json:"expiresAt,omitempty"`
	Status           string     `json:"status"`
	Source           string     `json:"source"`
	Confidence       string     `json:"confidence"`
}

type AccountSummary struct {
	ID              string    `json:"id"`
	Provider        string    `json:"provider"`
	Region          string    `json:"region"`
	Alias           string    `json:"alias"`
	Services        []string  `json:"services"`
	PrimaryMetric   string    `json:"primaryMetric"`
	SecondaryMetric string    `json:"secondaryMetric"`
	Status          string    `json:"status"`
	Source          string    `json:"source"`
	LastRefreshedAt time.Time `json:"lastRefreshedAt"`
	NextRefreshAt   time.Time `json:"nextRefreshAt"`
	Error           string    `json:"error,omitempty"`
}

type Alert struct {
	ID        string    `json:"id"`
	Severity  string    `json:"severity"`
	Title     string    `json:"title"`
	Message   string    `json:"message"`
	Provider  string    `json:"provider"`
	CreatedAt time.Time `json:"createdAt"`
	Recovery  string    `json:"recovery"`
}

type Provider struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Region        string    `json:"region"`
	Tier          string    `json:"tier"`
	Status        string    `json:"status"`
	AuthMethods   []string  `json:"authMethods"`
	Capabilities  []string  `json:"capabilities"`
	AccountCount  int       `json:"accountCount"`
	LastCheckedAt time.Time `json:"lastCheckedAt"`
}
