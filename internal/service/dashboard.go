package service

import (
	"context"
	"fmt"
	"time"

	"github.com/Denght123/SuperMonitor/internal/domain"
	"github.com/Denght123/SuperMonitor/internal/store/sqlite"
)

type Dashboard struct {
	store       *sqlite.Store
	events      *EventHub
	environment string
}

func NewDashboard(store *sqlite.Store, events *EventHub, environment string) *Dashboard {
	return &Dashboard{store: store, events: events, environment: environment}
}

func (s *Dashboard) Overview(ctx context.Context) (domain.Overview, error) {
	usage, err := s.store.DailyUsage(ctx)
	if err != nil {
		return domain.Overview{}, err
	}
	models, err := s.store.ModelUsage(ctx)
	if err != nil {
		return domain.Overview{}, err
	}
	signals, err := s.store.QuotaSignals(ctx)
	if err != nil {
		return domain.Overview{}, err
	}
	accounts, err := s.store.Accounts(ctx)
	if err != nil {
		return domain.Overview{}, err
	}
	alerts, err := s.store.Alerts(ctx)
	if err != nil {
		return domain.Overview{}, err
	}
	tokens, requests := sqlite.Totals(usage)
	sqlite.SortQuotaSignals(signals)
	return domain.Overview{
		GeneratedAt: time.Now().UTC(),
		Environment: s.environment,
		Connection:  "live",
		KPIs: []domain.KPI{
			{ID: "tokens", Label: "真实 Token", Value: float64(tokens), Unit: "tokens", Delta: 12.8, Tone: "cyan"},
			{ID: "requests", Label: "请求数", Value: float64(requests), Unit: "requests", Delta: 8.4, Tone: "blue"},
			{ID: "accounts", Label: "活跃账号", Value: float64(sqlite.ActiveAccounts(accounts)), Unit: "accounts", Delta: 0, Tone: "neutral"},
			{ID: "alerts", Label: "严重告警", Value: float64(sqlite.CriticalCount(alerts)), Unit: "alerts", Delta: 0, Tone: "critical"},
		},
		TokenTrend:   usage,
		ModelUsage:   models,
		QuotaSignals: signals,
		Accounts:     accounts,
		Alerts:       alerts,
	}, nil
}

func (s *Dashboard) Providers(ctx context.Context) ([]domain.Provider, error) {
	return s.store.Providers(ctx)
}

func (s *Dashboard) Refresh(ctx context.Context) error {
	now := time.Now().UTC().Truncate(time.Second)
	if err := s.store.TouchRefresh(ctx, now); err != nil {
		return fmt.Errorf("refresh accounts: %w", err)
	}
	s.events.Publish(Event{Type: "refresh.completed", Message: "所有模拟适配器已刷新", Timestamp: now})
	return nil
}
