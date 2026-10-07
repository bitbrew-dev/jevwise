# Connect a local agent to Jevwise

Use Codex or Claude Code on the **same computer** as the Jevwise MCP server. Jevwise serves Streamable HTTP at a literal-loopback `/mcp` endpoint, not stdio or legacy `/sse`. Do not register `jev mcp` as a stdio command or expose it through a public tunnel.

MCP is available in v1.1.0. Recorded PID/schema-2 state requires the newer unreleased checkout; see [release availability](../README.md#local-mcp).

## 1. Start Jevwise in the server terminal

Prerequisites:

- Install/build `jev` and configure the Jev provider as in the [README](../README.md#build-and-run).
- Prepare a random, separate agent bearer token in a protected current-user-only file. Follow the [token and runtime permissions](../README.md#credentials-state-and-safety), including Windows ACL requirements.
- Replace the example token-file path below with your own existing protected file. Do not reuse your upstream API key or the runtime management key.

```sh
# macOS/Linux shell; use jev.exe on Windows.
jev mcp --background --token-file /absolute/path/to/protected/agent-token \
  --listen 127.0.0.1:8080
jev mcp status
```

Foreground mode omits `--background`. Keep its terminal open; Ctrl+C stops it. If you set `--runtime-dir`, use the same directory for start, status and stop.

## 2. Load only the agent token in a separate client terminal

The agent needs the agent bearer token, **not** the Jev API key or management key. Use a fresh client terminal without `TYPESAFE_API_KEY` or `TS_JEV_API_KEY`. Do not paste token values into client commands, configuration files or chat.

macOS/Linux Bash or Zsh (remove the supported final LF/CRLF):

```sh
export JEV_MCP_TOKEN_FILE=/absolute/path/to/protected/agent-token
JEV_MCP_TOKEN="$(cat "$JEV_MCP_TOKEN_FILE")"
export JEV_MCP_TOKEN="${JEV_MCP_TOKEN%$'\r'}"
```

Windows PowerShell, using your protected token file:

```powershell
$env:JEV_MCP_TOKEN_FILE = 'C:\private\agent-token'
$env:JEV_MCP_TOKEN = (Get-Content -Raw -LiteralPath $env:JEV_MCP_TOKEN_FILE) -replace '\r?\n$', ''
```

Clients read the token from their process environment. Launch the client from this terminal; an already-running desktop app may not inherit it. Reopen the client after token changes. If an existing client server is already named `jevwise`, inspect it before adding or replacing configuration.

## 3. Choose a client

### Codex

Register the HTTP endpoint, storing the **environment variable name**, not its value:

```sh
codex mcp add jevwise --url http://127.0.0.1:8080/mcp \
  --bearer-token-env-var JEV_MCP_TOKEN
codex mcp get jevwise
```

Alternatively, merge this entry into your existing Codex `config.toml`, rather than replacing that file:

```toml
[mcp_servers.jevwise]
url = "http://127.0.0.1:8080/mcp"
bearer_token_env_var = "JEV_MCP_TOKEN"
```

`codex mcp get`/`list` inspect configuration, not a successful live handshake. Start Codex from the token-bearing terminal and use `/mcp` to check that `jevwise` is connected and advertises `decide`. Do not request a decision just to check connectivity.

Configuration location, project trust and available options are described in the [official OpenAI MCP documentation](https://developers.openai.com/codex/mcp). This is an MCP client connection: the Jevwise `--provider codex` placeholder remains unimplemented.

### Claude Code

In the project where you use Claude Code, add a local-scoped HTTP entry. The single quotes below preserve `${JEV_MCP_TOKEN}` as a literal environment reference, keeping the token out of command arguments and stored configuration:

```sh
# macOS/Linux shell
claude mcp add-json --scope local jevwise \
  '{"type":"http","url":"http://127.0.0.1:8080/mcp","headers":{"Authorization":"Bearer ${JEV_MCP_TOKEN}"}}'
claude mcp get jevwise
```

For a project-scoped setup, merge the following entry into `.mcp.json` without replacing other servers. This JSON also avoids native-command quoting differences on Windows:

```json
{
  "mcpServers": {
    "jevwise": {
      "type": "http",
      "url": "http://127.0.0.1:8080/mcp",
      "headers": {"Authorization": "Bearer ${JEV_MCP_TOKEN}"}
    }
  }
}
```

Review and approve project-scoped servers in Claude Code when prompted. An unapproved `.mcp.json` entry does not prove connectivity. Use `claude mcp get jevwise` or `/mcp` to inspect connection health, then confirm `decide` appears. Do not run `claude -p` merely to test discovery.

See [Claude Code's official MCP documentation](https://code.claude.com/docs/en/mcp) for scope, approvals and environment expansion. This connection does not implement the Jevwise `--provider claude` placeholder.

## 4. Verify without a paid decision

| Check | What it establishes |
| --- | --- |
| `jev mcp status` | The exact background instance authenticated its private management response. |
| Client connected status and `decide` discovery | The client can authenticate and discover the MCP tool. |
| Calling `decide` | A real decision request, which can incur upstream API charges. Not needed for setup verification. |

If you intentionally want a decision, supply a `prompt` and at least two distinct `options`. Keep tool approval enabled and review the call before sending it.

## Troubleshooting and cleanup

| Symptom | Check |
| --- | --- |
| Connection refused | Server still running; same computer, literal IP, port and `/mcp` path. |
| Authentication required | Agent token matches the server token; variable is set in the actual client process. Jevwise uses a bearer token, not an OAuth login. |
| Host/Origin rejected | Use the exact configured IP and port, not `localhost`, a proxy or a public URL. |
| Claude project server pending approval | Review the project and approve its `.mcp.json` entry in the client. |
| Cannot verify background state | Inspect manually; do not delete state or kill a PID based only on metadata. |

Stop with `jev mcp stop` using the same runtime directory. Acknowledgement is not a guarantee cleanup has already finished. Stop/restart after executable or credential changes. Use the matching/newer CLI for schema-2 instances.

Remove only the client entry you added: `codex mcp remove jevwise`, or `claude mcp remove jevwise --scope local` for the local-scoped example. For project JSON, remove only that entry. Removing client configuration does not stop Jevwise. Clear `JEV_MCP_TOKEN` from the client terminal when finished.
