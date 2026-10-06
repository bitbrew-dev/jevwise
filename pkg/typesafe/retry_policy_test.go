package typesafe

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"testing"
	"time"
)

func TestRetryPolicyDefaultsAndSelection(t *testing.T) {
	p := DefaultRetryPolicy()
	if p.MaxRetries != 2 || p.BackoffInitial != 500*time.Millisecond || p.BackoffMax != 5*time.Second || p.BackoffJitter != 0.25 || p.Budget != 30*time.Second || !p.RespectRetryAfter || p.Validate() != nil {
		t.Fatalf("defaults: %+v", p)
	}
	for status := 100; status < 650; status++ {
		want := status == 408 || status == 429 || (status >= 500 && status < 600)
		if p.Retryable(fmt.Errorf("wrapped: %w", &APIError{StatusCode: status})) != want {
			t.Errorf("status %d", status)
		}
	}
	for _, err := range []error{&ConnectionError{}, &TimeoutError{}} {
		if !p.Retryable(err) || (RetryPolicy{}).Retryable(err) {
			t.Errorf("built-in %T", err)
		}
	}
	p.HTTPStatuses[500] = false
	if !DefaultRetryPolicy().HTTPStatuses[500] {
		t.Fatal("default policy maps alias")
	}
	p.HTTPStatuses = map[int]bool{418: true}
	if !p.Retryable(&APIError{StatusCode: 418}) || p.Retryable(&APIError{StatusCode: 500}) || p.Retryable(errors.New("other")) || p.Retryable(nil) {
		t.Fatal("custom status selection")
	}
	called := 0
	p.Predicate = func(error) bool { called++; return true }
	if !p.Retryable(&APIError{StatusCode: 418}) || called != 0 || !p.Retryable(errors.New("custom")) || called != 1 {
		t.Fatal("predicate must be additive and short-circuit")
	}
}

func TestRetryPolicyValidationAndBackoff(t *testing.T) {
	for _, p := range []RetryPolicy{{MaxRetries: -1}, {BackoffInitial: -1}, {BackoffMax: -1}, {Budget: -1}, {BackoffJitter: -0.1}, {BackoffJitter: 1.1}, {BackoffJitter: math.NaN()}, {BackoffJitter: math.Inf(1)}} {
		if p.Validate() == nil {
			t.Errorf("accepted invalid policy %+v", p)
		}
	}
	p := DefaultRetryPolicy()
	for _, tc := range []struct {
		attempt int
		random  float64
		want    time.Duration
	}{{1, 0, 500 * time.Millisecond}, {1, 1, 375 * time.Millisecond}, {2, 0, time.Second}, {5, 0, 5 * time.Second}, {math.MaxInt, 1, 3750 * time.Millisecond}, {0, 0, 0}, {1, math.NaN(), 0}, {1, -1, 0}, {1, 2, 0}} {
		if got := p.Backoff(tc.attempt, tc.random); got != tc.want {
			t.Errorf("backoff %d/%v: %v, want %v", tc.attempt, tc.random, got, tc.want)
		}
	}
	p.BackoffJitter = 0
	if p.Backoff(1, 1) != p.BackoffInitial || (RetryPolicy{}).Backoff(1, 0) != 0 || (RetryPolicy{}).Validate() != nil {
		t.Fatal("zero policy or jitter")
	}
	p.BackoffInitial, p.BackoffMax = math.MaxInt64, math.MaxInt64
	p.BackoffJitter = 1
	if p.Backoff(math.MaxInt, 1) != 0 || p.Backoff(math.MaxInt, 0) != math.MaxInt64 {
		t.Fatal("overflow at maximum duration")
	}
	p.BackoffInitial, p.BackoffMax = time.Second, 0
	if p.Backoff(1, 0) != 0 {
		t.Fatal("zero max must disable backoff")
	}
}

func TestRetryAfterParsingAndDelay(t *testing.T) {
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		ms, seconds string
		want        time.Duration
		valid       bool
	}{
		{"250.5", "10", 250500 * time.Microsecond, true},
		{"bad", "1.5", 1500 * time.Millisecond, true}, {"-1", "2", 2 * time.Second, true},
		{"NaN", "2", 2 * time.Second, true}, {"Inf", "3", 3 * time.Second, true},
		{"bad", "-1", 0, false}, {"bad", "NaN", 0, false}, {"bad", "Inf", 0, false},
		{"1e100", "1e100", 0, false}, {"bad", "invalid", 0, false},
		{"bad", now.Add(10 * time.Second).Format(http.TimeFormat), 10 * time.Second, true},
		{"bad", now.Add(-time.Hour).Format(http.TimeFormat), 0, true},
		{"bad", "", 0, true}, {" 10 ", "5", 10 * time.Millisecond, true},
		{"0x1p0", "2", 2 * time.Second, true}, {"bad", "0x1p0", 0, false},
		{"+0X1p0", "2", 2 * time.Second, true}, {"-0x1p0", "2", 2 * time.Second, true},
	} {
		headers := http.Header{"retry-after-ms": {tc.ms}, "rEtRy-AfTeR": {tc.seconds}}
		if got, ok := ParseRetryAfter(headers, now); got != tc.want || ok != tc.valid {
			t.Errorf("headers %v: %v/%v, want %v/%v", headers, got, ok, tc.want, tc.valid)
		}
	}
	if _, ok := ParseRetryAfter(nil, now); ok {
		t.Fatal("absent headers are not an explicit zero delay")
	}
	p := DefaultRetryPolicy()
	err := fmt.Errorf("wrapped: %w", &APIError{Headers: http.Header{"Retry-After": {"60"}}})
	if p.Delay(err, 1, 0, now) != time.Minute {
		t.Fatal("server delay must not be capped by backoff max")
	}
	p.RespectRetryAfter = false
	if p.Delay(err, 1, 0, now) != 500*time.Millisecond || p.Delay(errors.New("other"), 1, 1, now) != 375*time.Millisecond {
		t.Fatal("fallback backoff")
	}
}
