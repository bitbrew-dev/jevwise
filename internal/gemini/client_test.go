package gemini

import (
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
)

const fixtureKey = "fixture-gemini-key"

func fixture(t *testing.T, handler http.HandlerFunc) (*Client, *atomic.Int32) {
	t.Helper()
	calls := &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	client, err := New(Options{APIKey: fixtureKey, BaseURL: server.URL + "/", Model: "fixture-model", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	return client, calls
}

func choiceBody(choice string, probabilities string) string {
	return `{"answers":{"decision":{"type":"choice","choice":"` + choice + `","confidence":0.7,"probabilities":` + probabilities + `}},"usage":{"total_input_tokens":12,"total_output_tokens":0}}`
}

var request = ChoiceRequest{Input: "fixture input", Instructions: "Pick one.", Options: []string{"A", "B"}}

func TestChooseWireShapeAndAnswer(t *testing.T) {
	client, calls := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model, Input string
			Questions    map[string]question
		}
		data, _ := io.ReadAll(r.Body)
		if r.Method != http.MethodPost || r.URL.Path != "/v1beta/decisions" || r.Header.Get("x-goog-api-key") != fixtureKey || r.Header.Get("Authorization") != "" || json.Unmarshal(data, &body) != nil {
			t.Error("unexpected request line or headers")
		}
		q := body.Questions["decision"]
		if body.Model != "fixture-model" || body.Input != "fixture input" || q.Type != "choice" || q.Instructions != "Pick one." || len(q.Criteria) != 2 || q.Criteria["A"] != "" || !strings.Contains(string(data), `"criteria":{"A":"","B":""}`) {
			t.Error("unexpected request body", string(data))
		}
		_, _ = io.WriteString(w, choiceBody("B", `{"A":0.3,"B":0.7}`))
	})
	answer, err := client.Choose(context.Background(), request)
	if err != nil || answer.Choice != "B" || answer.Confidence != 0.7 || answer.Probabilities["A"] != 0.3 || *answer.InputTokens != 12 || *answer.OutputTokens != 0 || calls.Load() != 1 {
		t.Fatal("unexpected answer", answer, err)
	}
}

func TestChooseRejectsInvalidResponses(t *testing.T) {
	for _, body := range []string{
		`not json`, `{}`, `{"answers":{"decision":{"type":"noul","noul":0.5,"confidence":0.5}}}`,
		choiceBody("C", `{"A":0.3,"B":0.7}`), choiceBody("A", `{"A":0.3}`), choiceBody("A", `{"A":0.3,"B":0.7,"C":0}`),
		choiceBody("A", `{"A":1.3,"B":0.7}`), `{"answers":{"decision":{"type":"choice","choice":"A","probabilities":{"A":0.5,"B":0.5}}}}`,
	} {
		client, _ := fixture(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, body) })
		if _, err := client.Choose(context.Background(), request); !errors.Is(err, ErrInvalidResponse) {
			t.Fatal("invalid response accepted", body, err)
		}
	}
}

func TestChooseBlockedAndAPIErrorsAreSafeAndNotRetried(t *testing.T) {
	for _, tt := range []struct {
		status  int
		body    string
		blocked bool
	}{
		{200, `{"prompt_feedback":{"block_reason":"prohibited_content"}}`, true},
		{400, `{"error":{"code":"safety","message":"Request blocked"}}`, true},
		{400, `{"error":{"code":400,"status":"INVALID_ARGUMENT","message":"bad key ` + fixtureKey + ` and input"}}`, false},
		{403, `not json`, false},
	} {
		client, calls := fixture(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tt.status)
			_, _ = io.WriteString(w, tt.body)
		})
		_, err := client.Choose(context.Background(), request)
		var blocked *BlockedError
		var api *APIError
		if errors.As(err, &blocked) != tt.blocked || !tt.blocked && (!errors.As(err, &api) || api.StatusCode != tt.status) || calls.Load() != 1 {
			t.Fatal("wrong error or retried", tt.status, err)
		}
		if api != nil && (strings.Contains(err.Error(), fixtureKey) || strings.Contains(err.Error(), "input") || tt.status == 400 && api.Code != "INVALID_ARGUMENT") {
			t.Fatal("API error leaked key or message", err)
		}
	}
}

func TestChooseRetriesTransientFailures(t *testing.T) {
	attempts := 0
	client, calls := fixture(t, func(w http.ResponseWriter, _ *http.Request) {
		if attempts++; attempts < 3 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(w, choiceBody("A", `{"A":0.6,"B":0.4}`))
	})
	if answer, err := client.Choose(context.Background(), request); err != nil || answer.Choice != "A" || calls.Load() != 3 {
		t.Fatal("transient failures not retried", err, calls.Load())
	}
	client, calls = fixture(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusInternalServerError)
	})
	var api *APIError
	if _, err := client.Choose(context.Background(), request); !errors.As(err, &api) || calls.Load() != 3 {
		t.Fatal("retry count not bounded", err, calls.Load())
	}
	client, calls = fixture(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "10")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := client.Choose(ctx, request); err == nil || calls.Load() != 1 || time.Since(start) > time.Second {
		t.Fatal("retry waited past the caller deadline", err)
	}
}

func TestRedirectsAreRefusedWithoutForwardingKey(t *testing.T) {
	forwarded := false
	target := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { forwarded = r.Header.Get("x-goog-api-key") != "" }))
	defer target.Close()
	client, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	})
	var api *APIError
	if _, err := client.Choose(context.Background(), request); !errors.As(err, &api) || api.StatusCode != http.StatusTemporaryRedirect || forwarded {
		t.Fatal("redirect followed or key forwarded", err)
	}
}

func TestValidation(t *testing.T) {
	for _, options := range []Options{{Timeout: 1}, {APIKey: "a key", Timeout: 1}, {APIKey: "k", BaseURL: "ftp://x.test", Timeout: 1}, {APIKey: "k", BaseURL: "https://u:p@x.test", Timeout: 1}, {APIKey: "k"}} {
		if _, err := New(options); err == nil {
			t.Fatal("invalid options accepted", options.BaseURL)
		}
	}
	client, err := New(Options{APIKey: "k", Timeout: time.Second})
	if err != nil || client.endpoint != DefaultBaseURL+"/v1beta/decisions" || client.model != DefaultModel {
		t.Fatal("defaults not applied", err)
	}
	many := strings.Split(strings.Repeat("x,", MaxOptions)+"y", ",")
	for _, r := range []ChoiceRequest{{Instructions: "i", Options: []string{"A"}}, {Instructions: "i", Options: []string{"A", "A"}}, {Instructions: " ", Options: []string{"A", "B"}}, {Instructions: "i", Options: many}} {
		if _, err := client.Choose(context.Background(), r); err == nil {
			t.Fatal("invalid request accepted", len(r.Options))
		}
	}
}
