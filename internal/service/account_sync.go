package service

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/Denght123/SuperMonitor/internal/domain"
)

const (
	DefaultAccountSyncInterval = 15 * time.Minute
	defaultAccountSyncTimeout  = 3 * time.Minute
	minimumStartupDelay        = 5 * time.Second
	maximumStartupDelay        = 60 * time.Second
)

var ErrAccountSyncBusy = errors.New("账号自动同步正在执行")

type allAccountRefresher interface {
	RefreshAll(context.Context) error
}

type verifiedActivityScanner interface {
	Evaluate(context.Context) (NotificationEvaluation, error)
}

// AccountSync coordinates manual and scheduled account refreshes. A run is
// always serial, bounded by a timeout, and followed by the same verified-only
// activity scan used by the notification service.
type AccountSync struct {
	accounts     allAccountRefresher
	events       *EventHub
	logger       *slog.Logger
	interval     time.Duration
	timeout      time.Duration
	initialDelay func() time.Duration
	now          func() time.Time
	reset        chan struct{}

	runMu sync.Mutex
	mu    sync.RWMutex
	scan  verifiedActivityScanner
	state domain.AccountSyncStatus
}

func NewAccountSync(accounts *Accounts, events *EventHub, logger *slog.Logger) *AccountSync {
	return newAccountSync(accounts, events, logger, DefaultAccountSyncInterval, defaultAccountSyncTimeout, randomStartupDelay)
}

func newAccountSync(accounts allAccountRefresher, events *EventHub, logger *slog.Logger, interval, timeout time.Duration, initialDelay func() time.Duration) *AccountSync {
	if logger == nil {
		logger = slog.Default()
	}
	if interval <= 0 {
		interval = DefaultAccountSyncInterval
	}
	if timeout <= 0 {
		timeout = defaultAccountSyncTimeout
	}
	if initialDelay == nil {
		initialDelay = randomStartupDelay
	}
	return &AccountSync{
		accounts: accounts, events: events, logger: logger, interval: interval, timeout: timeout,
		initialDelay: initialDelay, now: time.Now, reset: make(chan struct{}, 1),
		state: domain.AccountSyncStatus{
			Enabled: true, IntervalSeconds: int64(interval / time.Second), TimeoutSeconds: int64(timeout / time.Second),
			LastResult: "never", ActivityMode: "verified_only",
		},
	}
}

// SetActivityScanner attaches the verified activity evaluator after all
// services have been constructed. Unknown providers remain excluded by the
// Accounts activity registry.
func (s *AccountSync) SetActivityScanner(scanner verifiedActivityScanner) {
	s.mu.Lock()
	s.scan = scanner
	s.mu.Unlock()
}

func (s *AccountSync) Status() domain.AccountSyncStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

// Refresh performs one coordinated refresh. The trigger is either "manual"
// or "scheduled"; manual runs postpone the next scheduled run to prevent a
// second burst immediately afterwards.
func (s *AccountSync) Refresh(parent context.Context, trigger string) error {
	if !s.runMu.TryLock() {
		return ErrAccountSyncBusy
	}
	defer s.runMu.Unlock()

	started := s.now().UTC().Truncate(time.Second)
	s.mu.Lock()
	s.state.Running = true
	s.state.CurrentTrigger = trigger
	s.state.CurrentRunStartedAt = timePointer(started)
	s.state.LastRunStartedAt = timePointer(started)
	s.state.LastError = ""
	s.state.LastActivityProbeError = ""
	s.mu.Unlock()

	ctx, cancel := context.WithTimeout(parent, s.timeout)
	defer cancel()
	refreshErr := s.accounts.RefreshAll(ctx)

	s.mu.RLock()
	scanner := s.scan
	s.mu.RUnlock()
	var activityErr error
	if scanner != nil && ctx.Err() == nil {
		_, activityErr = scanner.Evaluate(ctx)
		if errors.Is(activityErr, ErrNotificationEvaluationBusy) {
			// Another verified activity scan is already in progress. Treat it as
			// coverage rather than launching a speculative parallel scan.
			activityErr = nil
		}
	}

	completed := s.now().UTC().Truncate(time.Second)
	result := "success"
	lastError := ""
	if refreshErr != nil {
		result = "failed"
		lastError = "部分账号自动同步失败"
	}
	activityError := ""
	if activityErr != nil {
		if refreshErr == nil {
			result = "partial"
		}
		activityError = "部分已验证活动状态探测失败"
	}

	s.mu.Lock()
	s.state.Running = false
	s.state.CurrentTrigger = ""
	s.state.CurrentRunStartedAt = nil
	s.state.LastCompletedAt = timePointer(completed)
	s.state.LastResult = result
	s.state.LastError = lastError
	if scanner != nil {
		s.state.LastActivityProbeAt = timePointer(completed)
		s.state.LastActivityProbeError = activityError
	}
	if trigger != "scheduled" {
		next := completed.Add(s.interval)
		s.state.NextScheduledAt = timePointer(next)
	}
	s.mu.Unlock()

	if trigger != "scheduled" {
		select {
		case s.reset <- struct{}{}:
		default:
		}
	}
	if refreshErr != nil {
		s.logger.Warn("account sync completed with refresh errors", "trigger", trigger, "error", refreshErr)
		if s.events != nil {
			s.events.Publish(Event{Type: "refresh.failed", Message: "部分账号自动同步失败", Timestamp: completed})
		}
		return refreshErr
	}
	if activityErr != nil {
		s.logger.Warn("verified activity scan completed with errors after account sync", "trigger", trigger, "error", activityErr)
	}
	if s.events != nil {
		message := "全部账号额度已自动同步"
		if trigger == "manual" {
			message = "全部账号额度已刷新"
		}
		s.events.Publish(Event{Type: "refresh.completed", Message: message, Timestamp: completed})
	}
	return nil
}

// RunScheduler keeps account data fresh even when no browser is connected.
// The next timer is armed only after the previous run completes, so slow
// providers cannot create overlapping refresh waves.
func (s *AccountSync) RunScheduler(ctx context.Context) {
	delay := s.initialDelay()
	if delay < 0 {
		delay = 0
	}
	started := s.now().UTC()
	s.mu.Lock()
	s.state.StartupDelaySeconds = int64(delay / time.Second)
	s.state.NextScheduledAt = timePointer(started.Add(delay))
	s.mu.Unlock()

	timer := time.NewTimer(delay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			s.mu.Lock()
			s.state.NextScheduledAt = nil
			s.mu.Unlock()
			return
		case <-s.reset:
			next := s.now().UTC().Add(s.interval)
			s.schedule(timer, next)
		case <-timer.C:
			_ = s.Refresh(ctx, "scheduled")
			next := s.now().UTC().Add(s.interval)
			s.schedule(timer, next)
		}
	}
}

func (s *AccountSync) schedule(timer *time.Timer, next time.Time) {
	s.mu.Lock()
	s.state.NextScheduledAt = timePointer(next)
	s.mu.Unlock()
	delay := time.Until(next)
	if delay < 0 {
		delay = 0
	}
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(delay)
}

func randomStartupDelay() time.Duration {
	span := maximumStartupDelay - minimumStartupDelay
	if span <= 0 {
		return minimumStartupDelay
	}
	return minimumStartupDelay + time.Duration(rand.Int64N(int64(span)+1))
}

func timePointer(value time.Time) *time.Time {
	return &value
}
