# Go SDK and CLI implementation plan

## Confirmed scope

- Plan first, then implement the tickets below in order of dependencies.
- Port the public Python SDK to idiomatic Go, with one context-aware client instead of separate sync/async clients.
- CLI sends prompts and options to Jev and prints the service's probabilities. Never invent probabilities.
- Codex and Claude Code are placeholders only: return a clear not-implemented error, without starting processes.
- Remote `bitbrew-dev/jevwise` is ready. The user authorized one PR at a time, with passing CI before squash merge; use an explicit PR title when it contains multiple commits.

## Source baseline

- Upstream: https://github.com/typesafe-ai/typesafe-sdk-python
- Pinned revision: `f078f1e208a0d885154dc758344ae4fce77ac168` (v0.7.2).
- Wire contract: `src/typesafe_sdk/_schemas/models.py`.
- Behavior: `_core/config.py`, `questions.py`, `response_types.py`, `transport.py`, `retry.py`, `schemas/base.py`.
- Endpoints: `POST /v1/systemone` and `GET /v1/models`.
- Defaults: `https://api.typesafe.ai`, `jev-latest`, 10-second operation timeout, two retries.
- Context7 documentation checked for the Python SDK, Viper, and Cobra. Logging API checked against the vendored phuslu/log documentation.

## Architecture

- Public SDK package `pkg/typesafe`: questions, answers, request/response metadata, client, errors, retry policy. The repository root stays free of SDK source files, as requested by the user.
- SDK import: `github.com/benbenbang/ts-jev-go-sdk/pkg/typesafe`. All SDK source and adjacent tests, including future endpoints and retries, live in this package.
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

- Keep `virtual-main` as the integration branch. After approved remote merges, fast-forward local `main` and `virtual-main` to `origin/main`.
- Each ticket has its own `feature/NN-name` branch. Independent agents use isolated worktrees under `.agent/worktrees/`.
- Soft cap: 300 changed lines. Hard cap: 450 added + deleted lines against the ticket's parent, including tests and docs. Split a ticket before exceeding 450.
- Review and test each branch before fast-forwarding `virtual-main` to it. Rebase independent work onto the latest `virtual-main`, then rerun QA.
- Commit only using `git ch`; `csl --help` and `csl generate --help` have been inspected.
- No parallel PRs: after a remote exists, open only the oldest unmerged feature PR, with a base containing its prerequisites.
- Force-add only selected planning/QA documents because `docs/internal/` is currently ignored. Keep the user's spec private and untouched.
- Record parent, branch, changed-line count, tests, and review result in `docs/internal/qa.md`.
- Keep worktrees and temporary files until PRs are merged without errors, then clean task-owned leftovers only.

## Epics

- [x] Translate Python SDK to Golang: Complete tickets 02-07 and the SDK portions of ticket 12.
- [x] Support CLI mode: Complete tickets 08-12, using Jev with deferred external-session providers.

## Tickets

- [x] 01 - Implementation plan: Pin upstream, capture clarified scope, architecture, acceptance criteria, dependencies, branch limits, and QA policy. Branch `feature/01-plan`; target <150 lines.
- [x] 02 - Request primitives: Add Noul, Choice, Score, raw questions, JSON content validation, and System One request normalization. Test wire tags, omitted optional fields, nested nulls, invalid state, and empty criteria. Depends on 01; branch `feature/02-questions`; target 300-400 lines.
- [x] 03 - Response models: Decode discriminated answers, typed accessors, integer-keyed score maps, nullable usage, model metadata, and raw response metadata. Test malformed known answers and unknown future types. Depends on 02; branch `feature/03-responses`; target 300-400 lines.
- [x] 04 - Client configuration: Resolve API key/base URL/model/timeouts, validate secrets without disclosure, support custom headers and HTTP-client injection, and define ownership. Test environment fallback, explicit overrides, invalid keys/URLs/timeouts, and client isolation. Depends on 03; branch `feature/04-client`; target <300 lines.
- [x] 05 - HTTP transport and errors: Implement authenticated context-aware HTTP, protected headers, request IDs, bounded response reading, typed status/connection/timeout/validation errors, and safe metadata. Test status mapping, malformed JSON, cancellation, headers, and response closure. Depends on 04; branch `feature/05-transport`; target 300-400 lines.
- [x] 05c - SDK package layout: Move the existing SDK source and tests into `pkg/typesafe`, update imports and unfinished feature worktrees, and verify tests/race/vet/build. Keep completed feature history intact and use rename-aware line counts. Depends on 05b; branch `feature/05c-pkg-layout`; target <300 changed lines.
- [x] 06 - SDK endpoints and overrides: Implement SystemOne and ListModels, request-specific model/timeout/headers/extra-body overrides, custom decode support, and raw HTTP response capture. Test exact endpoint paths, serialized bodies, shallow merges, and metadata. Depends on 05; branch `feature/06-endpoints`; target <300 lines.
- [x] 07 - Retry policy: Add bounded exponential backoff/jitter, 408/429/5xx and connection/timeout retries, Retry-After, attempt headers, configurable disable/statuses/predicate, retry budget, and cancellation. Test retry/no-retry outcomes and replayed bodies without real sleeps. Depends on 06; branch `feature/07-retries`; target 300-400 lines.
- [x] 08 - TOML configuration: Replace the config stub with isolated Viper loading and validated runtime settings. Test XDG/HOME discovery, missing optional versus explicit files, malformed TOML, defaults, environment, and flag precedence. Depends on 01; branch `feature/08-config`; target <300 lines.
- [x] 09 - Cobra executable: Add Cobra dependency, fresh testable root command, persistent config/provider/API/model/timeout flags, help, injected streams, cancellation, and stderr logging. Test help/no-network, bad flags/config, and isolated command instances. Depends on 08; branch `feature/09-cli-root`; target <300 lines.
- [x] 10 - Provider interface and placeholders: Define a context-aware decision service and Jev adapter. Reserve `codex`/`claude` providers with clear not-implemented errors and no subprocess calls. Test adapter mapping and placeholder/unknown-provider failures. Depends on 06; branch `feature/10-service`; target <300 lines.
- [x] 11 - Decision CLI: Add `jev decide --prompt ... --option ...` and explicit stdin prompt support. Require distinct nonblank options, emit JSON probabilities from Jev, honor config overrides, and propagate errors. Test through injected service and local HTTP server, including invalid input and unavailable providers. Depends on 07,09,10; branch `feature/11-decide`; target 300-400 lines.
- [x] 12 - Usage and final QA: Add public SDK/CLI quickstarts and example TOML, compatibility notes, local stack/QA ledger, and update ticket progress. Run race tests, vet, build, command smoke tests, and verify branch caps. Depends on all tickets; branch `feature/12-docs`; target <300 lines.

## Acceptance gates

- Every implementation ticket includes its own deterministic tests. No paid API requests in QA.
- `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go build ./cmd` pass on the completed stack.
- CLI help works without credentials or a config file. Errors do not leak API keys or pollute JSON stdout.
- Upstream compatibility gaps and Go-specific ownership/custom-decoder choices are documented explicitly.
- Every branch stays below 450 changed lines and preserves a linear ancestry; update `main` only through approved, passing PR merges.
- PR publication and merge happen sequentially after QA. A ticket is complete only when its required implementation and merge are finished.

## Follow-up scope added by the user

- [x] 13 - `jev skill`: Add a standalone follow-up branch for the upstream TypeSafe AI skill, default Jev provider and hidden `--jev`, plus exclusive `--online` / `--local` modes. Online mode gives the LLM a GitHub source to fetch; local mode downloads the latest `SKILL.md`. Confirmed default: local download to `.agent/skills/typesafe-ai/SKILL.md`; `--online` prints the upstream GitHub URL without fetching. Test help visibility, mode validation, content delivery, and failed downloads through an injected HTTP client. Depends on 09; target <300 lines, hard 450. This follow-up is not part of the initial SDK/decision CLI acceptance gates.
- Source: https://github.com/typesafe-ai/skills/blob/main/skills/typesafe-ai/SKILL.md
- [x] 14 - Skill installation targets: Extend `jev skill` with mutually exclusive `--claude` / `--agent` destination flags, installing under project-local `.claude` / `.agent` respectively. Confirmed layout: `.agent/skills/typesafe-ai/SKILL.md` by default or with `--agent`; `--claude` selects `.claude/skills/typesafe-ai/SKILL.md`. Existing files require `--force`; symlinks and nonregular destinations are rejected. Test both paths, conflicting flags, directory creation, existing files, and write failures. Depends on 13; separate feature branch, target <300 lines and hard 450. Preserve `.agent` exactly as requested.

## Resumed implementation: 2026-10-06

- Foundation PRs 1-11, provider-service PR 12, and endpoint PR 13 are merged; current baseline is `4701d21`. Ticket 10 uses an injected callback, so endpoint wiring remains separate.
- Ticket 06: add typed and raw System One/model-list methods plus `RawResponse.Decode`. Request options permit one optional override object; timeout zero inherits and a model pointer distinguishes absent from explicitly empty strings.
- System One model precedence: client default < nonempty request model < explicit call model < shallow extra-body model. Validate original questions before extra-body replacement; do not mutate caller data.
- Go adaptation: validate original state as well as original questions before extra-body replacement; invalid original state cannot be rescued by an override.
- HTTP and optional typed decoding share one attempt helper, allowing ticket 07 predicates to retry decoding errors without changing the public endpoint API. Model-list calls reject unsupported model/extra-body overrides.
- Ticket 07 is split into 07a policy, 07b pure retry engine, and 07c client integration, each with deterministic tests and its own feature branch. This keeps implementation and tests within the 450-line cap.
- Follow with ticket 11 decision CLI and ticket 12 usage/final QA. Skill installation, Go module/import rename, version display, release checking, and verified self-update remain separate follow-up features. The GitHub repository is already named `jevwise`.

## Initial scope completion

- Historical checkpoint before skill confirmation: tickets 01-12 were implemented and validated, while follow-up tickets 13-14 remained deferred. Final documentation/QA is published as the last sequential PR before its completion markers enter `main`.
- All SDK and CLI source remains in `pkg/typesafe`, `internal`, and `cmd`; no paid API calls were used for QA.
- Remaining user-requested work: Go module/import rename, version output, new-release checks, and verified self-update. Plan these as separate small features, including release assets/checksums and update safety, rather than changing workflows without review.

## Confirmed skill implementation split

- 13: implement a bounded, context-aware upstream Markdown downloader and tested command foundation with hidden `--jev`, exclusive modes, safe errors, and injected fetch/install callbacks. Register the command only in 14 so no intermediate CLI default contradicts the confirmed install destination.
- 14: add project-local targets and explicit force replacement, register `jev skill`, and document usage. Conflicting mode/target flags and online installation flags fail before network/filesystem effects.
- Validate UTF-8 Markdown/front matter and reject empty, oversized, or HTML payloads. No API credentials, redirects, response bodies, or arbitrary transport errors are printed; online mode performs no download/write.
- Install complete bytes atomically inside the current project using directory-scoped filesystem APIs; preserve existing files on errors and reject symlink/nonregular targets. Tests use injected transports and temporary directories, never paid API requests.
