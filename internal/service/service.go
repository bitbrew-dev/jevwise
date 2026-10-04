// Package service adapts Jev decisions and reserves deferred external providers.
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	typesafe "github.com/benbenbang/ts-jev-go-sdk/pkg/typesafe"
)

// ErrNotImplemented identifies a reserved provider with no executable integration.
var ErrNotImplemented = errors.New("provider is not implemented")

// Request contains a prompt and distinct option labels. Labels are trimmed;
// prompt content is preserved verbatim when sent as System One state.
type Request struct {
	Prompt  string
	Options []string
}

// Validate checks input without exposing prompt or option content in errors.
func (r Request) Validate() error {
	if strings.TrimSpace(r.Prompt) == "" {
		return errors.New("prompt must not be blank")
	}
	if len(r.Options) < 2 {
		return errors.New("at least two options are required")
	}
	seen := make(map[string]bool, len(r.Options))
	for _, option := range r.Options {
		label := strings.TrimSpace(option)
		if label == "" {
			return errors.New("options must not be blank")
		}
		if seen[label] {
			return errors.New("options must be distinct")
		}
		seen[label] = true
	}
	return nil
}

// Response contains only the service's distribution and result metadata.
// No probabilities are inferred, normalized, or manufactured locally.
type Response struct {
	Model         string             `json:"model"`
	Choice        string             `json:"choice"`
	Confidence    float64            `json:"confidence"`
	Probabilities map[string]float64 `json:"probabilities"`
	Usage         *typesafe.Usage    `json:"usage,omitempty"`
}

type DecisionService interface {
	Decide(context.Context, Request) (Response, error)
}

// SystemOneFunc permits an injected client without depending on per-call options.
type SystemOneFunc func(context.Context, typesafe.SystemOneRequest) (*typesafe.SystemOneResponse, error)

type jev struct{ send SystemOneFunc }

// New selects a provider. Placeholders fail before checking clients/credentials
// and never invoke callbacks or start subprocesses.
func New(provider string, send SystemOneFunc) (DecisionService, error) {
	switch provider {
	case "jev":
		if send == nil {
			return nil, errors.New("jev requires a System One client")
		}
		return jev{send}, nil
	case "codex", "claude":
		return nil, fmt.Errorf("%s: %w", provider, ErrNotImplemented)
	default:
		return nil, errors.New("unknown decision provider")
	}
}

func (s jev) Decide(ctx context.Context, r Request) (Response, error) {
	if err := r.Validate(); err != nil {
		return Response{}, err
	}
	criteria := make(map[string]typesafe.JSONContent, len(r.Options))
	for _, option := range r.Options {
		criteria[strings.TrimSpace(option)] = nil
	}
	result, err := s.send(ctx, typesafe.SystemOneRequest{
		State: r.Prompt,
		Questions: map[string]typesafe.Question{
			"decision": typesafe.Choice{Criteria: criteria},
		},
	})
	if err != nil {
		return Response{}, err
	}
	if result == nil {
		return Response{}, errors.New("jev returned no response")
	}
	answer, ok := result.Answers["decision"].(typesafe.ChoiceAnswer)
	if !ok || answer.Probabilities == nil {
		return Response{}, errors.New("jev response is missing a decision choice answer")
	}
	out := Response{Model: result.Model, Choice: answer.Choice, Confidence: answer.Confidence,
		Probabilities: make(map[string]float64, len(answer.Probabilities))}
	for label, probability := range answer.Probabilities {
		out.Probabilities[label] = probability
	}
	if result.Usage.InputTokens != nil || result.Usage.OutputTokens != nil {
		out.Usage = &typesafe.Usage{}
		if n := result.Usage.InputTokens; n != nil {
			value := *n
			out.Usage.InputTokens = &value
		}
		if n := result.Usage.OutputTokens; n != nil {
			value := *n
			out.Usage.OutputTokens = &value
		}
	}
	return out, nil
}
