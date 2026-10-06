package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

const testAddress = "127.0.0.1:8080"
const testInstance = "instance-0123456789"
const testControlToken = "control-012345678901234567890123456789"

func testManagement(t *testing.T, cancel context.CancelFunc) *Management {
	t.Helper()
	m, err := NewManagement(testAddress, testInstance, testControlToken, cancel)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func managementRequest(method, path string) *http.Request {
	r := httptest.NewRequest(method, "http://"+testAddress+path, nil)
	r.RemoteAddr = "127.0.0.1:40123"
	r.Header.Set("Authorization", "Bearer "+testControlToken)
	r.Header.Set(instanceHeader, testInstance)
	return r
}

func TestManagementLifecycle(t *testing.T) {
	var calls atomic.Int32
	var flushed atomic.Bool
	m := testManagement(t, func() {
		if !flushed.Load() {
			t.Error("canceled before acknowledgement flush")
		}
		calls.Add(1)
	})
	check := func(path, want string) {
		t.Helper()
		method := http.MethodGet
		if path == "/_jev/stop" {
			method = http.MethodPost
		}
		w := &flushRecorder{ResponseRecorder: httptest.NewRecorder(), flushed: &flushed}
		m.Handler().ServeHTTP(w, managementRequest(method, path))
		if w.Body.Len() > 4<<10 || w.Header().Get("Content-Length") != strconv.Itoa(w.Body.Len()) {
			t.Fatal("management response length does not match bounded body")
		}
		var status ManagementStatus
		if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
			t.Fatal(err)
		}
		if w.Code != 200 || status.Instance != testInstance || status.State != want {
			t.Fatalf("status = %d, %+v", w.Code, status)
		}
	}
	check("/_jev/status", "prepared")
	if calls.Load() != 0 {
		t.Fatal("status canceled lifetime")
	}
	if err := m.MarkRunning(); err != nil {
		t.Fatal(err)
	}
	if err := m.MarkRunning(); err == nil {
		t.Fatal("duplicate acknowledgement succeeded")
	}
	check("/_jev/status", "running")
	check("/_jev/stop", "stopping")
	check("/_jev/stop", "stopping")
	if calls.Load() != 1 || m.MarkRunning() == nil {
		t.Fatal("stop was repeated or instance revived")
	}
}

type flushRecorder struct {
	*httptest.ResponseRecorder
	flushed *atomic.Bool
}

func (w *flushRecorder) Flush() {
	w.ResponseRecorder.Flush()
	w.flushed.Store(true)
}

func TestManagementGuards(t *testing.T) {
	tests := []struct {
		name string
		edit func(*http.Request)
		code int
	}{
		{"wrong host", func(r *http.Request) { r.Host = "localhost:8080" }, 403},
		{"public peer", func(r *http.Request) { r.RemoteAddr = "192.0.2.1:9" }, 403},
		{"missing peer", func(r *http.Request) { r.RemoteAddr = "" }, 403},
		{"foreign origin", func(r *http.Request) { r.Header.Set("Origin", "https://example.com") }, 403},
		{"empty origin", func(r *http.Request) { r.Header.Set("Origin", "") }, 403},
		{"duplicate origin", func(r *http.Request) {
			r.Header.Add("Origin", "http://"+testAddress)
			r.Header.Add("Origin", "http://"+testAddress)
		}, 403},
		{"no authorization", func(r *http.Request) { r.Header.Del("Authorization") }, 401},
		{"agent token", func(r *http.Request) { r.Header.Set("Authorization", "Bearer agent-token") }, 401},
		{"duplicate authorization", func(r *http.Request) { r.Header.Add("Authorization", "Bearer "+testControlToken) }, 401},
		{"wrong instance", func(r *http.Request) { r.Header.Set(instanceHeader, "another-instance") }, 409},
		{"no instance", func(r *http.Request) { r.Header.Del(instanceHeader) }, 409},
		{"duplicate instance", func(r *http.Request) { r.Header.Add(instanceHeader, testInstance) }, 409},
		{"query", func(r *http.Request) { r.URL.RawQuery = "secret=value" }, 404},
		{"unknown path", func(r *http.Request) { r.URL.Path = "/mcp" }, 404},
		{"wrong method", func(r *http.Request) { r.Method = http.MethodGet }, 405},
		{"request body", func(r *http.Request) { r.ContentLength = 1 }, 400},
		{"chunked body", func(r *http.Request) { r.TransferEncoding = []string{"chunked"} }, 400},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := testManagement(t, func() { t.Error("rejected request canceled lifetime") })
			r := managementRequest(http.MethodPost, "/_jev/stop")
			tt.edit(r)
			w := httptest.NewRecorder()
			m.Handler().ServeHTTP(w, r)
			if w.Code != tt.code || w.Body.String() != "{\"error\":\"management request rejected\"}\n" {
				t.Fatalf("response = %d %q", w.Code, w.Body.String())
			}
			if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Content-Type") != "application/json" {
				t.Fatal("missing private JSON response headers")
			}
			if err := m.MarkRunning(); err != nil {
				t.Fatal("rejection changed lifecycle")
			}
		})
	}
}

func TestManagementConcurrentStop(t *testing.T) {
	var calls atomic.Int32
	m := testManagement(t, func() { calls.Add(1) })
	var workers sync.WaitGroup
	for range 30 {
		workers.Go(func() {
			m.Handler().ServeHTTP(httptest.NewRecorder(), managementRequest(http.MethodPost, "/_jev/stop"))
		})
	}
	workers.Wait()
	if calls.Load() != 1 {
		t.Fatalf("cancellations = %d", calls.Load())
	}
	if m.MarkRunning() == nil {
		t.Fatal("prepared instance revived after stop")
	}
}

func TestManagementConfiguration(t *testing.T) {
	for _, address := range []string{"0.0.0.0:8080", "localhost:8080", "192.0.2.1:8080", "[::]:8080", "[::1%en0]:8080", "127.0.0.1:0", "127.0.0.1:65536", "127.0.0.1"} {
		if _, err := NewManagement(address, testInstance, testControlToken, func() {}); err == nil {
			t.Fatalf("accepted address %q", address)
		}
	}
	for _, value := range []string{"", "short", strings.Repeat("x", 257), strings.Repeat("x", 32) + "\n"} {
		if _, err := NewManagement(testAddress, testInstance, value, func() {}); err == nil {
			t.Fatal("accepted unsafe credential")
		}
	}
	if _, err := NewManagement(testAddress, "short", testControlToken, func() {}); err == nil {
		t.Fatal("accepted unsafe instance")
	}
	if _, err := NewManagement(testAddress, testInstance, testControlToken, nil); err == nil {
		t.Fatal("accepted nil cancellation")
	}
	m, err := NewManagement("[::1]:8080", testInstance, testControlToken, func() {})
	if err != nil {
		t.Fatal(err)
	}
	r := managementRequest(http.MethodGet, "/_jev/status")
	r.Host = "[::1]:8080"
	r.RemoteAddr = "[::1]:40123"
	r.Header.Set("Origin", "http://[::1]:8080")
	w := httptest.NewRecorder()
	m.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("IPv6 status = %d", w.Code)
	}
}

func TestManagementRejectsEscapedPathAliases(t *testing.T) {
	for _, path := range []string{"/%5fjev/status", "/_jev/%73tatus", "/%5fjev/stop", "/_jev/%73top"} {
		t.Run(path, func(t *testing.T) {
			m := testManagement(t, func() { t.Error("escaped alias canceled lifetime") })
			w := httptest.NewRecorder()
			m.Handler().ServeHTTP(w, managementRequest(http.MethodPost, path))
			if w.Code != http.StatusNotFound || w.Body.String() != "{\"error\":\"management request rejected\"}\n" {
				t.Fatalf("alias response = %d %q", w.Code, w.Body.String())
			}
			if err := m.MarkRunning(); err != nil {
				t.Fatal("escaped alias changed lifecycle")
			}
		})
	}
}
