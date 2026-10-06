package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bitbrew-dev/jevwise/internal/service"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type agentTransport struct{ base http.RoundTripper }

func (t agentTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer token")
	return t.base.RoundTrip(r)
}

func TestHTTPClientNegotiationAndDecisions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var calls atomic.Int32
	server, err := New(ctx, decisionFunc(func(context.Context, service.Request) (service.Response, error) {
		calls.Add(1)
		return service.Response{Choice: "a", Confidence: .75, Probabilities: map[string]float64{"a": .75, "b": .25}}, nil
	}), time.Second)
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
	defer fixture.Close()
	client := &http.Client{Transport: agentTransport{fixture.Client().Transport}}
	versions := append(mcp.SupportedProtocolVersions(), "2030-01-01")
	for _, version := range versions {
		t.Run(version, func(t *testing.T) {
			session, err := mcp.NewClient(&mcp.Implementation{Name: "fixture", Version: "dev"}, nil).Connect(ctx,
				&mcp.StreamableClientTransport{Endpoint: fixture.URL + "/mcp", HTTPClient: client}, &mcp.ClientSessionOptions{ProtocolVersion: version})
			if err != nil {
				t.Fatal("negotiation failed", err)
			}
			defer session.Close()
			want := version
			if version == "2030-01-01" {
				want = mcp.SupportedProtocolVersions()[0]
			}
			if session.InitializeResult().ProtocolVersion != want {
				t.Fatal("wrong negotiated revision")
			}
			tools, err := session.ListTools(ctx, nil)
			if err != nil || len(tools.Tools) != 1 || tools.Tools[0].Name != "decide" {
				t.Fatal("tool discovery failed", err)
			}
			result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "decide", Arguments: map[string]any{"prompt": "valid", "options": []string{"a", "b"}}})
			if err != nil || result.IsError {
				t.Fatal("HTTP decision failed", err)
			}
			var response service.Response
			data, _ := json.Marshal(result.StructuredContent)
			if json.Unmarshal(data, &response) != nil || response.Probabilities["a"] != .75 || response.Choice != "a" {
				t.Fatal("HTTP result changed")
			}
		})
	}
	if int(calls.Load()) != len(versions) {
		t.Fatal("unexpected decision count")
	}
}
