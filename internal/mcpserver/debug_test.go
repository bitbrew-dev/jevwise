package mcpserver

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bitbrew-dev/jevwise/internal/debuglog"
	"github.com/bitbrew-dev/jevwise/internal/service"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestDebugHTTPRejectsWithoutLoggingPeerData(t *testing.T) {
	var logs bytes.Buffer
	ctx := debuglog.WithWriter(context.Background(), &logs)
	server, err := New(ctx, decisionFunc(func(context.Context, service.Request) (service.Response, error) {
		t.Fatal("rejected request dispatched")
		return service.Response{}, nil
	}), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewHTTP(server, "127.0.0.1:8080", "private-token")
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/mcp", strings.NewReader("private-payload")).WithContext(ctx)
	r.Header.Set("Authorization", "Bearer private-invalid-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, r)
	if response.Code != 401 || !strings.Contains(logs.String(), `"value":401`) || strings.Contains(logs.String(), "private") || strings.Contains(logs.String(), "127.0.0.1") {
		t.Fatal("rejection diagnostics missing or exposed peer data")
	}
}

func TestDebugToolCancellation(t *testing.T) {
	var logs bytes.Buffer
	lifetime := debuglog.WithWriter(context.Background(), &logs)
	ctx, cancel := context.WithCancel(lifetime)
	cancel()
	result, err := decisionHandler(lifetime, nil, time.Second)(ctx, &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: []byte(`{"prompt":"private","options":["a","b"]}`)},
	})
	if err != nil || !result.IsError || !strings.Contains(logs.String(), `"canceled":true`) || strings.Contains(logs.String(), "private") {
		t.Fatal("tool cancellation diagnostic lost or leaked input")
	}
}
