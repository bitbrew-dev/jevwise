package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const controllerInstance = "instance-0123456789"
const controllerToken = "control-012345678901234567890123456789"

func controllerFor(t *testing.T, server *httptest.Server) *Controller {
	t.Helper()
	c, err := NewController(strings.TrimPrefix(server.URL, "http://"), controllerInstance, controllerToken)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func statusJSON(state string) string {
	return fmt.Sprintf(`{"instance":%q,"state":%q}`, controllerInstance, state)
}

func TestControllerOwnedLifecycle(t *testing.T) {
	var cancels atomic.Int32
	canceled := make(chan struct{})
	var manager *Management
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/json" || r.ContentLength != 0 || r.URL.RawQuery != "" {
			t.Error("unexpected controller request")
		}
		manager.Handler().ServeHTTP(w, r)
	}))
	defer server.Close()
	c := controllerFor(t, server)
	var err error
	manager, err = NewManagement(c.address, controllerInstance, controllerToken, func() {
		cancels.Add(1)
		close(canceled)
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("NO_PROXY", "")
	status, err := c.Status(context.Background())
	if err != nil || status.State != "prepared" || status.Instance != controllerInstance || cancels.Load() != 0 {
		t.Fatalf("prepared status = %+v, %v", status, err)
	}
	if err := manager.MarkRunning(); err != nil {
		t.Fatal(err)
	}
	status, err = c.Status(context.Background())
	if err != nil || status.State != "running" {
		t.Fatalf("running status = %+v, %v", status, err)
	}
	status, err = c.Stop(context.Background())
	if err != nil || status.State != "stopping" {
		t.Fatalf("stop = %+v, %v", status, err)
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("stop did not cancel owned lifetime")
	}
	if cancels.Load() != 1 {
		t.Fatal("unexpected cancellation count")
	}
}

func TestControllerRejectsResponses(t *testing.T) {
	for name, body := range map[string]string{
		"malformed":      `{"instance":`,
		"array":          `[]`,
		"missing":        `{"instance":"instance-0123456789"}`,
		"unknown":        `{"instance":"instance-0123456789","state":"running","secret":"backend-secret"}`,
		"duplicate":      `{"instance":"instance-0123456789","state":"prepared","state":"running"}`,
		"wrong instance": `{"instance":"other-instance-0123456789","state":"running"}`,
		"invalid state":  statusJSON("untrusted"),
		"null state":     `{"instance":"instance-0123456789","state":null}`,
		"trailing":       statusJSON("running") + `{}`,
		"oversized":      strings.Repeat("backend-secret", controlResponseLimit),
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(body))
			}))
			defer server.Close()
			status, err := controllerFor(t, server).Status(context.Background())
			if err == nil || err.Error() != "management request failed" || status != (ManagementStatus{}) {
				t.Fatalf("unsafe response = %+v, %v", status, err)
			}
		})
	}
}

func TestControllerRejectsHTTPMetadata(t *testing.T) {
	for name, edit := range map[string]func(http.ResponseWriter){
		"unauthorized":   func(w http.ResponseWriter) { w.WriteHeader(401) },
		"bad media type": func(w http.ResponseWriter) { w.Header().Set("Content-Type", "text/plain") },
		"double type":    func(w http.ResponseWriter) { w.Header().Add("Content-Type", "application/json") },
		"large header":   func(w http.ResponseWriter) { w.Header().Set("X-Sensitive", strings.Repeat("x", 20<<10)) },
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				edit(w)
				_, _ = w.Write([]byte(statusJSON("running")))
			}))
			defer server.Close()
			if _, err := controllerFor(t, server).Status(context.Background()); err == nil || err.Error() != "management request failed" {
				t.Fatalf("metadata error = %v", err)
			}
		})
	}
}

func TestControllerDoesNotRedirect(t *testing.T) {
	var destinationCalls atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		destinationCalls.Add(1)
	}))
	defer destination.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	if _, err := controllerFor(t, server).Stop(context.Background()); err == nil || destinationCalls.Load() != 0 {
		t.Fatalf("redirect followed: %v, calls %d", err, destinationCalls.Load())
	}
}

func TestControllerStopRequiresStopping(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/_jev/stop" || r.Header.Get(instanceHeader) != controllerInstance || r.Header.Get("Authorization") != "Bearer "+controllerToken {
			t.Error("unexpected stop request")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(statusJSON("running")))
	}))
	defer server.Close()
	if _, err := controllerFor(t, server).Stop(context.Background()); err == nil {
		t.Fatal("accepted an unacknowledged stop")
	}
}

func TestControllerDeadlineAndConfiguration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()
	c := controllerFor(t, server)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := c.Status(ctx); !errors.Is(err, context.DeadlineExceeded) || err.Error() != "management request failed" {
		t.Fatalf("deadline error = %v", err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	cancel()
	if _, err := c.Status(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v", err)
	}
	if _, err := c.Status(nil); err == nil {
		t.Fatal("accepted nil context")
	}
	for _, address := range []string{"localhost:8080", "0.0.0.0:8080", "[::]:8080", "127.0.0.1:0", "127.0.0.1:8080/mcp"} {
		if _, err := NewController(address, controllerInstance, controllerToken); err == nil {
			t.Fatalf("accepted address %q", address)
		}
	}
	if _, err := NewController("127.0.0.1:8080", "short", controllerToken); err == nil {
		t.Fatal("accepted invalid instance")
	}
	if _, err := NewController("127.0.0.1:8080", controllerInstance, "secret\n"); err == nil {
		t.Fatal("accepted unsafe credential")
	}
}

type canceledResponseTransport struct{ cancel context.CancelFunc }

func (t canceledResponseTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}},
		Body: io.NopCloser(canceledResponseReader{strings.NewReader(statusJSON("running")), t.cancel})}, nil
}

type canceledResponseReader struct {
	reader io.Reader
	cancel context.CancelFunc
}

func (r canceledResponseReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.cancel()
	return n, err
}

func TestControllerRejectsCancellationDuringResponseRead(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, err := NewController("127.0.0.1:8080", controllerInstance, controllerToken)
	if err != nil {
		t.Fatal(err)
	}
	c.client = &http.Client{Transport: canceledResponseTransport{cancel}}
	status, err := c.Status(ctx)
	if !errors.Is(err, context.Canceled) || err.Error() != "management request failed" || status != (ManagementStatus{}) {
		t.Fatalf("canceled response accepted: %+v, %v", status, err)
	}
}
