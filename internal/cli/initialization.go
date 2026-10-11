package cli

import (
	"context"
	"errors"

	"github.com/bitbrew-dev/jevwise/internal/debuglog"
	"github.com/bitbrew-dev/jevwise/internal/service"
)

// Only trusted initialization failures get specific messages. Never expose factory errors.
type initializationError struct {
	kind  string
	cause error
}

func (e *initializationError) Error() string {
	switch e.kind {
	case "api_key_missing":
		return "API key is required"
	case "api_key_invalid":
		return "API key must contain printable ASCII without whitespace; check the selected credential"
	default:
		return "cannot initialize Jev client: check base URL, model and timeout"
	}
}
func (e *initializationError) Unwrap() error { return e.cause }

func initializationFailure(ctx context.Context, fallback string, err error) error {
	event, message := "service.initialize.factory_failed", fallback
	var known *initializationError
	switch {
	case errors.Is(err, service.ErrNotImplemented):
		event, message = "service.initialize.provider_unimplemented", "provider is not implemented"
	case errors.As(err, &known) && known != nil:
		message = known.Error()
		// Do not put an unchecked kind, even from an injected factory, in logs.
		switch known.kind {
		case "api_key_missing":
			event = "service.initialize.api_key_missing"
			message += ": check the selected TOML api_key and its environment references, or set an upstream API-key environment variable (see docs/configuration.md)"
		case "api_key_invalid":
			event = "service.initialize.api_key_invalid"
		default:
			event = "service.initialize.client_configuration"
		}
	case err == nil:
		event = "service.initialize.nil_service"
	case errors.Is(err, context.Canceled):
		event = "service.initialize.canceled"
	case errors.Is(err, context.DeadlineExceeded):
		event = "service.initialize.timeout"
	}
	debuglog.Event(ctx, event)
	return &decisionError{message, err}
}
