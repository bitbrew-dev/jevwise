// Package gemini is a minimal client for one choice question on the Gemini
// Decisions API (POST /v1beta/decisions). It follows pkg/typesafe safety rules:
// no redirects, bounded responses, redacted errors and deadline-bound retries.
package gemini

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/bitbrew-dev/jevwise/internal/debuglog"
	typesafe "github.com/bitbrew-dev/jevwise/pkg/typesafe"
)

const (
	// DefaultBaseURL is the Gemini Developer API host.
	DefaultBaseURL = "https://generativelanguage.googleapis.com"
	// DefaultModel is the Gemini Decisions model.
	DefaultModel = "gemini-decision-1"
	// MaxOptions is the upstream limit for one choice question.
	MaxOptions = 255

	endpointPath     = "/v1beta/decisions"
	questionName     = "decision"
	maxResponseBytes = 8 << 20
	maxRetries       = 2
	backoffInitial   = 500 * time.Millisecond
	backoffMax       = 5 * time.Second
	maxRetryAfter    = 30 * time.Second
)

// Options configures a Client. Redirects are always refused.
type Options struct {
	APIKey, BaseURL, Model string
	Timeout                time.Duration
}

// Client sends choice questions with one API key, host, model and per-attempt timeout.
type Client struct {
	apiKey, endpoint, model string
	timeout                 time.Duration
	http                    http.Client
}

// ChoiceRequest is one shared input with distinct option labels.
type ChoiceRequest struct {
	Input, Instructions string
	Options             []string
}

// Result holds the service distribution, never normalized locally, and the
// reported token counts when present.
type Result struct {
	Choice                    string
	Confidence                float64
	Probabilities             map[string]float64
	InputTokens, OutputTokens *int
}

// APIError is a non-2xx response. Upstream messages are dropped: they may echo input.
type APIError struct {
	StatusCode int
	Code       string
}

func (e *APIError) Error() string {
	return strings.TrimSpace(fmt.Sprintf("gemini: API error %d %s", e.StatusCode, e.Code))
}

// BlockedError reports input rejected by content filters, by status or prompt_feedback.
type BlockedError struct{ Reason string }

func (e *BlockedError) Error() string {
	return "gemini: input blocked by content filters (" + e.Reason + ")"
}

// ErrInvalidResponse reports a response that does not match the request.
var ErrInvalidResponse = errors.New("gemini: invalid decision response")

// Transport failures hide URLs and causes, which may carry request details.
var (
	errTimeout    = errors.New("gemini: request timed out")
	errConnection = errors.New("gemini: connection failed")
)

// New validates options. The key must be printable ASCII; the base URL must be an
// absolute HTTP(S) URL without credentials, query or fragment.
func New(options Options) (*Client, error) {
	key := strings.TrimSpace(options.APIKey)
	if key == "" || strings.IndexFunc(key, func(r rune) bool { return r < 0x21 || r > 0x7e }) >= 0 {
		return nil, errors.New("gemini: API key must be nonblank printable ASCII")
	}
	base := strings.TrimRight(cmp.Or(options.BaseURL, DefaultBaseURL), "/")
	u, err := url.Parse(base)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("gemini: base URL must be an absolute HTTP(S) URL without credentials, query or fragment")
	}
	if options.Timeout <= 0 {
		return nil, errors.New("gemini: timeout must be positive")
	}
	c := &Client{apiKey: key, endpoint: base + endpointPath, model: cmp.Or(strings.TrimSpace(options.Model), DefaultModel), timeout: options.Timeout}
	c.http.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return c, nil
}

type question struct {
	Type         string            `json:"type"`
	Instructions string            `json:"instructions"`
	Criteria     map[string]string `json:"criteria"`
}

type response struct {
	Answers map[string]struct {
		Type          string             `json:"type"`
		Choice        string             `json:"choice"`
		Confidence    *float64           `json:"confidence"`
		Probabilities map[string]float64 `json:"probabilities"`
	} `json:"answers"`
	Usage struct {
		Input  *int `json:"total_input_tokens"`
		Output *int `json:"total_output_tokens"`
	} `json:"usage"`
	Feedback struct {
		BlockReason string `json:"block_reason"`
	} `json:"prompt_feedback"`
}

// Choose asks one choice question with an empty description for every label.
func (c *Client) Choose(ctx context.Context, r ChoiceRequest) (Result, error) {
	if len(r.Options) < 2 || len(r.Options) > MaxOptions || strings.TrimSpace(r.Instructions) == "" {
		return Result{}, fmt.Errorf("gemini: a choice needs instructions and 2 to %d options", MaxOptions)
	}
	criteria := make(map[string]string, len(r.Options))
	for _, option := range r.Options {
		criteria[option] = ""
	}
	if len(criteria) != len(r.Options) {
		return Result{}, errors.New("gemini: options must be distinct")
	}
	body, err := json.Marshal(map[string]any{"model": c.model, "input": r.Input,
		"questions": map[string]question{questionName: {"choice", r.Instructions, criteria}}})
	if err != nil {
		return Result{}, err
	}
	payload, err := c.send(ctx, body)
	if err != nil {
		return Result{}, err
	}
	var decoded response
	if json.Unmarshal(payload, &decoded) != nil {
		return Result{}, ErrInvalidResponse
	}
	if reason := decoded.Feedback.BlockReason; reason != "" {
		return Result{}, &BlockedError{Reason: safeCode(reason)}
	}
	answer, ok := decoded.Answers[questionName]
	if !ok || answer.Type != "choice" || answer.Confidence == nil || !validDistribution(answer.Choice, answer.Probabilities, r.Options) {
		return Result{}, ErrInvalidResponse
	}
	return Result{Choice: answer.Choice, Confidence: *answer.Confidence, Probabilities: answer.Probabilities,
		InputTokens: decoded.Usage.Input, OutputTokens: decoded.Usage.Output}, nil
}

func validDistribution(choice string, probabilities map[string]float64, options []string) bool {
	if len(probabilities) != len(options) || !slices.Contains(options, choice) {
		return false
	}
	for _, option := range options {
		p, ok := probabilities[option]
		if !ok || math.IsNaN(p) || p < 0 || p > 1 {
			return false
		}
	}
	return true
}

// send retries 408, 409, 429, 5xx, connection and timeout failures at most twice,
// never past the caller's deadline. Other statuses, including 400, fail at once.
func (c *Client) send(ctx context.Context, body []byte) ([]byte, error) {
	for attempt := 0; ; attempt++ {
		payload, headers, err := c.once(ctx, body)
		if err == nil || attempt == maxRetries || !retryable(err) {
			return payload, err
		}
		delay := min(backoffInitial<<attempt, backoffMax)
		delay = time.Duration(float64(delay) * (0.75 + rand.Float64()/2))
		if after, ok := typesafe.ParseRetryAfter(headers, time.Now()); ok {
			delay = min(after, maxRetryAfter)
		}
		if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) <= delay {
			return nil, err
		}
		debuglog.Event(ctx, "gemini.retry")
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}
}

func retryable(err error) bool {
	var api *APIError
	if errors.As(err, &api) {
		return api.StatusCode == 408 || api.StatusCode == 409 || api.StatusCode == 429 || api.StatusCode >= 500
	}
	return errors.Is(err, errTimeout) || errors.Is(err, errConnection)
}

func (c *Client) once(ctx context.Context, body []byte) (_ []byte, _ http.Header, resultErr error) {
	finish := debuglog.Trace(ctx, "gemini.request")
	defer func() { finish(resultErr) }()
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	attemptCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(attemptCtx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, nil, errors.New("gemini: cannot build request")
	}
	req.Header.Set("x-goog-api-key", c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, nil, ctxErr
		}
		if attemptCtx.Err() != nil {
			return nil, nil, errTimeout
		}
		return nil, nil, errConnection
	}
	defer res.Body.Close()
	debuglog.Count(ctx, "gemini.response.status", res.StatusCode)
	payload, err := io.ReadAll(io.LimitReader(res.Body, maxResponseBytes+1))
	if err != nil || len(payload) > maxResponseBytes {
		return nil, res.Header, ErrInvalidResponse
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return nil, res.Header, apiError(res.StatusCode, payload)
	}
	return payload, res.Header, nil
}

func apiError(status int, payload []byte) error {
	var body struct {
		Error struct {
			Code   json.RawMessage `json:"code"`
			Status string          `json:"status"`
		} `json:"error"`
	}
	_ = json.Unmarshal(payload, &body)
	code := body.Error.Status
	var text string
	if json.Unmarshal(body.Error.Code, &text) == nil && text != "" {
		code = text
	}
	if status == http.StatusBadRequest && strings.EqualFold(code, "safety") {
		return &BlockedError{Reason: "safety"}
	}
	return &APIError{StatusCode: status, Code: safeCode(code)}
}

// safeCode keeps short identifier-like codes and replaces anything else.
func safeCode(code string) string {
	if !codePattern.MatchString(code) {
		return "other"
	}
	return code
}

var codePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
