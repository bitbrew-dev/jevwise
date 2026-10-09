package typesafe

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/bitbrew-dev/jevwise/internal/debuglog"
)

func TestDebugTransportRetriesWithoutPayloads(t *testing.T) {
	clientEnvironment(t)
	var logs bytes.Buffer
	ctx := debuglog.WithWriter(context.Background(), &logs)
	calls := 0
	c := transportClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		status := 503
		if calls > 1 {
			status = 200
		}
		return &http.Response{StatusCode: status, Header: http.Header{"X-Private": {"private-header"}}, Body: io.NopCloser(strings.NewReader(`{"private-response":true}`))}, nil
	}))
	policy := RetryPolicy{MaxRetries: 1, HTTPStatuses: map[int]bool{503: true}}
	_, err := runRetry(ctx, policy, func(ctx context.Context, index int) (*RawResponse, error) {
		return c.sendOnce(ctx, "POST", "/private-path", []byte("private-prompt"), 0, nil, index)
	}, retryHooks{})
	if err != nil || calls != 2 || !strings.Contains(logs.String(), "api.retry.wait") || !strings.Contains(logs.String(), `"value":503`) {
		t.Fatal("retry diagnostics missing", err)
	}
	for _, secret := range []string{"private", "secret-key", "example.com", "Authorization"} {
		if strings.Contains(logs.String(), secret) {
			t.Fatal("transport logs exposed request/response data")
		}
	}
}
