package typesafe

import (
	"context"
	"errors"
	"fmt"
	"math"
	"testing"
	"time"
)

type runnerClock struct {
	now    time.Time
	sleeps []time.Duration
}

func (c *runnerClock) hooks() retryHooks {
	return retryHooks{now: func() time.Time { return c.now }, random: func() float64 { return 0 },
		sleep: func(_ context.Context, delay time.Duration) error {
			c.sleeps = append(c.sleeps, delay)
			c.now = c.now.Add(delay)
			return nil
		}}
}

func TestRunRetrySelectionAndCounts(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want int
	}{
		{"rate limit", &APIError{StatusCode: 429}, 3},
		{"wrapped server", fmt.Errorf("wrapped: %w", &APIError{StatusCode: 503}), 3},
		{"bad request", &APIError{StatusCode: 400}, 1},
		{"connection", &ConnectionError{}, 3}, {"timeout", &TimeoutError{}, 3},
		{"validation", &ResponseValidationError{}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clock := &runnerClock{}
			calls := 0
			var last *RawResponse
			policy := DefaultRetryPolicy()
			if tc.want == 3 {
				policy.Predicate = func(error) bool { t.Fatal("built-in rule did not short-circuit predicate"); return false }
			}
			raw, err := runRetry(context.Background(), policy, func(_ context.Context, index int) (*RawResponse, error) {
				if index != calls {
					t.Fatalf("attempt index %d, want %d", index, calls)
				}
				calls++
				last = &RawResponse{StatusCode: calls}
				return last, tc.err
			}, clock.hooks())
			if calls != tc.want || raw != last || err != tc.err || len(clock.sleeps) != tc.want-1 {
				t.Fatalf("calls/raw/error/sleeps: %d/%v/%v/%v", calls, raw, err, clock.sleeps)
			}
			if tc.want == 3 && (clock.sleeps[0] != 500*time.Millisecond || clock.sleeps[1] != time.Second) {
				t.Fatal("incorrect exponential delays")
			}
		})
	}
	for _, maxRetries := range []int{0, math.MaxInt} {
		policy := DefaultRetryPolicy()
		policy.MaxRetries = maxRetries
		calls, predicates := 0, 0
		policy.Predicate = func(error) bool { predicates++; return true }
		clock := &runnerClock{}
		_, err := runRetry(context.Background(), policy, func(context.Context, int) (*RawResponse, error) {
			calls++
			if calls == 1 {
				return nil, &ResponseValidationError{}
			}
			return nil, nil
		}, clock.hooks())
		if maxRetries == 0 && (calls != 1 || predicates != 0 || err == nil) || maxRetries == math.MaxInt && (calls != 2 || predicates != 1 || err != nil) {
			t.Fatal("zero count, additive predicate, or MaxInt handling")
		}
	}
}

func TestRunRetryContextPrecedence(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		if deadline {
			ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		} else {
			cancel()
		}
		_, err := runRetry(ctx, DefaultRetryPolicy(), func(context.Context, int) (*RawResponse, error) {
			t.Fatal("expired context dispatched")
			return nil, nil
		}, retryHooks{})
		if err != ctx.Err() {
			t.Fatal("context sentinel not returned", err)
		}
		cancel()
	}
	for _, where := range []string{"attempt", "predicate true", "predicate false", "sleep"} {
		t.Run(where, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			policy := DefaultRetryPolicy()
			calls, predicates := 0, 0
			policy.Predicate = func(error) bool {
				predicates++
				if where != "sleep" {
					cancel()
				}
				return where != "predicate false"
			}
			clock := &runnerClock{}
			hooks := clock.hooks()
			hooks.sleep = func(context.Context, time.Duration) error { cancel(); return nil }
			last := &RawResponse{Body: []byte("last")}
			raw, err := runRetry(ctx, policy, func(context.Context, int) (*RawResponse, error) {
				calls++
				if where == "attempt" {
					cancel()
				}
				return last, &ResponseValidationError{}
			}, hooks)
			if err != context.Canceled || calls != 1 || raw != last || where == "attempt" && predicates != 0 {
				t.Fatalf("cancellation did not win: %v/%d/%d", err, calls, predicates)
			}
		})
	}
}

func TestRunRetryBudget(t *testing.T) {
	for _, tc := range []struct {
		name    string
		budget  time.Duration
		attempt time.Duration
		excess  time.Duration
		success bool
		calls   int
		sleeps  int
	}{
		{"exact delay boundary", 500 * time.Millisecond, 0, 0, false, 1, 0},
		{"attempt plus delay", 1500 * time.Millisecond, time.Second, 0, false, 1, 0},
		{"oversleep", 2 * time.Second, 0, 2 * time.Second, false, 1, 1},
		{"attempt budget exhausted", time.Second, 2 * time.Second, 0, false, 1, 0},
		{"ongoing successful attempt", time.Second, 2 * time.Second, 0, true, 1, 0},
		{"unbounded budget", 0, time.Hour, 0, false, 3, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clock := &runnerClock{}
			hooks := clock.hooks()
			sleep := hooks.sleep
			hooks.sleep = func(ctx context.Context, d time.Duration) error {
				clock.now = clock.now.Add(tc.excess)
				return sleep(ctx, d)
			}
			policy := DefaultRetryPolicy()
			policy.Budget = tc.budget
			calls := 0
			last := errors.New("last error")
			policy.Predicate = func(error) bool { return true }
			_, err := runRetry(context.Background(), policy, func(ctx context.Context, _ int) (*RawResponse, error) {
				calls++
				clock.now = clock.now.Add(tc.attempt)
				if ctx.Err() != nil {
					t.Fatal("retry budget canceled an ongoing attempt")
				}
				if tc.success {
					return nil, nil
				}
				return nil, last
			}, hooks)
			if calls != tc.calls || len(clock.sleeps) != tc.sleeps || tc.success && err != nil || !tc.success && err != last {
				t.Fatalf("budget behavior: %d/%v/%v", calls, clock.sleeps, err)
			}
		})
	}
}

func TestRunRetryInvalidInputsAndDefaultHooks(t *testing.T) {
	callback := func(context.Context, int) (*RawResponse, error) { t.Fatal("invalid input dispatched"); return nil, nil }
	if _, err := runRetry(context.Background(), RetryPolicy{MaxRetries: -1}, callback, retryHooks{}); err == nil {
		t.Fatal("invalid policy accepted")
	}
	if _, err := runRetry(context.Background(), RetryPolicy{}, nil, retryHooks{}); err == nil {
		t.Fatal("nil callback accepted")
	}
	if _, err := runRetry(nil, RetryPolicy{}, callback, retryHooks{}); err == nil {
		t.Fatal("nil context accepted")
	}
	expected := &RawResponse{}
	if raw, err := runRetry(context.Background(), RetryPolicy{}, func(context.Context, int) (*RawResponse, error) { return expected, nil }, retryHooks{}); err != nil || raw != expected {
		t.Fatal("missing hooks failed initial success", err)
	}
	if err := retrySleep(context.Background(), 0); err != nil {
		t.Fatal("zero delay sleep failed", err)
	}
}
