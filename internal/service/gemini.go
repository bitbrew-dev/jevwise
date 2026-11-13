package service

import (
	"context"
	"errors"
	"strings"

	"github.com/bitbrew-dev/jevwise/internal/gemini"
	typesafe "github.com/bitbrew-dev/jevwise/pkg/typesafe"
)

// GeminiInstructions is the single instruction sent with every Gemini choice; the
// prompt is the shared input, so it is billed once.
const GeminiInstructions = "Choose the option that best answers or fits the input."

// ChooseFunc permits an injected Gemini client.
type ChooseFunc func(context.Context, gemini.ChoiceRequest) (gemini.Result, error)

type geminiService struct {
	choose ChooseFunc
	model  string
}

// NewGemini adapts Gemini Decisions. Responses carry no model, so the configured
// model is reported instead.
func NewGemini(choose ChooseFunc, model string) (DecisionService, error) {
	if choose == nil {
		return nil, errors.New("gemini requires a Gemini Decisions client")
	}
	return geminiService{choose, model}, nil
}

func (s geminiService) Decide(ctx context.Context, r Request) (Response, error) {
	if err := r.Validate(); err != nil {
		return Response{}, err
	}
	if len(r.Options) > gemini.MaxOptions {
		return Response{}, errors.New("gemini accepts at most 255 options")
	}
	labels := make([]string, len(r.Options))
	for i, option := range r.Options {
		labels[i] = strings.TrimSpace(option)
	}
	result, err := s.choose(ctx, gemini.ChoiceRequest{Input: r.Prompt, Instructions: GeminiInstructions, Options: labels})
	if err != nil {
		return Response{}, err
	}
	out := Response{Model: s.model, Choice: result.Choice, Confidence: result.Confidence,
		Probabilities: make(map[string]float64, len(result.Probabilities))}
	for label, probability := range result.Probabilities {
		out.Probabilities[label] = probability
	}
	if result.InputTokens != nil || result.OutputTokens != nil {
		out.Usage = &typesafe.Usage{InputTokens: copyInt(result.InputTokens), OutputTokens: copyInt(result.OutputTokens)}
	}
	return out, nil
}

func copyInt(n *int) *int {
	if n == nil {
		return nil
	}
	value := *n
	return &value
}
