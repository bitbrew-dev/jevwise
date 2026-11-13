package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/bitbrew-dev/jevwise/internal/gemini"
)

func TestGeminiAdapterSendsPromptAsInputAndReportsConfiguredModel(t *testing.T) {
	tokens := 9
	result := gemini.Result{Choice: "B", Confidence: 0.6, Probabilities: map[string]float64{"A": 0.4, "B": 0.6}, InputTokens: &tokens}
	svc, err := NewGemini(func(_ context.Context, r gemini.ChoiceRequest) (gemini.Result, error) {
		if r.Input != " exact prompt\n" || r.Instructions != GeminiInstructions || strings.Join(r.Options, "|") != "A|B" {
			t.Error("unexpected Gemini request")
		}
		return result, nil
	}, "fixture-model")
	if err != nil {
		t.Fatal(err)
	}
	out, err := svc.Decide(context.Background(), Request{Prompt: " exact prompt\n", Options: []string{" A ", "B"}})
	if err != nil || out.Model != "fixture-model" || out.Choice != "B" || out.Confidence != 0.6 || out.Probabilities["A"] != 0.4 || *out.Usage.InputTokens != 9 || out.Usage.OutputTokens != nil {
		t.Fatal("unexpected decision", out, err)
	}
	result.Probabilities["A"], tokens = 0, 1
	if out.Probabilities["A"] != 0.4 || *out.Usage.InputTokens != 9 {
		t.Fatal("response shares client data")
	}
}

func TestGeminiAdapterValidationAndErrors(t *testing.T) {
	if _, err := NewGemini(nil, "m"); err == nil {
		t.Fatal("nil client accepted")
	}
	cause := errors.New("fixture failure")
	calls := 0
	svc, _ := NewGemini(func(context.Context, gemini.ChoiceRequest) (gemini.Result, error) {
		calls++
		return gemini.Result{}, cause
	}, "m")
	many := make([]string, gemini.MaxOptions+1)
	for i := range many {
		many[i] = strings.Repeat("x", i+1)
	}
	for _, r := range []Request{{Prompt: " ", Options: []string{"A", "B"}}, {Prompt: "p", Options: []string{"A", "A"}}, {Prompt: "p", Options: many}} {
		if _, err := svc.Decide(context.Background(), r); err == nil || calls != 0 {
			t.Fatal("invalid request reached client")
		}
	}
	if _, err := svc.Decide(context.Background(), Request{Prompt: "p", Options: []string{"A", "B"}}); !errors.Is(err, cause) {
		t.Fatal("client error lost", err)
	}
	if _, err := New("gemini", nil); err == nil {
		t.Fatal("gemini selected without its client")
	}
}
