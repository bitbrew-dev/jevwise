package typesafe

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

func endpointRequest() SystemOneRequest {
	return SystemOneRequest{State: map[string]any{"integer": uint64(9007199254740993)}, Questions: map[string]Question{"q": Noul{}}}
}

const endpointResponse = `{"model":"service-model","usage":{"input_tokens":2},"answers":{"q":{"type":"noul","noul":0.7}}}`

func TestSystemOneEndpointAndOptions(t *testing.T) {
	clientEnvironment(t)
	model := "option-model"
	extra := map[string]any{"state": map[string]any{"replacement": nil}, "model": "extra-model", "questions": nil}
	headers := http.Header{"X-Call": {"custom"}, "Authorization": {"spoof"}}
	request := endpointRequest()
	request.Model = "request-model"
	cases := []struct {
		options      RequestOptions
		requestModel string
		wantModel    string
	}{
		{RequestOptions{}, "request-model", "request-model"},
		{RequestOptions{}, "", DefaultModel},
		{RequestOptions{Model: &model}, "request-model", "option-model"},
		{RequestOptions{Model: &model, ExtraBody: extra, Headers: headers, Timeout: time.Second}, "request-model", "extra-model"},
	}
	for _, tc := range cases {
		request.Model = tc.requestModel
		c := transportClient(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.Method != "POST" || r.URL.Path != "/prefix/v1/systemone" || r.Header.Get("Authorization") != "Bearer secret-key" {
				t.Error("wrong System One request")
			}
			if tc.options.Timeout != 0 {
				if deadline, ok := r.Context().Deadline(); !ok || time.Until(deadline) > 2*time.Second {
					t.Error("per-call timeout not applied")
				}
			}
			body, _ := io.ReadAll(r.Body)
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(body, &fields); err != nil || string(fields["model"]) != `"`+tc.wantModel+`"` {
				t.Errorf("unexpected wire body: %s", body)
			}
			if tc.options.ExtraBody != nil {
				if string(fields["questions"]) != "null" || string(fields["state"]) != `{"replacement":null}` || r.Header.Get("X-Call") != "custom" {
					t.Error("extra body was not a shallow last-wins merge")
				}
			} else if !strings.Contains(string(fields["state"]), "9007199254740993") {
				t.Error("large state integer lost precision")
			}
			return &http.Response{StatusCode: 200, Header: http.Header{requestIDHeader: {"req_06"}}, Body: io.NopCloser(strings.NewReader(endpointResponse))}, nil
		}))
		result, err := c.SystemOne(context.Background(), request, tc.options)
		if err != nil || result.Model != "service-model" || result.Nouls()["q"].Noul != 0.7 || result.Raw.StatusCode != 200 || result.Raw.RequestID != "req_06" || string(result.Raw.Body) != endpointResponse {
			t.Fatalf("unexpected typed response: %#v %v", result, err)
		}
	}
	if request.Model != "request-model" || request.Questions["q"] == nil || request.State.(map[string]any)["integer"] != uint64(9007199254740993) || len(extra) != 3 || headers.Get("Authorization") != "spoof" {
		t.Fatal("caller data was mutated")
	}
	for _, value := range []string{"", " \t "} {
		c := transportClient(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
			body, _ := io.ReadAll(r.Body)
			var fields map[string]any
			_ = json.Unmarshal(body, &fields)
			if fields["model"] != value {
				t.Error("explicit blank model changed")
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(endpointResponse))}, nil
		}))
		if _, err := c.SystemOne(context.Background(), endpointRequest(), RequestOptions{Model: &value}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestListModelsAndRawCustomDecode(t *testing.T) {
	clientEnvironment(t)
	body := `{"models":[{"name":"jev","description":"model","release_date":"2026"}]}`
	c := transportClient(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" || r.URL.Path != "/prefix/v1/models" || r.Header.Get("Content-Type") != "" {
			t.Error("wrong models request")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{requestIDHeader: {"models-id"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	}))
	models, err := c.ListModels(context.Background())
	if err != nil || len(models.Models) != 1 || models.Models[0].Name != "jev" || models.Raw.RequestID != "models-id" {
		t.Fatalf("unexpected models: %#v %v", models, err)
	}
	raw, err := c.ListModelsRaw(context.Background())
	if err != nil || raw.StatusCode != 200 || raw.Headers.Get(requestIDHeader) != "models-id" {
		t.Fatalf("unexpected raw metadata: %#v %v", raw, err)
	}
	var custom map[string]any
	if err := raw.Decode(&custom); err != nil || len(custom) != 1 {
		t.Fatal("custom models decode failed", err)
	}
	c = transportClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"future":[null,"kept"]}`))}, nil
	}))
	raw, err = c.SystemOneRaw(context.Background(), endpointRequest())
	var future struct{ Future []any }
	if err != nil || raw.Decode(&future) != nil || !reflect.DeepEqual(future.Future, []any{nil, "kept"}) {
		t.Fatal("raw custom schema incorrectly required standard answers")
	}
	_, err = c.SystemOne(context.Background(), endpointRequest())
	var invalid *ResponseValidationError
	if !errors.As(err, &invalid) || invalid.Path != "model" {
		t.Fatal("typed decode missing safe model path", err)
	}
}

type endpointSecretQuestion struct{}

func (endpointSecretQuestion) MarshalJSON() ([]byte, error) { return nil, errors.New("private-state") }

func TestEndpointInvalidInputDoesNotDispatch(t *testing.T) {
	clientEnvironment(t)
	c := transportClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid input dispatched")
		return nil, nil
	}))
	invalidRequest := endpointRequest()
	invalidRequest.Questions = map[string]Question{"private-state": endpointSecretQuestion{}}
	for _, request := range []SystemOneRequest{{}, invalidRequest} {
		if _, err := c.SystemOne(context.Background(), request, RequestOptions{ExtraBody: map[string]any{"questions": map[string]any{}}}); err == nil || strings.Contains(err.Error(), "private-state") {
			t.Fatal("invalid original request accepted or disclosed", err)
		}
	}
	for _, opts := range [][]RequestOptions{{{}, {}}, {{Timeout: -1}}, {{ExtraBody: map[string]any{"private-state": make(chan int)}}}} {
		if _, err := c.SystemOneRaw(context.Background(), endpointRequest(), opts...); err == nil || strings.Contains(err.Error(), "private-state") {
			t.Fatal("invalid options accepted or disclosed", err)
		}
	}
	model := "jev"
	for _, opts := range []RequestOptions{{Model: &model}, {ExtraBody: map[string]any{}}, {Timeout: -1}} {
		if _, err := c.ListModels(context.Background(), opts); err == nil {
			t.Fatal("unsupported models option accepted")
		}
	}
}

func TestRawDecodeSafeFailuresAndHTTPStatus(t *testing.T) {
	var custom struct {
		Value int `json:"private-state"`
	}
	var invalid *ResponseValidationError
	if err := (&RawResponse{Body: []byte(`{"private-state":"secret"}`)}).Decode(&custom); !errors.As(err, &invalid) || invalid.Path != "private-state" || strings.Contains(err.Error(), "private-state") || strings.Contains(err.Error(), "secret") {
		t.Fatal("custom decode path missing or disclosed", err)
	}

	for _, raw := range []*RawResponse{nil, {Body: []byte(`{"model":42}`)}, {Body: []byte("invalid")}} {
		var typed SystemOneResponse
		var invalid *ResponseValidationError
		if err := raw.Decode(&typed); !errors.As(err, &invalid) || typed.Raw != nil || strings.Contains(err.Error(), "42") {
			t.Fatal("decode error not safe or metadata attached on failure", err)
		}
	}
	clientEnvironment(t)
	c := transportClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 429, Body: io.NopCloser(strings.NewReader(`{"error":"slow down"}`))}, nil
	}))
	_, err := c.SystemOneRaw(context.Background(), endpointRequest())
	var api *APIError
	if !errors.As(err, &api) || api.Kind != RateLimit {
		t.Fatal("HTTP error was replaced by decode error", err)
	}
}

type endpointDecodeObserver struct{ called bool }

func (o *endpointDecodeObserver) UnmarshalJSON([]byte) error {
	o.called = true
	return errors.New("private-response")
}

func TestSystemOneIntoCustomAttempt(t *testing.T) {
	clientEnvironment(t)
	status, dispatched := 200, false
	c := transportClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		dispatched = true
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(`{"future":7}`))}, nil
	}))
	var custom struct{ Future int }
	if raw, err := c.SystemOneInto(context.Background(), endpointRequest(), &custom); err != nil || custom.Future != 7 || raw.StatusCode != 200 {
		t.Fatal("independent custom schema failed", err)
	}
	observer := &endpointDecodeObserver{}
	var invalid *ResponseValidationError
	if _, err := c.SystemOneInto(context.Background(), endpointRequest(), observer); !errors.As(err, &invalid) || !observer.called || strings.Contains(err.Error(), "private-response") {
		t.Fatal("custom attempt error not wrapped safely", err)
	}
	status, observer.called = 429, false
	var api *APIError
	if _, err := c.SystemOneInto(context.Background(), endpointRequest(), observer); !errors.As(err, &api) || observer.called {
		t.Fatal("custom decoder ran before HTTP error", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	dispatched = false
	if _, err := c.SystemOneInto(ctx, endpointRequest(), observer); !errors.Is(err, context.Canceled) || dispatched || observer.called {
		t.Fatal("cancelled custom request was dispatched or decoded", err)
	}
	for _, destination := range []any{nil, custom, (*endpointDecodeObserver)(nil)} {
		if _, err := c.SystemOneInto(context.Background(), endpointRequest(), destination); err == nil || dispatched {
			t.Fatal("invalid custom target dispatched", err)
		}
	}
}

func retryEndpointClient(t *testing.T, tr http.RoundTripper, policy *RetryPolicy) (*Client, *runnerClock) {
	t.Helper()
	c, err := NewClient(ClientOptions{APIKey: "secret-key", BaseURL: "https://example.com", HTTPClient: &http.Client{Transport: tr}, Retry: policy})
	if err != nil {
		t.Fatal(err)
	}
	clock := &runnerClock{}
	c.retryHooks = clock.hooks()
	return c, clock
}

func retryReply(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Retry-After": {"0"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestEndpointDefaultRetryReplay(t *testing.T) {
	headers := http.Header{retryCountHeader: {"99"}, "X-Custom": {"original"}}
	request := endpointRequest()
	calls, predicates := 0, 0
	var frozen string
	c, clock := retryEndpointClient(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("X-Custom") != "original" {
			t.Fatal("caller header mutation changed a retry")
		}
		want := []string{"", "1", "2"}[calls]
		if r.Header.Get(retryCountHeader) != want {
			t.Fatalf("retry header: %q, want %q", r.Header.Get(retryCountHeader), want)
		}
		body, _ := io.ReadAll(r.Body)
		if calls == 0 {
			frozen = string(body)
			headers.Set("X-Custom", "mutated")
			request.State.(map[string]any)["integer"] = 7
		} else if string(body) != frozen {
			t.Fatal("request was remarshaled between attempts")
		}
		calls++
		if calls < 3 {
			return retryReply(503, `{}`), nil
		}
		return retryReply(200, endpointResponse), nil
	}), nil)
	c.retry.Predicate = func(error) bool { predicates++; return false }
	result, err := c.SystemOne(context.Background(), request, RequestOptions{Headers: headers})
	if err != nil || result.Nouls()["q"].Noul != 0.7 || calls != 3 || predicates != 0 || len(clock.sleeps) != 2 {
		t.Fatalf("default retries: %v/%d/%d/%v", err, calls, predicates, clock.sleeps)
	}
}

func TestEndpointRetryOverrideIsolation(t *testing.T) {
	for _, maxRetries := range []int{0, 1} {
		calls := 0
		policy := DefaultRetryPolicy()
		policy.MaxRetries = maxRetries
		c, _ := retryEndpointClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++
			policy.MaxRetries, policy.HTTPStatuses[503] = 0, false
			return retryReply(503, `{}`), nil
		}), nil)
		_, err := c.SystemOneRaw(context.Background(), endpointRequest(), RequestOptions{Retry: &policy})
		if err == nil || calls != maxRetries+1 || c.retry.MaxRetries != 2 || !c.retry.HTTPStatuses[503] {
			t.Fatal("per-call policy was not isolated or inherited client fields")
		}
		calls = 0
		if _, err := c.ListModelsRaw(context.Background()); err == nil || calls != 3 {
			t.Fatal("override leaked into subsequent call")
		}
	}
	calls := 0
	without, _ := retryEndpointClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return retryReply(503, `{}`), nil }), nil)
	if _, err := without.ListModelsRaw(context.Background(), RequestOptions{Retry: &RetryPolicy{MaxRetries: 2}}); err == nil || calls != 1 {
		t.Fatal("override inherited default status selection")
	}
	c, _ := retryEndpointClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) { t.Fatal("invalid policy dispatched"); return nil, nil }), nil)
	bad := &RetryPolicy{MaxRetries: -1}
	if _, err := c.SystemOneRaw(context.Background(), endpointRequest(), RequestOptions{Retry: bad}); err == nil {
		t.Fatal("invalid System One policy accepted")
	}
	if _, err := c.ListModels(context.Background(), RequestOptions{Retry: bad}); err == nil {
		t.Fatal("invalid models policy accepted")
	}
}

func TestEndpointDecodeRetries(t *testing.T) {
	for _, kind := range []string{"raw", "typed", "models", "custom"} {
		t.Run(kind, func(t *testing.T) {
			calls := 0
			c, _ := retryEndpointClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				body := endpointResponse
				if kind == "models" {
					body = `{"models":[]}`
				} else if kind == "custom" {
					body = `{"value":7}`
				}
				if calls == 1 {
					body = `{}`
				}
				return retryReply(200, body), nil
			}), nil)
			policy := DefaultRetryPolicy()
			policy.Predicate = func(err error) bool { var invalid *ResponseValidationError; return errors.As(err, &invalid) }
			opts := RequestOptions{Retry: &policy}
			var err error
			switch kind {
			case "raw":
				var raw *RawResponse
				raw, err = c.SystemOneRaw(context.Background(), endpointRequest(), opts)
				var typed SystemOneResponse
				if raw.Decode(&typed) == nil || calls != 1 {
					t.Fatal("standalone raw decode retried HTTP")
				}
			case "typed":
				_, err = c.SystemOne(context.Background(), endpointRequest(), opts)
			case "models":
				_, err = c.ListModels(context.Background(), opts)
			case "custom":
				observer := &retryCustomDecoder{}
				_, err = c.SystemOneInto(context.Background(), endpointRequest(), observer, opts)
				if observer.calls != 2 {
					t.Fatal("custom decode was not inside retry attempt")
				}
			}
			if err != nil || kind != "raw" && calls != 2 {
				t.Fatalf("decode retries: %v/%d", err, calls)
			}
		})
	}
}

type retryCustomDecoder struct{ calls int }

func (d *retryCustomDecoder) UnmarshalJSON([]byte) error {
	d.calls++
	if d.calls == 1 {
		return errors.New("custom schema failure")
	}
	return nil
}

func TestEndpointRetryTerminalAndTimeoutCases(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		calls  int
	}{{"forbidden", 403, 1}, {"validation", 200, 1}, {"timeout success", 0, 2}, {"terminal timeout", 0, 3}, {"connection success", -1, 2}} {
		calls := 0
		c, _ := retryEndpointClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++
			if tc.status <= 0 && (calls == 1 || tc.name == "terminal timeout") {
				if tc.status == -1 {
					return nil, errors.New("connection failure")
				}
				return nil, context.DeadlineExceeded
			}
			if tc.status > 0 {
				return retryReply(tc.status, `{}`), nil
			}
			return retryReply(200, endpointResponse), nil
		}), nil)
		_, err := c.SystemOne(context.Background(), endpointRequest())
		if calls != tc.calls || tc.calls == 2 && err != nil || tc.calls != 2 && err == nil {
			t.Fatalf("%s: %v/%d", tc.name, err, calls)
		}
		if tc.name == "terminal timeout" {
			var timeout *TimeoutError
			if !errors.As(err, &timeout) || timeout.Method != "POST" || timeout.URL != "https://example.com/v1/systemone" || !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal("per-attempt timeout metadata lost")
			}
		}
	}
}

func TestEndpointCallerCancellationAndRetryBudget(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		if deadline {
			ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		} else {
			cancel()
		}
		c, _ := retryEndpointClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) { t.Fatal("caller expiration dispatched"); return nil, nil }), nil)
		c.retry.Predicate = func(error) bool { t.Fatal("caller expiration invoked predicate"); return true }
		_, err := c.SystemOneRaw(ctx, endpointRequest())
		var timeout *TimeoutError
		if !errors.Is(err, ctx.Err()) || deadline && (!errors.As(err, &timeout) || timeout.URL != "https://example.com/v1/systemone") {
			t.Fatal("caller expiration mapping failed", err)
		}
		cancel()
	}
	calls := 0
	c, clock := retryEndpointClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		reply := retryReply(503, `{}`)
		reply.Header = nil
		return reply, nil
	}), nil)
	policy := DefaultRetryPolicy()
	policy.Budget = 500 * time.Millisecond
	if _, err := c.ListModelsRaw(context.Background(), RequestOptions{Retry: &policy}); err == nil || calls != 1 || len(clock.sleeps) != 0 {
		t.Fatal("exact retry delay budget boundary dispatched again")
	}
}
