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
	accounts    *Accounts
	accountSync *AccountSync
	environment string
}

func NewDashboard(store *sqlite.Store, events *EventHub, accounts *Accounts, environment string) *Dashboard {
	return &Dashboard{store: store, events: events, accounts: accounts, environment: environment}
}

func (s *Dashboard) SetAccountSync(accountSync *AccountSync) {
	s.accountSync = accountSync
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
	if usage == nil {
		usage = []domain.DailyUsage{}
	}
	if models == nil {
		models = []domain.ModelUsage{}
	}
	if signals == nil {
		signals = []domain.QuotaSignal{}
	}
	if accounts == nil {
		accounts = []domain.AccountSummary{}
	}
	if alerts == nil {
		alerts = []domain.Alert{}
	}
	tokens, requests := sqlite.Totals(usage)
	for index := range accounts {
		signals = append(signals, accounts[index].QuotaWindows...)
	}
	sqlite.SortQuotaSignals(signals)
	return domain.Overview{
		GeneratedAt: time.Now().UTC(),
		Environment: s.environment,
		Connection:  "live",
		KPIs: []domain.KPI{
			{ID: "tokens", Label: "真实 Token", Value: float64(tokens), Unit: "tokens", Tone: "cyan"},
			{ID: "requests", Label: "请求数", Value: float64(requests), Unit: "requests", Tone: "blue"},
			{ID: "accounts", Label: "活跃账号", Value: float64(sqlite.ActiveAccounts(accounts)), Unit: "accounts", Tone: "neutral"},
			{ID: "alerts", Label: "严重告警", Value: float64(sqlite.CriticalCount(alerts)), Unit: "alerts", Tone: "critical"},
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
	if s.accountSync != nil {
		if err := s.accountSync.Refresh(ctx, "manual"); err != nil {
			return err
		}
	} else if s.accounts != nil {
		if err := s.accounts.RefreshAll(ctx); err != nil {
			return err
		}
	}
	if err := s.store.TouchRefresh(ctx, now); err != nil {
		return fmt.Errorf("refresh accounts: %w", err)
	}
	if s.accountSync == nil {
		s.events.Publish(Event{Type: "refresh.completed", Message: "全部账号额度已刷新", Timestamp: now})
	}
	return nil
}
