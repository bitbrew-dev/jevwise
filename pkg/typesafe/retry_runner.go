package typesafe

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"

	"github.com/bitbrew-dev/jevwise/internal/debuglog"
)

// retryHooks are per-call seams. Missing hooks use production defaults; no
// process-wide mutable clock, sleeper, or random source is shared by tests.
type retryHooks struct {
	now    func() time.Time
	sleep  func(context.Context, time.Duration) error
	random func() float64
}

func retrySleep(ctx context.Context, delay time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return ctx.Err()
	}
}

// runRetry repeats an entire HTTP-plus-decode attempt. The retry budget only
// prevents starting another attempt: it does not cancel in-flight work. Context
// expiration returns its sentinel so the client can attach safe HTTP metadata.
func runRetry(ctx context.Context, policy RetryPolicy, attempt func(context.Context, int) (*RawResponse, error), hooks retryHooks) (_ *RawResponse, resultErr error) {
	finish := debuglog.Trace(ctx, "api.retry")
	defer func() { finish(resultErr) }()
	if ctx == nil {
		return nil, errors.New("typesafe: retry context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	if attempt == nil {
		return nil, errors.New("typesafe: retry attempt is required")
	}
	if hooks.now == nil {
		hooks.now = time.Now
	}
	if hooks.sleep == nil {
		hooks.sleep = retrySleep
	}
	if hooks.random == nil {
		hooks.random = rand.Float64
	}
	started := hooks.now()
	var raw *RawResponse
	var err error
	for index := 0; ; index++ {
		if contextErr := ctx.Err(); contextErr != nil {
			return raw, contextErr
		}
		if index > 0 && policy.Budget > 0 && hooks.now().Sub(started) >= policy.Budget {
			return raw, err
		}
		debuglog.Count(ctx, "api.attempt", index)
		raw, err = attempt(ctx, index)
		if contextErr := ctx.Err(); contextErr != nil {
			return raw, contextErr
		}
		if err == nil || index >= policy.MaxRetries {
			return raw, err
		}
		retryable := policy.Retryable(err)
		if contextErr := ctx.Err(); contextErr != nil {
			return raw, contextErr
		}
		if !retryable {
			return raw, err
		}
		now := hooks.now()
		remaining := policy.Budget - now.Sub(started)
		if policy.Budget > 0 && remaining <= 0 {
			return raw, err
		}
		// index < MaxRetries here, so index+1 cannot overflow even at MaxInt.
		delay := policy.Delay(err, index+1, hooks.random(), now)
		if policy.Budget > 0 && delay >= remaining {
			return raw, err
		}
		debuglog.Event(ctx, "api.retry.wait")
		sleepErr := hooks.sleep(ctx, delay)
		if contextErr := ctx.Err(); contextErr != nil {
			return raw, contextErr
		}
		if sleepErr != nil {
			return raw, sleepErr
		}
	}
}
