package mcpserver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bitbrew-dev/jevwise/internal/service"
)

// Explicitly opted-in discovery only: no prompts, model turns, or user auth.
func TestRealAgentMCPDiscovery(t *testing.T) {
	if os.Getenv("JEVWISE_MCP_AGENT_SMOKE") != "1" {
		t.Skip("set JEVWISE_MCP_AGENT_SMOKE=1 for installed-client discovery")
	}
	for _, agent := range []string{"codex", "claude"} {
		t.Run(agent, func(t *testing.T) {
			binary, err := exec.LookPath(agent)
			if err != nil {
				t.Fatal("opt-in requires installed", agent)
			}
			binary, err = filepath.Abs(binary)
			if err != nil {
				t.Fatal("cannot resolve agent executable")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			var initialized, listed, calls, decisions, backend atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				backend.Add(1)
				http.Error(w, "model requests forbidden", http.StatusForbidden)
			}))
			defer upstream.Close()
			server, err := New(ctx, decisionFunc(func(context.Context, service.Request) (service.Response, error) {
				decisions.Add(1)
				return service.Response{}, errors.New("smoke forbids decisions")
			}), time.Second)
			if err != nil {
				t.Fatal("cannot construct synthetic MCP server")
			}
			const token = "synthetic-agent-smoke-token"
			fixture := httptest.NewUnstartedServer(nil)
			handler, err := NewHTTP(server, fixture.Listener.Addr().String(), token)
			if err != nil {
				fixture.Close()
				t.Fatal("cannot construct production HTTP handler")
			}
			fixture.Config.MaxHeaderBytes = HTTPHeaderLimit
			fixture.Config.ReadHeaderTimeout = 2 * time.Second
			fixture.Config.ReadTimeout = 5 * time.Second
			fixture.Config.WriteTimeout = 5 * time.Second
			fixture.Config.IdleTimeout = 2 * time.Second
			fixture.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body []byte
				if r.Body != nil {
					body, _ = io.ReadAll(io.LimitReader(r.Body, inputLimit+1))
					_ = r.Body.Close()
				}
				r.Body = io.NopCloser(bytes.NewReader(body))
				var message struct{ Method string }
				_ = json.Unmarshal(body, &message)
				if message.Method == "tools/call" {
					calls.Add(1)
				}
				status := &smokeStatusWriter{ResponseWriter: w}
				handler.ServeHTTP(status, r)
				// Production HTTP 200 already proves bearer authentication.
				if status.status == http.StatusOK {
					switch message.Method {
					case "initialize":
						initialized.Add(1)
					case "tools/list":
						listed.Add(1)
					}
				}
			})
			fixture.Start()
			defer func() { cancel(); fixture.CloseClientConnections(); fixture.Close() }()
			t.Cleanup(func() {
				if calls.Load() != 0 || decisions.Load() != 0 || backend.Load() != 0 {
					t.Errorf("forbidden activity: tool calls=%d decisions=%d backend requests=%d", calls.Load(), decisions.Load(), backend.Load())
				}
			})
			home := t.TempDir()
			for _, dir := range []string{"codex", "claude", "config", "cache", "data", "tmp"} {
				if os.Mkdir(filepath.Join(home, dir), 0700) != nil {
					t.Fatal("cannot prepare isolated directories")
				}
			}
			env := []string{"PATH=" + filepath.Dir(binary) + string(os.PathListSeparator) + "/usr/bin:/bin", "HOME=" + home,
				"USERPROFILE=" + home, "CODEX_HOME=" + filepath.Join(home, "codex"), "CLAUDE_CONFIG_DIR=" + filepath.Join(home, "claude"),
				"XDG_CONFIG_HOME=" + filepath.Join(home, "config"), "XDG_CACHE_HOME=" + filepath.Join(home, "cache"),
				"XDG_DATA_HOME=" + filepath.Join(home, "data"), "TMPDIR=" + filepath.Join(home, "tmp"), "TEMP=" + filepath.Join(home, "tmp"),
				"JEV_MCP_TOKEN=" + token, "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1", "DISABLE_AUTOUPDATER=1",
				"DISABLE_TELEMETRY=1", "DISABLE_ERROR_REPORTING=1", "NO_COLOR=1"}
			command := func(args ...string) *exec.Cmd {
				cmd := exec.CommandContext(ctx, binary, args...)
				cmd.Dir, cmd.Env, cmd.WaitDelay = home, env, 2*time.Second
				return cmd
			}
			endpoint := fixture.URL + "/mcp"
			if agent == "codex" {
				configuration := fmt.Sprintf(`cli_auth_credentials_store = "file"
mcp_oauth_credentials_store = "file"
model_provider = "local-smoke"
[analytics]
enabled = false
[model_providers.local-smoke]
name = "Synthetic smoke backend"
base_url = %q
wire_api = "responses"
requires_openai_auth = false
[mcp_servers.jevwise]
url = %q
bearer_token_env_var = "JEV_MCP_TOKEN"
startup_timeout_sec = 10
`, upstream.URL, endpoint)
				if os.WriteFile(filepath.Join(home, "codex", "config.toml"), []byte(configuration), 0600) != nil {
					t.Fatal("cannot write isolated Codex configuration")
				}
				smokeCodex(t, ctx, command("app-server", "--listen", "stdio://"))
			} else {
				configuration := fmt.Sprintf(`{"type":"http","url":%q,"headers":{"Authorization":"Bearer ${JEV_MCP_TOKEN}"}}`, endpoint)
				for _, args := range [][]string{{"--bare", "mcp", "add-json", "--scope", "local", "jevwise", configuration},
					{"--bare", "mcp", "get", "jevwise"}, {"--bare", "mcp", "list"}} {
					var output smokeOutput
					cmd := command(args...)
					cmd.Stdout, cmd.Stderr = &output, &output
					if err := cmd.Run(); err != nil {
						t.Fatal("Claude discovery command failed; client output withheld")
					}
					if args[2] != "add-json" && !strings.Contains(output.String(), "Connected") {
						t.Fatal("Claude did not report connected; client output withheld")
					}
				}
			}
			if agent == "codex" && initialized.Load() == 0 {
				t.Fatal("Codex did not complete authenticated initialization")
			}
			if listed.Load() == 0 {
				t.Fatal("real client did not perform authenticated tool discovery")
			}
			t.Logf("authenticated initialize=%d tools/list=%d; Claude health does not claim catalog discovery", initialized.Load(), listed.Load())
		})
	}
}

type smokeStatusWriter struct {
	http.ResponseWriter
	status int
}

func (w *smokeStatusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
func (w *smokeStatusWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(data)
}
func (w *smokeStatusWriter) Flush() { w.ResponseWriter.(http.Flusher).Flush() }

type smokeOutput struct {
	mu sync.Mutex
	bytes.Buffer
}

func (w *smokeOutput) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := len(data)
	if remaining := 4096 - w.Len(); remaining > 0 {
		if len(data) > remaining {
			data = data[:remaining]
		}
		_, _ = w.Buffer.Write(data)
	}
	return n, nil
}
func (w *smokeOutput) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.Buffer.String()
}
func smokeCodex(t *testing.T, ctx context.Context, cmd *exec.Cmd) {
	t.Helper()
	cmd.Stderr = io.Discard
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal("cannot prepare Codex stdin")
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal("cannot prepare Codex stdout")
	}
	if cmd.Start() != nil {
		t.Fatal("cannot start installed Codex")
	}
	stop := context.AfterFunc(ctx, func() { _ = input.Close(); _ = output.Close() })
	defer stop()
	defer func() { _ = input.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	scanner := bufio.NewScanner(output)
	scanner.Buffer(make([]byte, 4096), 128<<10)
	rpc := func(id int, method string, params any) json.RawMessage {
		t.Helper()
		message := map[string]any{"method": method, "params": params}
		if id != 0 {
			message["id"] = id
		}
		if json.NewEncoder(input).Encode(message) != nil {
			t.Fatal("cannot write Codex discovery request")
		}
		if id == 0 {
			return nil
		}
		for count := 0; count < 100 && scanner.Scan(); count++ {
			var response struct {
				ID     int
				Result json.RawMessage
				Error  json.RawMessage
			}
			if json.Unmarshal(scanner.Bytes(), &response) != nil {
				t.Fatal("invalid Codex discovery framing")
			}
			if response.ID == id {
				if len(response.Error) != 0 || len(response.Result) == 0 {
					t.Fatal("Codex discovery RPC failed; no raw response logged")
				}
				return response.Result
			}
		}
		t.Fatal("Codex discovery response missing; client output withheld")
		return nil
	}
	rpc(1, "initialize", map[string]any{"clientInfo": map[string]string{"name": "jevwise-smoke", "version": "1"},
		"capabilities": map[string]bool{"experimentalApi": true}})
	rpc(0, "initialized", map[string]any{})
	var status struct {
		Data []struct {
			Name               string
			Tools              map[string]struct{ Name string }
			ServerCapabilities json.RawMessage
		}
	}
	data := rpc(2, "mcpServerStatus/list", map[string]any{"serverName": "jevwise", "detail": "toolsAndAuthOnly"})
	if json.Unmarshal(data, &status) != nil || len(status.Data) != 1 || status.Data[0].Name != "jevwise" {
		t.Fatal("Codex did not discover the isolated server")
	}
	item := status.Data[0]
	if len(item.ServerCapabilities) == 0 || string(item.ServerCapabilities) == "null" || len(item.Tools) != 1 {
		t.Fatal("Codex catalog was not initialized or had unexpected tools")
	}
	for _, tool := range item.Tools {
		if tool.Name != "decide" {
			t.Fatal("Codex did not discover decide")
		}
	}
}
