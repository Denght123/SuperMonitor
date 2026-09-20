package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Denght123/SuperMonitor/internal/domain"
)

func TestAllProductionAdaptersAreConnectable(t *testing.T) {
	for _, providerID := range []string{
		"codex", "workbuddy-cn", "workbuddy-global", "trae-cn", "qoder-cn", "qoder-global",
		"coze-cn", "bailian", "kiro", "cursor", "deepseek", "mimo", "tokenrhythm",
		"zhipu", "gemini-cli", "claude-code",
	} {
		_, _, connectable := providerPresentation(providerID)
		if !connectable {
			t.Errorf("provider %s has a production adapter but liveAuth=false", providerID)
		}
	}
}

func TestSaveConnectedAccountUpdatesProviderLastCheckedAt(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "provider-check.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if err := store.EnsureProviderCatalog(ctx); err != nil {
		t.Fatal(err)
	}
	const stale = "2020-01-01T00:00:00Z"
	if _, err := store.db.ExecContext(ctx, "UPDATE providers SET last_checked_at=? WHERE id=?", stale, "zhipu"); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	if err := store.SaveConnectedAccount(ctx, domain.ConnectedAccount{
		ID: "zhipu-check", ProviderID: "zhipu", Alias: "智谱", AuthMethod: "api_key",
		Status: "healthy", Source: "test", LastRefreshedAt: now, NextRefreshAt: now.Add(time.Minute),
	}, []byte("encrypted")); err != nil {
		t.Fatal(err)
	}
	providers, err := store.Providers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, provider := range providers {
		if provider.ID == "zhipu" {
			if !provider.LastCheckedAt.Equal(now) {
				t.Fatalf("lastCheckedAt = %s, want %s", provider.LastCheckedAt, now)
			}
			return
		}
	}
	t.Fatal("zhipu provider not found")
}

func TestDeleteConnectedAccountRemovesCredentialAndQuotaWindows(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "delete.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if err := store.EnsureProviderCatalog(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	account := domain.ConnectedAccount{
		ID: "codex-delete-me", ProviderID: "codex", Alias: "待删除账号", AuthMethod: "credential_import",
		Status: "healthy", Source: "test", LastRefreshedAt: now, NextRefreshAt: now.Add(time.Minute),
		QuotaWindows: []domain.QuotaSignal{{Label: "5 小时限额", Kind: "rate_window", Value: 50, Unit: "%", Status: "healthy", Source: "test"}},
	}
	if err := store.SaveConnectedAccount(ctx, account, []byte("encrypted-credential")); err != nil {
		t.Fatal(err)
	}
	channel := domain.NotificationChannel{ID: "delete-test-channel", Kind: "feishu", Name: "测试渠道", Target: "已加密", Enabled: true}
	if err := store.SaveNotificationChannel(ctx, channel, []byte("encrypted-webhook")); err != nil {
		t.Fatal(err)
	}
	const dedupeKey = "low_quota:codex-delete-me:test"
	const alertID = "notify-delete-test"
	if _, err := store.SaveNotificationEvent(ctx, dedupeKey, account.ID, alertID, "low_quota", now); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveNotificationState(ctx, "low_quota_state:"+account.ID, account.ID, "active:"+dedupeKey, now); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveAlert(ctx, domain.Alert{ID: alertID, Severity: "critical", Title: "额度告警", Message: account.Alias, Provider: "Codex", CreatedAt: now, Recovery: "测试"}); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordNotificationDelivery(ctx, dedupeKey, channel.ID, "low_quota", now); err != nil {
		t.Fatal(err)
	}

	deleted, err := store.DeleteConnectedAccount(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if deleted.ID != account.ID || deleted.Alias != account.Alias {
		t.Fatalf("unexpected deleted account: %+v", deleted)
	}
	if _, _, err := store.ConnectedAccount(ctx, account.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected account to be absent, got %v", err)
	}
	for _, table := range []string{"connected_accounts", "account_credentials", "account_quota_windows"} {
		var count int
		if err := store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table+" WHERE "+map[string]string{
			"connected_accounts": "id", "account_credentials": "account_id", "account_quota_windows": "account_id",
		}[table]+"=?", account.ID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("expected %s rows to be deleted, got %d", table, count)
		}
	}
	for table, query := range map[string]string{
		"notification_events":     "SELECT COUNT(*) FROM notification_events WHERE account_id=?",
		"notification_states":     "SELECT COUNT(*) FROM notification_states WHERE account_id=?",
		"notification_deliveries": "SELECT COUNT(*) FROM notification_deliveries WHERE dedupe_key=?",
		"alerts":                  "SELECT COUNT(*) FROM alerts WHERE id=?",
	} {
		value := account.ID
		if table == "notification_deliveries" {
			value = dedupeKey
		}
		if table == "alerts" {
			value = alertID
		}
		var count int
		if err := store.db.QueryRowContext(ctx, query, value).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("expected %s notification data to be deleted, got %d", table, count)
		}
	}
	if _, err := store.DeleteConnectedAccount(ctx, account.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected repeated deletion to return sql.ErrNoRows, got %v", err)
	}
}

func TestNotificationEventAndAlertStayAtomicWithAccountDelete(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "notification-delete-race.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if err := store.EnsureProviderCatalog(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)

	for index := 0; index < 40; index++ {
		accountID := fmt.Sprintf("codex-notification-race-%d", index)
		dedupeKey := fmt.Sprintf("low_quota:%s:test", accountID)
		alertID := fmt.Sprintf("notify-race-%d", index)
		account := domain.ConnectedAccount{
			ID: accountID, ProviderID: "codex", Alias: accountID, AuthMethod: "credential_import",
			Status: "healthy", Source: "test", LastRefreshedAt: now, NextRefreshAt: now.Add(time.Minute),
		}
		if err := store.SaveConnectedAccount(ctx, account, []byte("encrypted")); err != nil {
			t.Fatal(err)
		}
		alert := domain.Alert{ID: alertID, Severity: "critical", Title: "额度告警", Message: accountID, Provider: "Codex", CreatedAt: now, Recovery: "测试"}
		start := make(chan struct{})
		saveDone := make(chan error, 1)
		deleteDone := make(chan error, 1)
		go func() {
			<-start
			_, saveErr := store.SaveNotificationEventAndAlert(ctx, dedupeKey, accountID, alertID, "low_quota", now, &alert)
			saveDone <- saveErr
		}()
		go func() {
			<-start
			_, deleteErr := store.DeleteConnectedAccount(ctx, accountID)
			deleteDone <- deleteErr
		}()
		close(start)
		saveErr, deleteErr := <-saveDone, <-deleteDone
		if deleteErr != nil {
			t.Fatalf("iteration %d delete failed: %v", index, deleteErr)
		}
		// When deletion wins the database lock, the event insert correctly
		// fails its account foreign key instead of creating an orphan alert.
		if saveErr != nil && !strings.Contains(saveErr.Error(), "FOREIGN KEY constraint failed") {
			t.Fatalf("iteration %d unexpected atomic save error: %v", index, saveErr)
		}
		for table, query := range map[string]string{
			"events": "SELECT COUNT(*) FROM notification_events WHERE account_id=?",
			"alerts": "SELECT COUNT(*) FROM alerts WHERE id=?",
		} {
			value := accountID
			if table == "alerts" {
				value = alertID
			}
			var count int
			if err := store.db.QueryRowContext(ctx, query, value).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 0 {
				t.Fatalf("iteration %d left orphan %s rows: %d", index, table, count)
			}
		}
	}
}
