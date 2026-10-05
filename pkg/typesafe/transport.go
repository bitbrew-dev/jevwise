package typesafe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Responses are bounded to 8 MiB, including error bodies, to prevent unbounded
// allocation. Oversized responses fail validation rather than being retried.
const maxResponseBytes = 8 << 20
const retryCountHeader = "X-TypeSafe-Retry-Count"
const sdkIdentity = "typesafe-sdk-go/dev"

// sendOnce executes one attempt. A zero timeout uses the client default. The
// enclosing retry loop is responsible for incrementing the zero-based attempt.
func (c *Client) sendOnce(ctx context.Context, method, path string, body []byte, timeout time.Duration, headers http.Header, attempt int) (*RawResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, transportError(ctx, method, c.redact(c.baseURL+path), err)
	}
	if timeout == 0 {
		timeout = c.timeout
	}
	if timeout < 0 {
		return nil, fmt.Errorf("typesafe: timeout must be positive")
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	endpoint := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, &ConnectionError{Method: method, URL: c.redact(endpoint), Err: err}
	}
	req.Header = make(http.Header)
	for _, source := range []http.Header{c.headers, headers} {
		for key, values := range source {
			req.Header[http.CanonicalHeaderKey(key)] = append([]string(nil), values...)
		}
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", sdkIdentity)
	req.Header.Set("X-TypeSafe-SDK", sdkIdentity)
	req.Header.Set("X-TypeSafe-Runtime", runtime.Version()+" ("+runtime.GOOS+"; "+runtime.GOARCH+")")
	req.Header.Del("Content-Type")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Del(retryCountHeader)
	if attempt > 0 {
		req.Header.Set(retryCountHeader, strconv.Itoa(attempt))
	}
	// Preserve caller ownership while disallowing all redirects, including
	// same-origin redirects. No second request can leak authentication.
	httpClient := *c.httpClient
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := httpClient.Do(req)
	if response != nil && response.Body != nil {
		defer response.Body.Close()
	}
	if err != nil {
		return nil, transportError(ctx, method, c.redact(endpoint), err)
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return nil, transportError(ctx, method, c.redact(endpoint), err)
	}
	if len(payload) > maxResponseBytes {
		return nil, &ResponseValidationError{Path: "$", Err: errors.New("response exceeds 8 MiB limit")}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		apiError := newAPIError(response.StatusCode, method, c.redact(endpoint), response.Header, payload)
		apiError.Message = c.redact(apiError.Message)
		apiError.RequestID = c.redact(apiError.RequestID)
		return nil, apiError
	}
	if !json.Valid(payload) {
		return nil, &ResponseValidationError{Path: "$", Err: errors.New("response is not valid JSON")}
	}
	return &RawResponse{StatusCode: response.StatusCode, Headers: response.Header.Clone(),
		Body: bytes.Clone(payload), RequestID: response.Header.Get(requestIDHeader)}, nil
}

func transportError(ctx context.Context, method, endpoint string, err error) error {
	if errors.Is(ctx.Err(), context.Canceled) {
		return context.Canceled
	}
	var networkError net.Error
	if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &networkError) && networkError.Timeout() {
		return &TimeoutError{Method: method, URL: endpoint, Err: err}
	}
	return &ConnectionError{Method: method, URL: endpoint, Err: err}
}

func (c *Client) redact(value string) string {
	if c.apiKey == "" {
		return value
	}
	return strings.ReplaceAll(value, c.apiKey, "[REDACTED]")
}
