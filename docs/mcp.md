# Local MCP

[Documentation index](../README.md#documentation)

MCP is included in v1.1.0; diagnostic PID tracking and schema-2 state are included in v1.2.0. The executable rename is newer than those releases.

| Capability | v1.0.0 | v1.1.0 | v1.2.0 | Current checkout |
| --- | --- | --- | --- | --- |
| Foreground and background MCP | Not included | Included | Included | Included |
| Authenticated status and stop | Not included | Included | Included | Included |
| Recorded child PID and schema-2 state | Not included | Not included | Included | Included |

The examples below describe the current checkout, using synthetic paths and addresses. They are not services started by this documentation.

### Start and manage

Configure upstream credentials as for `jevwise decide`. Separately prepare a random agent bearer token in a protected, current-user-only file. Do not reuse your API key.

```sh
# Synthetic examples: supply your own protected token file before running.
jevwise mcp --token-file /private/example/agent-token --listen 127.0.0.1:8080
jevwise mcp --background --token-file /private/example/agent-token
jevwise mcp status
jevwise mcp stop
```

| Command or flag | Behavior |
| --- | --- |
| `jevwise mcp` | Foreground server; prints the public `/mcp` endpoint after startup. Ctrl+C stops it. |
| `--background` | Starts a terminal-independent child after authenticated readiness and ownership checks. |
| `jevwise mcp status` | Authenticates the exact instance and reports its startup/running state. v1.2.0 and newer also show the recorded PID when available. |
| `jevwise mcp stop` | Authenticates the exact instance and requests shutdown; an acknowledgement is not a promise that cleanup has already finished. |
| `--listen` | Literal loopback IP and nonzero port; default `127.0.0.1:8080`. |
| `--runtime-dir` | Private background state directory; default `os.UserCacheDir()/jevwise-mcp`. Use the same override for start, status and stop. |
| `--token-file` | Explicit protected agent-token file, overriding `JEV_MCP_TOKEN`; unreadable or unsafe files fail without fallback. |

- Without `--token-file`, startup reads `JEV_MCP_TOKEN`. Tokens must be nonempty printable ASCII without whitespace; a file may end in one LF or CRLF. No secret-valued token flag exists.
- Keep credentials out of command arguments and shell history. Use protected files or appropriately secured environment configuration instead of `--api-key`.
- `status` and `stop` do not load decision configuration, API keys, the decision service or the agent token file. They use the private runtime management key, with no upstream API request.
- Background mode does not install a login item, reboot service or automatic restart. On Windows, a restrictive parent Job Object can prevent detachment or terminate the child when its parent exits.
- A repeated background start returns an endpoint only for an authenticated running instance. If it is still preparing or stopping, wait and check status instead.
- Stop and restart after changing credentials, decision configuration or the executable. Updating a binary does not change an already-running process.
- PID support (v1.2.0 and newer): background children save their own PID in private `<runtime-dir>/state.json`; authenticated status prints `PID: NUMBER (inspection only)`. Normal owned shutdown removes that state along with the key and lease. PID reuse means this number is not proof of ownership or liveness: use `jevwise mcp stop`, not a blind PID-based kill.
- State format (v1.2.0 and newer): updated children write schema-2 state. The updated CLI still manages schema-1 instances without displaying a PID; older CLIs, including v1.1.0, cannot read schema-2 state. Stop an old instance before upgrading/restarting, and use the matching or newer CLI to manage a new child.

### Agent connection and limits

For token-safe Codex and Claude Code setup, discovery checks and cleanup, follow the [local agent onboarding guide](mcp-agents.md).

Connect an HTTP-capable local agent to `http://127.0.0.1:8080/mcp`, supplying `Authorization: Bearer <agent-token>`. The only tool is `decide`: send `prompt` and at least two distinct `options`. Results contain Jev probabilities and optional usage; ordinary decisions can incur upstream API charges.

- Stateless Streamable HTTP uses POST with SSE responses, not legacy `/sse` or standalone GET streams. GET/DELETE do not create sessions. Supported revisions: 2026-07-28, 2025-11-25, 2025-06-18, 2025-03-26 and 2024-11-05; JSON-RPC batches are rejected.
- Only literal loopback addresses are supported. Requests need the exact configured Host and, when supplied, the exact `http://IP:port` Origin. Proxy headers do not establish trust; this is not a public-network deployment mode.
- Limits: 1 MiB encoded request, 8 MiB encoded decision JSON and 32 MiB SSE event. Escaped text and structured output appear together, so an event can be larger than the decision JSON. Go SDK clients needing maximum-size results should set `StreamableClientTransport.MaxEventSize: 32 << 20`; its default is 16 MiB.
- Four immediate request slots prevent queued decisions. Requests beyond capacity are rejected, not delayed for a later paid call.
- `--timeout` bounds each decision, including retries and waits, not daemon lifetime. Modern request disconnect cancels work; older protocols rely on decision deadlines and lifetime shutdown. Shutdown cancels active decisions and drains connections for up to five seconds.
- Services, startup callbacks and output writers must cooperate with cancellation. Blocking caller code and filesystem calls cannot be forcibly interrupted safely.

### Credentials, state and safety

| Credential | Purpose | Where it belongs |
| --- | --- | --- |
| Upstream API key | Server-to-Jev decisions | Existing decision configuration; never give it to the agent. |
| Agent bearer token | Agent-to-local `/mcp` access | `JEV_MCP_TOKEN` or an explicit protected token file. |
| Management key | Status/stop and instance verification | Separate private runtime file; never give it to the agent or transmit it as a bearer token. |

- Background bootstrap passes secrets through owned anonymous pipes, not child arguments, public state or logs. State contains bounded instance metadata, never credentials; the post-v1.1.0 checkout also records an inspection-only PID. Agent and API credentials must differ; the management key is separately generated.
- Management requests and responses use nonce-bound HMAC proofs for the exact instance, method and path. The private control key itself is never transmitted. Status/stop do not trust PIDs, signal unrelated processes or follow HTTP redirects/proxies.
- Fresh nonces prevent accepting replayed responses. Signed requests are not replay-cached; stop is idempotent and bound to the exact instance.
- Child stdout/stderr are discarded, with no retained output logs. Diagnostics are bounded, fixed safe messages through authenticated status/startup handling; prompts and arbitrary backend errors are not logged.
- Runtime files and token files must be owner-private. Unsafe permissions, links or ACLs are refused, not repaired. macOS refuses extended ACLs; Windows requires a protected current-user-only runtime-directory DACL and protected token-file DACLs. Background publication requires filesystem hardlink support and refuses unsupported storage.
- Existing locks, partial state, mismatched instances and unverified listeners require manual inspection. No automatic stale-lock deletion or PID-based kill occurs. Do not delete a runtime directory until you have established that its instance is stopped.
- Trusted runtime parent directories must already exist; startup creates only the private runtime leaf.
- Filesystem protection assumes caller-trusted runtime/token parent directories and ordinary cooperative local storage. It does not defend against hostile mount changes or ancestor-directory renaming. Local HTTP is not TLS, and same-user compromise is outside this boundary.
