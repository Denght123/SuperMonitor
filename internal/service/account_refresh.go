package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	defaultAccountRefreshConcurrency = 4
	defaultPerAccountRefreshTimeout  = 45 * time.Second
	maxRefreshFailureDetails         = 4
)

type accountRefreshFailure struct {
	index     int
	accountID string
	err       error
}

// accountRefreshBatchError keeps every account failure available through
// errors.Is/errors.As while bounding the human-readable log line. Large
// account pools therefore retain complete programmatic error information
// without producing unbounded API responses or logs.
type accountRefreshBatchError struct {
	failures []accountRefreshFailure
}

func (e *accountRefreshBatchError) Error() string {
	if e == nil || len(e.failures) == 0 {
		return ""
	}
	detailCount := len(e.failures)
	if detailCount > maxRefreshFailureDetails {
		detailCount = maxRefreshFailureDetails
	}
	details := make([]string, 0, detailCount+1)
	for _, failure := range e.failures[:detailCount] {
		details = append(details, fmt.Sprintf("账号 %s: %s", failure.accountID, compactRefreshError(failure.err)))
	}
	if remaining := len(e.failures) - detailCount; remaining > 0 {
		details = append(details, fmt.Sprintf("另有 %d 个失败", remaining))
	}
	return fmt.Sprintf("%d 个真实账号刷新失败: %s", len(e.failures), strings.Join(details, "; "))
}

func (e *accountRefreshBatchError) Unwrap() []error {
	if e == nil {
		return nil
	}
	errs := make([]error, 0, len(e.failures))
	for _, failure := range e.failures {
		errs = append(errs, failure.err)
	}
	return errs
}

func compactRefreshError(err error) string {
	if err == nil {
		return "未知错误"
	}
	message := strings.Join(strings.Fields(err.Error()), " ")
	const maxRunes = 240
	runes := []rune(message)
	if len(runes) <= maxRunes {
		return message
	}
	return string(runes[:maxRunes]) + "…"
}

// refreshAccountIDs refreshes a bounded number of accounts in parallel. Each
// account receives a fresh timeout that starts only when a worker begins that
// account; the parent context still cancels the entire wave immediately.
// Results are sorted back into account-pool order so concurrent completion does
// not make API responses and logs nondeterministic.
func refreshAccountIDs(
	ctx context.Context,
	ids []string,
	concurrency int,
	perAccountTimeout time.Duration,
	refresh func(context.Context, string) error,
) error {
	if len(ids) == 0 {
		return nil
	}
	if refresh == nil {
		return errors.New("账号刷新函数不能为空")
	}
	if concurrency <= 0 {
		concurrency = 1
	}
	if concurrency > len(ids) {
		concurrency = len(ids)
	}
	if perAccountTimeout <= 0 {
		perAccountTimeout = defaultPerAccountRefreshTimeout
	}

	type job struct {
		index int
		id    string
	}
	jobs := make(chan job, len(ids))
	results := make(chan accountRefreshFailure, len(ids))
	for index, id := range ids {
		jobs <- job{index: index, id: id}
	}
	close(jobs)

	var workers sync.WaitGroup
	workers.Add(concurrency)
	for range concurrency {
		go func() {
			defer workers.Done()
			for account := range jobs {
				if err := ctx.Err(); err != nil {
					results <- accountRefreshFailure{index: account.index, accountID: account.id, err: err}
					continue
				}
				accountCtx, cancel := context.WithTimeout(ctx, perAccountTimeout)
				err := refresh(accountCtx, account.id)
				if err == nil {
					err = accountCtx.Err()
				}
				cancel()
				if err != nil {
					results <- accountRefreshFailure{index: account.index, accountID: account.id, err: err}
				}
			}
		}()
	}
	workers.Wait()
	close(results)

	failures := make([]accountRefreshFailure, 0)
	for result := range results {
		failures = append(failures, result)
	}
	if len(failures) == 0 {
		return nil
	}
	sort.Slice(failures, func(i, j int) bool { return failures[i].index < failures[j].index })
	return &accountRefreshBatchError{failures: failures}
}
