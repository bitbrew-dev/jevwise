package service

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	typesafe "github.com/bitbrew-dev/jevwise/pkg/typesafe"
)

func TestJevMappingAndIndependentServerMetadata(t *testing.T) {
	tokens := 17
	answer := typesafe.ChoiceAnswer{Choice: "yes", Confidence: .76,
		Probabilities: map[string]float64{"yes": .76, "no": .21, "future": .02}}
	upstream := &typesafe.SystemOneResponse{Model: "server-model",
		Answers: map[string]typesafe.Answer{"decision": answer}, Usage: typesafe.Usage{InputTokens: &tokens}}
	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "marker")
	svc, err := New("jev", func(got context.Context, req typesafe.SystemOneRequest) (*typesafe.SystemOneResponse, error) {
		if got != ctx || req.State != " prompt \n" || req.Model != "" || len(req.Questions) != 1 {
			t.Fatalf("request/context mapping failed: %#v", req)
		}
		q, ok := req.Questions["decision"].(typesafe.Choice)
		if !ok || q.Instructions != nil || !reflect.DeepEqual(q.Criteria, map[string]typesafe.JSONContent{"yes": nil, "no": nil}) {
			t.Fatalf("choice mapping failed: %#v", q)
		}
		if _, err := json.Marshal(req); err != nil {
			t.Fatalf("request cannot be encoded: %v", err)
		}
		return upstream, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := svc.Decide(ctx, Request{Prompt: " prompt \n", Options: []string{" yes ", "no"}})
	if err != nil || out.Model != upstream.Model || out.Choice != answer.Choice || out.Confidence != answer.Confidence || !reflect.DeepEqual(out.Probabilities, answer.Probabilities) {
		t.Fatalf("response mapping failed: %#v, %v", out, err)
	}
	out.Probabilities["yes"] = 0
	*out.Usage.InputTokens = 0
	if answer.Probabilities["yes"] != .76 || tokens != 17 || out.Usage.OutputTokens != nil {
		t.Fatal("response shares mutable metadata or manufactured a token count")
	}
}

func TestProviderSelectionNeverInvokesDeferredProviders(t *testing.T) {
	send := func(context.Context, typesafe.SystemOneRequest) (*typesafe.SystemOneResponse, error) {
		t.Fatal("placeholder or unknown provider invoked client")
		return nil, nil
	}
	for _, provider := range []string{"codex", "claude", "unknown-secret"} {
		for _, callback := range []SystemOneFunc{nil, send} {
			svc, err := New(provider, callback)
			if svc != nil || err == nil || strings.Contains(err.Error(), "unknown-secret") {
				t.Fatalf("provider failure = %v, %v", svc, err)
			}
			if provider != "unknown-secret" && !errors.Is(err, ErrNotImplemented) {
				t.Fatalf("missing placeholder sentinel: %v", err)
			}
		}
	}
	if _, err := New("jev", nil); err == nil {
		t.Fatal("nil Jev client accepted")
	}
}

func TestValidationDoesNotSendInvalidInput(t *testing.T) {
	svc, _ := New("jev", func(context.Context, typesafe.SystemOneRequest) (*typesafe.SystemOneResponse, error) {
		t.Fatal("invalid request sent")
		return nil, nil
	})
	for _, req := range []Request{
		{Prompt: " \n", Options: []string{"a", "b"}},
		{Prompt: "secret", Options: nil},
		{Prompt: "secret", Options: []string{"a"}},
		{Prompt: "secret", Options: []string{"a", " \n"}},
		{Prompt: "secret", Options: []string{"a", " a "}},
	} {
		if _, err := svc.Decide(context.Background(), req); err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("validation error = %v", err)
		}
	}
}

func TestClientErrorsAndCancellationPassThrough(t *testing.T) {
	for _, want := range []error{errors.New("client error"), context.Canceled, context.DeadlineExceeded} {
		svc, _ := New("jev", func(context.Context, typesafe.SystemOneRequest) (*typesafe.SystemOneResponse, error) {
			return nil, want
		})
		if _, err := svc.Decide(context.Background(), Request{"prompt", []string{"a", "b"}}); err != want {
			t.Fatalf("error was replaced: got %v, want %v", err, want)
		}
	}
}

func TestMissingAndUnexpectedAnswersFail(t *testing.T) {
	for _, result := range []*typesafe.SystemOneResponse{nil, {},
		{Answers: map[string]typesafe.Answer{"decision": typesafe.NoulAnswer{Noul: .5}}},
		{Answers: map[string]typesafe.Answer{"decision": typesafe.ChoiceAnswer{}}},
	} {
		svc, _ := New("jev", func(context.Context, typesafe.SystemOneRequest) (*typesafe.SystemOneResponse, error) {
			return result, nil
		})
		if _, err := svc.Decide(context.Background(), Request{"prompt", []string{"a", "b"}}); err == nil {
			t.Fatal("missing/wrong answer accepted")
		}
	}
}

func TestUsageOmittedWhenNoCounts(t *testing.T) {
	svc, _ := New("jev", func(context.Context, typesafe.SystemOneRequest) (*typesafe.SystemOneResponse, error) {
		return &typesafe.SystemOneResponse{Answers: map[string]typesafe.Answer{"decision": typesafe.ChoiceAnswer{
			Choice: "a", Probabilities: map[string]float64{"a": 1, "b": 0}}}}, nil
	})
	out, err := svc.Decide(context.Background(), Request{"prompt", []string{"a", "b"}})
	data, marshalErr := json.Marshal(out)
	if err != nil || marshalErr != nil || strings.Contains(string(data), "usage") {
		t.Fatalf("optional usage incorrectly encoded: %s, %v, %v", data, err, marshalErr)
	}
}
