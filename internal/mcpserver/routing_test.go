package mcpserver

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bitbrew-dev/jevwise/internal/service"
)

func TestManagementRoutingDuringPreparedStartup(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	agent, control := "fixture-agent-token", "fixture-private-management-token"
	var calls atomic.Int32
	management := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer "+control {
			http.Error(w, "control authorization required", http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	client := &http.Client{Timeout: time.Second}
	request := func(lifetime context.Context, path, token string) (int, error) {
		r, err := http.NewRequestWithContext(lifetime, http.MethodGet, "http://"+listener.Addr().String()+path, nil)
		if err != nil {
			return 0, err
		}
		r.Header.Set("Authorization", "Bearer "+token)
		response, err := client.Do(r)
		if err != nil {
			return 0, err
		}
		defer response.Body.Close()
		_, err = io.Copy(io.Discard, response.Body)
		return response.StatusCode, err
	}
	prepared, ack, promoted := make(chan struct{}), make(chan struct{}), make(chan struct{})
	stopped := make(chan error, 1)
	svc := decisionFunc(func(context.Context, service.Request) (service.Response, error) {
		t.Error("routing probes made a decision")
		return service.Response{}, nil
	})
	go func() {
		stopped <- Serve(ctx, listener, svc, time.Second, RunOptions{Token: agent, Management: management,
			Ready: func(lifetime context.Context, endpoint string) error {
				if strings.Contains(endpoint, agent) || strings.Contains(endpoint, control) {
					return errors.New("unsafe readiness metadata")
				}
				// Parent can verify preparation before sending its bootstrap ACK.
				if status, err := request(lifetime, "/_jev/status", control); err != nil || status != http.StatusNoContent {
					return errors.New("management preparation probe failed")
				}
				close(prepared)
				select {
				case <-ack:
					close(promoted)
					return nil
				case <-lifetime.Done():
					return lifetime.Err()
				}
			}})
	}()
	select {
	case <-prepared:
	case <-time.After(2 * time.Second):
		t.Fatal("management did not become prepared")
	}
	for _, fixture := range []struct {
		path, token string
		want        int
	}{
		{"/_jev/status", agent, http.StatusUnauthorized},
		{"/_jev/stop", agent, http.StatusUnauthorized},
		{"/_jev/status", control, http.StatusNoContent},
		{"/_jev/stop", control, http.StatusNoContent},
		{"/mcp", control, http.StatusUnauthorized},
		{"/mcp", agent, http.StatusMethodNotAllowed},
	} {
		status, err := request(ctx, fixture.path, fixture.token)
		if err != nil || status != fixture.want {
			t.Fatalf("route %s returned %d, error %v", fixture.path, status, err)
		}
	}
	before := calls.Load()
	for _, path := range []string{"/%5fjev/status", "/_jev/%73tatus", "/_jev/status/", "/_jev/status/../stop", "/_jev/unknown"} {
		status, err := request(ctx, path, agent)
		if err != nil || status != http.StatusNotFound {
			t.Fatalf("nonexact route %s returned %d, error %v", path, status, err)
		}
	}
	if calls.Load() != before {
		t.Fatal("nonexact path reached private management handler")
	}
	client.CloseIdleConnections()
	close(ack)
	select {
	case <-promoted:
	case <-time.After(time.Second):
		t.Fatal("prepared startup was not acknowledged")
	}
	cancel()
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("prepared server did not stop")
	}
}

func TestForegroundHasNoManagementRoutes(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc := decisionFunc(func(context.Context, service.Request) (service.Response, error) { return service.Response{}, nil })
	stopped := make(chan error, 1)
	go func() {
		stopped <- Serve(ctx, listener, svc, time.Second, RunOptions{Token: "fixture-agent-token",
			Ready: func(lifetime context.Context, _ string) error {
				r, _ := http.NewRequestWithContext(lifetime, http.MethodGet, "http://"+listener.Addr().String()+"/_jev/status", nil)
				r.Header.Set("Authorization", "Bearer fixture-agent-token")
				response, err := (&http.Client{Timeout: time.Second}).Do(r)
				if err != nil {
					return err
				}
				defer response.Body.Close()
				_, _ = io.Copy(io.Discard, response.Body)
				if response.StatusCode != http.StatusNotFound {
					return errors.New("foreground exposed management")
				}
				cancel()
				return nil
			}})
	}()
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("foreground probe did not stop")
	}
}
