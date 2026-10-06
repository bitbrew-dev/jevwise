package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bitbrew-dev/jevwise/internal/service"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const decisionJSON = `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"decide","arguments":{"prompt":"valid","options":["a","b"]}}}`

func protocolRequest(body, version string) *http.Request {
	r := httptest.NewRequest("POST", "http://127.0.0.1:8080/mcp", strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer token")
	r.Header.Set("Accept", "application/json, text/event-stream")
	r.Header.Set("Content-Type", "application/json")
	if version != "" {
		r.Header.Set("MCP-Protocol-Version", version)
	}
	return r
}

func TestHTTPRejectsBatchesForEveryProtocol(t *testing.T) {
	server, _ := New(context.Background(), decisionFunc(func(context.Context, service.Request) (service.Response, error) {
		t.Error("batch dispatched a decision")
		return service.Response{}, nil
	}), time.Second)
	handler, _ := NewHTTP(server, "127.0.0.1:8080", "token")
	for _, version := range append([]string{""}, mcp.SupportedProtocolVersions()...) {
		for _, body := range []string{"[" + decisionJSON + "]", " \n[" + decisionJSON + "," + decisionJSON + "]"} {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, protocolRequest(body, version))
			if w.Code != http.StatusBadRequest || w.Body.String() != "MCP request rejected\n" {
				t.Fatal("batch accepted", version, w.Code)
			}
		}
	}
}

func TestHTTPFourDecisionsWithoutQueuedCalls(t *testing.T) {
	started, release, finished := make(chan struct{}, httpConcurrency), make(chan struct{}), make(chan struct{}, httpConcurrency)
	var calls atomic.Int32
	lifetime, stop := context.WithCancel(context.Background())
	defer stop()
	server, _ := New(lifetime, decisionFunc(func(ctx context.Context, _ service.Request) (service.Response, error) {
		calls.Add(1)
		started <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
			return service.Response{}, ctx.Err()
		}
		return service.Response{Choice: "a"}, nil
	}), 5*time.Second)
	handler, _ := NewHTTP(server, "127.0.0.1:8080", "token")
	for range httpConcurrency {
		go func() {
			handler.ServeHTTP(httptest.NewRecorder(), protocolRequest(decisionJSON, "2025-11-25"))
			finished <- struct{}{}
		}()
	}
	for range httpConcurrency {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("decision did not start")
		}
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, protocolRequest(decisionJSON, "2025-11-25"))
	if w.Code != http.StatusServiceUnavailable || calls.Load() != httpConcurrency {
		t.Fatal("concurrency gate queued or exceeded decisions")
	}
	close(release)
	for range httpConcurrency {
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			t.Fatal("decision did not complete")
		}
	}
	if calls.Load() != httpConcurrency {
		t.Fatal("rejected request dispatched later")
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, protocolRequest(decisionJSON, "2025-11-25"))
	if w.Code != http.StatusOK || calls.Load() != httpConcurrency+1 {
		t.Fatal("slot did not recover")
	}
}

func TestHTTPJSONErrorsPreserveOnlySafeNegotiationData(t *testing.T) {
	for _, kind := range []string{"supported", "unknown", "unrelated", "partial", "plaintext", "not-error"} {
		w := httptest.NewRecorder()
		writer := &httpPrivacyWriter{ResponseWriter: w}
		writer.Header().Set("Content-Type", "application/json")
		writer.Header().Set("Content-Length", "99999")
		if kind == "plaintext" {
			writer.Header().Set("Content-Type", "text/plain")
		}
		writer.WriteHeader(http.StatusBadRequest)
		code := mcp.CodeUnsupportedProtocolVersion
		supported := []string{"2026-07-28", "2025-11-25"}
		if kind == "unknown" {
			supported = []string{"private-version"}
		}
		if kind == "unrelated" {
			code = -32602
		}
		packet, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": "correlation", "error": map[string]any{"code": code, "message": "private-input", "data": map[string]any{"supported": supported, "requested": "private-version", "private-extra": true}}})
		if kind == "partial" {
			packet = []byte(`{"private`)
		}
		if kind == "not-error" {
			packet = []byte(`{"jsonrpc":"2.0","id":1,"result":"private"}`)
		}
		for _, char := range packet {
			_, _ = writer.Write([]byte{char})
			writer.Flush()
		}
		writer.finish()
		if w.Code != http.StatusBadRequest || strings.Contains(w.Body.String(), "private") || w.Header().Get("Content-Length") != "" {
			t.Fatal("error privacy failed", kind, w.Body.String())
		}
		if kind == "partial" || kind == "plaintext" || kind == "not-error" {
			if w.Body.String() != "MCP request rejected\n" {
				t.Fatal("unsafe fallback", kind)
			}
			continue
		}
		var result struct {
			ID    string
			Error struct {
				Code    int
				Message string
				Data    json.RawMessage
			}
		}
		if json.Unmarshal(w.Body.Bytes(), &result) != nil || result.ID != "correlation" || result.Error.Code != code || result.Error.Message != "MCP request failed" {
			t.Fatal("RPC error compatibility lost", kind)
		}
		if (len(result.Error.Data) != 0) != (kind == "supported") {
			t.Fatal("unverified error data survived", kind)
		}
	}
}
