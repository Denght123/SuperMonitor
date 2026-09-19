package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Denght123/SuperMonitor/internal/domain"
	"github.com/Denght123/SuperMonitor/internal/secure"
	"github.com/Denght123/SuperMonitor/internal/store/sqlite"
)

func TestNotificationChannelsHideAndEncryptSecrets(t *testing.T) {
	service, store := newNotificationTestService(t)
	ctx := context.Background()

	channel, err := service.CreateChannel(ctx, NotificationChannelInput{
		Kind: NotificationKindQQMail, Sender: "sender@qq.com", Recipient: "receiver@example.com", AuthCode: "smtp-secret-code",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(channel.Target, "sender@qq.com") || strings.Contains(channel.Target, "receiver@example.com") {
		t.Fatalf("channel target must be masked: %q", channel.Target)
	}
	_, encrypted, err := store.NotificationChannel(ctx, channel.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encrypted), "smtp-secret-code") {
		t.Fatal("SMTP authorization code was stored in plaintext")
	}

	var captured qqMailCredential
	service.sendMail = func(_ context.Context, credential qqMailCredential, _, _ string) error {
		captured = credential
		return nil
	}
	if err := service.TestChannel(ctx, channel.ID); err != nil {
		t.Fatal(err)
	}
	if captured.AuthCode != "smtp-secret-code" || captured.Sender != "sender@qq.com" {
		t.Fatalf("decrypted credential mismatch: %#v", captured)
	}
}

func TestNotificationEvaluateDeduplicatesLowQuotaAndResetThresholds(t *testing.T) {
	service, store := newNotificationTestService(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 19, 8, 0, 0, 0, time.UTC)
	resetAt := now.Add(48 * time.Hour)
	remaining := 12.0
	total := 100.0
	account := domain.ConnectedAccount{
		ID: "codex-alert-test", ProviderID: "codex", Alias: "告警测试账号", AuthMethod: "credential_import",
		Status: "healthy", Source: "test", LastRefreshedAt: now, NextRefreshAt: now.Add(time.Minute),
		QuotaWindows: []domain.QuotaSignal{{Label: "周限额", Kind: "rate_window", Value: remaining, Total: &total, Unit: "%", RemainingPercent: &remaining, ResetAt: &resetAt, Status: "critical", Source: "test"}},
	}
	if err := store.SaveConnectedAccount(ctx, account, []byte("encrypted-test-credential")); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateChannel(ctx, NotificationChannelInput{Kind: NotificationKindFeishu, WebhookURL: "https://open.feishu.cn/open-apis/bot/v2/hook/test-0001"}); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	service.http = &http.Client{Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		requests.Add(1)
		if request.URL.Host != "open.feishu.cn" {
			t.Fatalf("unexpected webhook host: %s", request.URL.Host)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"code":0,"msg":"success"}`)), Header: make(http.Header)}, nil
	})}
	service.now = func() time.Time { return now }

	first, err := service.Evaluate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if first.TriggeredAlerts != 2 || first.DeliveredMessages != 2 || requests.Load() != 2 {
		t.Fatalf("unexpected first evaluation: %#v, requests=%d", first, requests.Load())
	}
	second, err := service.Evaluate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if second.TriggeredAlerts != 0 || second.DeliveredMessages != 0 || requests.Load() != 2 {
		t.Fatalf("duplicate messages were delivered: %#v, requests=%d", second, requests.Load())
	}

	service.now = func() time.Time { return resetAt.Add(-20 * time.Hour) }
	third, err := service.Evaluate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if third.DeliveredMessages != 1 || requests.Load() != 3 {
		t.Fatalf("one-day reset reminder was not sent exactly once: %#v, requests=%d", third, requests.Load())
	}
}

func TestLowQuotaTriggersAtThresholdOnceAndRearmsAfterRecovery(t *testing.T) {
	service, store := newNotificationTestService(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 19, 8, 0, 0, 0, time.UTC)
	total := 100.0
	remaining := 15.0
	account := domain.ConnectedAccount{
		ID: "codex-crossing-test", ProviderID: "codex", Alias: "阈值测试", AuthMethod: "credential_import",
		Status: "healthy", Source: "test", LastRefreshedAt: now, NextRefreshAt: now.Add(time.Minute),
		QuotaWindows: []domain.QuotaSignal{{Label: "5 小时限额", Kind: "rate_window", Value: remaining, Total: &total, Unit: "%", RemainingPercent: &remaining, Status: "warning", Source: "test"}},
	}
	if err := store.SaveConnectedAccount(ctx, account, []byte("credential")); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateChannel(ctx, NotificationChannelInput{Kind: NotificationKindFeishu, WebhookURL: "https://open.feishu.cn/open-apis/bot/v2/hook/crossing-test"}); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	service.http = successfulFeishuClient(&requests)
	service.now = func() time.Time { return now }

	if result, err := service.Evaluate(ctx); err != nil || result.DeliveredMessages != 1 {
		t.Fatalf("15%% must trigger the configured threshold alert: result=%#v err=%v", result, err)
	}
	for _, value := range []float64{14.999, 13} {
		remaining = value
		account.QuotaWindows[0].Value = value
		account.QuotaWindows[0].RemainingPercent = &remaining
		if err := store.SaveConnectedAccount(ctx, account, []byte("credential")); err != nil {
			t.Fatal(err)
		}
		if _, err := service.Evaluate(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if requests.Load() != 1 {
		t.Fatalf("continuous low quota must send once, got %d messages", requests.Load())
	}

	remaining = 16
	account.QuotaWindows[0].Value = remaining
	account.QuotaWindows[0].RemainingPercent = &remaining
	if err := store.SaveConnectedAccount(ctx, account, []byte("credential")); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Evaluate(ctx); err != nil {
		t.Fatal(err)
	}
	alerts, err := store.Alerts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(alerts) != 0 {
		t.Fatalf("recovered low quota alert must be cleared: %#v", alerts)
	}

	now = now.Add(time.Hour)
	remaining = 15
	account.QuotaWindows[0].Value = remaining
	account.QuotaWindows[0].RemainingPercent = &remaining
	if err := store.SaveConnectedAccount(ctx, account, []byte("credential")); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Evaluate(ctx); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 2 {
		t.Fatalf("a new drop after recovery must send again, got %d messages", requests.Load())
	}
}

func TestFeishuFailuresNeverExposeWebhookToken(t *testing.T) {
	service, _ := newNotificationTestService(t)
	const token = "private-webhook-token"
	credential := feishuCredential{WebhookURL: "https://open.feishu.cn/open-apis/bot/v2/hook/" + token}
	service.http = &http.Client{Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("dial failed for %s", request.URL.String())
	})}
	err := service.sendFeishu(context.Background(), credential, "title", "message")
	if err == nil || strings.Contains(err.Error(), token) {
		t.Fatalf("network error leaked webhook token: %v", err)
	}

	for _, body := range []string{"", `{}`, `{"code":19001,"msg":"bad"}`} {
		service.http = &http.Client{Transport: roundTripperFunc(func(_ *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		})}
		if err := service.sendFeishu(context.Background(), credential, "title", "message"); err == nil {
			t.Fatalf("expected ambiguous or failed body %q to be rejected", body)
		}
	}
}

func TestNotificationEvaluateRejectsConcurrentRun(t *testing.T) {
	service, store := newNotificationTestService(t)
	ctx := context.Background()
	now := time.Now().UTC()
	remaining := 1.0
	if err := store.SaveConnectedAccount(ctx, domain.ConnectedAccount{
		ID: "codex-concurrency", ProviderID: "codex", Alias: "并发测试", AuthMethod: "credential_import", Status: "healthy", Source: "test",
		LastRefreshedAt: now, NextRefreshAt: now.Add(time.Minute), QuotaWindows: []domain.QuotaSignal{{Label: "周限额", Kind: "rate_window", Value: 1, Unit: "%", RemainingPercent: &remaining, Status: "critical", Source: "test"}},
	}, []byte("credential")); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateChannel(ctx, NotificationChannelInput{Kind: NotificationKindFeishu, WebhookURL: "https://open.feishu.cn/open-apis/bot/v2/hook/test-0002"}); err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	service.http = &http.Client{Transport: roundTripperFunc(func(_ *http.Request) (*http.Response, error) {
		close(entered)
		<-release
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"code":0}`)), Header: make(http.Header)}, nil
	})}
	done := make(chan error, 1)
	go func() {
		_, err := service.Evaluate(ctx)
		done <- err
	}()
	<-entered
	if _, err := service.Evaluate(ctx); !errors.Is(err, ErrNotificationEvaluationBusy) {
		t.Fatalf("expected concurrent evaluation to be rejected, got %v", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestShortQuotaWindowDoesNotCreateDayBasedResetReminder(t *testing.T) {
	service, store := newNotificationTestService(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 19, 8, 0, 0, 0, time.UTC)
	resetAt := now.Add(2 * time.Hour)
	remaining := 80.0
	if err := store.SaveConnectedAccount(ctx, domain.ConnectedAccount{
		ID: "codex-short-window", ProviderID: "codex", Alias: "短窗口测试", AuthMethod: "credential_import", Status: "healthy", Source: "test",
		LastRefreshedAt: now, NextRefreshAt: now.Add(time.Minute), QuotaWindows: []domain.QuotaSignal{{Label: "5 小时限额", Kind: "rate_window", Value: 80, Unit: "%", RemainingPercent: &remaining, WindowSeconds: 5 * 60 * 60, ResetAt: &resetAt, Status: "healthy", Source: "test"}},
	}, []byte("credential")); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateChannel(ctx, NotificationChannelInput{Kind: NotificationKindFeishu, WebhookURL: "https://open.feishu.cn/open-apis/bot/v2/hook/test-0003"}); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	service.http = successfulFeishuClient(&requests)
	service.now = func() time.Time { return now }
	result, err := service.Evaluate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.TriggeredAlerts != 0 || requests.Load() != 0 {
		t.Fatalf("short quota windows must not create 3-day/1-day reset reminders: %#v", result)
	}
}

func TestNotificationValidationRejectsUnsafeWebhookAndInvalidMailbox(t *testing.T) {
	service, _ := newNotificationTestService(t)
	ctx := context.Background()
	for _, webhook := range []string{
		"http://open.feishu.cn/open-apis/bot/v2/hook/test-token",
		"https://example.com/open-apis/bot/v2/hook/test-token",
		"https://open.feishu.cn/not-a-webhook",
		"https://open.feishu.cn:8443/open-apis/bot/v2/hook/test-token",
		"https://open.feishu.cn/open-apis/bot/v2/hook/test-token?redirect=1",
	} {
		if _, err := service.CreateChannel(ctx, NotificationChannelInput{Kind: NotificationKindFeishu, WebhookURL: webhook}); err == nil {
			t.Fatalf("expected webhook %q to be rejected", webhook)
		}
	}
	if _, err := service.CreateChannel(ctx, NotificationChannelInput{Kind: NotificationKindQQMail, Sender: "not-qq@example.com", Recipient: "valid@example.com", AuthCode: "secret"}); err == nil {
		t.Fatal("expected non-QQ sender to be rejected")
	}
	if _, err := service.CreateChannel(ctx, NotificationChannelInput{Kind: NotificationKindQQMail, Sender: "sender@qq.com", Recipient: "bad\nBcc: attacker@example.com", AuthCode: "secret"}); err == nil {
		t.Fatal("expected header injection recipient to be rejected")
	}
}

func TestNotificationSignalKeyIsStableAcrossStoredWindowReordering(t *testing.T) {
	total := 100.0
	resetAt := time.Date(2026, 9, 21, 8, 0, 0, 0, time.UTC)
	first := domain.QuotaSignal{ID: "account-0", Kind: "rate_window", Label: "周限额", Unit: "%", Source: "test", Value: 15, Total: &total, RemainingPercent: floatPointer(15), ResetAt: &resetAt}
	second := domain.QuotaSignal{ID: "account-3", Kind: "rate_window", Label: "周限额", Unit: "%", Source: "test", Value: 7, Total: &total, RemainingPercent: floatPointer(7), ResetAt: &resetAt}
	if notificationSignalKey(first) != notificationSignalKey(second) {
		t.Fatal("notification signal identity must not depend on row index or changing balance")
	}
}

func TestNotificationSignalKeyDistinguishesSameNamedWorkBuddyPackages(t *testing.T) {
	expiresA := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	expiresB := expiresA.Add(24 * time.Hour)
	total6, total66 := 6.0, 66.0
	base := domain.QuotaSignal{Kind: "credits", Label: "拉新权益包", Unit: "credits", Source: "WorkBuddy Billing", Total: &total6, ExpiresAt: &expiresA}
	differentTotal := base
	differentTotal.Total = &total66
	differentExpiry := base
	differentExpiry.ExpiresAt = &expiresB
	if notificationSignalKey(base) == notificationSignalKey(differentTotal) {
		t.Fatal("same-named packages with different totals must have different identities")
	}
	if notificationSignalKey(base) == notificationSignalKey(differentExpiry) {
		t.Fatal("same-named packages with different expiries must have different identities")
	}
}

func TestSameNamedPackagesKeepIndependentStateAcrossReordering(t *testing.T) {
	service, store := newNotificationTestService(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 19, 8, 0, 0, 0, time.UTC)
	expiresAt := now.AddDate(0, 1, 0)
	total6, total66 := 6.0, 66.0
	low, healthy := 10.0, 80.0
	lowPackage := domain.QuotaSignal{Label: "拉新权益包", Kind: "credits", Value: 0.6, Total: &total6, Unit: "credits", RemainingPercent: &low, ExpiresAt: &expiresAt, Status: "critical", Source: "WorkBuddy Billing"}
	healthyPackage := domain.QuotaSignal{Label: "拉新权益包", Kind: "credits", Value: 52.8, Total: &total66, Unit: "credits", RemainingPercent: &healthy, ExpiresAt: &expiresAt, Status: "healthy", Source: "WorkBuddy Billing"}
	account := domain.ConnectedAccount{
		ID: "workbuddy-semantic-identity", ProviderID: "codex", Alias: "同名积分包测试", AuthMethod: "credential_import",
		Status: "healthy", Source: "test", LastRefreshedAt: now, NextRefreshAt: now.Add(time.Minute),
		QuotaWindows: []domain.QuotaSignal{lowPackage, healthyPackage},
	}
	if err := store.SaveConnectedAccount(ctx, account, []byte("credential")); err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return now }
	first, err := service.Evaluate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if first.TriggeredAlerts != 1 {
		t.Fatalf("expected only the low package to trigger, got %#v", first)
	}

	account.QuotaWindows = []domain.QuotaSignal{healthyPackage, lowPackage}
	if err := store.SaveConnectedAccount(ctx, account, []byte("credential")); err != nil {
		t.Fatal(err)
	}
	second, err := service.Evaluate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if second.TriggeredAlerts != 0 {
		t.Fatalf("reordering same-named packages must not re-arm the alert: %#v", second)
	}
	alerts, err := store.Alerts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(alerts) != 1 {
		t.Fatalf("healthy same-named package cleared or duplicated the low alert: %#v", alerts)
	}
}

func TestNotificationEvaluationCleansDisappearedSignalStateAndAlert(t *testing.T) {
	service, store := newNotificationTestService(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 19, 8, 0, 0, 0, time.UTC)
	resetAt := now.Add(48 * time.Hour)
	total, remaining := 100.0, 12.0
	account := domain.ConnectedAccount{
		ID: "codex-disappeared-signal", ProviderID: "codex", Alias: "消失窗口测试", AuthMethod: "credential_import",
		Status: "healthy", Source: "test", LastRefreshedAt: now, NextRefreshAt: now.Add(time.Minute),
		QuotaWindows: []domain.QuotaSignal{{Label: "周限额", Kind: "rate_window", Value: remaining, Total: &total, Unit: "%", RemainingPercent: &remaining, ResetAt: &resetAt, Status: "critical", Source: "test"}},
	}
	if err := store.SaveConnectedAccount(ctx, account, []byte("credential")); err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return now }
	first, err := service.Evaluate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if first.TriggeredAlerts != 2 {
		t.Fatalf("expected low quota and reset alerts, got %#v", first)
	}
	alerts, err := store.Alerts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(alerts) != 2 {
		t.Fatalf("expected two active alerts before signal disappears, got %#v", alerts)
	}

	account.QuotaWindows = nil
	if err := store.SaveConnectedAccount(ctx, account, []byte("credential")); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Evaluate(ctx); err != nil {
		t.Fatal(err)
	}
	alerts, err = store.Alerts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(alerts) != 0 {
		t.Fatalf("disappeared signal alerts were not cleared: %#v", alerts)
	}
	states, err := store.NotificationStates(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	for key := range states {
		if strings.HasPrefix(key, "low_quota_state:") || strings.HasPrefix(key, "quota_reset_state:") {
			t.Fatalf("disappeared signal state was not cleared: %s", key)
		}
	}
}

func floatPointer(value float64) *float64 { return &value }

func newNotificationTestService(t *testing.T) (*Notifications, *sqlite.Store) {
	t.Helper()
	root := t.TempDir()
	store, err := sqlite.Open(filepath.Join(root, "notifications.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.EnsureProviderCatalog(context.Background()); err != nil {
		t.Fatal(err)
	}
	vault, err := secure.OpenVault(filepath.Join(root, "credential.key"))
	if err != nil {
		t.Fatal(err)
	}
	hub := NewEventHub()
	accounts := NewAccounts(store, vault, hub)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewNotifications(store, vault, accounts, hub, logger), store
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (fn roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func successfulFeishuClient(requests *atomic.Int32) *http.Client {
	return &http.Client{Transport: roundTripperFunc(func(_ *http.Request) (*http.Response, error) {
		requests.Add(1)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"code":0,"msg":"success"}`)), Header: make(http.Header)}, nil
	})}
}
