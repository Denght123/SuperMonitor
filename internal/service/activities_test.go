package service

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Denght123/SuperMonitor/internal/domain"
	"github.com/Denght123/SuperMonitor/internal/secure"
	"github.com/Denght123/SuperMonitor/internal/store/sqlite"
)

func TestListActivitiesSurfacesPerAccountCredentialFailure(t *testing.T) {
	root := t.TempDir()
	store, err := sqlite.Open(filepath.Join(root, "activities.db"))
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
	encrypted, err := vault.Encrypt([]byte("{not-valid-json"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	account := domain.ConnectedAccount{
		ID: "workbuddy-invalid-activity", ProviderID: "workbuddy-cn", ProviderName: "WorkBuddy / CodeBuddy 国内版",
		Alias: "认证失效账号", AuthMethod: "oauth", Status: "healthy", Source: "test",
		LastRefreshedAt: now, NextRefreshAt: now.Add(10 * time.Minute), QuotaWindows: []domain.QuotaSignal{},
	}
	if err := store.SaveConnectedAccount(context.Background(), account, encrypted); err != nil {
		t.Fatal(err)
	}

	accounts := NewAccounts(store, vault, NewEventHub())
	activities, listErr := accounts.ListActivities(context.Background())
	if listErr == nil {
		t.Fatal("invalid activity credential must be reported to the scheduler")
	}
	if len(activities) != 1 {
		t.Fatalf("expected one visible error card, got %#v", activities)
	}
	activity := activities[0]
	if activity.AccountID != account.ID || activity.Status != "error" || activity.Description == "" {
		t.Fatalf("unexpected activity error card: %#v", activity)
	}
}
