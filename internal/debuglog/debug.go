// Package debuglog provides opt-in, context-scoped diagnostics, never payload logs.
package debuglog

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/phuslu/log"
)

type key struct{}
type lockedWriter struct {
	mu     sync.Mutex
	writer io.Writer
}

func (w *lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.writer.Write(p)
}

// WithWriter enables debug output to writer, or disables inherited logging with nil.
// The writer must not be concurrently used outside this logger without its own lock.
// Logging is best-effort: write failures never replace an operation's result.
func WithWriter(ctx context.Context, writer io.Writer) context.Context {
	var logger *log.Logger
	if writer != nil {
		logger = &log.Logger{Level: log.DebugLevel, Writer: log.IOWriter{Writer: &lockedWriter{writer: writer}}}
	}
	return context.WithValue(ctx, key{}, logger)
}

func from(ctx context.Context) *log.Logger {
	if ctx == nil {
		return nil
	}
	logger, _ := ctx.Value(key{}).(*log.Logger)
	return logger
}

// Event and Count accept fixed event names only, never user-controlled strings.
func Event(ctx context.Context, name string) {
	if logger := from(ctx); logger != nil {
		logger.Debug().Msg(name)
	}
}

func Count(ctx context.Context, name string, value int) {
	if logger := from(ctx); logger != nil {
		logger.Debug().Int("value", value).Msg(name)
	}
}

// Trace logs timing and outcome, never error text, causes or returned data.
func Trace(ctx context.Context, operation string) func(error) {
	logger := from(ctx)
	if logger == nil {
		return func(error) {}
	}
	started := time.Now()
	logger.Debug().Str("operation", operation).Msg("started")
	return func(err error) {
		logger.Debug().Str("operation", operation).Bool("success", err == nil).
			Bool("canceled", errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)).
			Int64("elapsed_ms", time.Since(started).Milliseconds()).Msg("finished")
	}
}
