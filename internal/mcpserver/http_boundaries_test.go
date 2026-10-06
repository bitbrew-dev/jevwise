package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bitbrew-dev/jevwise/internal/service"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func boundaryFixture(t *testing.T, lifetime context.Context, svc service.DecisionService) (*httptest.Server, *http.Client) {
	t.Helper()
	server, err := New(lifetime, svc, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	fixture := httptest.NewUnstartedServer(nil)
	handler, err := NewHTTP(server, fixture.Listener.Addr().String(), "token")
	if err != nil {
		fixture.Close()
		t.Fatal(err)
	}
	fixture.Config.Handler = handler
	fixture.Start()
	t.Cleanup(fixture.Close)
	return fixture, &http.Client{Transport: agentTransport{fixture.Client().Transport}}
}

func boundaryRequest(ctx context.Context, endpoint, version string) *http.Request {
	body := decisionJSON
	if version == "2026-07-28" {
		params := map[string]any{"name": "decide", "arguments": map[string]any{"prompt": "valid", "options": []string{"a", "b"}}, "_meta": map[string]any{
			mcp.MetaKeyProtocolVersion: version, mcp.MetaKeyClientInfo: map[string]any{"name": "fixture", "version": "dev"}, mcp.MetaKeyClientCapabilities: map[string]any{},
		}}
		data, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": params})
		body = string(data)
	}
	r, _ := http.NewRequestWithContext(ctx, "POST", endpoint+"/mcp", strings.NewReader(body))
	r.Header.Set("Accept", "application/json, text/event-stream")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("MCP-Protocol-Version", version)
	r.Header.Set("Mcp-Method", "tools/call")
	r.Header.Set("Mcp-Name", "decide")
	return r
}

func TestHTTPDisconnectAndLifetimeCancellation(t *testing.T) {
	for _, modern := range []bool{true, false} {
		t.Run(map[bool]string{true: "modern-disconnect", false: "legacy-lifetime"}[modern], func(t *testing.T) {
			lifetime, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			started, canceled := make(chan struct{}), make(chan error, 1)
			fixture, client := boundaryFixture(t, lifetime, decisionFunc(func(ctx context.Context, _ service.Request) (service.Response, error) {
				close(started)
				<-ctx.Done()
				canceled <- ctx.Err()
				return service.Response{}, ctx.Err()
			}))
			transportCtx, endTransport := context.WithTimeout(context.Background(), 5*time.Second)
			defer endTransport()
			requestCtx, disconnect := context.WithCancel(transportCtx)
			defer disconnect()
			version := "2025-11-25"
			if modern {
				version = "2026-07-28"
			}
			done := make(chan error, 1)
			go func() {
				response, err := client.Do(boundaryRequest(requestCtx, fixture.URL, version))
				if response != nil {
					data, _ := io.ReadAll(response.Body)
					if response.StatusCode != http.StatusOK {
						err = fmt.Errorf("HTTP %d: %s", response.StatusCode, data)
					}
					_ = response.Body.Close()
				}
				done <- err
			}()
			select {
			case <-started:
			case err := <-done:
				t.Fatal("HTTP request ended before service started", err)
			case <-lifetime.Done():
				t.Fatal("service did not start")
			}
			if modern {
				disconnect()
			} else {
				stop()
			}
			select {
			case err := <-canceled:
				if !errors.Is(err, context.Canceled) {
					t.Fatal("service cancellation lost", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("HTTP/lifetime cancellation did not reach service")
			}
			select {
			case err := <-done:
				if !modern && err != nil {
					t.Fatal("legacy request was canceled instead of server lifetime", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("HTTP call did not terminate")
			}
		})
	}
}

func TestHTTPLargeEscapedDecisionRemainsComplete(t *testing.T) {
	lifetime, stop := context.WithTimeout(context.Background(), 15*time.Second)
	defer stop()
	choice := strings.Repeat(`"\`, (outputLimit-512)/4)
	fixture, client := boundaryFixture(t, lifetime, decisionFunc(func(context.Context, service.Request) (service.Response, error) {
		return service.Response{Choice: choice, Confidence: 1, Probabilities: map[string]float64{"a": 1, "b": 0}}, nil
	}))
	response, err := client.Do(boundaryRequest(lifetime, fixture.URL, "2025-11-25"))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	wire, err := io.ReadAll(io.LimitReader(response.Body, httpEventLimit+1))
	if err != nil || response.StatusCode != http.StatusOK || len(wire) <= mcp.DefaultMaxEventSize || len(wire) > httpEventLimit {
		t.Fatal("escaped result did not fit server event bound", len(wire), err)
	}
	start := bytes.Index(wire, []byte("data: "))
	if start < 0 {
		t.Fatal("SSE data missing")
	}
	var envelope struct {
		Result struct {
			StructuredContent service.Response
			Content           []struct{ Text string }
		}
	}
	if json.Unmarshal(bytes.TrimSpace(wire[start+6:]), &envelope) != nil || envelope.Result.StructuredContent.Choice != choice || len(envelope.Result.Content) != 1 {
		t.Fatal("large structured result changed")
	}
	var textual service.Response
	if json.Unmarshal([]byte(envelope.Result.Content[0].Text), &textual) != nil || textual.Choice != choice {
		t.Fatal("large text result changed")
	}
	// SDK clients accepting this maximum output must raise their 16MiB default
	// StreamableClientTransport.MaxEventSize to the server's 32MiB frame bound.
}

func TestHTTPWriterBoundariesAndFailures(t *testing.T) {
	for _, contentType := range []string{"application/json", "text/event-stream"} {
		w := httptest.NewRecorder()
		writer := &httpPrivacyWriter{ResponseWriter: w}
		writer.Header().Set("Content-Type", contentType)
		_, _ = writer.Write([]byte(`{"private":"incomplete`))
		writer.Flush()
		writer.finish()
		if w.Body.Len() != 0 || len(writer.pending) != 0 {
			t.Fatal("incomplete frame escaped")
		}
		w = httptest.NewRecorder()
		writer = &httpPrivacyWriter{ResponseWriter: w}
		writer.Header().Set("Content-Type", contentType)
		prefix, suffix := `{"jsonrpc":"2.0","id":1,"result":"`, `"}`
		if contentType == "text/event-stream" {
			prefix = "data: " + prefix
			suffix += "\n\n"
		}
		oversized := []byte(prefix + strings.Repeat("x", httpEventLimit+1-len(prefix)-len(suffix)) + suffix)
		_, err := writer.Write(oversized)
		writer.finish()
		if err == nil || len(writer.pending) != 0 || w.Body.String() != "MCP request rejected\n" {
			t.Fatal("frame cap not enforced")
		}
	}
	w := httptest.NewRecorder()
	writer := &httpPrivacyWriter{ResponseWriter: w}
	writer.WriteHeader(http.StatusBadRequest)
	_, _ = writer.Write([]byte("private HTTP error"))
	if w.Body.String() != "MCP request rejected\n" {
		t.Fatal("plaintext error escaped")
	}
	cause := errors.New("private-write-error")
	packet := []byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32602,"message":"private-backend"}}`)
	for _, failure := range []error{cause, io.ErrShortWrite} {
		destination := &boundaryWriter{header: make(http.Header), err: failure}
		writer = &httpPrivacyWriter{ResponseWriter: destination}
		if _, err := writer.Write(packet); !errors.Is(err, failure) || len(writer.pending) != 0 || bytes.Contains(destination.body, []byte("private")) {
			t.Fatal("partial/error writer leaked or lost cause", err)
		}
		if _, err := writer.Write(packet); !errors.Is(err, failure) {
			t.Fatal("writer failure was not terminal")
		}
	}
}

type boundaryWriter struct {
	header http.Header
	body   []byte
	err    error
}

func (w *boundaryWriter) Header() http.Header { return w.header }
func (*boundaryWriter) WriteHeader(int)       {}
func (w *boundaryWriter) Write(data []byte) (int, error) {
	if w.err == io.ErrShortWrite {
		w.body = append(w.body, data[:len(data)-1]...)
		return len(data) - 1, nil
	}
	return 0, w.err
}

func TestHTTPListenerAddressCanonicalForms(t *testing.T) {
	for address, want := range map[string]string{"[::ffff:127.0.0.1]:8080": "127.0.0.1:8080", "[0:0:0:0:0:0:0:1]:8080": "[::1]:8080"} {
		got, err := ValidateAddress(address)
		if err != nil || got != want {
			t.Fatal("listener address was not canonical", got, err)
		}
	}
}
