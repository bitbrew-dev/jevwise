# Jevwise

A Go SDK for TypeSafe AI and the `jev` decision CLI.

- SDK: `pkg/typesafe`, with typed questions, responses, raw metadata, and retries.
- CLI: send a prompt and options to Jev, then print the service's probabilities as JSON.
- Codex and Claude providers are placeholders only. No external sessions are started.

## Build and run

Use Go 1.27.1 or newer, matching `go.mod`. Run these commands from the checkout:

```sh
git clone https://github.com/bitbrew-dev/jevwise.git
cd jevwise
go build -o bin/jev ./cmd
bin/jev --help
bin/jev decide --help
```

Help does not read credentials, configuration files, or contact the API.

```sh
export TYPESAFE_API_KEY='<your-key>'
bin/jev decide --prompt 'Which task should I tackle first?' \
  --option 'Fix the bug' --option 'Write documentation'

printf '%s' 'Which task should I tackle first?' | bin/jev decide --stdin \
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
bin/jev skill          # Default: .agent/skills/typesafe-ai/SKILL.md
bin/jev skill --agent  # Explicit default target
bin/jev skill --claude # .claude/skills/typesafe-ai/SKILL.md
bin/jev skill --force  # Replace an existing regular skill file
bin/jev skill --online # Print the upstream GitHub URL only
```

- Downloads the latest [upstream skill](https://github.com/typesafe-ai/skills/blob/main/skills/typesafe-ai/SKILL.md) unchanged, relative to the current project directory.
- Local installation is the default; `--local` selects it explicitly. `--online` and `--local`, or `--agent` and `--claude`, cannot be combined. Online mode rejects installation flags and performs no download or write.
- Hidden `--jev` selects the only supported skill. No decision credentials/configuration are read or sent. `--timeout` sets a `10s` default deadline for fetching and checks before file publication; filesystem calls are not forcibly interrupted.
- Downloads require HTTP 200 and valid UTF-8 Markdown/front matter, are limited to 1 MiB, and never follow redirects. Invalid downloads leave existing skills untouched.
- Existing files require `--force`. Static symlinks and nonregular destinations are refused; staging files are cleaned and complete bytes published without truncating the old inode.
- Native Unix supports force replacement. Windows force replacement and JS/WASI/Plan 9 installation are unsupported; filesystems without hardlinks fail safely.
- Filesystem operations assume ordinary native filesystems without hostile mounts/directory renames. Failure/cancellation after publication may mean the skill is already installed; no unsafe rollback is attempted.

## Go SDK quickstart

The repository name is `jevwise`, but the declared Go module is still `github.com/benbenbang/ts-jev-go-sdk`. Its rename is a separate follow-up. The example below uses the current import path inside this checkout.

```go
package main

import (
    "context"
    "fmt"

    "github.com/benbenbang/ts-jev-go-sdk/pkg/typesafe"
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
- [Compatibility notes](docs/internal/compatibility.md) describe Go-specific validation, ownership, decoding, and retry differences.
- Remaining follow-ups: module rename, version display, release checking, and verified self-update. These features are not implemented yet.
