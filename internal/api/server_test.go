package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Denght123/SuperMonitor/internal/api"
	"github.com/Denght123/SuperMonitor/internal/domain"
	"github.com/Denght123/SuperMonitor/internal/secure"
	"github.com/Denght123/SuperMonitor/internal/service"
	"github.com/Denght123/SuperMonitor/internal/store/sqlite"
)

func TestOverviewEndpoint(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SeedSyntheticData(context.Background()); err != nil {
		t.Fatal(err)
	}
	hub := service.NewEventHub()
	accounts := service.NewAccounts(store, mustVault(t), hub)
	handler := api.New(api.Dependencies{
		Dashboard: service.NewDashboard(store, hub, accounts, "test"),
		Accounts:  accounts,
		Events:    hub,
		Web:       http.NotFoundHandler(),
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		Version:   "test",
	})

	request := httptest.NewRequest(http.MethodGet, "/api/v1/overview", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", response.Code, response.Body.String())
	}
	if response.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("security headers were not applied")
	}
}

func TestAccountSyncStatusEndpoint(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "sync-status.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.EnsureProviderCatalog(context.Background()); err != nil {
		t.Fatal(err)
	}
	hub := service.NewEventHub()
	accounts := service.NewAccounts(store, mustVault(t), hub)
	syncer := service.NewAccountSync(accounts, hub, slog.New(slog.NewTextHandler(io.Discard, nil)))
	dashboard := service.NewDashboard(store, hub, accounts, "test")
	dashboard.SetAccountSync(syncer)
	handler := api.New(api.Dependencies{
		Dashboard: dashboard, Accounts: accounts, AccountSync: syncer, Events: hub,
		Web: http.NotFoundHandler(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Version: "test",
	})

	request := httptest.NewRequest(http.MethodGet, "/api/v1/sync/status", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", response.Code, response.Body.String())
	}
	var status domain.AccountSyncStatus
	if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	if !status.Enabled || status.IntervalSeconds != 900 || status.ActivityMode != "verified_only" {
		t.Fatalf("unexpected account sync status: %#v", status)
	}
}

func TestUnknownAPIRouteReturnsStructuredJSONError(t *testing.T) {
	handler := api.New(api.Dependencies{
		Events: service.NewEventHub(), Web: http.NotFoundHandler(),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Version: "test",
	})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/does-not-exist", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
	if contentType := response.Header().Get("Content-Type"); contentType != "application/json; charset=utf-8" {
		t.Fatalf("content type = %q", contentType)
	}
	if body := response.Body.String(); !strings.Contains(body, `"code":"route_not_found"`) {
		t.Fatalf("unexpected body: %s", body)
	}
}

func TestDeleteAccountEndpointRemovesAccountFromOverviewAndActivities(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "delete-api.db"))
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
		ID: "workbuddy-delete-me", ProviderID: "workbuddy-cn", Alias: "待删除 WorkBuddy", AuthMethod: "credential_import",
		Status: "healthy", Source: "test", LastRefreshedAt: now, NextRefreshAt: now.Add(time.Minute),
		QuotaWindows: []domain.QuotaSignal{{Label: "总积分余额", Kind: "credits", Value: 100, Unit: "credits", Status: "healthy", Source: "test"}},
	}
	if err := store.SaveConnectedAccount(ctx, account, []byte("encrypted-credential")); err != nil {
		t.Fatal(err)
	}
	hub := service.NewEventHub()
	accounts := service.NewAccounts(store, mustVault(t), hub)
	handler := api.New(api.Dependencies{
		Dashboard: service.NewDashboard(store, hub, accounts, "test"),
		Accounts:  accounts, Events: hub, Web: http.NotFoundHandler(),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Version: "test",
	})

	request := httptest.NewRequest(http.MethodDelete, "/api/v1/accounts/"+account.ID, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d: %s", response.Code, response.Body.String())
	}

	overviewRequest := httptest.NewRequest(http.MethodGet, "/api/v1/overview", nil)
	overviewResponse := httptest.NewRecorder()
	handler.ServeHTTP(overviewResponse, overviewRequest)
	if overviewResponse.Code != http.StatusOK {
		t.Fatalf("expected overview status 200, got %d: %s", overviewResponse.Code, overviewResponse.Body.String())
	}
	var overview domain.Overview
	if err := json.NewDecoder(overviewResponse.Body).Decode(&overview); err != nil {
		t.Fatal(err)
	}
	for _, item := range overview.Accounts {
		if item.ID == account.ID {
			t.Fatal("deleted account is still present in overview")
		}
	}
	for _, signal := range overview.QuotaSignals {
		if signal.Label == "总积分余额" {
			t.Fatal("deleted account quota is still present in overview")
		}
	}

	activitiesRequest := httptest.NewRequest(http.MethodGet, "/api/v1/activities", nil)
	activitiesResponse := httptest.NewRecorder()
	handler.ServeHTTP(activitiesResponse, activitiesRequest)
	if activitiesResponse.Code != http.StatusOK || activitiesResponse.Body.String() != "{\"items\":[]}\n" {
		t.Fatalf("expected no activities after deletion, got %d: %s", activitiesResponse.Code, activitiesResponse.Body.String())
	}

	repeatedRequest := httptest.NewRequest(http.MethodDelete, "/api/v1/accounts/"+account.ID, nil)
	repeatedResponse := httptest.NewRecorder()
	handler.ServeHTTP(repeatedResponse, repeatedRequest)
	if repeatedResponse.Code != http.StatusNotFound {
		t.Fatalf("expected repeated deletion status 404, got %d: %s", repeatedResponse.Code, repeatedResponse.Body.String())
	}
}

func TestNotificationChannelAPIEncryptsAndNeverReturnsWebhook(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "notification-api.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if err := store.EnsureProviderCatalog(ctx); err != nil {
		t.Fatal(err)
	}
	vault := mustVault(t)
	hub := service.NewEventHub()
	accounts := service.NewAccounts(store, vault, hub)
	notifications := service.NewNotifications(store, vault, accounts, hub, slog.New(slog.NewTextHandler(io.Discard, nil)))
	handler := api.New(api.Dependencies{
		Dashboard: service.NewDashboard(store, hub, accounts, "test"), Accounts: accounts, Notifications: notifications,
		Events: hub, Web: http.NotFoundHandler(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Version: "test",
	})

	const token = "api-must-not-return-this-token"
	body := bytes.NewBufferString(`{"kind":"feishu","name":"开发组","webhookUrl":"https://open.feishu.cn/open-apis/bot/v2/hook/` + token + `"}`)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/notifications/channels", body)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d: %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), token) {
		t.Fatal("notification channel response leaked the webhook token")
	}

	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/notifications/channels", nil)
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK || strings.Contains(listResponse.Body.String(), token) {
		t.Fatalf("notification channel list leaked the webhook token: %s", listResponse.Body.String())
	}

	policyRequest := httptest.NewRequest(http.MethodGet, "/api/v1/notifications/policy", nil)
	policyResponse := httptest.NewRecorder()
	handler.ServeHTTP(policyResponse, policyRequest)
	if policyResponse.Code != http.StatusOK || !strings.Contains(policyResponse.Body.String(), `"lowQuotaPercent":15`) {
		t.Fatalf("unexpected notification policy: %d %s", policyResponse.Code, policyResponse.Body.String())
	}
}

func TestCodexUsageImportAddsRealModelToOverview(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "codex-usage-api.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if err := store.EnsureProviderCatalog(ctx); err != nil {
		t.Fatal(err)
	}
	account := domain.ConnectedAccount{
		ID: "codex-sol", ProviderID: "codex", Alias: "Codex Sol", AuthMethod: "device_code",
		Status: "healthy", Source: "test", LastRefreshedAt: time.Now().UTC(), NextRefreshAt: time.Now().UTC().Add(time.Minute),
	}
	if err := store.SaveConnectedAccount(ctx, account, []byte("encrypted-credential")); err != nil {
		t.Fatal(err)
	}
	hub := service.NewEventHub()
	accounts := service.NewAccounts(store, mustVault(t), hub)
	handler := api.New(api.Dependencies{
		Dashboard: service.NewDashboard(store, hub, accounts, "test"), Accounts: accounts, Events: hub,
		Web: http.NotFoundHandler(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Version: "test",
	})
	payload := `{"sources":[{"sourceId":"session-sol","contentHash":"hash-v1","entries":[{"date":"2026-09-21","model":"gpt-5.6-sol","counters":{"inputTokens":700,"outputTokens":200,"cacheTokens":300,"requests":1}}]}]}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/accounts/"+account.ID+"/usage/codex-import", strings.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"importedTokens":1200`) {
		t.Fatalf("unexpected import response: %d %s", response.Code, response.Body.String())
	}

	overviewRequest := httptest.NewRequest(http.MethodGet, "/api/v1/overview", nil)
	overviewResponse := httptest.NewRecorder()
	handler.ServeHTTP(overviewResponse, overviewRequest)
	if overviewResponse.Code != http.StatusOK || !strings.Contains(overviewResponse.Body.String(), `"model":"gpt-5.6-sol"`) {
		t.Fatalf("Codex model usage missing from overview: %d %s", overviewResponse.Code, overviewResponse.Body.String())
	}
}

func mustVault(t *testing.T) *secure.Vault {
	t.Helper()
	vault, err := secure.OpenVault(filepath.Join(t.TempDir(), "credential.key"))
	if err != nil {
		t.Fatal(err)
	}
	return vault
}
