package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRefreshAccountIDsLimitsConcurrency(t *testing.T) {
	ids := make([]string, 12)
	for index := range ids {
		ids[index] = fmt.Sprintf("account-%02d", index)
	}
	started := make(chan struct{}, len(ids))
	release := make(chan struct{})
	var current atomic.Int32
	var maximum atomic.Int32
	var calls atomic.Int32

	done := make(chan error, 1)
	go func() {
		done <- refreshAccountIDs(context.Background(), ids, 4, time.Second, func(ctx context.Context, _ string) error {
			calls.Add(1)
			running := current.Add(1)
			defer current.Add(-1)
			for {
				observed := maximum.Load()
				if running <= observed || maximum.CompareAndSwap(observed, running) {
					break
				}
			}
			started <- struct{}{}
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()

	for range 4 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("four workers did not start")
		}
	}
	select {
	case <-started:
		t.Fatal("more than four accounts started before a worker was released")
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if calls.Load() != int32(len(ids)) {
		t.Fatalf("refresh calls = %d, want %d", calls.Load(), len(ids))
	}
	if maximum.Load() != 4 {
		t.Fatalf("maximum concurrency = %d, want 4", maximum.Load())
	}
}

func TestRefreshAccountIDsAppliesIndependentTimeouts(t *testing.T) {
	ids := []string{"account-a", "account-b", "account-c"}
	var calls atomic.Int32
	err := refreshAccountIDs(context.Background(), ids, 2, 20*time.Millisecond, func(ctx context.Context, _ string) error {
		calls.Add(1)
		<-ctx.Done()
		return ctx.Err()
	})
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want deadline exceeded", err)
	}
	if calls.Load() != int32(len(ids)) {
		t.Fatalf("refresh calls = %d, want %d", calls.Load(), len(ids))
	}
	var batchErr *accountRefreshBatchError
	if !errors.As(err, &batchErr) || len(batchErr.failures) != len(ids) {
		t.Fatalf("batch error = %#v, want %d failures", batchErr, len(ids))
	}
}

func TestRefreshAccountIDsPropagatesParentCancellationWithoutDroppingResults(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{}, 2)
	var calls atomic.Int32
	done := make(chan error, 1)
	go func() {
		done <- refreshAccountIDs(ctx, []string{"a", "b", "c", "d", "e"}, 2, time.Second, func(ctx context.Context, _ string) error {
			calls.Add(1)
			started <- struct{}{}
			<-ctx.Done()
			return ctx.Err()
		})
	}()
	for range 2 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("workers did not start")
		}
	}
	cancel()
	err := <-done
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context canceled", err)
	}
	var batchErr *accountRefreshBatchError
	if !errors.As(err, &batchErr) || len(batchErr.failures) != 5 {
		t.Fatalf("batch error = %#v, want all five accounts represented", batchErr)
	}
	if calls.Load() != 2 {
		t.Fatalf("refresh calls after cancellation = %d, want only two in-flight calls", calls.Load())
	}
}

func TestRefreshAccountIDsReportsFailuresInPoolOrder(t *testing.T) {
	err := refreshAccountIDs(context.Background(), []string{"first", "second"}, 2, time.Second, func(_ context.Context, id string) error {
		if id == "first" {
			time.Sleep(20 * time.Millisecond)
		}
		return fmt.Errorf("%s failed", id)
	})
	if err == nil {
		t.Fatal("expected refresh error")
	}
	message := err.Error()
	if strings.Index(message, "first") > strings.Index(message, "second") {
		t.Fatalf("failure order is nondeterministic: %s", message)
	}
}

func TestRefreshAccountIDsBoundsFailureDetails(t *testing.T) {
	ids := []string{"account-1", "account-2", "account-3", "account-4", "account-5", "account-6"}
	err := refreshAccountIDs(context.Background(), ids, 4, time.Second, func(_ context.Context, id string) error {
		return fmt.Errorf("%s failed", id)
	})
	if err == nil {
		t.Fatal("expected refresh error")
	}
	message := err.Error()
	if !strings.HasPrefix(message, "6 个真实账号刷新失败:") {
		t.Fatalf("failure count missing from summary: %s", message)
	}
	for _, id := range ids[:4] {
		if !strings.Contains(message, id) {
			t.Fatalf("summary omitted one of the first four failures: %s", message)
		}
	}
	if strings.Contains(message, "account-5") || strings.Contains(message, "account-6") {
		t.Fatalf("summary included more than four failure details: %s", message)
	}
	if !strings.Contains(message, "另有 2 个失败") {
		t.Fatalf("summary did not compact remaining failures: %s", message)
	}
	var batchErr *accountRefreshBatchError
	if !errors.As(err, &batchErr) || len(batchErr.Unwrap()) != len(ids) {
		t.Fatalf("programmatic error list lost failures: %#v", batchErr)
	}
}
