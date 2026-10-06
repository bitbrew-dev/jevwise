package typesafe

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// RetryPolicy configures retries after the initial attempt. Its zero value
// disables retries. Budget zero is unbounded; jitter zero disables randomness.
// Nil/empty HTTPStatuses retries no HTTP errors. Start with DefaultRetryPolicy
// to customize defaults. Clients must copy HTTPStatuses before storing a policy.
type RetryPolicy struct {
	MaxRetries        int
	BackoffInitial    time.Duration
	BackoffMax        time.Duration
	BackoffJitter     float64
	HTTPStatuses      map[int]bool
	RespectRetryAfter bool
	ConnectionErrors  bool
	TimeoutErrors     bool
	Predicate         func(error) bool
	Budget            time.Duration
}

// DefaultRetryPolicy returns independent maps with the pinned SDK defaults.
func DefaultRetryPolicy() RetryPolicy {
	statuses := map[int]bool{408: true, 429: true}
	for status := 500; status < 600; status++ {
		statuses[status] = true
	}
	return RetryPolicy{MaxRetries: 2, BackoffInitial: 500 * time.Millisecond,
		BackoffMax: 5 * time.Second, BackoffJitter: 0.25, HTTPStatuses: statuses,
		RespectRetryAfter: true, ConnectionErrors: true, TimeoutErrors: true,
		Budget: 30 * time.Second}
}

func (p RetryPolicy) Validate() error {
	if p.MaxRetries < 0 || p.BackoffInitial < 0 || p.BackoffMax < 0 || p.Budget < 0 {
		return fmt.Errorf("typesafe: retry counts and durations must not be negative")
	}
	if math.IsNaN(p.BackoffJitter) || p.BackoffJitter < 0 || p.BackoffJitter > 1 {
		return fmt.Errorf("typesafe: retry jitter must be between zero and one")
	}
	return nil
}

// Retryable applies built-in rules OR the additional predicate, short-circuiting
// when a built-in rule matches. The runner separately enforces count and budget.
func (p RetryPolicy) Retryable(err error) bool {
	if err == nil {
		return false
	}
	var timeout *TimeoutError
	var connection *ConnectionError
	var api *APIError
	builtin := false
	switch {
	case errors.As(err, &timeout) && timeout != nil:
		builtin = p.TimeoutErrors
	case errors.As(err, &connection) && connection != nil:
		builtin = p.ConnectionErrors
	case errors.As(err, &api) && api != nil:
		builtin = p.HTTPStatuses[api.StatusCode]
	}
	return builtin || (p.Predicate != nil && p.Predicate(err))
}

// Backoff uses attempt 1 for the first retry and a randomValue in [0,1].
// Exponential growth is capped before subtractive jitter. Invalid inputs return
// zero, and the loop stops at the cap without overflowing even for huge attempts.
func (p RetryPolicy) Backoff(attempt int, randomValue float64) time.Duration {
	if attempt < 1 || p.Validate() != nil || p.BackoffInitial == 0 || p.BackoffMax == 0 ||
		math.IsNaN(randomValue) || randomValue < 0 || randomValue > 1 {
		return 0
	}
	delay := min(p.BackoffInitial, p.BackoffMax)
	for i := 1; i < attempt && delay < p.BackoffMax; i++ {
		if delay > p.BackoffMax/2 {
			delay = p.BackoffMax
		} else {
			delay *= 2
		}
	}
	// Compute the subtraction rather than multiply delay by a near-one float,
	// which could round MaxInt64 up to an overflowing duration.
	reduction := float64(delay) * randomValue * p.BackoffJitter
	if reduction >= float64(delay) {
		return 0
	}
	return delay - time.Duration(reduction)
}

// Delay honors valid server delays without applying the exponential backoff cap.
func (p RetryPolicy) Delay(err error, attempt int, randomValue float64, now time.Time) time.Duration {
	var api *APIError
	if p.RespectRetryAfter && errors.As(err, &api) && api != nil {
		if delay, ok := ParseRetryAfter(api.Headers, now); ok {
			return delay
		}
	}
	return p.Backoff(attempt, randomValue)
}

// ParseRetryAfter prioritizes retry-after-ms, then numeric seconds or an HTTP
// date in Retry-After. Invalid/negative/nonfinite values fall through. Past dates
// mean zero. Values not representable as a time.Duration are rejected, not capped.
func ParseRetryAfter(headers http.Header, now time.Time) (time.Duration, bool) {
	for _, header := range []struct {
		name string
		unit time.Duration
	}{{"retry-after-ms", time.Millisecond}, {"Retry-After", time.Second}} {
		raw, present := retryHeader(headers, header.name)
		if !present {
			continue
		}
		raw = strings.TrimSpace(raw)
		if raw == "" {
			raw = "0"
		}
		if strings.HasPrefix(strings.TrimLeft(strings.ToLower(raw), "+-"), "0x") {
			continue // Python float accepts decimal strings, not hexadecimal floats.
		}
		value, err := strconv.ParseFloat(raw, 64)
		if err == nil {
			nanos := value * float64(header.unit)
			if value >= 0 && !math.IsNaN(nanos) && !math.IsInf(nanos, 0) && nanos < float64(math.MaxInt64) {
				return time.Duration(nanos), true
			}
		} else if header.name == "Retry-After" {
			date, err := http.ParseTime(raw)
			if err == nil && !date.After(now.Add(time.Duration(math.MaxInt64))) {
				return max(0, date.Sub(now)), true
			}
		}
	}
	return 0, false
}

func retryHeader(headers http.Header, name string) (string, bool) {
	if values, ok := headers[http.CanonicalHeaderKey(name)]; ok && len(values) > 0 {
		return values[0], true
	}
	for key, values := range headers {
		if strings.EqualFold(key, name) && len(values) > 0 {
			return values[0], true
		}
	}
	return "", false
}
