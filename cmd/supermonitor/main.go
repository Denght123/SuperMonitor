package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Denght123/SuperMonitor/internal/api"
	"github.com/Denght123/SuperMonitor/internal/config"
	"github.com/Denght123/SuperMonitor/internal/secure"
	"github.com/Denght123/SuperMonitor/internal/service"
	"github.com/Denght123/SuperMonitor/internal/store/sqlite"
	"github.com/Denght123/SuperMonitor/internal/webui"
)

var version = "dev"

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		logger.Error("create data directory", "error", err)
		os.Exit(1)
	}

	store, err := sqlite.Open(filepath.Join(cfg.DataDir, "supermonitor.db"))
	if err != nil {
		logger.Error("open database", "error", err)
		os.Exit(1)
	}
	defer store.Close()
	vault, err := secure.OpenVault(filepath.Join(cfg.DataDir, "credential.key"))
	if err != nil {
		logger.Error("open credential vault", "error", err)
		os.Exit(1)
	}

	if err := store.EnsureProviderCatalog(context.Background()); err != nil {
		logger.Error("ensure provider catalog", "error", err)
		os.Exit(1)
	}
	if err := store.CleanupSyntheticData(context.Background()); err != nil {
		logger.Error("remove legacy synthetic data", "error", err)
		os.Exit(1)
	}

	hub := service.NewEventHub()
	accounts := service.NewAccounts(store, vault, hub)
	dashboard := service.NewDashboard(store, hub, accounts, cfg.Environment)
	notifications := service.NewNotifications(store, vault, accounts, hub, logger)
	handler := api.New(api.Dependencies{
		Dashboard:     dashboard,
		Accounts:      accounts,
		Notifications: notifications,
		Events:        hub,
		Web:           webui.Handler(),
		Logger:        logger,
		Version:       version,
	})
	schedulerCtx, stopScheduler := context.WithCancel(context.Background())
	defer stopScheduler()
	go notifications.RunScheduler(schedulerCtx)

	server := &http.Server{
		Addr:              cfg.Listen,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("SuperMonitor started", "address", cfg.Listen, "environment", cfg.Environment, "version", version)
		serverErrors <- server.ListenAndServe()
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-stop:
		logger.Info("shutdown requested", "signal", sig.String())
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server stopped", "error", err)
		}
	}
	stopScheduler()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		logger.Error("graceful shutdown", "error", err)
	}
}
