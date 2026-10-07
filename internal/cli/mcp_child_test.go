//go:build linux || darwin || windows

package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bitbrew-dev/jevwise/internal/config"
	"github.com/bitbrew-dev/jevwise/internal/daemon"
	"github.com/bitbrew-dev/jevwise/internal/service"
)

func childBootstrap(t *testing.T) daemon.Bootstrap {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	return daemon.Bootstrap{Config: config.Config{APIKey: "private-bootstrap-key", BaseURL: "http://127.0.0.1:1", Model: "fixture", Provider: "jev", Timeout: 100 * time.Millisecond},
		Address: address, AgentToken: "private-agent-token", RuntimeDir: filepath.Join(t.TempDir(), "runtime"), Instance: strings.Repeat("a", 32), ControlToken: strings.Repeat("b", 64)}
}

func waitChildPhase(t *testing.T, controller *daemon.Controller, phase string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for ctx.Err() == nil {
		status, err := controller.Status(ctx)
		if err == nil && status.State == phase {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("child did not reach expected phase", phase)
}

func childToolCall(t *testing.T, boot daemon.Bootstrap) []byte {
	t.Helper()
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"decide","arguments":{"prompt":"private-prompt","options":["a","b"]}}}`
	request, _ := http.NewRequest(http.MethodPost, "http://"+boot.Address+"/mcp", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+boot.AgentToken)
	request.Header.Set("Accept", "application/json, text/event-stream")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Mcp-Protocol-Version", "2025-11-25")
	client := &http.Client{Timeout: 2 * time.Second}
	defer client.CloseIdleConnections()
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 4096))
	if err != nil || response.StatusCode != http.StatusOK || bytes.Contains(data, []byte("private")) {
		t.Fatal("unsafe child tool response", err, response.StatusCode)
	}
	return data
}

func TestMCPChildPreparedACKIndependentLifeAndAuthenticatedStop(t *testing.T) {
	boot := childBootstrap(t)
	store, err := daemon.OpenStore(boot.RuntimeDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var created, called, cleaned atomic.Int32
	factory := func(cfg config.Config) (service.DecisionService, func(), error) {
		created.Add(1)
		if cfg != boot.Config {
			t.Error("child did not use its validated bootstrap configuration")
		}
		return decideFunc(func(context.Context, service.Request) (service.Response, error) {
			called.Add(1)
			return service.Response{Model: "fixture", Choice: "a", Confidence: .8, Probabilities: map[string]float64{"a": .8, "b": .2}}, nil
		}), func() { cleaned.Add(1) }, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	input, output := io.Pipe()
	defer output.Close()
	cmd := NewRootWithFactory(factory)
	cmd.SetIn(input)
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	// Inherited configuration is deliberately unusable, but child never loads it.
	cmd.SetArgs([]string{daemon.ChildArgument, "--config", "/nonexistent", "--api-key", "wrong-key"})
	stopped := make(chan error, 1)
	go func() { stopped <- cmd.ExecuteContext(ctx) }()
	if err := daemon.WriteBootstrap(output, boot, daemon.ValidateBootstrap); err != nil {
		t.Fatal(err)
	}
	controller, err := daemon.NewController(boot.Address, boot.Instance, boot.ControlToken)
	if err != nil {
		t.Fatal(err)
	}
	waitChildPhase(t, controller, "prepared")
	if result := childToolCall(t, boot); !bytes.Contains(result, []byte(`"isError":true`)) || called.Load() != 0 {
		t.Fatal("prepared child dispatched a decision")
	}
	if err := daemon.WriteAcknowledgement(output); err != nil {
		t.Fatal(err)
	}
	_ = output.Close()
	waitChildPhase(t, controller, "running")
	state, key, err := daemon.Read(store)
	if err != nil || state.Address != boot.Address || state.Instance != boot.Instance || key != boot.ControlToken || state.Schema != 2 || state.PID != os.Getpid() {
		t.Fatal("published state changed", err)
	}
	// Parent pipe is gone and the short operation timeout does not own server life.
	time.Sleep(150 * time.Millisecond)
	if result := childToolCall(t, boot); bytes.Contains(result, []byte(`"isError":true`)) || called.Load() != 1 {
		t.Fatal("acknowledged child did not dispatch exactly once")
	}
	stop, err := controller.Stop(context.Background())
	if err != nil || stop.State != "stopping" {
		t.Fatal("authenticated stop failed", err)
	}
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(7 * time.Second):
		t.Fatal("child did not drain and clean up")
	}
	if created.Load() != 1 || cleaned.Load() != 1 || stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatal("child leaked output or mismanaged its service")
	}
	if _, _, err := daemon.Read(store); !errors.Is(err, daemon.ErrAbsent) {
		t.Fatal("child left instance state", err)
	}
	lease, err := store.Acquire()
	if err != nil {
		t.Fatal("child left its ownership lease", err)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestMCPChildHiddenHelpAndOwnedInput(t *testing.T) {
	factory := func(config.Config) (service.DecisionService, func(), error) {
		t.Fatal("help or invalid bootstrap initialized service")
		return nil, nil, nil
	}
	out, _, err := execute(NewRootWithFactory(factory), "--help")
	if err != nil || strings.Contains(out, daemon.ChildArgument) {
		t.Fatal("private child command appeared in root help", err)
	}
	cmd := NewRootWithFactory(factory)
	cmd.SetIn(strings.NewReader("private-bootstrap-content"))
	out, stderr, err := execute(cmd, daemon.ChildArgument)
	if err == nil || strings.Contains(err.Error(), "private") || out != "" || stderr != "" {
		t.Fatal("unowned input was accepted or echoed", err)
	}
}

func TestMCPChildPreparedStopClosesOpenParentPipe(t *testing.T) {
	boot := childBootstrap(t)
	var created, cleaned atomic.Int32
	factory := func(config.Config) (service.DecisionService, func(), error) {
		created.Add(1)
		return &childNilService{}, func() { cleaned.Add(1) }, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	input, output := io.Pipe()
	defer output.Close()
	cmd := NewRootWithFactory(factory)
	cmd.SetIn(input)
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{daemon.ChildArgument})
	stopped := make(chan error, 1)
	go func() { stopped <- cmd.ExecuteContext(ctx) }()
	if err := daemon.WriteBootstrap(output, boot, daemon.ValidateBootstrap); err != nil {
		t.Fatal(err)
	}
	controller, err := daemon.NewController(boot.Address, boot.Instance, boot.ControlToken)
	if err != nil {
		t.Fatal(err)
	}
	waitChildPhase(t, controller, "prepared")
	if _, err := controller.Stop(context.Background()); err != nil {
		t.Fatal("prepared stop failed", err)
	}
	// Parent still holds its writer open and sends no ACK or EOF.
	select {
	case err := <-stopped:
		if err == nil || strings.Contains(err.Error(), "private") {
			t.Fatal("prepared child exit was not safely rejected", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("prepared stop remained blocked on parent input")
	}
	if created.Load() != 1 || cleaned.Load() != 1 || stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatal("prepared stop leaked output or mismanaged service")
	}
	store, err := daemon.OpenStore(boot.RuntimeDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, _, err := daemon.Read(store); !errors.Is(err, daemon.ErrAbsent) {
		t.Fatal("prepared stop left instance state", err)
	}
	lease, err := store.Acquire()
	if err != nil {
		t.Fatal("prepared stop left ownership", err)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
}

type childNilService struct{}

func (*childNilService) Decide(context.Context, service.Request) (service.Response, error) {
	return service.Response{}, nil
}

func TestReadyMCPServiceAndTypedNilChecks(t *testing.T) {
	for _, svc := range []service.DecisionService{nil, decideFunc(nil), (*childNilService)(nil)} {
		if initializedMCPService(svc) {
			t.Fatal("nil service accepted")
		}
	}
	called := 0
	svc := decideFunc(func(context.Context, service.Request) (service.Response, error) {
		called++
		return service.Response{}, nil
	})
	if !initializedMCPService(svc) {
		t.Fatal("initialized service rejected")
	}
	var management *daemon.Management
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { management.Handler().ServeHTTP(w, r) }))
	defer server.Close()
	management, err := daemon.NewManagement(server.Listener.Addr().String(), strings.Repeat("a", 32), strings.Repeat("b", 64), func() {})
	if err != nil {
		t.Fatal(err)
	}
	guarded := readyMCPService{svc, management}
	if _, err := guarded.Decide(context.Background(), service.Request{}); err == nil || called != 0 {
		t.Fatal("prepared service dispatched a decision")
	}
	if err := management.MarkRunning(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := guarded.Decide(ctx, service.Request{}); !errors.Is(err, context.Canceled) || called != 0 {
		t.Fatal("canceled service dispatched a decision")
	}
	controller, err := daemon.NewController(server.Listener.Addr().String(), strings.Repeat("a", 32), strings.Repeat("b", 64))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if management.IsRunning() {
		t.Fatal("stopping management admitted decisions")
	}
	if _, err := guarded.Decide(context.Background(), service.Request{}); err == nil || called != 0 {
		t.Fatal("stopping service dispatched a decision")
	}
}
