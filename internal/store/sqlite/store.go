package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/Denght123/SuperMonitor/internal/domain"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	store := &Store{db: db}
	if err := store.migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate(ctx context.Context) error {
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		body, err := migrations.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return err
		}
		if _, err := s.db.ExecContext(ctx, string(body)); err != nil {
			return fmt.Errorf("migration %s: %w", entry.Name(), err)
		}
	}
	return nil
}

func (s *Store) SeedSyntheticData(ctx context.Context) error {
	var count int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM providers").Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now().UTC().Truncate(time.Second)
	providers := []struct {
		id, name, region, tier, status string
		auth, capabilities             []string
	}{
		{"codex", "Codex", "Global", "community", "healthy", []string{"oauth", "credential_import"}, []string{"quota", "usage"}},
		{"workbuddy-cn", "WorkBuddy", "CN", "community", "healthy", []string{"device_code", "credential_import"}, []string{"credits", "usage", "checkin"}},
		{"deepseek", "DeepSeek", "CN", "official", "healthy", []string{"api_key"}, []string{"balance"}},
		{"bailian", "阿里云百炼", "CN", "official", "warning", []string{"api_key", "credential_import"}, []string{"token_plan", "usage"}},
		{"gemini-cli", "Gemini CLI", "Global", "official", "healthy", []string{"oauth"}, []string{"quota"}},
	}
	for _, provider := range providers {
		auth, _ := json.Marshal(provider.auth)
		capabilities, _ := json.Marshal(provider.capabilities)
		if _, err := tx.ExecContext(ctx, `INSERT INTO providers
			(id, name, region, tier, status, auth_methods, capabilities, last_checked_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, provider.id, provider.name, provider.region, provider.tier, provider.status, string(auth), string(capabilities), now.Format(time.RFC3339)); err != nil {
			return err
		}
	}

	accounts := []domain.AccountSummary{
		{ID: "acc-codex-main", Provider: "Codex", Region: "Global", Alias: "主力开发", Services: []string{"Codex CLI"}, PrimaryMetric: "5h 剩余 73%", SecondaryMetric: "周限额剩余 61%", Status: "healthy", Source: "社区适配", LastRefreshedAt: now.Add(-2 * time.Minute), NextRefreshAt: now.Add(3 * time.Minute)},
		{ID: "acc-workbuddy-a", Provider: "WorkBuddy", Region: "CN", Alias: "工作账号 A", Services: []string{"WorkBuddy", "CodeBuddy CN"}, PrimaryMetric: "386.4 credits", SecondaryMetric: "09/23 到期", Status: "warning", Source: "社区适配", LastRefreshedAt: now.Add(-4 * time.Minute), NextRefreshAt: now.Add(6 * time.Minute)},
		{ID: "acc-deepseek-main", Provider: "DeepSeek", Region: "CN", Alias: "API 主账户", Services: []string{"DeepSeek API"}, PrimaryMetric: "¥ 128.42", SecondaryMetric: "余额", Status: "healthy", Source: "官方接口", LastRefreshedAt: now.Add(-7 * time.Minute), NextRefreshAt: now.Add(8 * time.Minute)},
		{ID: "acc-bailian-plan", Provider: "阿里云百炼", Region: "CN", Alias: "Token Plan", Services: []string{"百炼 Token Plan"}, PrimaryMetric: "18.7M / 50M", SecondaryMetric: "剩余 37.4%", Status: "critical", Source: "官方接口", LastRefreshedAt: now.Add(-18 * time.Minute), NextRefreshAt: now.Add(2 * time.Minute), Error: "连续两次刷新超时，当前显示最后一次成功数据"},
		{ID: "acc-gemini-lab", Provider: "Gemini CLI", Region: "Global", Alias: "实验账号", Services: []string{"Gemini Code Assist"}, PrimaryMetric: "日限额剩余 84%", SecondaryMetric: "2h 18m 后重置", Status: "healthy", Source: "官方接口", LastRefreshedAt: now.Add(-3 * time.Minute), NextRefreshAt: now.Add(4 * time.Minute)},
	}
	for _, account := range accounts {
		providerID := map[string]string{"Codex": "codex", "WorkBuddy": "workbuddy-cn", "DeepSeek": "deepseek", "阿里云百炼": "bailian", "Gemini CLI": "gemini-cli"}[account.Provider]
		services, _ := json.Marshal(account.Services)
		if _, err := tx.ExecContext(ctx, `INSERT INTO accounts
			(id, provider_id, alias, services, primary_metric, secondary_metric, status, source, last_refreshed_at, next_refresh_at, error)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, account.ID, providerID, account.Alias, string(services), account.PrimaryMetric, account.SecondaryMetric, account.Status, account.Source, account.LastRefreshedAt.Format(time.RFC3339), account.NextRefreshAt.Format(time.RFC3339), account.Error); err != nil {
			return err
		}
	}

	for day := 29; day >= 0; day-- {
		date := now.AddDate(0, 0, -day).Format("2006-01-02")
		phase := float64(29-day) / 4.2
		input := int64(6_200_000 + 2_100_000*math.Sin(phase) + float64((day%5)*190_000))
		output := int64(float64(input)*0.28 + 260_000*math.Cos(phase*1.4))
		cache := int64(float64(input)*0.42 + 480_000*math.Sin(phase*0.7))
		requests := int64(430 + (29-day)*9 + int(120*math.Sin(phase)))
		if _, err := tx.ExecContext(ctx, `INSERT INTO usage_daily
			(usage_date, input_tokens, output_tokens, cache_tokens, requests) VALUES (?, ?, ?, ?, ?)`, date, input, output, cache, requests); err != nil {
			return err
		}
	}

	models := []domain.ModelUsage{
		{Model: "GPT-5", Tokens: 74_200_000, Color: "#6ae4ff"},
		{Model: "Claude Sonnet", Tokens: 52_800_000, Color: "#a8b4ff"},
		{Model: "Gemini Pro", Tokens: 31_600_000, Color: "#ffbd66"},
		{Model: "DeepSeek V3", Tokens: 22_400_000, Color: "#68d7a2"},
		{Model: "GLM", Tokens: 13_200_000, Color: "#f48ba5"},
	}
	for _, model := range models {
		if _, err := tx.ExecContext(ctx, "INSERT INTO model_usage (model, tokens, color) VALUES (?, ?, ?)", model.Model, model.Tokens, model.Color); err != nil {
			return err
		}
	}

	percent73, percent61, percent37, percent84 := 73.0, 61.0, 37.4, 84.0
	total100, total800, total50m := 100.0, 800.0, 50_000_000.0
	reset5h, resetWeek := now.Add(2*time.Hour+14*time.Minute), now.Add(4*24*time.Hour+9*time.Hour)
	expiry := now.AddDate(0, 0, 5)
	signals := []domain.QuotaSignal{
		{ID: "codex-5h", Provider: "Codex", Label: "5h 限额", Kind: "rate_window", Value: 73, Total: &total100, Unit: "%", RemainingPercent: &percent73, ResetAt: &reset5h, Status: "healthy", Source: "社区适配", Confidence: "verified"},
		{ID: "codex-week", Provider: "Codex", Label: "周限额", Kind: "rate_window", Value: 61, Total: &total100, Unit: "%", RemainingPercent: &percent61, ResetAt: &resetWeek, Status: "healthy", Source: "社区适配", Confidence: "verified"},
		{ID: "workbuddy-credit", Provider: "WorkBuddy", Label: "签到积分包", Kind: "credits", Value: 386.4, Total: &total800, Unit: "credits", ExpiresAt: &expiry, Status: "warning", Source: "社区适配", Confidence: "verified"},
		{ID: "deepseek-balance", Provider: "DeepSeek", Label: "账户余额", Kind: "balance", Value: 128.42, Unit: "CNY", Status: "healthy", Source: "官方接口", Confidence: "official"},
		{ID: "bailian-plan", Provider: "阿里云百炼", Label: "Token Plan", Kind: "token_plan", Value: 18_700_000, Total: &total50m, Unit: "tokens", RemainingPercent: &percent37, Status: "critical", Source: "官方接口", Confidence: "stale"},
		{ID: "gemini-daily", Provider: "Gemini CLI", Label: "日限额", Kind: "rate_window", Value: 84, Total: &total100, Unit: "%", RemainingPercent: &percent84, ResetAt: &reset5h, Status: "healthy", Source: "官方接口", Confidence: "official"},
	}
	for _, signal := range signals {
		var resetAt, expiresAt any
		if signal.ResetAt != nil {
			resetAt = signal.ResetAt.Format(time.RFC3339)
		}
		if signal.ExpiresAt != nil {
			expiresAt = signal.ExpiresAt.Format(time.RFC3339)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO quota_signals
			(id, provider, label, kind, value, total, unit, remaining_percent, reset_at, expires_at, status, source, confidence)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, signal.ID, signal.Provider, signal.Label, signal.Kind, signal.Value, signal.Total, signal.Unit, signal.RemainingPercent, resetAt, expiresAt, signal.Status, signal.Source, signal.Confidence); err != nil {
			return err
		}
	}

	alert := domain.Alert{ID: "alert-bailian-timeout", Severity: "critical", Title: "百炼额度数据已过期", Message: "Token Plan 连续两次刷新超时，当前数据已超过 15 分钟。", Provider: "阿里云百炼", CreatedAt: now.Add(-6 * time.Minute), Recovery: "检查网络或凭据后立即刷新"}
	if _, err := tx.ExecContext(ctx, `INSERT INTO alerts
		(id, severity, title, message, provider, created_at, recovery) VALUES (?, ?, ?, ?, ?, ?, ?)`, alert.ID, alert.Severity, alert.Title, alert.Message, alert.Provider, alert.CreatedAt.Format(time.RFC3339), alert.Recovery); err != nil {
		return err
	}

	return tx.Commit()
}

func (s *Store) DailyUsage(ctx context.Context) ([]domain.DailyUsage, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT usage_date, input_tokens, output_tokens, cache_tokens, requests FROM usage_daily ORDER BY usage_date")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []domain.DailyUsage
	for rows.Next() {
		var item domain.DailyUsage
		if err := rows.Scan(&item.Date, &item.InputTokens, &item.OutputTokens, &item.CacheTokens, &item.Requests); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) ModelUsage(ctx context.Context) ([]domain.ModelUsage, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT model, tokens, color FROM model_usage ORDER BY tokens DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []domain.ModelUsage
	for rows.Next() {
		var item domain.ModelUsage
		if err := rows.Scan(&item.Model, &item.Tokens, &item.Color); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) QuotaSignals(ctx context.Context) ([]domain.QuotaSignal, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, provider, label, kind, value, total, unit, remaining_percent,
		reset_at, expires_at, status, source, confidence FROM quota_signals ORDER BY rowid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []domain.QuotaSignal
	for rows.Next() {
		var item domain.QuotaSignal
		var total, percent sql.NullFloat64
		var resetAt, expiresAt sql.NullString
		if err := rows.Scan(&item.ID, &item.Provider, &item.Label, &item.Kind, &item.Value, &total, &item.Unit, &percent, &resetAt, &expiresAt, &item.Status, &item.Source, &item.Confidence); err != nil {
			return nil, err
		}
		if total.Valid {
			item.Total = &total.Float64
		}
		if percent.Valid {
			item.RemainingPercent = &percent.Float64
		}
		item.ResetAt = parseOptionalTime(resetAt)
		item.ExpiresAt = parseOptionalTime(expiresAt)
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) Accounts(ctx context.Context) ([]domain.AccountSummary, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT a.id, p.name, p.region, a.alias, a.services, a.primary_metric,
		a.secondary_metric, a.status, a.source, a.last_refreshed_at, a.next_refresh_at, a.error
		FROM accounts a JOIN providers p ON p.id = a.provider_id ORDER BY a.rowid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []domain.AccountSummary
	for rows.Next() {
		var item domain.AccountSummary
		var services, lastRefreshed, nextRefresh string
		if err := rows.Scan(&item.ID, &item.Provider, &item.Region, &item.Alias, &services, &item.PrimaryMetric, &item.SecondaryMetric, &item.Status, &item.Source, &lastRefreshed, &nextRefresh, &item.Error); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(services), &item.Services)
		item.LastRefreshedAt, _ = time.Parse(time.RFC3339, lastRefreshed)
		item.NextRefreshAt, _ = time.Parse(time.RFC3339, nextRefresh)
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) Alerts(ctx context.Context) ([]domain.Alert, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, severity, title, message, provider, created_at, recovery FROM alerts ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []domain.Alert
	for rows.Next() {
		var item domain.Alert
		var createdAt string
		if err := rows.Scan(&item.ID, &item.Severity, &item.Title, &item.Message, &item.Provider, &createdAt, &item.Recovery); err != nil {
			return nil, err
		}
		item.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) Providers(ctx context.Context) ([]domain.Provider, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT p.id, p.name, p.region, p.tier, p.status, p.auth_methods,
		p.capabilities, COUNT(a.id), p.last_checked_at
		FROM providers p LEFT JOIN accounts a ON a.provider_id = p.id GROUP BY p.id ORDER BY p.rowid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []domain.Provider
	for rows.Next() {
		var item domain.Provider
		var auth, capabilities, checkedAt string
		if err := rows.Scan(&item.ID, &item.Name, &item.Region, &item.Tier, &item.Status, &auth, &capabilities, &item.AccountCount, &checkedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(auth), &item.AuthMethods)
		_ = json.Unmarshal([]byte(capabilities), &item.Capabilities)
		item.LastCheckedAt, _ = time.Parse(time.RFC3339, checkedAt)
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) TouchRefresh(ctx context.Context, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE accounts
		SET last_refreshed_at = ?, next_refresh_at = ?`, now.UTC().Format(time.RFC3339), now.UTC().Add(10*time.Minute).Format(time.RFC3339))
	return err
}

func parseOptionalTime(value sql.NullString) *time.Time {
	if !value.Valid {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339, value.String)
	if err != nil {
		return nil
	}
	return &parsed
}

func Totals(usage []domain.DailyUsage) (tokens, requests int64) {
	for _, item := range usage {
		tokens += item.InputTokens + item.OutputTokens + item.CacheTokens
		requests += item.Requests
	}
	return tokens, requests
}

func ActiveAccounts(accounts []domain.AccountSummary) int {
	count := 0
	for _, account := range accounts {
		if account.Status != "disabled" {
			count++
		}
	}
	return count
}

func CriticalCount(alerts []domain.Alert) int {
	count := 0
	for _, alert := range alerts {
		if alert.Severity == "critical" {
			count++
		}
	}
	return count
}

func SortQuotaSignals(signals []domain.QuotaSignal) {
	sort.SliceStable(signals, func(i, j int) bool {
		order := map[string]int{"critical": 0, "warning": 1, "healthy": 2}
		return order[signals[i].Status] < order[signals[j].Status]
	})
}
