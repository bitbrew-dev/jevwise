package mcpserver

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/bitbrew-dev/jevwise/internal/service"
)

const runtimeToken = "foreground-fixture-token"

func runtimeListener(t *testing.T) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	return listener
}

func runtimeStopped(t *testing.T, stopped <-chan error) {
	t.Helper()
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not stop")
	}
}

func TestRuntimeReadinessAndIndependentLifetime(t *testing.T) {
	listener := runtimeListener(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan string, 1)
	stopped := make(chan error, 1)
	svc := decisionFunc(func(context.Context, service.Request) (service.Response, error) {
		t.Error("readiness must not make decisions")
		return service.Response{}, nil
	})
	go func() {
		stopped <- Serve(ctx, listener, svc, 10*time.Millisecond, RunOptions{Token: runtimeToken,
			Ready: func(lifetime context.Context, endpoint string) error {
				if lifetime.Err() != nil || strings.Contains(endpoint, runtimeToken) {
					return errors.New("unsafe readiness")
				}
				// The server must accept requests while readiness is being reported.
				request, _ := http.NewRequestWithContext(lifetime, http.MethodGet, endpoint, nil)
				request.Header.Set("Authorization", "Bearer "+runtimeToken)
				client := &http.Client{Timeout: time.Second}
				response, err := client.Do(request)
				if err != nil {
					return err
				}
				_, _ = io.Copy(io.Discard, response.Body)
				_ = response.Body.Close()
				client.CloseIdleConnections()
				if response.StatusCode != http.StatusMethodNotAllowed {
					return errors.New("unexpected readiness response")
				}
				ready <- endpoint
				return nil
			}})
	}()
	select {
	case endpoint := <-ready:
		if endpoint != "http://"+listener.Addr().String()+"/mcp" {
			t.Fatal("incorrect endpoint")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server was not ready")
	}
	select {
	case err := <-stopped:
		t.Fatal("decision timeout ended server lifetime", err)
	case <-time.After(50 * time.Millisecond):
	}
	cancel()
	runtimeStopped(t, stopped)
}

func TestRuntimeCancelsActiveDecision(t *testing.T) {
	listener := runtimeListener(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan string, 1)
	started, canceled := make(chan struct{}), make(chan struct{})
	svc := decisionFunc(func(ctx context.Context, _ service.Request) (service.Response, error) {
		close(started)
		<-ctx.Done()
		close(canceled)
		return service.Response{}, ctx.Err()
	})
	stopped := make(chan error, 1)
	go func() {
		stopped <- Serve(ctx, listener, svc, time.Minute, RunOptions{Token: runtimeToken,
			Ready: func(_ context.Context, endpoint string) error { ready <- endpoint; return nil }})
	}()
	var endpoint string
	select {
	case endpoint = <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("server was not ready")
	}
	responseDone := make(chan struct{})
	go func() {
		defer close(responseDone)
		body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"decide","arguments":{"prompt":"fixture","options":["a","b"]}}}`
		request, _ := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(body))
		request.Header.Set("Authorization", "Bearer "+runtimeToken)
		request.Header.Set("Accept", "application/json, text/event-stream")
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Mcp-Protocol-Version", "2025-11-25")
		response, err := (&http.Client{Timeout: 2 * time.Second}).Do(request)
		if err == nil {
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
		}
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("decision did not start")
	}
	cancel()
	runtimeStopped(t, stopped)
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("decision lifetime was not canceled")
	}
	<-responseDone
}

func TestRuntimeStartupFailuresCloseOwnedListener(t *testing.T) {
	cause := errors.New("private-readiness-error")
	svc := decisionFunc(func(context.Context, service.Request) (service.Response, error) { return service.Response{}, nil })
	for _, kind := range []string{"ready", "token", "service", "timeout", "context", "canceled", "overflow"} {
		t.Run(kind, func(t *testing.T) {
			listener := runtimeListener(t)
			ctx := context.Background()
			timeout, current := time.Second, service.DecisionService(svc)
			readiness := false
			options := RunOptions{Token: runtimeToken, Ready: func(context.Context, string) error { readiness = true; return nil }}
			switch kind {
			case "ready":
				options.Ready = func(context.Context, string) error { return cause }
			case "token":
				options.Token = ""
			case "service":
				current = nil
			case "timeout":
				timeout = 0
			case "context":
				ctx = nil
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "overflow":
				timeout = time.Duration(1<<63 - 1)
			}
			err := Serve(ctx, listener, current, timeout, options)
			if err == nil || strings.Contains(err.Error(), "private") || (kind == "ready" && !errors.Is(err, cause)) {
				t.Fatal("unsafe startup failure", err)
			}
			if kind != "ready" && readiness {
				t.Fatal("invalid startup reported readiness")
			}
			if connection, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second); err == nil {
				_ = connection.Close()
				t.Fatal("startup failure left listener open")
			}
		})
	}
}

func TestRunRefusesOccupiedAndNonLoopbackAddresses(t *testing.T) {
	listener := runtimeListener(t)
	for _, address := range []string{listener.Addr().String(), "0.0.0.0:8080", "localhost:8080", "private-input"} {
		err := Run(context.Background(), nil, time.Second, RunOptions{Address: address})
		if err == nil || strings.Contains(err.Error(), "private-input") {
			t.Fatal("unsafe bind failure", err)
		}
	}
	if err := Run(nil, nil, 0, RunOptions{}); err == nil {
		t.Fatal("nil context accepted")
	}
	if err := Serve(context.Background(), nil, nil, 0, RunOptions{}); err == nil {
		t.Fatal("nil listener accepted")
	}
}
