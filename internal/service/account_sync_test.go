package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"
)

type accountRefresherFunc func(context.Context) error

func (fn accountRefresherFunc) RefreshAll(ctx context.Context) error {
	return fn(ctx)
}

type activityScannerFunc func(context.Context) (NotificationEvaluation, error)

func (fn activityScannerFunc) Evaluate(ctx context.Context) (NotificationEvaluation, error) {
	return fn(ctx)
}

func TestAccountSyncRefreshIsNonReentrantAndProbesVerifiedActivities(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	refresher := accountRefresherFunc(func(ctx context.Context) error {
		close(started)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-release:
			return nil
		}
	})
	hub := NewEventHub()
	events, unsubscribe := hub.Subscribe()
	defer unsubscribe()
	syncer := newAccountSync(refresher, hub, testSyncLogger(), time.Minute, time.Second, func() time.Duration { return 0 })
	var probes atomic.Int32
	syncer.SetActivityScanner(activityScannerFunc(func(context.Context) (NotificationEvaluation, error) {
		probes.Add(1)
		return NotificationEvaluation{}, nil
	}))

	done := make(chan error, 1)
	go func() { done <- syncer.Refresh(context.Background(), "manual") }()
	<-started
	status := syncer.Status()
	if !status.Running || status.CurrentTrigger != "manual" || status.CurrentRunStartedAt == nil {
		t.Fatalf("unexpected running status: %#v", status)
	}
	if err := syncer.Refresh(context.Background(), "manual"); !errors.Is(err, ErrAccountSyncBusy) {
		t.Fatalf("expected a non-reentrant refresh error, got %v", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if probes.Load() != 1 {
		t.Fatalf("expected one verified activity probe, got %d", probes.Load())
	}
	status = syncer.Status()
	if status.Running || status.LastResult != "success" || status.LastCompletedAt == nil || status.NextScheduledAt == nil {
		t.Fatalf("unexpected completed status: %#v", status)
	}
	if status.ActivityMode != "verified_only" || status.LastActivityProbeAt == nil {
		t.Fatalf("activity policy was not exposed: %#v", status)
	}
	select {
	case payload := <-events:
		if len(payload) == 0 {
			t.Fatal("refresh event was empty")
		}
	case <-time.After(time.Second):
		t.Fatal("successful refresh did not publish an event")
	}
}

func TestAccountSyncBoundsProviderRefreshWithTimeout(t *testing.T) {
	refresher := accountRefresherFunc(func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})
	syncer := newAccountSync(refresher, nil, testSyncLogger(), time.Minute, 20*time.Millisecond, func() time.Duration { return 0 })
	var probes atomic.Int32
	syncer.SetActivityScanner(activityScannerFunc(func(context.Context) (NotificationEvaluation, error) {
		probes.Add(1)
		return NotificationEvaluation{}, nil
	}))

	started := time.Now()
	err := syncer.Refresh(context.Background(), "scheduled")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("refresh timeout was not enforced: %s", elapsed)
	}
	if probes.Load() != 0 {
		t.Fatal("activity probe must not start after the refresh context expires")
	}
	status := syncer.Status()
	if status.Running || status.LastResult != "failed" || status.LastError == "" {
		t.Fatalf("unexpected timeout status: %#v", status)
	}
}

func TestAccountSyncSchedulerRunsSerially(t *testing.T) {
	var running atomic.Int32
	var maximum atomic.Int32
	var runs atomic.Int32
	refresher := accountRefresherFunc(func(context.Context) error {
		current := running.Add(1)
		for {
			observed := maximum.Load()
			if current <= observed || maximum.CompareAndSwap(observed, current) {
				break
			}
		}
		runs.Add(1)
		time.Sleep(12 * time.Millisecond)
		running.Add(-1)
		return nil
	})
	syncer := newAccountSync(refresher, nil, testSyncLogger(), 5*time.Millisecond, time.Second, func() time.Duration { return time.Millisecond })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		syncer.RunScheduler(ctx)
		close(done)
	}()
	deadline := time.After(time.Second)
	for runs.Load() < 3 {
		select {
		case <-deadline:
			cancel()
			<-done
			t.Fatalf("scheduler only completed %d runs", runs.Load())
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	<-done
	if maximum.Load() != 1 {
		t.Fatalf("scheduled refreshes overlapped; maximum concurrency was %d", maximum.Load())
	}
	if status := syncer.Status(); status.NextScheduledAt != nil {
		t.Fatalf("shutdown scheduler retained a next run: %#v", status)
	}
}

func TestDefaultAccountSyncPolicyIsLowFrequency(t *testing.T) {
	syncer := NewAccountSync(nil, nil, testSyncLogger())
	status := syncer.Status()
	if status.IntervalSeconds != int64((15*time.Minute)/time.Second) {
		t.Fatalf("unexpected default sync interval: %d seconds", status.IntervalSeconds)
	}
	if status.TimeoutSeconds != int64((10*time.Minute)/time.Second) || status.ActivityMode != "verified_only" {
		t.Fatalf("unexpected default sync policy: %#v", status)
	}
}

func testSyncLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
