package cli

import (
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/bitbrew-dev/jevwise/internal/config"
	"github.com/bitbrew-dev/jevwise/internal/service"
	"github.com/bitbrew-dev/jevwise/pkg/typesafe"
	"github.com/spf13/cobra"
)

// ServiceFactory creates a service and optional cleanup owned by one invocation.
// It must not log credentials or input. Returned failures are wrapped for safe output.
type ServiceFactory func(config.Config) (service.DecisionService, func(), error)

// decisionError retains inspectable causes without printing arbitrary service,
// factory, reader or writer text, which may contain credentials or input.
type decisionError struct {
	message string
	cause   error
}

func (e *decisionError) Error() string { return e.message }
func (e *decisionError) Unwrap() error { return e.cause }

func defaultServiceFactory(cfg config.Config) (service.DecisionService, func(), error) {
	if cfg.Provider != "jev" {
		svc, err := service.New(cfg.Provider, nil)
		return svc, nil, err
	}
	// An explicit empty CLI key must not fall back to an SDK environment key.
	if cfg.APIKey == "" {
		return nil, nil, errors.New("API key is required")
	}
	client, err := typesafe.NewClient(typesafe.ClientOptions{APIKey: cfg.APIKey,
		BaseURL: cfg.BaseURL, DefaultModel: cfg.Model, Timeout: cfg.Timeout})
	if err != nil {
		return nil, nil, err
	}
	svc, err := service.New("jev", func(ctx context.Context, request typesafe.SystemOneRequest) (*typesafe.SystemOneResponse, error) {
		return client.SystemOne(ctx, request)
	})
	return svc, func() { _ = client.Close() }, err
}

const maxPromptBytes = 1 << 20

func newDecide(factory ServiceFactory) *cobra.Command {
	var prompt string
	var options []string
	var stdin bool
	cmd := &cobra.Command{Use: "decide", Short: "Return Jev's probabilities for repeated options",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 0 {
				return errors.New("decide does not accept positional arguments")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if err := ctx.Err(); err != nil {
				return err
			}
			if cmd.Flags().Changed("prompt") == cmd.Flags().Changed("stdin") {
				return errors.New("select exactly one of --prompt or --stdin")
			}
			if cmd.Flags().Changed("stdin") && !stdin {
				return errors.New("--stdin must be enabled")
			}
			if stdin {
				// Arbitrary caller-owned blocking readers cannot be interrupted safely.
				// Check cancellation before/after reading; never close or spawn a goroutine.
				data, err := io.ReadAll(io.LimitReader(cmd.InOrStdin(), maxPromptBytes+1))
				if err != nil {
					return &decisionError{"cannot read stdin prompt", err}
				}
				if len(data) > maxPromptBytes {
					return errors.New("stdin prompt exceeds 1 MiB limit")
				}
				prompt = string(data)
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			request := service.Request{Prompt: prompt, Options: options}
			if err := request.Validate(); err != nil {
				return err
			}
			cfg, err := LoadConfig(cmd)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
			defer cancel()
			if err := ctx.Err(); err != nil {
				return err
			}
			svc, cleanup, err := factory(cfg)
			if cleanup != nil {
				defer cleanup()
			}
			if errors.Is(err, service.ErrNotImplemented) {
				return &decisionError{"provider is not implemented", err}
			}
			if err != nil || svc == nil {
				return &decisionError{"cannot initialize decision service", err}
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			response, err := svc.Decide(ctx, request)
			if err != nil {
				return &decisionError{"decision request failed", err}
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := json.NewEncoder(cmd.OutOrStdout()).Encode(response); err != nil {
				return &decisionError{"cannot write decision response", err}
			}
			return nil
		}}
	cmd.Flags().StringVar(&prompt, "prompt", "", "Prompt text (exclusive with --stdin)")
	cmd.Flags().BoolVar(&stdin, "stdin", false, "Read prompt from stdin (up to 1 MiB)")
	cmd.Flags().StringArrayVar(&options, "option", nil, "Option label; repeat at least twice")
	return cmd
}
