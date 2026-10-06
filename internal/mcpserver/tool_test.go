package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/bitbrew-dev/jevwise/internal/buildinfo"
	"github.com/bitbrew-dev/jevwise/internal/service"
	"github.com/bitbrew-dev/jevwise/pkg/typesafe"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type decisionFunc func(context.Context, service.Request) (service.Response, error)

func (f decisionFunc) Decide(ctx context.Context, request service.Request) (service.Response, error) {
	return f(ctx, request)
}

func TestToolInteroperabilityAndSafeErrors(t *testing.T) {
	cause := errors.New("private-backend-content")
	calls := 0
	svc := decisionFunc(func(ctx context.Context, request service.Request) (service.Response, error) {
		calls++
		if request.Prompt == "failure" {
			return service.Response{}, cause
		}
		if request.Prompt != "  preserved prompt  " || strings.Join(request.Options, ",") != "a,b" {
			t.Error("request was changed")
		}
		if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) <= 0 || time.Until(deadline) > time.Minute {
			t.Error("call deadline missing")
		}
		tokens := 3
		return service.Response{Model: "jev", Choice: "a", Confidence: .8, Probabilities: map[string]float64{"a": .8, "b": .2}, Usage: &typesafe.Usage{InputTokens: &tokens}}, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	server, err := New(ctx, svc, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	ct, st := mcp.NewInMemoryTransports()
	ss, err := server.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "fixture", Version: "dev"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	if info := cs.InitializeResult().ServerInfo; info.Name != "jevwise" || info.Version != buildinfo.Version {
		t.Fatal("wrong server identity")
	}
	tools, err := cs.ListTools(ctx, nil)
	if err != nil || len(tools.Tools) != 1 || tools.Tools[0].Name != "decide" || tools.Tools[0].OutputSchema == nil {
		t.Fatal("unexpected tools", err)
	}
	result, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "decide", Arguments: map[string]any{"prompt": "  preserved prompt  ", "options": []string{"a", "b"}}})
	if err != nil || result.IsError {
		t.Fatal("decision failed", err)
	}
	encoded, _ := json.Marshal(result.StructuredContent)
	text := result.Content[0].(*mcp.TextContent).Text
	var response service.Response
	if json.Unmarshal(encoded, &response) != nil || response.Choice != "a" || response.Probabilities["a"] != .8 || response.Usage == nil || *response.Usage.InputTokens != 3 || response.Usage.OutputTokens != nil {
		t.Fatal("structured response changed")
	}
	var textResponse service.Response
	if json.Unmarshal([]byte(text), &textResponse) != nil || !reflect.DeepEqual(response, textResponse) {
		t.Fatal("text and structured output differ")
	}
	for _, arguments := range []any{map[string]any{"prompt": "failure", "options": []string{"a", "b"}}, map[string]any{"prompt": "private-input", "options": []string{"a", "a"}}, map[string]any{"prompt": "private-input", "options": []string{"a", "b"}, "private-field": true}} {
		result, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "decide", Arguments: arguments})
		if err != nil || !result.IsError || result.StructuredContent != nil || result.GetError() != nil {
			t.Fatal("error protocol is unsafe", err)
		}
		wire, _ := json.Marshal(result)
		if bytes.Contains(wire, []byte("private")) {
			t.Fatal("wire failure leaked input/cause")
		}
	}
	if calls != 2 {
		t.Fatal("invalid inputs dispatched", calls)
	}
}

func TestToolStrictInputsAndConstructor(t *testing.T) {
	forbidden := decisionFunc(func(context.Context, service.Request) (service.Response, error) {
		t.Fatal("invalid input dispatched")
		return service.Response{}, nil
	})
	handler := decisionHandler(context.Background(), forbidden, time.Second)
	for _, input := range []string{
		"", "{\"prompt\":\"\xff\",\"options\":[\"a\",\"b\"]}", "null", "[]", "{",
		`{"prompt":"private","options":["a","b"]} {}`,
		`{"Prompt":"private","options":["a","b"]}`,
		`{"prompt":"private","options":["a","b"],"private-field":1}`,
		`{"prompt":1,"options":["a","b"]}`,
		`{"prompt":" ","options":["a","b"]}`,
		`{"prompt":"private","options":["a"]}`,
		`{"prompt":"private","options":["a"," a "]}`,
		`{"prompt":"private","options":["a",""]}`,
		`{"prompt":"private earlier prompt","prompt":null,"options":["a","b"]}`,
		`{"prompt":"private","prompt":"later","options":["a","b"]}`,
		`{"prompt":"private","options":["a","b"],"options":null}`,
		strings.Repeat("x", inputLimit+1),
	} {
		result, err := handler(context.Background(), &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(input)}})
		if err != nil || !result.IsError {
			t.Fatal("invalid input accepted")
		}
		wire, _ := json.Marshal(result)
		if bytes.Contains(wire, []byte("private")) {
			t.Fatal("invalid input leaked")
		}
	}
	for _, call := range []*mcp.CallToolRequest{nil, {}} {
		result, err := handler(context.Background(), call)
		if err != nil || !result.IsError {
			t.Fatal("nil request accepted")
		}
	}
	if result, _ := handler(nil, nil); !result.IsError {
		t.Fatal("nil context accepted")
	}
	for _, svc := range []service.DecisionService{nil, decisionFunc(nil)} {
		if _, err := New(context.Background(), svc, time.Second); err == nil {
			t.Fatal("nil service accepted")
		}
	}
	for _, timeout := range []time.Duration{0, -1} {
		if _, err := New(context.Background(), forbidden, timeout); err == nil {
			t.Fatal("invalid timeout accepted")
		}
	}
	if _, err := New(nil, forbidden, time.Second); err == nil {
		t.Fatal("nil lifetime accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New(ctx, forbidden, time.Second); !errors.Is(err, context.Canceled) {
		t.Fatal("ended lifetime accepted", err)
	}
}

func TestToolCancellationTimeoutAndEncoding(t *testing.T) {
	cause := errors.New("private-service-error")
	for _, kind := range []string{"before", "after", "lifetime-before", "lifetime-after", "active-lifetime", "timeout", "caller-deadline", "lifetime-deadline", "error", "encoding", "oversize"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			lifetime, stop := context.WithCancel(context.Background())
			defer stop()
			if kind == "before" {
				cancel()
			}
			if kind == "lifetime-before" {
				stop()
			}
			if kind == "lifetime-deadline" {
				var done context.CancelFunc
				lifetime, done = context.WithDeadline(lifetime, time.Now().Add(-time.Hour))
				defer done()
			}
			if kind == "caller-deadline" {
				var done context.CancelFunc
				ctx, done = context.WithDeadline(ctx, time.Now().Add(-time.Hour))
				defer done()
			}
			calls := 0
			svc := decisionFunc(func(callCtx context.Context, _ service.Request) (service.Response, error) {
				calls++
				if kind == "after" {
					cancel()
				}
				if kind == "lifetime-after" || kind == "active-lifetime" {
					stop()
				}
				if kind == "timeout" || kind == "active-lifetime" {
					<-callCtx.Done()
					return service.Response{}, callCtx.Err()
				}
				if kind == "oversize" {
					return service.Response{Model: strings.Repeat("x", outputLimit+1)}, nil
				}
				if kind == "encoding" {
					return service.Response{Confidence: math.NaN()}, nil
				}
				return service.Response{}, cause
			})
			timeout := time.Minute
			if kind == "timeout" {
				timeout = 20 * time.Millisecond
			}
			handler := decisionHandler(lifetime, svc, timeout)
			result, err := handler(ctx, &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{"prompt":"valid","options":["a","b"]}`)}})
			if err != nil || !result.IsError || result.StructuredContent != nil {
				t.Fatal("unsafe failure result", err)
			}
			wantCalls := 1
			if kind == "before" || kind == "lifetime-before" || kind == "caller-deadline" || kind == "lifetime-deadline" {
				wantCalls = 0
			}
			if calls != wantCalls || strings.Contains(result.Content[0].(*mcp.TextContent).Text, "private") {
				t.Fatal("cancellation leaked or dispatched", calls)
			}
			if kind == "error" && !errors.Is(result.GetError(), cause) {
				t.Fatal("server-only cause lost")
			}
			if kind == "timeout" || kind == "caller-deadline" || kind == "lifetime-deadline" {
				if !errors.Is(result.GetError(), context.DeadlineExceeded) {
					t.Fatal("deadline lost")
				}
			} else if kind != "error" && kind != "encoding" && kind != "oversize" && !errors.Is(result.GetError(), context.Canceled) {
				t.Fatal("cancellation lost")
			}
		})
	}
}
