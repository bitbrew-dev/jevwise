# Go SDK and CLI implementation plan

## Confirmed scope

- Plan first, then implement the tickets below in order of dependencies.
- Port the public Python SDK to idiomatic Go, with one context-aware client instead of separate sync/async clients.
- CLI sends prompts and options to Jev and prints the service's probabilities. Never invent probabilities.
- Codex and Claude Code are placeholders only: return a clear not-implemented error, without starting processes.
- No remote, push, PR creation, or merge into `main` until the user adds a remote and starts the merge phase.

## Source baseline

- Upstream: https://github.com/typesafe-ai/typesafe-sdk-python
- Pinned revision: `f078f1e208a0d885154dc758344ae4fce77ac168` (v0.7.2).
- Wire contract: `src/typesafe_sdk/_schemas/models.py`.
- Behavior: `_core/config.py`, `questions.py`, `response_types.py`, `transport.py`, `retry.py`, `schemas/base.py`.
- Endpoints: `POST /v1/systemone` and `GET /v1/models`.
- Defaults: `https://api.typesafe.ai`, `jev-latest`, 10-second operation timeout, two retries.
- Context7 documentation checked for the Python SDK, Viper, and Cobra. Logging API checked against the vendored phuslu/log documentation.

## Architecture

- Public root package `typesafe`: questions, answers, request/response metadata, client, errors, retry policy.
- `internal/config`: independent Viper loader, TOML + environment + flags.
- `internal/cli`: Cobra command tree, injectable input/output and service factory, stderr-only logging.
- `internal/service`: decision interface, Jev adapter, explicit external-session placeholders.
- `cmd`: executable entrypoint, cancellation on interrupt, errors on stderr and nonzero exit status.
- SDK configuration follows upstream `TYPESAFE_*` environment names. CLI also supports `TS_JEV_*` names.
- CLI precedence: changed flags > environment > TOML > defaults.
- Config discovery: `${XDG_CONFIG_HOME}/ts-jev/config.toml`, otherwise `${HOME}/.config/ts-jev/config.toml`.
- SDK accepts injected HTTP clients, per-call overrides, raw/custom responses, and Go error inspection.
- Preserve structured string/object/array content, nested nulls, score integer keys, optional token counts, unknown future answer types, and raw HTTP metadata.
- Go adaptations: `context.Context` replaces async client and retry cancellation; typed decoding replaces Pydantic subclassing; caller-owned injected clients are not closed.

## Branch and QA rules

- Start `virtual-main` at the original `main`; leave `main` unchanged.
- Each ticket has its own `feature/NN-name` branch. Independent agents use isolated worktrees.
- Soft cap: 300 changed lines. Hard cap: 450 added + deleted lines against the ticket's parent, including tests and docs. Split a ticket before exceeding 450.
- Review and test each branch before fast-forwarding `virtual-main` to it. Rebase independent work onto the latest `virtual-main`, then rerun QA.
- Commit only using `git ch`; `csl --help` and `csl generate --help` have been inspected.
- No parallel PRs: after a remote exists, open only the oldest unmerged feature PR, with a base containing its prerequisites.
- Force-add only selected planning/QA documents because `docs/internal/` is currently ignored. Keep the user's spec private and untouched.
- Record parent, branch, changed-line count, tests, and review result in `docs/internal/qa.md`.
- Keep worktrees and temporary files until PRs are merged without errors, then clean task-owned leftovers only.

## Epics

- [ ] Translate Python SDK to Golang: Complete tickets 02-07 and the SDK portions of ticket 12.
- [ ] Support CLI mode: Complete tickets 08-12, using Jev with deferred external-session providers.

## Tickets

- [ ] 01 - Implementation plan: Pin upstream, capture clarified scope, architecture, acceptance criteria, dependencies, branch limits, and QA policy. Branch `feature/01-plan`; target <150 lines.
- [ ] 02 - Request primitives: Add Noul, Choice, Score, raw questions, JSON content validation, and System One request normalization. Test wire tags, omitted optional fields, nested nulls, invalid state, and empty criteria. Depends on 01; branch `feature/02-questions`; target 300-400 lines.
- [ ] 03 - Response models: Decode discriminated answers, typed accessors, integer-keyed score maps, nullable usage, model metadata, and raw response metadata. Test malformed known answers and unknown future types. Depends on 02; branch `feature/03-responses`; target 300-400 lines.
- [ ] 04 - Client configuration: Resolve API key/base URL/model/timeouts, validate secrets without disclosure, support custom headers and HTTP-client injection, and define ownership. Test environment fallback, explicit overrides, invalid keys/URLs/timeouts, and client isolation. Depends on 03; branch `feature/04-client`; target <300 lines.
- [ ] 05 - HTTP transport and errors: Implement authenticated context-aware HTTP, protected headers, request IDs, bounded response reading, typed status/connection/timeout/validation errors, and safe metadata. Test status mapping, malformed JSON, cancellation, headers, and response closure. Depends on 04; branch `feature/05-transport`; target 300-400 lines.
- [ ] 06 - SDK endpoints and overrides: Implement SystemOne and ListModels, request-specific model/timeout/headers/extra-body overrides, custom decode support, and raw HTTP response capture. Test exact endpoint paths, serialized bodies, shallow merges, and metadata. Depends on 05; branch `feature/06-endpoints`; target <300 lines.
- [ ] 07 - Retry policy: Add bounded exponential backoff/jitter, 408/429/5xx and connection/timeout retries, Retry-After, attempt headers, configurable disable/statuses/predicate, retry budget, and cancellation. Test retry/no-retry outcomes and replayed bodies without real sleeps. Depends on 06; branch `feature/07-retries`; target 300-400 lines.
- [ ] 08 - TOML configuration: Replace the config stub with isolated Viper loading and validated runtime settings. Test XDG/HOME discovery, missing optional versus explicit files, malformed TOML, defaults, environment, and flag precedence. Depends on 01; branch `feature/08-config`; target <300 lines.
- [ ] 09 - Cobra executable: Add Cobra dependency, fresh testable root command, persistent config/provider/API/model/timeout flags, help, injected streams, cancellation, and stderr logging. Test help/no-network, bad flags/config, and isolated command instances. Depends on 08; branch `feature/09-cli-root`; target <300 lines.
- [ ] 10 - Provider interface and placeholders: Define a context-aware decision service and Jev adapter. Reserve `codex`/`claude` providers with clear not-implemented errors and no subprocess calls. Test adapter mapping and placeholder/unknown-provider failures. Depends on 06; branch `feature/10-service`; target <300 lines.
- [ ] 11 - Decision CLI: Add `jev decide --prompt ... --option ...` and explicit stdin prompt support. Require distinct nonblank options, emit JSON probabilities from Jev, honor config overrides, and propagate errors. Test through injected service and local HTTP server, including invalid input and unavailable providers. Depends on 07,09,10; branch `feature/11-decide`; target 300-400 lines.
- [ ] 12 - Usage and final QA: Add public SDK/CLI quickstarts and example TOML, compatibility notes, local stack/QA ledger, and update ticket progress. Run race tests, vet, build, command smoke tests, and verify branch caps. Depends on all tickets; branch `feature/12-docs`; target <300 lines.

## Acceptance gates

- Every implementation ticket includes its own deterministic tests. No paid API requests in QA.
- `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go build ./cmd` pass on the completed stack.
- CLI help works without credentials or a config file. Errors do not leak API keys or pollute JSON stdout.
- Upstream compatibility gaps and Go-specific ownership/custom-decoder choices are documented explicitly.
- Every branch stays below 450 changed lines and preserves a linear ancestry; main is unchanged.
- PR publication and merge are pending user action, not treated as completed tickets.

## Follow-up scope added by the user

- [ ] 13 - `jev skill`: Add a standalone follow-up branch for the upstream TypeSafe AI skill, default Jev provider and hidden `--jev`, plus exclusive `--online` / `--local` modes. Online mode gives the LLM a GitHub source to fetch; local mode downloads the latest `SKILL.md`. Confirm default mode and output/install destination before implementation. Test help visibility, mode validation, content delivery, and failed downloads through an injected HTTP client. Depends on 09; target <300 lines, hard 450. This follow-up is not part of the initial SDK/decision CLI acceptance gates.
- Source: https://github.com/typesafe-ai/skills/blob/main/skills/typesafe-ai/SKILL.md
- [ ] 14 - Skill installation targets: Extend `jev skill` with mutually exclusive `--claude` / `--agent` destination flags, installing under project-local `.claude` / `.agent` respectively. Confirm nested `SKILL.md` layout, default destination, and overwrite policy before implementation. Test both paths, conflicting flags, directory creation, existing files, and write failures. Depends on 13; separate feature branch, target <300 lines and hard 450. Preserve `.agent` exactly as requested.
