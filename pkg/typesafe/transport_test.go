package typesafe

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type trackedBody struct {
	io.Reader
	closed bool
}

func (b *trackedBody) Close() error { b.closed = true; return nil }

type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }

func transportClient(t *testing.T, tr http.RoundTripper) *Client {
	t.Helper()
	c, err := NewClient(ClientOptions{APIKey: "secret-key", BaseURL: "https://example.com/prefix", HTTPClient: &http.Client{Transport: tr}, Retry: &RetryPolicy{}})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestTransportProtectedHeadersAndMetadata(t *testing.T) {
	clientEnvironment(t)
	headers := http.Header{"authorization": {"bad"}, "accept": {"bad"}, "content-type": {"bad"},
		"user-agent": {"bad"}, "x-typesafe-sdk": {"bad"}, "x-typesafe-runtime": {"bad"},
		"x-typesafe-retry-count": {"99"}, "X-Custom": {"call"}}
	responseHeaders := http.Header{requestIDHeader: {"req_123"}}
	body := &trackedBody{Reader: strings.NewReader(`{"ok":true}`)}
	attempt := 0
	c := transportClient(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != "https://example.com/prefix/v1/systemone" {
			t.Errorf("unexpected endpoint: %s", req.URL)
		}
		for key, want := range map[string]string{"Authorization": "Bearer secret-key", "Accept": "application/json",
			"Content-Type": "application/json", "User-Agent": sdkIdentity, "X-TypeSafe-SDK": sdkIdentity, "X-Custom": "call"} {
			if req.Header.Get(key) != want {
				t.Errorf("header %s: %q", key, req.Header.Get(key))
			}
		}
		if req.Header.Get("X-TypeSafe-Runtime") == "bad" || req.Header.Get("X-TypeSafe-Runtime") == "" {
			t.Error("runtime header not protected")
		}
		want := ""
		if attempt == 2 {
			want = "2"
		}
		if req.Header.Get(retryCountHeader) != want {
			t.Error("retry count spoofed")
		}
		payload, _ := io.ReadAll(req.Body)
		if string(payload) != `{"request":true}` {
			t.Error("request body changed")
		}
		return &http.Response{StatusCode: 200, Header: responseHeaders, Body: body}, nil
	}))
	c.headers = http.Header{"Authorization": {"also bad"}, "X-Custom": {"default"}}
	for _, attempt = range []int{0, 2} {
		body.Reader, body.closed = strings.NewReader(`{"ok":true}`), false
		raw, err := c.sendOnce(context.Background(), "POST", "/v1/systemone", []byte(`{"request":true}`), 0, headers, attempt)
		if err != nil || !body.closed || raw.RequestID != "req_123" || raw.StatusCode != 200 {
			t.Fatalf("bad response or closure: %#v %v", raw, err)
		}
		raw.Headers.Set(requestIDHeader, "changed")
		if responseHeaders.Get(requestIDHeader) != "req_123" {
			t.Error("response headers not cloned")
		}
	}
	if headers["authorization"][0] != "bad" || c.headers.Get("X-Custom") != "default" {
		t.Fatal("supplied headers mutated")
	}
}

func TestTransportResponseFailuresCloseBody(t *testing.T) {
	clientEnvironment(t)
	sentinel := errors.New("read secret-key failed")
	cases := []struct {
		name   string
		status int
		reader io.Reader
		want   string
	}{
		{"status", 401, strings.NewReader(`{"error":"secret-key denied"}`), "api"},
		{"malformed", 200, strings.NewReader("invalid"), "validation"},
		{"oversized", 200, strings.NewReader(strings.Repeat("x", maxResponseBytes+1)), "validation"},
		{"read", 200, failingReader{sentinel}, "connection"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := &trackedBody{Reader: tc.reader}
			c := transportClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tc.status, Header: http.Header{requestIDHeader: {"secret-key"}}, Body: body}, nil
			}))
			_, err := c.sendOnce(context.Background(), "GET", "/v1/models", nil, 0, nil, 0)
			if err == nil || !body.closed || strings.Contains(err.Error(), "secret-key") {
				t.Fatalf("bad failure or unsafe error: %v", err)
			}
			var api *APIError
			var validation *ResponseValidationError
			var connection *ConnectionError
			if tc.want == "api" && (!errors.As(err, &api) || api.Kind != Authentication) ||
				tc.want == "validation" && !errors.As(err, &validation) ||
				tc.want == "connection" && (!errors.As(err, &connection) || !errors.Is(err, sentinel)) {
				t.Fatalf("wrong failure type: %T", err)
			}
		})
	}
}

func TestTransportCancellationAndTimeout(t *testing.T) {
	clientEnvironment(t)
	called := false
	c := transportClient(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		called = true
		if deadline, ok := req.Context().Deadline(); !ok || time.Until(deadline) > time.Second {
			t.Error("earlier parent deadline not respected")
		}
		<-req.Context().Done()
		return nil, req.Context().Err()
	}))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.sendOnce(ctx, "GET", "/v1/models", nil, 0, nil, 0); !errors.Is(err, context.Canceled) || called {
		t.Fatal("cancelled request dispatched")
	}
	ctx, cancel = context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	_, err := c.sendOnce(ctx, "GET", "/v1/models", nil, time.Minute, nil, 0)
	var timeout *TimeoutError
	if !errors.As(err, &timeout) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("wrong timeout: %v", err)
	}
	c.timeout = time.Millisecond
	_, err = c.sendOnce(context.Background(), "GET", "/v1/models", nil, 0, nil, 0)
	if !errors.As(err, &timeout) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("SDK timeout not applied: %v", err)
	}
	if _, err := c.sendOnce(context.Background(), "GET", "/v1/models", nil, -1, nil, 0); err == nil {
		t.Fatal("negative timeout accepted")
	}
}

func TestTransportRedirectDoesNotForwardAuthentication(t *testing.T) {
	clientEnvironment(t)
	forwarded := false
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { forwarded = true }))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret-key" || r.Header.Get("Content-Type") != "" {
			t.Error("bad initial request headers")
		}
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer origin.Close()
	redirectCalled := false
	injected := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { redirectCalled = true; return nil }, Timeout: time.Minute}
	c, err := NewClient(ClientOptions{APIKey: "secret-key", BaseURL: origin.URL, HTTPClient: injected})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.sendOnce(context.Background(), "GET", "/v1/models", nil, 0, http.Header{"Content-Type": {"spoof"}}, 0)
	var api *APIError
	if !errors.As(err, &api) || api.StatusCode != 302 || forwarded || redirectCalled || injected.Timeout != time.Minute || injected.CheckRedirect == nil {
		t.Fatalf("redirect followed or caller changed: %v", err)
	}
}
