# Connect a local agent to Jevwise

Use Codex or Claude Code on the **same computer** as the Jevwise MCP server. Jevwise serves Streamable HTTP at a literal-loopback `/mcp` endpoint, not stdio or legacy `/sse`. Do not register `jevwise mcp` as a stdio command or expose it through a public tunnel.

MCP is available in v1.1.0. Recorded PID/schema-2 state requires v1.2.0 or newer; see [release availability](mcp.md).

The commands below use the renamed checkout CLI, `jevwise`; published releases through v1.2.0 use `jev`.

## 1. Start Jevwise in the server terminal

Prerequisites:

- Install/build `jevwise` and configure the Jev provider using the [installation guide](../README.md#installation) and [configuration guide](configuration.md).
- The current checkout creates/reuses a separate protected `agent.token` when no environment token or `--token-file` override is supplied. Follow the [token and runtime permissions](mcp.md#credentials-state-and-safety). Never reuse your API key or management key.

```sh
# macOS/Linux shell; use jevwise.exe on Windows.
jevwise mcp --background --listen 127.0.0.1:8080
jevwise mcp status
```

Foreground mode omits `--background`. Keep its terminal open; Ctrl+C stops it. If you set `--runtime-dir`, use the same directory for start, status, stop and `token-path`.

## 2. Load only the agent token in a separate client terminal

The agent needs the agent bearer token, **not** the Jev API key or management key. Use a fresh client terminal without upstream credentials (`JEV_API_KEY`, `JEVWISE_API_KEY`, `TS_JEV_API_KEY`, `TYPESAFE_API_KEY`, or any custom API-key variable). Do not paste token values into client commands, configuration files or chat.

macOS/Linux Bash or Zsh (remove the supported final LF/CRLF):

```sh
export JEVWISE_MCP_TOKEN_FILE="$(jevwise mcp token-path)"
JEVWISE_MCP_TOKEN="$(cat "$JEVWISE_MCP_TOKEN_FILE")"
export JEVWISE_MCP_TOKEN="${JEVWISE_MCP_TOKEN%$'\r'}"
```

Windows PowerShell, using your protected token file:

```powershell
$env:JEVWISE_MCP_TOKEN_FILE = (jevwise mcp token-path)
$env:JEVWISE_MCP_TOKEN = (Get-Content -Raw -LiteralPath $env:JEVWISE_MCP_TOKEN_FILE) -replace '\r?\n$', ''
```

These examples use the automatic file after successful startup. With an explicit token override, load that token instead: `token-path` always reports the automatic path, not the active override.

Clients read the token from their process environment. Launch the client from this terminal; an already-running desktop app may not inherit it. Reopen the client after token changes. If an existing client server is already named `jevwise`, inspect it before adding or replacing configuration.

## 3. Choose a client

### One-command registration (current checkout)

After starting MCP and loading the token in your client terminal, run:

```sh
jevwise mcp install codex
jevwise mcp install claude
# Match a custom server port:
jevwise mcp install codex --listen 127.0.0.1:9090
# Claude-only scopes:
jevwise mcp install claude --scope local   # Current project, private settings
jevwise mcp install claude --scope project # Shared .mcp.json, requires client approval
```

- Both default to user-wide settings. Requires the selected `codex` or `claude` executable on PATH; Claude Code must support `--bare`. Standard client configuration overrides (`CODEX_HOME`, `CLAUDE_CONFIG_DIR`) are honored.
- Uses native registration commands, preserving unrelated settings. An existing `jevwise` entry is refused; inspect it before intentionally removing/updating it with the client's own commands. Configuration assumes trusted clients and cooperative writers; do not run competing installers/settings edits concurrently.
- Stores `JEVWISE_MCP_TOKEN` as an environment reference, never its value. The installer does not export the token into your shell, read Jev configuration, start MCP, verify connectivity or make a paid decision. Use section 2 to load the token before launching your client. `--listen` must match the running server; it does not change the server port.
- Child processes do not inherit API keys or MCP tokens. Client output is bounded and withheld, including diagnostics; failures may have partially registered an entry, so inspect settings before retrying. `--timeout` bounds registration (default `10s`), not a server/model request. Only the direct process is managed, not wrapper descendants.
- Manual setup remains available below. Installed registration is not proof of a live authenticated connection; verify via `/mcp` without calling `decide`.

### Codex

Register the HTTP endpoint, storing the **environment variable name**, not its value:

```sh
codex mcp add jevwise --url http://127.0.0.1:8080/mcp \
  --bearer-token-env-var JEVWISE_MCP_TOKEN
codex mcp get jevwise
```

Alternatively, merge this entry into your existing Codex `config.toml`, rather than replacing that file:

```toml
[mcp_servers.jevwise]
url = "http://127.0.0.1:8080/mcp"
bearer_token_env_var = "JEVWISE_MCP_TOKEN"
```

`codex mcp get`/`list` inspect configuration, not a successful live handshake. Start Codex from the token-bearing terminal and use `/mcp` to check that `jevwise` is connected and advertises `decide`. Do not request a decision just to check connectivity.

Configuration location, project trust and available options are described in the [official OpenAI MCP documentation](https://developers.openai.com/codex/mcp). This is an MCP client connection: the Jevwise `--provider codex` placeholder remains unimplemented.

### Claude Code

In the project where you use Claude Code, add a local-scoped HTTP entry. The single quotes below preserve `${JEVWISE_MCP_TOKEN}` as a literal environment reference, keeping the token out of command arguments and stored configuration:

```sh
# macOS/Linux shell
claude mcp add-json --scope local jevwise \
  '{"type":"http","url":"http://127.0.0.1:8080/mcp","headers":{"Authorization":"Bearer ${JEVWISE_MCP_TOKEN}"}}'
claude mcp get jevwise
```

For a project-scoped setup, merge the following entry into `.mcp.json` without replacing other servers. This JSON also avoids native-command quoting differences on Windows:

```json
{
  "mcpServers": {
    "jevwise": {
      "type": "http",
      "url": "http://127.0.0.1:8080/mcp",
      "headers": {"Authorization": "Bearer ${JEVWISE_MCP_TOKEN}"}
    }
  }
}
```

Review and approve project-scoped servers in Claude Code when prompted. An unapproved `.mcp.json` entry does not prove connectivity. Use `claude mcp get jevwise` or `/mcp` to inspect connection health, then confirm `decide` appears. Do not run `claude -p` merely to test discovery.

See [Claude Code's official MCP documentation](https://code.claude.com/docs/en/mcp) for scope, approvals and environment expansion. This connection does not implement the Jevwise `--provider claude` placeholder.

## 4. Verify without a paid decision

| Check | What it establishes |
| --- | --- |
| `jevwise mcp status` | The exact background instance authenticated its private management response. |
| Client connected status and `decide` discovery | The client can authenticate and discover the MCP tool. |
| Calling `decide` | A real decision request, which can incur upstream API charges. Not needed for setup verification. |

If you intentionally want a decision, supply a `prompt` and at least two distinct `options`. Keep tool approval enabled and review the call before sending it.

### Optional installed-client smoke test (source checkout)

From the source checkout, with both `codex` and `claude` installed on PATH, run in a macOS/Linux shell:

```sh
JEVWISE_MCP_AGENT_SMOKE=1 go test -mod=readonly -run '^TestRealAgentMCPDiscovery$' -count=1 -v ./internal/mcpserver
```

- Checks Codex tool discovery and Claude Code connection health against the production MCP HTTP handler with a synthetic service. It asserts zero tool calls, decisions and model-backend requests; no model prompts/turns are started.
- Uses temporary client homes/settings and a scrubbed environment, with Codex file-only credential storage and Claude bare mode. It does not register servers in your normal client configuration. Managed host policies still apply; this is not an OS sandbox.
- Normal test runs skip these probes. Explicit opt-in fails when either client is missing, incompatible or cannot connect; a successful command exit alone is not treated as proof of connectivity. Verified on macOS with Codex 0.160.1 and Claude Code 2.1.292.

## Troubleshooting and cleanup

| Symptom | Check |
| --- | --- |
| Connection refused | Server still running; same computer, literal IP, port and `/mcp` path. |
| Authentication required | Agent token matches the server token; variable is set in the actual client process. Jevwise uses a bearer token, not an OAuth login. |
| Host/Origin rejected | Use the exact configured IP and port, not `localhost`, a proxy or a public URL. |
| Claude project server pending approval | Review the project and approve its `.mcp.json` entry in the client. |
| Cannot verify background state | Inspect manually; do not delete state or kill a PID based only on metadata. |

Stop with `jevwise mcp stop` using the same runtime directory. Acknowledgement is not a guarantee cleanup has already finished. Stop/restart after executable or credential changes. Use the matching/newer CLI for schema-2 instances.

Remove only the client entry you added: `codex mcp remove jevwise`, or `claude mcp remove jevwise --scope local` for the local-scoped example. For project JSON, remove only that entry. Removing client configuration does not stop Jevwise. Clear `JEVWISE_MCP_TOKEN` from the client terminal when finished.
