package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bitbrew-dev/jevwise/internal/config"
	"github.com/bitbrew-dev/jevwise/internal/service"
)

type decideFunc func(context.Context, service.Request) (service.Response, error)

func (f decideFunc) Decide(ctx context.Context, req service.Request) (service.Response, error) {
	return f(ctx, req)
}

func decisionEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, key := range []string{"TS_JEV_API_KEY", "TS_JEV_MODEL", "TS_JEV_PROVIDER", "TS_JEV_BASE_URL", "TS_JEV_TIMEOUT", "TYPESAFE_API_KEY", "TYPESAFE_DEFAULT_MODEL", "TYPESAFE_BASE_URL"} {
		t.Setenv(key, "")
	}
}

func TestDecideFakeServiceAndConfiguration(t *testing.T) {
	decisionEnvironment(t)
	t.Setenv("TS_JEV_MODEL", "env-model")
	for _, useStdin := range []bool{false, true} {
		cleaned := false
		factory := func(cfg config.Config) (service.DecisionService, func(), error) {
			if cfg.Model != "flag-model" || cfg.Timeout != time.Second {
				t.Error("changed flags did not override environment")
			}
			return decideFunc(func(ctx context.Context, req service.Request) (service.Response, error) {
				deadline, ok := ctx.Deadline()
				if !ok || time.Until(deadline) > time.Second || req.Prompt != " exact\n" || len(req.Options) != 2 || req.Options[0] != "A,B" {
					t.Error("input content or operation deadline changed")
				}
				return service.Response{Probabilities: map[string]float64{"A,B": 0.2, "C": 0.8}}, nil
			}), func() { cleaned = true }, nil
		}
		cmd := NewRootWithFactory(factory)
		cmd.SetIn(strings.NewReader(" exact\n"))
		args := []string{"decide", "--option", "A,B", "--option", "C", "--model", "flag-model", "--timeout", "1s"}
		if useStdin {
			args = append(args, "--stdin")
		} else {
			args = append(args, "--prompt", " exact\n")
		}
		out, stderr, err := execute(cmd, args...)
		var result service.Response
		if err != nil || stderr != "" || !cleaned || !strings.HasSuffix(out, "\n") || json.Unmarshal([]byte(out), &result) != nil || result.Probabilities["A,B"] != 0.2 {
			t.Fatalf("unexpected output/cleanup: %q %q %v", out, stderr, err)
		}
	}
}

func TestDecideInvalidInputAndHelpAvoidFactory(t *testing.T) {
	decisionEnvironment(t)
	factory := func(config.Config) (service.DecisionService, func(), error) {
		t.Fatal("invalid input or help created service")
		return nil, nil, nil
	}
	for _, args := range [][]string{
		{"decide"}, {"decide", "private-input"}, {"decide", "--bad=private-input"},
		{"decide", "--prompt", "secret", "--stdin"}, {"decide", "--stdin=false"},
		{"decide", "--prompt", " "}, {"decide", "--prompt", "secret", "--option", "one"},
		{"decide", "--prompt", "secret", "--option", " A ", "--option", "A"},
		{"decide", "--prompt", "secret", "--option", "A", "--option", " "},
	} {
		out, stderr, err := execute(NewRootWithFactory(factory), args...)
		if err == nil || out != "" || stderr != "" || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "private-input") {
			t.Fatal("invalid input leaked or succeeded", err)
		}
	}
	out, _, err := execute(NewRootWithFactory(factory), "decide", "--help", "--config", "/missing")
	if err != nil || !strings.Contains(out, "--stdin") {
		t.Fatal("decision help failed", err)
	}
}

type secretIO struct{}

func (secretIO) Read([]byte) (int, error)  { return 0, errors.New("private-content") }
func (secretIO) Write([]byte) (int, error) { return 0, errors.New("private-content") }

type cancelInput struct{ cancel context.CancelFunc }

func (r cancelInput) Read(b []byte) (int, error) { r.cancel(); return copy(b, "prompt"), io.EOF }

func TestDecideFailureSafetyAndCancellation(t *testing.T) {
	decisionEnvironment(t)
	sentinel := errors.New("private-content")
	args := []string{"decide", "--stdin", "--option", "A", "--option", "B"}
	for _, reader := range []io.Reader{secretIO{}, strings.NewReader(strings.Repeat("x", maxPromptBytes+1))} {
		cmd := NewRootWithFactory(func(config.Config) (service.DecisionService, func(), error) {
			t.Fatal("bad input dispatched")
			return nil, nil, nil
		})
		cmd.SetIn(reader)
		if _, _, err := execute(cmd, args...); err == nil || strings.Contains(err.Error(), "private-content") {
			t.Fatal("stdin failure was not safe", err)
		}
	}
	for _, factoryFailure := range []bool{true, false} {
		cleaned := false
		cmd := NewRootWithFactory(func(config.Config) (service.DecisionService, func(), error) {
			if factoryFailure {
				return nil, func() { cleaned = true }, sentinel
			}
			return decideFunc(func(context.Context, service.Request) (service.Response, error) { return service.Response{}, sentinel }), func() { cleaned = true }, nil
		})
		cmd.SetIn(strings.NewReader("prompt"))
		out, _, err := execute(cmd, args...)
		if !errors.Is(err, sentinel) || strings.Contains(err.Error(), "private-content") || out != "" || !cleaned {
			t.Fatal("failure cause/cleanup unsafe", err)
		}
	}
	for _, duringRead := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		cmd := NewRootWithFactory(func(config.Config) (service.DecisionService, func(), error) {
			t.Fatal("cancelled input dispatched")
			return nil, nil, nil
		})
		cmd.SetArgs(args)
		if duringRead {
			cmd.SetIn(cancelInput{cancel})
		} else {
			cancel()
			cmd.SetIn(secretIO{})
		}
		if err := cmd.ExecuteContext(ctx); !errors.Is(err, context.Canceled) {
			t.Fatal("cancellation lost", err)
		}
		cancel()
	}
	cmd := NewRootWithFactory(func(config.Config) (service.DecisionService, func(), error) {
		return decideFunc(func(context.Context, service.Request) (service.Response, error) { return service.Response{}, nil }), nil, nil
	})
	cmd.SetIn(strings.NewReader("prompt"))
	cmd.SetOut(secretIO{})
	cmd.SetArgs(args)
	if err := cmd.Execute(); err == nil || strings.Contains(err.Error(), "private-content") {
		t.Fatal("writer failure unsafe", err)
	}
}

func TestDecidePlaceholdersAndExplicitEmptyKey(t *testing.T) {
	decisionEnvironment(t)
	for _, provider := range []string{"codex", "claude"} {
		_, _, err := execute(NewRoot(), "decide", "--prompt", "prompt", "--option", "A", "--option", "B", "--provider", provider)
		if !errors.Is(err, service.ErrNotImplemented) || !strings.Contains(err.Error(), "not implemented") {
			t.Fatal("placeholder not clear", err)
		}
	}
	t.Setenv("TYPESAFE_API_KEY", "private-key")
	_, _, err := execute(NewRoot(), "decide", "--prompt", "prompt", "--option", "A", "--option", "B", "--api-key", "")
	if err == nil || strings.Contains(err.Error(), "private-key") {
		t.Fatal("empty CLI key fell back", err)
	}
}

func TestDecideLocalHTTP(t *testing.T) {
	decisionEnvironment(t)
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			State, Model string
			Questions    map[string]struct {
				Type     string
				Criteria map[string]any
			}
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil || request.State != "prompt" || request.Model != "chosen" || request.Questions["decision"].Type != "choice" || len(request.Questions["decision"].Criteria) != 2 || r.Header.Get("Authorization") != "Bearer local-key" || r.Method != "POST" || r.URL.Path != "/v1/systemone" {
			t.Error("wrong Jev request")
		}
		attempt := attempts.Add(1)
		if attempt == 1 {
			if r.Header.Get("X-TypeSafe-Retry-Count") != "" {
				t.Error("initial retry count spoofed")
			}
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, `{"error":"temporary"}`)
			return
		}
		if r.Header.Get("X-TypeSafe-Retry-Count") != "1" {
			t.Error("retry attempt header missing")
		}
		_, _ = io.WriteString(w, `{"model":"service-model","usage":{},"answers":{"decision":{"type":"choice","choice":"B","confidence":0.8,"probabilities":{"A":0.2,"B":0.8}}}}`)
	}))
	defer server.Close()
	out, stderr, err := execute(NewRoot(), "decide", "--prompt", "prompt", "--option", " A ", "--option", "B", "--api-key", "local-key", "--base-url", server.URL, "--model", "chosen")
	var response service.Response
	if attempts.Load() != 2 || err != nil || stderr != "" || json.Unmarshal([]byte(out), &response) != nil || response.Model != "service-model" || response.Probabilities["B"] != 0.8 {
		t.Fatal("HTTP output changed", out, err)
	}
	var extra any
	decoder := json.NewDecoder(bytes.NewBufferString(out))
	_ = decoder.Decode(&extra)
	if decoder.Decode(&extra) != io.EOF {
		t.Fatal("stdout contains extra output")
	}
}

func TestDecideCallerDeadlineAndIsolatedFlags(t *testing.T) {
	decisionEnvironment(t)
	deadline := time.Now().Add(time.Hour)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	first := NewRootWithFactory(func(config.Config) (service.DecisionService, func(), error) {
		return decideFunc(func(actual context.Context, _ service.Request) (service.Response, error) {
			if got, ok := actual.Deadline(); !ok || !got.Equal(deadline) {
				t.Error("shorter caller deadline changed")
			}
			return service.Response{Probabilities: map[string]float64{"A": 1}}, nil
		}), nil, nil
	})
	first.SetArgs([]string{"decide", "--prompt", "prompt", "--option", "A", "--option", "B", "--timeout", "2h"})
	first.SetOut(io.Discard)
	if err := first.ExecuteContext(ctx); err != nil {
		t.Fatal(err)
	}
	second := NewRootWithFactory(func(config.Config) (service.DecisionService, func(), error) {
		t.Fatal("flags leaked")
		return nil, nil, nil
	})
	if _, _, err := execute(second, "decide"); err == nil {
		t.Fatal("prompt/options leaked between roots")
	}
}

func TestDecideCancellationDuringFactoryPreventsDispatch(t *testing.T) {
	decisionEnvironment(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cleaned := false
	cmd := NewRootWithFactory(func(config.Config) (service.DecisionService, func(), error) {
		cancel()
		return decideFunc(func(context.Context, service.Request) (service.Response, error) {
			t.Fatal("cancelled factory dispatched service")
			return service.Response{}, nil
		}), func() { cleaned = true }, nil
	})
	cmd.SetArgs([]string{"decide", "--prompt", "prompt", "--option", "A", "--option", "B"})
	if err := cmd.ExecuteContext(ctx); !errors.Is(err, context.Canceled) || !cleaned {
		t.Fatal("factory cancellation or cleanup lost", err)
	}
}
