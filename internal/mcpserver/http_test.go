package mcpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bitbrew-dev/jevwise/internal/service"
)

func TestHTTPAddressTokenAndConfiguration(t *testing.T) {
	for _, address := range []string{"localhost:8080", ":8080", "0.0.0.0:8080", "[::]:8080", "192.0.2.1:8080", "127.0.0.1:0", "127.0.0.1:65536", "[::1%zone]:8080"} {
		if _, err := ValidateAddress(address); err == nil {
			t.Fatal("unsafe address accepted")
		}
	}
	for _, address := range []string{"127.0.0.1:8080", "[::1]:8080"} {
		if got, err := ValidateAddress(address); err != nil || got != address {
			t.Fatal("loopback rejected", err)
		}
	}
	for _, token := range []string{"", "private secret", "private\nsecret", "非ascii", strings.Repeat("x", 4097)} {
		if err := ValidateToken(token); err == nil || strings.Contains(err.Error(), "private") {
			t.Fatal("invalid token accepted or leaked")
		}
	}
	if _, err := NewHTTP(nil, "127.0.0.1:8080", "token"); err == nil {
		t.Fatal("nil server accepted")
	}
}

func TestHTTPRejectsBeforeDecision(t *testing.T) {
	svc := decisionFunc(func(context.Context, service.Request) (service.Response, error) {
		t.Fatal("rejected HTTP request dispatched")
		return service.Response{}, nil
	})
	server, _ := New(context.Background(), svc, time.Second)
	handler, err := NewHTTP(server, "127.0.0.1:8080", "agent-secret")
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"host", "origin", "empty-origin", "duplicate-origin", "auth", "duplicate-auth", "get", "delete", "path", "query", "headers", "size", "malformed", "unknown-method", "malformed-params"} {
		t.Run(kind, func(t *testing.T) {
			body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"decide","arguments":{"prompt":"private-prompt","options":["a","b"]}}}`
			if kind == "size" {
				body = strings.Repeat("private", inputLimit/7+1)
			}
			if kind == "malformed" {
				body = `{"private`
			}
			if kind == "unknown-method" {
				body = `{"jsonrpc":"2.0","id":1,"method":"private-method","params":{}}`
			}
			if kind == "malformed-params" {
				body = `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":{},"arguments":{"prompt":"private-prompt"}}}`
			}
			r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/mcp", strings.NewReader(body))
			r.Header.Set("Authorization", "Bearer agent-secret")
			r.Header.Set("Accept", "application/json, text/event-stream")
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("MCP-Protocol-Version", "2025-11-25")
			want := http.StatusForbidden
			switch kind {
			case "host":
				r.Host = "private-host:8080"
			case "origin":
				r.Header.Set("Origin", "http://private-origin")
			case "empty-origin":
				r.Header.Set("Origin", "")
			case "duplicate-origin":
				r.Header.Add("Origin", "http://127.0.0.1:8080")
				r.Header.Add("Origin", "http://127.0.0.1:8080")
			case "auth":
				r.Header.Set("Authorization", "Bearer private-auth")
				want = http.StatusUnauthorized
			case "duplicate-auth":
				r.Header.Add("Authorization", "Bearer agent-secret")
				want = http.StatusUnauthorized
			case "get", "delete":
				r.Method = strings.ToUpper(kind)
				want = http.StatusMethodNotAllowed
			case "path":
				r.URL.Path = "/private"
				want = http.StatusNotFound
			case "query":
				r.URL.RawQuery = "private=value"
				want = http.StatusNotFound
			case "headers":
				r.Header.Set("X-Private", strings.Repeat("private", HTTPHeaderLimit/7))
				want = http.StatusRequestHeaderFieldsTooLarge
			case "size":
				want = http.StatusRequestEntityTooLarge
			case "malformed", "unknown-method":
				want = http.StatusBadRequest
			case "malformed-params":
				want = http.StatusOK
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != want || strings.Contains(w.Body.String(), "private") || strings.Contains(w.Body.String(), "agent-secret") {
				t.Fatal("unsafe HTTP rejection", w.Code, w.Body.String())
			}
			if want == http.StatusMethodNotAllowed && w.Header().Get("Allow") != "POST" {
				t.Fatal("missing method policy")
			}
		})
	}
}

func TestHTTPPrivacyFramesAndFailures(t *testing.T) {
	for _, sse := range []bool{false, true} {
		raw := `{"jsonrpc":"2.0","id":"correlation","error":{"code":-32602,"message":"private","data":{"private":true}}}`
		w := httptest.NewRecorder()
		writer := &httpPrivacyWriter{ResponseWriter: w}
		if sse {
			writer.Header().Set("Content-Type", "text/event-stream")
			raw = ": prime\n\nevent: message\ndata: " + raw + "\n\n"
		}
		for _, char := range []byte(raw) {
			if _, err := writer.Write([]byte{char}); err != nil {
				t.Fatal(err)
			}
			writer.Flush()
		}
		writer.finish()
		if strings.Contains(w.Body.String(), "private") || !strings.Contains(w.Body.String(), "correlation") || !strings.Contains(w.Body.String(), "-32602") || !strings.Contains(w.Body.String(), "MCP request failed") {
			t.Fatal("RPC error not sanitized")
		}
		if sse && !strings.HasPrefix(w.Body.String(), ": prime\n\n") {
			t.Fatal("priming comment lost")
		}
	}
	w := httptest.NewRecorder()
	writer := &httpPrivacyWriter{ResponseWriter: w}
	writer.Header().Set("Content-Type", "text/event-stream")
	valid := "data: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"error\":\"allowed-output\"}}\n\n"
	if _, err := writer.Write([]byte(valid + valid)); err != nil || w.Body.String() != valid+valid {
		t.Fatal("legitimate output changed", err)
	}
	_, _ = writer.Write([]byte("data: private-incomplete"))
	writer.Flush()
	writer.finish()
	if strings.Contains(w.Body.String(), "private") {
		t.Fatal("incomplete frame escaped")
	}

}
