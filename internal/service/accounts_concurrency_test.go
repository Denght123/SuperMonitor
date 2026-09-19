package service

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Denght123/SuperMonitor/internal/domain"
	"github.com/Denght123/SuperMonitor/internal/secure"
	"github.com/Denght123/SuperMonitor/internal/store/sqlite"
)

func TestDeleteWaitsForAccountOperationAndRemovesItsFinalWrite(t *testing.T) {
	store, accounts, _ := newAccountConcurrencyTest(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	account := domain.ConnectedAccount{
		ID: "codex-delete-race", ProviderID: "codex", Alias: "并发删除账号", AuthMethod: "credential_import",
		Status: "healthy", Source: "test", LastRefreshedAt: now, NextRefreshAt: now.Add(time.Minute),
		QuotaWindows: []domain.QuotaSignal{{Label: "周限额", Kind: "rate_window", Value: 50, Unit: "%", Status: "healthy", Source: "test"}},
	}
	if err := store.SaveConnectedAccount(ctx, account, []byte("initial-encrypted-credential")); err != nil {
		t.Fatal(err)
	}

	// Simulate an already-running refresh or activity that has read the account
	// and is about to persist its final credential/quota update.
	accounts.accountOps.RLock()
	deleteStarted := make(chan struct{})
	deleteDone := make(chan error, 1)
	go func() {
		close(deleteStarted)
		deleteDone <- accounts.Delete(ctx, account.ID)
	}()
	<-deleteStarted
	select {
	case err := <-deleteDone:
		accounts.accountOps.RUnlock()
		t.Fatalf("delete completed while an account operation was still active: %v", err)
	case <-time.After(30 * time.Millisecond):
	}

	account.QuotaWindows[0].Value = 10
	if err := store.SaveConnectedAccount(ctx, account, []byte("rotated-encrypted-credential")); err != nil {
		accounts.accountOps.RUnlock()
		t.Fatal(err)
	}
	accounts.accountOps.RUnlock()

	select {
	case err := <-deleteDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("delete did not complete after the account operation released its read lock")
	}
	if _, _, err := store.ConnectedAccount(ctx, account.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("in-flight write resurrected deleted account: %v", err)
	}
}

func TestRefreshAndActivityParticipateInAccountOperationBarrier(t *testing.T) {
	store, accounts, vault := newAccountConcurrencyTest(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	encrypted, err := vault.Encrypt([]byte(`{"invalid":`))
	if err != nil {
		t.Fatal(err)
	}
	for _, account := range []domain.ConnectedAccount{
		{ID: "codex-operation-lock", ProviderID: "codex", Alias: "Codex", AuthMethod: "credential_import", Status: "healthy", Source: "test", LastRefreshedAt: now, NextRefreshAt: now.Add(time.Minute)},
		{ID: "workbuddy-operation-lock", ProviderID: "workbuddy-cn", Alias: "WorkBuddy", AuthMethod: "credential_import", Status: "healthy", Source: "test", LastRefreshedAt: now, NextRefreshAt: now.Add(time.Minute)},
	} {
		if err := store.SaveConnectedAccount(ctx, account, encrypted); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		name string
		run  func() error
	}{
		{name: "refresh", run: func() error { _, err := accounts.RefreshOne(ctx, "codex-operation-lock"); return err }},
		{name: "activity", run: func() error {
			_, err := accounts.RunActivity(ctx, "workbuddy-operation-lock", "daily-checkin")
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			accounts.accountOps.Lock()
			started := make(chan struct{})
			done := make(chan error, 1)
			go func() {
				close(started)
				done <- test.run()
			}()
			<-started
			select {
			case err := <-done:
				accounts.accountOps.Unlock()
				t.Fatalf("operation bypassed account barrier: %v", err)
			case <-time.After(30 * time.Millisecond):
			}
			accounts.accountOps.Unlock()
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("expected malformed test credential to fail after acquiring the barrier")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("operation did not resume after account barrier was released")
			}
		})
	}
}

func newAccountConcurrencyTest(t *testing.T) (*sqlite.Store, *Accounts, *secure.Vault) {
	t.Helper()
	root := t.TempDir()
	store, err := sqlite.Open(filepath.Join(root, "accounts.db"))
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
	return store, NewAccounts(store, vault, NewEventHub()), vault
}
