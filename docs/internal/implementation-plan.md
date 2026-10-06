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
- SDK import: `github.com/bitbrew-dev/jevwise/pkg/typesafe`. All SDK source and adjacent tests, including future endpoints and retries, live in this package.
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
- At that checkpoint, remaining work was the module/import rename, version output, release checks, and verified self-update. The confirmed sequential follow-ups below cover these features, including release assets/checksums and update safety.

## Confirmed skill implementation split

- 13: implement a bounded, context-aware upstream Markdown downloader and tested command foundation with hidden `--jev`, exclusive modes, safe errors, and injected fetch/install callbacks. Register the command only in 14 so no intermediate CLI default contradicts the confirmed install destination.
- 14: add project-local targets and explicit force replacement, register `jev skill`, and document usage. Conflicting mode/target flags and online installation flags fail before network/filesystem effects.
- Validate UTF-8 Markdown/front matter and reject empty, oversized, or HTML payloads. No API credentials, redirects, response bodies, or arbitrary transport errors are printed; online mode performs no download/write.
- Install complete bytes atomically inside the current project using directory-scoped filesystem APIs; preserve existing files on errors and reject symlink/nonregular targets. Tests use injected transports and temporary directories, never paid API requests.

## Confirmed release/update sequence: 2026-10-06

User approved all four follow-ups sequentially. Preserve CLI `jev`, SDK `pkg/typesafe`, existing configuration names, and the manual semantic-release workflow. Work in `.agent/worktrees`; one PR at a time, each under 450 added/deleted lines including tests/docs. Use reviewed `virtual-main`, independent QA, `git ch --verbose` with OpenAI/gpt-5.6-luna, passing CI before squash merge, and cleanup after final checks.

- [x] 15 - Module rename: declare `github.com/bitbrew-dev/jevwise`, update active imports/usage examples, document the SDK import-path migration, and validate a consumer build. No compatibility alias or configuration rename. Depends on merged 14.
- [x] 16 - Version reporting: add credential/config-independent `jev version` and `jev --version`, with release version, commit, and build date injected through Go linker flags; development builds are explicitly `dev`. Include deterministic output/error tests. Depends on 15.
- [x] 17a - Release assets: add tested native binary packaging and SHA-256 manifest for Linux/macOS/Windows amd64/arm64. Preserve semantic-release ownership of versions/tags; publish only against an existing verified release tag using a release-event/manual assets workflow. Do not trigger an actual release during implementation. Depends on 16.
- [x] 17b - Release metadata foundation: implement bounded, context-aware, credential-free GitHub stable-release lookup and strict version comparison. Reject malformed/draft/prerelease metadata, use strict stable semantic versions, handle absent releases/rate limits safely, never downgrade, and test only injected fixtures. Depends on 17a.
- [x] 17c - Release-check CLI: register credential/config-independent `jev update --check`, positive operation timeout, safe writer/lookup errors, absent-release handling and explicit unknown/development status. Split from 17b to retain readable transport and CLI tests within 450 lines each. Depends on 17b.
- [x] 18a - Verified download: select the exact platform binary and checksum manifest from the selected release, restrict source/redirect hosts, bound reads, and require exact SHA-256 verification before any executable write. Checksums protect integrity, not a compromised publisher; trust remains GitHub/repository ownership. Depends on 17c.
- [x] 18b1 - Version guard foundation: embed an inspectable release stamp in trimmed binaries, validate module/platform/checksum and strictly newer candidate identity without executing it, and require on-disk version to match the running version under the future lock. Unknown/custom unstamped builds require manual updates. Depends on 18a.
- [x] 18b2 - Safe replacement: root filesystem operations at the executable directory, reject static symlinks/nonregular/multiply-linked destinations, stage a complete executable with sync/close, and atomically replace the directory entry only after verification and cancellation checks. Preserve the original on prepublication failure; document ordinary-filesystem and postpublication limitations. Linux/macOS automatic replacement only; Windows/manual and package-managed installs are not silently elevated. Depends on 18b1.
- [x] 18c - Update integration: wire `jev update` to lookup/download/verify/replace, keep `--check` read-only, refuse unknown/development versions for automatic installation, document package-manager/manual use, and test injected end-to-end failures. Depends on 18b2. Split further only if needed to preserve the hard cap, never by compressing readable code.

Release/update defaults (Windows binaries explicitly confirmed by the user): `jev update --check` checks; `jev update` installs; release binaries cover Linux/macOS/Windows amd64/arm64; native Linux/macOS auto-replacement, Windows manual updates. No automatic background checks, credentials, arbitrary repositories, downgrade, shell installer execution, or workflow/release dispatch during QA. Verify both Windows architectures by cross-compilation and add native Windows CLI smoke CI in 17a; Windows manual updates are distinct from binary support.

## MCP server and background lifecycle: 2026-10-06

- User requested an independent local MCP server with HTTP/SSE and background start/status/stop. Command proposal: `jev mcp`, `jev mcp --background`, `jev mcp status`, `jev mcp stop`; foreground remains the default. No automatic login/reboot startup in this epic.
- Architecture: keep protocol/security/lifetime code in `internal/mcpserver`, background runtime/process code in `internal/daemon`, and thin command wiring in `internal/cli`. Reuse the existing decision service and public SDK unchanged; Codex/Claude providers remain placeholders.
- Pin the official [Go MCP SDK v1.8.0](https://github.com/modelcontextprotocol/go-sdk/tree/v1.8.0), verified through Context7 and pinned upstream source. Use stateless Streamable HTTP: POST responses may be SSE, not deprecated two-endpoint HTTP+SSE or standalone GET streams. Document/test supported negotiated protocol revisions rather than inventing compatibility.
- Default endpoint: `http://127.0.0.1:8080/mcp`. Accept explicit loopback literals only, never wildcard/public binding or arbitrary reverse-proxy trust. A port conflict fails safely without adopting or stopping the existing process.
- Require an agent bearer token from `JEV_MCP_TOKEN` or a protected token file, distinct from upstream API credentials and the generated private management credential. Validate Host and any supplied Origin, limit bodies/headers/concurrency, and apply per-decision timeouts. Server lifetime is not bounded by the decision timeout.
- Expose only the `decide` tool with prompt/options input and existing structured probability output. Reject unsafe/malformed input with fixed messages; never echo prompts, credentials, raw backend errors or generated schema-validation details in errors/logs.
- Shutdown cancels active decisions and closes owned transports. SDK request-disconnect propagation differs by protocol revision, so explicit tool/lifetime cancellation and regression coverage are required.
- Background mode launches the same executable without a shell, detaches with platform-specific helpers, and transfers validated configuration through a bounded anonymous pipe. No upstream credentials in child argv, state files or logs; state stores only address/version/instance identity, with management key stored separately under protected user-only permissions.
- Use a private user runtime directory with Unix owner/mode and Windows ACL checks, exclusive startup ownership, bounded logs and atomic state publication. Never automatically remove an unverified stale lock or kill a PID from a state file.
- Status/stop use authenticated local management requests and verify the instance identity. Agent credentials do not authorize management. Startup confirms success only after an authenticated readiness check and parent acknowledgement; EOF/timeout before acknowledgement terminates the prepared child, while acknowledged children survive terminal closure.
- Preserve the user's manual release workflow and versioning configuration. No release dispatch, paid API call, global daemon registration or real user-state installation during implementation/QA.
- Retain one reviewed feature PR at a time, 300-line soft/450-line hard added+deleted caps including dependencies/tests/docs, independent QA and `git ch --verbose` using OpenAI/gpt-5.6-luna. Rebase only onto reviewed `virtual-main`, merge after exact-head CI, then clean merged task-owned worktrees and artifacts.

### Sequential tickets

- [x] 19a - MCP tool foundation: pin the SDK, add a safe injectable decision adapter and deterministic in-memory protocol tests. Include strict inputs, structured results, deadline/lifetime cancellation and fixed error coverage. Branch `feature/19a-mcp-tool`; hard cap 450 including this plan; dependency/protocol coverage justifies exceeding the soft cap.
- [x] 19b1 - Secure HTTP transport: literal-loopback validation, stateless POST/SSE, bearer/Host/Origin guards, bounded requests/concurrency and streaming error sanitization. SDK errors are scrubbed before publication; dependency 19a.
- [x] 19b2a - HTTP protocol and admission: reject legacy batches, bound actual decisions to four without queuing, preserve sanitized JSON-RPC errors and verified supported revisions, and test all supported revisions plus future-version fallback. Depends on 19b1.
- [x] 19b2b - HTTP boundary fixtures: modern disconnect and legacy lifetime cancellation, complete escaped-output/SSE frame bounds and writer failures, plus mapped-loopback canonicalization. Transport requirements complete; depends on 19b2a.
- [x] 19c1 - Foreground runtime: exclusive loopback listener, bounded HTTP timeouts, safe readiness callback, active-decision lifetime cancellation and five-second graceful shutdown. Ready/service callbacks must honor context. Depends on 19b2b.
- [x] 19c2 - Foreground command: lazy `jev mcp` registration/config/service ownership, endpoint output, help/no-effects and injected failures, plus protocol/cancellation/event-limit usage docs. Depends on 19c1.
- [x] 19d1a - Runtime ownership: private Unix directory validation, exclusive inode-pinned lease and conservative cleanup. Mode-only foundation, no state/secrets yet; depends on 19c2.
- [x] 19d1b - Private file operations: bounded exclusive writes and descriptor-validated reads, with fault fixtures. Depends on 19d1a.
- [x] 19d1c - Read-only storage: open existing private runtime directories without creating them for status/stop. Depends on 19d1b.
- [x] 19d2a1 - Private creation hooks: exclusive handle-relative Windows creation with explicit user owner/protected ACL, plus portable hooks. Depends on 19d1b.
- [x] 19d2a2 - Windows privacy: enable storage with exact opened-handle owner/DACL/reparse/link validation and native Windows fixtures. Depends on 19d2a1.
- [x] 19d2b - Darwin ACL privacy: reject extended ACLs through pinned descriptors; wire Unix validation and native macOS fixtures. Depends on 19d1b.
- [x] 19d3a - Strict instance reads: bounded state/control-key decoding, exact keys, safe canonical metadata and absent/partial distinction. Depends on platform privacy gates.
- [ ] 19d3a2 - Publication link validation: preserve all privacy checks for exactly one or two certified aliases. Depends on platform privacy gates.
- [ ] 19d3b - Owned publication: atomic no-overwrite state staging, pinned contents/identity and conservative rollback/cleanup. Depends on 19d3a/19d3a2.
- [ ] 19d3c - Publication regressions: transient ACL changes, exchanged aliases and unexpected hardlinks. Depends on 19d3b, before child exposure.
- [ ] 19e1 - Management endpoints: separate authenticated prepared/running/status/stop lifecycle, safe acknowledgements and graceful cancellation. Depends on state foundations.
- [ ] 19e2 - Management controller: bounded no-proxy/no-redirect local requests with authenticated instance/state verification. Depends on 19e1.
- [ ] 19e2b - Mutual control authentication: fresh nonce and domain-separated request/response HMAC proofs; never transmit the private management key or trust an echoed instance. Depends on 19e2, before CLI exposure.
- [ ] 19e3 - Runtime routing: route only exact management endpoints separately from agent MCP authentication. Depends on 19e1.
- [ ] 19f1a - Private bootstrap: bounded strict stdin framing, prepared-child lease and acknowledgement, independent child lifetime. Depends on existing configuration values and 19e2; callers must supply a pure validator.
- [ ] 19f1b - Detached launch: Unix/Windows same-executable detachment, anonymous stdin, bounded startup and cooperative pre-ACK cleanup; never kill post-ACK uncertainty. Depends on 19f1a.
- [ ] 19f2a1 - Pure configuration validation: validate already-loaded values without environment/files/flags. Depends on existing configuration package.
- [ ] 19f2a2 - Bootstrap validation: canonical local metadata, distinct credentials and fresh crypto-random management identity, no effects. Depends on 19f2a1/19e1.
- [ ] 19f2b - Child assembly: hidden child command, owned storage/listener/service, publication then ACK, and no paid decision before running. Depends on publication regressions, routing and bootstrap.
- [ ] 19f2b2 - Child failure regressions: additional initialization/EOF cleanup fixtures, before background CLI exposure. Depends on 19f2b.
- [ ] 19g1 - Protected agent token: bounded token-file reader with owner/ACL/link/inode validation, fixed safe errors. Depends on platform privacy gates.
- [ ] 19g2 - Token-file CLI: explicit file overrides environment, help/management skip token reads, lazy validation before factory. Depends on 19g1.
- [ ] 19f2c1 - Management CLI: read-only status/stop without config, service construction, agent token reads, directory creation or ownership acquisition. Depends on 19d1c/19e2.
- [ ] 19f2c2 - Background CLI: wire launch, duplicate prevention, authenticated readiness and conservative startup uncertainty. No credentials in argv. Depends on child, token and management CLI.
- [ ] 19g3 - Child-process integration: task-owned fake backend/process fixtures for independence, duplicate starts, stop and startup failure cleanup. Depends on background CLI.
- [ ] 19g4 - Final docs and QA: document protocols/limits/token setup/background lifecycle, native Linux/macOS/Windows gates, six cross-builds and task-owned artifact cleanup. Login/reboot services remain deferred; do not change published v1.0.0.
