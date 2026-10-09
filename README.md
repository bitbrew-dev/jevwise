# Jevwise

A Go SDK for TypeSafe AI and the `jevwise` decision CLI.

- SDK: `pkg/typesafe`, with typed questions, responses, raw metadata, and retries.
- CLI: send a prompt and options to Jev, then print the service's probabilities as JSON.
- Codex and Claude providers are placeholders only. No external sessions are started.

## Build and run

Use Go 1.27.2 or newer, matching `go.mod`. Run these commands from the checkout:

```sh
git clone https://github.com/bitbrew-dev/jevwise.git
cd jevwise
go build -o bin/jevwise ./cmd
bin/jevwise --help
bin/jevwise decide --help
```

Help does not read credentials, configuration files, or contact the API.

The current checkout uses `jevwise`; releases through v1.2.0 used `jev`. There is no `jev` alias. Existing configuration and the `jev` provider remain unchanged. Verify ownership before removing an older binary; another project may own the `jev` command.

```sh
export TYPESAFE_API_KEY='<your-key>'
bin/jevwise decide --prompt 'Which task should I tackle first?' \
  --option 'Fix the bug' --option 'Write documentation'

printf '%s' 'Which task should I tackle first?' | bin/jevwise decide --stdin \
  --option 'Fix the bug' --option 'Write documentation'
```

- Select exactly one of `--prompt` or `--stdin`. Stdin is read only when requested.
- Repeat `--option` at least twice. Labels must be distinct and nonblank after trimming.
- Commas remain part of a label; prompts preserve their original text and newlines.
- Stdin is limited to 1 MiB. Generic blocking input readers cannot be interrupted during a read; cancellation is checked before and after it.
- Stdout contains one JSON object with `model`, `choice`, `confidence`, `probabilities`, and optional `usage`. Values come from Jev, not local calculations.
- Errors go to stderr and cause a nonzero exit. Arbitrary service/input error text is not printed.
- `--timeout` covers the service operation, including retries and waits, after input is read.
- `--provider codex` and `--provider claude` return a clear not-implemented error without credentials or subprocesses.

## Version and platform builds

```sh
bin/jevwise version
bin/jevwise --version
make build-platform GOOS=windows GOARCH=amd64
make build-platform GOOS=windows GOARCH=arm64
```

- Both version forms report version, commit, and build date without credentials, config loading, or network requests.
- Plain `go build` reports `dev` with unknown metadata. Make injects metadata; only a clean exact tag is automatically a release version, otherwise `dev`.
- Release linker symbols are `github.com/bitbrew-dev/jevwise/internal/buildinfo.Version`, `.Commit`, and `.Date`.
- Make produces `build/jevwise-windows-amd64.exe` or `build/jevwise-windows-arm64.exe`. Make helpers require Unix shell tools; native Windows PowerShell can use `go build -o jevwise.exe ./cmd`, then `.\jevwise.exe --version`.

## Release checks and updates

```sh
bin/jevwise update --check
bin/jevwise update --check --timeout 20s
bin/jevwise update --timeout 60s # Install on supported standalone Linux/macOS builds
```

- `--check` checks the latest stable GitHub release without downloading a binary or changing files. No background checks, API credentials, or decision configuration are used.
- `--timeout` is a positive flag-only deadline for the whole update, default `10s`; decision timeout environment/config values do not affect updates. Increase it for slow downloads.
- Reports newer/current releases or no published release. Development/unknown builds report the latest release without inventing a version comparison.
- Accepts canonical stable `vX.Y.Z` tags only; prereleases/drafts and malformed or oversized responses are refused.
- Without `--check`, downloads and verifies a strictly newer release before replacement. Equal/newer installed versions are not rewritten or downgraded. Development, unknown and unstamped builds require manual installation.
- Automatic installation supports Linux/macOS amd64/arm64 only. Windows still supports version reporting and release checks, but updates are manual. Use your package manager for managed installations.
- After success, start a new invocation to use the new binary. An error saying the update was installed means publication succeeded but cancellation, cleanup or reporting failed: inspect `jevwise version` and any lock before retrying; no rollback occurs.

## Release binaries

The release-assets workflow attaches raw CLI binaries and `SHA256SUMS` to an existing stable [GitHub release](https://github.com/bitbrew-dev/jevwise/releases). It does not create versions or releases.

- Publishing supports the upstream repository whether public or private. Actions authenticates planning and upload; source-fetch credentials are temporary and never persisted or passed to builds.
- CLI release checks and self-update remain anonymous/public-only. For private releases, download assets through authenticated GitHub access and install manually.
- To retry asset publication without creating another release, run the release-assets workflow on `main` with the existing release tag. Existing assets are never overwritten or automatically resumed.

| Platform | Architectures | Asset pattern |
| --- | --- | --- |
| Linux | amd64, arm64 | `jevwise_vX.Y.Z_linux_ARCH` |
| macOS | amd64, arm64 | `jevwise_vX.Y.Z_darwin_ARCH` |
| Windows | amd64, arm64 | `jevwise_vX.Y.Z_windows_ARCH.exe` |

- Future releases use the patterns above; existing v1.0.0, v1.1.0 and v1.2.0 keep their legacy `jev_...` names and are not rewritten.
- Download the exact OS/architecture asset. Install it as `jevwise` (`jevwise.exe` on Windows); Unix users must make it executable. Verify `jevwise version` before use.
- For the first renamed release, install manually: older `jev` clients cannot find `jevwise_...` assets. Confirm binary ownership before removing an old copy; never remove another project's `jev`. Regenerate shell completions with `jevwise completion SHELL` and review saved scripts or agent commands.
- The new updater prefers `jevwise_...`, using `jev_...` only when the new exact platform name is absent from release metadata, never after a verification/download failure. It replaces its current executable path without renaming that path or installing a legacy alias.
- `SHA256SUMS` hashes the exact raw binary bytes. Verify the hash before use. Checksums detect corruption, not a compromised publisher; trust remains the repository and GitHub/TLS.
- The internal update downloader verifies exact platform asset names, metadata sizes and SHA-256 before returning bytes. Binary/manifest limits are 64 MiB/64 KiB; downloads use fixed release URLs and bounded HTTPS redirects to allowlisted GitHub hosts, without credentials or cookies.
- Release/Make builds embed a passive update stamp that survives stripping and path trimming. Version guards inspect Go module/platform metadata, recheck the candidate checksum, and reject equal/older or mismatched on-disk versions without executing binaries. Stamps are self-declared metadata, not signatures.
- Your manual semantic-release workflow still owns release creation. Asset publication runs after a published release, with a manual tag-based fallback. No release is created during development QA.
- Publication rejects existing target assets and uploads the checksum manifest last. If publication fails midway, inspect and remove incomplete assets manually before retrying; no automatic overwrite/delete/resume occurs.
- Native CI tests Linux, macOS, and Windows CLI execution. Windows arm64 is cross-built, not claimed as natively executed.
- On Windows, default config discovery uses `%USERPROFILE%\.config\ts-jev\config.toml` when `XDG_CONFIG_HOME` is unset.

## Replacement safety

- Automatic replacement is limited to standalone Linux/macOS binaries. Windows updates remain manual; the updater never uses sudo or elevates privileges.
- A per-executable `.<binary-name>.jev-update-lock` directory serializes cooperating updates. Crashed/stale locks require manual inspection and removal, never automatic deletion.
- Guards reject unsafe returned-path targets and changed versions. Only permission bits are preserved, not group ownership, ACLs or xattrs. A complete same-directory staging file is permissioned, synced and closed before renaming; failures before publication preserve the original. Errors after publication do not roll it back.
- Filesystem checks assume ordinary cooperative local storage, not hostile concurrent writers, mount changes or power-loss durability. Context cancellation is checked at safe boundaries; filesystem calls are not interruptible.
- Use your package manager for managed installs. `os.Executable` may resolve a symlink launch to its physical target, so the updater cannot reliably identify every symlink-invoked or package-managed installation.

## CLI configuration

Precedence: **changed flags > nonblank environment > TOML > defaults**.

| Setting | Flag | Environment | Default |
| --- | --- | --- | --- |
| API key | `--api-key` | `TS_JEV_API_KEY`, then `TYPESAFE_API_KEY` | Required for Jev |
| Base URL | `--base-url` | `TS_JEV_BASE_URL`, then `TYPESAFE_BASE_URL` | `https://api.typesafe.ai` |
| Model | `--model` | `TS_JEV_MODEL`, then `TYPESAFE_DEFAULT_MODEL` | `jev-latest` |
| Provider | `--provider` | `TS_JEV_PROVIDER` | `jev` |
| Operation timeout | `--timeout` | `TS_JEV_TIMEOUT` | `10s` |

- Explicit file: `--config /path/to/config.toml`. Missing or invalid explicit files fail.
- Otherwise: `$XDG_CONFIG_HOME/ts-jev/config.toml`, or `$HOME/.config/ts-jev/config.toml`. A missing discovered file is allowed.
- Start with [config.example.toml](config.example.toml). Prefer environment credentials rather than command-line secrets.

## Skill setup

```sh
bin/jevwise skill          # Default: .agent/skills/typesafe-ai/SKILL.md
bin/jevwise skill --agent  # Explicit default target
bin/jevwise skill --claude # .claude/skills/typesafe-ai/SKILL.md
bin/jevwise skill --force  # Replace an existing regular skill file
bin/jevwise skill --online # Print the upstream GitHub URL only
```

- Downloads the latest [upstream skill](https://github.com/typesafe-ai/skills/blob/main/skills/typesafe-ai/SKILL.md) unchanged, relative to the current project directory.
- Local installation is the default; `--local` selects it explicitly. `--online` and `--local`, or `--agent` and `--claude`, cannot be combined. Online mode rejects installation flags and performs no download or write.
- Hidden `--jev` selects the only supported skill. No decision credentials/configuration are read or sent. `--timeout` sets a `10s` default deadline for fetching and checks before file publication; filesystem calls are not forcibly interrupted.
- Downloads require HTTP 200 and valid UTF-8 Markdown/front matter, are limited to 1 MiB, and never follow redirects. Invalid downloads leave existing skills untouched.
- Existing files require `--force`. Static symlinks and nonregular destinations are refused; staging files are cleaned and complete bytes published without truncating the old inode.
- Native Unix supports force replacement. Windows force replacement and JS/WASI/Plan 9 installation are unsupported; filesystems without hardlinks fail safely.
- Filesystem operations assume ordinary native filesystems without hostile mounts/directory renames. Failure/cancellation after publication may mean the skill is already installed; no unsafe rollback is attempted.

## Go SDK quickstart

Import the public SDK from `github.com/bitbrew-dev/jevwise/pkg/typesafe`. The CLI executable is `jevwise`.

Migration: replace previous `github.com/benbenbang/ts-jev-go-sdk/pkg/typesafe` imports with the new path. No old-module compatibility alias is provided; SDK APIs and configuration names are unchanged.

```go
package main

import (
    "context"
    "fmt"

    "github.com/bitbrew-dev/jevwise/pkg/typesafe"
)

func main() {
    client, err := typesafe.NewClient(typesafe.ClientOptions{}) // TYPESAFE_API_KEY
    if err != nil { panic(err) }
    defer client.Close()

    ctx, cancel := context.WithTimeout(context.Background(), typesafe.DefaultTimeout)
    defer cancel()
    result, err := client.SystemOne(ctx, typesafe.SystemOneRequest{
        State: "Which task should I tackle first?",
        Questions: map[string]typesafe.Question{
            "decision": typesafe.Choice{Criteria: map[string]typesafe.JSONContent{
                "Fix the bug": nil, "Write documentation": nil,
            }},
        },
    })
    if err != nil { panic(err) }
    fmt.Println(result.Choices()["decision"].Probabilities)
}
```

| Need | API |
| --- | --- |
| Yes/no, choices, rubric scores | `Noul`, `Choice`, `Score` |
| Future/custom question fields | `RawQuestion` |
| Available models | `client.ListModels(ctx)` |
| Raw JSON and HTTP metadata | `SystemOneRaw`, `ListModelsRaw` |
| Independent custom schema | `SystemOneInto(ctx, request, &destination)` |
| Decode previously returned raw bytes | `raw.Decode(&destination)` |
| Per-call settings | Optional `RequestOptions` argument |

- SDK string options resolve explicit values > corresponding `TYPESAFE_*` environment > defaults. CLI-only `TS_JEV_*` variables do not configure SDK clients directly.
- `RequestOptions` supports model, timeout, headers, extra body, and retry policy. Model-list calls accept only timeout, headers, and retry.
- Model precedence: client default < nonempty request model < non-nil per-call model < extra-body model. A model pointer preserves explicit empty text.
- Extra body shallowly replaces fields, including nulls, after original state/questions are validated. Final overrides are not revalidated.
- Default timeout is 10 seconds **per attempt**. Set a caller context deadline to bound the entire SDK operation.
- Nil retry policy inherits defaults/client policy. `Retry: &typesafe.RetryPolicy{}` disables retries. Overrides replace the complete policy.
- Defaults: two retries, HTTP 408/429/500-599, connection/timeouts, 500 ms exponential backoff capped at 5 s, subtractive 25% jitter, 30 s retry budget.
- Retry budget includes attempts and waits but does not cancel in-flight work. Caller context expiration always stops further retries. Server retry delays are respected without the backoff cap.
- A predicate adds retryable errors to built-in rules. HTTP and typed/custom decoding share an attempt; standalone `RawResponse.Decode` never retries.
- Injected HTTP clients remain caller-owned. `Close` releases only SDK-owned idle connections. Configuration maps are copied; do not mutate inputs while constructing/preparing a call.
- Use `errors.As` with `APIError`, `ConnectionError`, `TimeoutError`, or `ResponseValidationError`. Inspectable raw bodies, headers, and causes may contain sensitive data; do not log them indiscriminately.

## Development

```sh
go mod tidy -diff
go test -count=1 ./...
go test -race -count=1 ./...
go vet ./...
go build ./...
```

Tests use injected services/transports and local HTTP fixtures, not paid API requests.

- Compatibility is pinned to Python SDK v0.7.2, revision `f078f1e208a0d885154dc758344ae4fce77ac168`.
- Release/update QA uses injected HTTP responses and temporary executables, never overwriting the developer's running CLI or publishing a release.

## Local MCP

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
bin/jevwise mcp --token-file /private/example/agent-token --listen 127.0.0.1:8080
bin/jevwise mcp --background --token-file /private/example/agent-token
bin/jevwise mcp status
bin/jevwise mcp stop
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

For token-safe Codex and Claude Code setup, discovery checks and cleanup, follow the [local agent onboarding guide](docs/mcp-agents.md).

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
