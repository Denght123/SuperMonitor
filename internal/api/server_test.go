package api_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Denght123/SuperMonitor/internal/api"
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
	handler := api.New(api.Dependencies{
		Dashboard: service.NewDashboard(store, hub, "test"),
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
