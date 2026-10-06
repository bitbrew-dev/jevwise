# Local stack QA

## Baseline and policy

- Original `main`: `10b25d9`. `main` must remain here until the user starts remote merges.
- Upstream Python baseline: `f078f1e208a0d885154dc758344ae4fce77ac168` (v0.7.2).
- Line counts mean total added + deleted lines against each feature's immediate parent.
- Soft cap: 300. Hard cap: 450, including tests and documentation.
- Agents build in isolated worktrees; a separate reviewer checks each feature before stacking.
- No live API calls, external-session subprocesses, remote pushes, or PRs during QA.

## Reviewed features

| Branch | Parent | Changed lines | QA | Outcome |
| --- | --- | ---: | --- | --- |
| `feature/01-plan` | `10b25d9` | 74 | Spec/scope review, `git diff --check`, baseline `go test ./...` | Passed |
| `feature/08-config` | `7a04b58` | 228 | Full tests/vet, config race tests, config precedence/security review | Passed |
| `feature/09-cli-root` | `2d9f65c` | 252 | Full tests/race/vet, help/no-network, flags/streams/cancellation review | Passed, stacked |
| `feature/01a-skill-plan` | `8f2cb6b` | 5 | Follow-up scope/spec review, append-only diff | Passed, stacked |
| `feature/02-questions` | `39995f0` | 282 | Full tests/race/vet, pinned raw/typed wire compatibility review; rerun after rebase | Passed, stacked |
| `feature/01b-skill-targets` | `76850fc` | 1 | Target flags/spec review, append-only diff | Passed, stacked |
| `feature/03-responses` | `1c43f44` | 420 | Full tests/race/vet; deterministic decoding and safe-error re-review; rerun after rebase | Passed, stacked; soft-cap exception for strict decoding coverage |
| `feature/04-client` | `3859662` | 255 | Full tests/race/vet; blank-environment correction re-review; rerun after rebase | Passed, stacked |
| `feature/05a-errors` | `ab60c81` | 283 | Full tests/race/vet; status/message/metadata review and whitespace-body correction; rerun after rebase | Passed, stacked as `a08e95e` |
| `feature/05b-transport` | `a08e95e` | 290 | Fresh full tests/race, vet, bounded HTTP/redirect/secret handling review | Passed, stacked as `5c52fdc` |
| `feature/05c-pkg-layout` | `5c52fdc` | 54 | Ten byte-identical moves; fresh full tests/race, vet, CLI build/help; independent structural review | Passed; rename-aware count, no behavior changes |

## Commit-generation permission

- Plan, config, Cobra foundation, request primitives, and skill follow-up planning are committed and fast-forwarded onto `virtual-main`.
- Response models and client configuration, including the whitespace-only environment fallback correction, are committed and stacked.
- One agent's explicit Bedrock commit-message generation attempt was rejected by the approval reviewer because staged code would leave the machine.
- No retry or provider switch was made until the user explicitly authorized `git ch` with OpenAI/Luna.
- All subsequent commit-message generation uses the root configuration's `gpt-5.6-luna` model and explicitly selected OpenAI provider.
- The approval reviewer rejected `errors.go` / `errors_test.go` diff transmission despite general OpenAI approval. After the user explicitly requested `git ch --verbose` for that staged payload, it was committed successfully. The user subsequently approved the exact remaining source/test/doc file list for OpenAI/Luna transmission.
- Historical pre-publication checkpoint: the remaining implementation tickets were still pending then. Subsequent reviewed merges are recorded below.

## Package layout change

- User requested `pkg/` instead of a root SDK package. Canonical SDK path is now `pkg/typesafe` with import `github.com/benbenbang/ts-jev-go-sdk/pkg/typesafe`.
- Move source and tests together without changing behavior. Completed feature branches retain their historical snapshots; unfinished branches adopt the new package before further implementation.
- Use rename-aware diffs for the feature cap: unchanged file moves count as zero added/deleted lines.
- Fresh `go test -count=1 ./...`, `go test -race -count=1 ./...`, `go vet ./...`, and CLI build/help passed with localhost-only fixture permission. The reviewer also checked every moved file against its parent by SHA256.

## Merge-phase handoff

- Remote `bitbrew-dev/jevwise` is ready; reviewed PRs are published and merged sequentially after CI.
- Publish only the oldest unmerged feature PR at a time, with prerequisites already in its base.
- Retain task-owned worktrees under `.agent/worktrees/` until their PRs are merged without errors.
- Then remove only these task-owned worktrees and leftover files; preserve other work and installed tools.

## Resumed endpoint QA: 2026-10-06

| Branch | Parent | Changed lines | QA | Outcome |
| --- | --- | ---: | --- | --- |
| `feature/06-endpoints` | `71d01c4` | 430 | Independent pinned-contract/security review; fresh full unit/race tests, tidy check, vet, build; fixtures only | Passed; soft-cap exception for overrides/raw/custom decode coverage |

- State and question validation before extra-body merge is an intentional stricter Go rule. Explicit model pointers preserve blank values; timeout zero inherits.
- Standalone raw decoding performs no retries. `SystemOneInto` custom decoding shares the HTTP attempt helper for later retry predicates; it follows standard Go JSON partial-mutation semantics.

## Retry policy 07a

- Parent: `4701d21`, endpoint PR 13 merged with passing CI.
- Branch: `feature/07a-retry-policy`; 277 changed lines including this ledger and plan updates.
- Independent QA and pinned Python audit passed; hexadecimal numeric retry headers are rejected to match Python.
- Fresh full tests/race, vet, build, tidy-diff, and whitespace checks passed after rebase.
- Go adaptations: nanosecond jitter, finite duration range, standard HTTP-date formats, zero policy disables retries, and zero budget is unbounded.

## Retry engine 07b

- Parent: `b572a20`, reviewed retry-policy stack; branch: `feature/07b-retry-runner`.
- Changed lines: 305 including this ledger; soft-cap exception for cancellation and budget boundary coverage, below hard 450.
- Independent QA: PASS, 50 repeated targeted tests plus race and vet; no real sleeps or paid API requests.
- Full fresh tests/race, vet, build, tidy-diff, and whitespace checks passed after stacking.
- Engine preserves latest response/error, gives caller cancellation precedence, and does not cancel in-flight attempts when the retry budget expires.

## Retry client integration 07c

- Parent: `853b1a3`, reviewed retry-engine stack; branch: `feature/07c-retry-client`.
- Changed lines: 308 including this ledger; soft-cap exception for explicit header/body replay and override-isolation regressions, below hard 450.
- Independent QA: PASS, 50 repeated integration tests, race, vet, and build; root repeated full tests/race, tidy-diff, vet/build, and whitespace checks.
- Constructor/call policies are validated and copied; nil inherits, explicit zero disables, overrides replace rather than merge.
- HTTP and custom/typed decoding share retries; caller deadline/cancellation wins, per-attempt timeout metadata is preserved, standalone raw decoding never retries.

## Decision CLI 11

- Parent: `6ee02e5`, retry integration PR 16 merged; branch: `feature/11-decide`.
- Changed lines: 408 including progress and this ledger; soft-cap exception for input/error-safety and end-to-end retry coverage, below hard 450.
- Independent QA: PASS, 50 repeated local HTTP retry/factory-cancellation regressions plus race, vet, and build.
- Root full tests/race, tidy-diff, vet/build, and whitespace checks passed after rebase; CLI help remains credential/config/network-free.
- Real Jev choice probabilities are emitted without local inference; placeholders launch no processes. Generic blocking stdin is checked for cancellation before/after reading, not forcibly interrupted.

## Usage and final QA 12

- Parent: `dfdba09`, decision CLI PR 17 merged; branch: `feature/12-docs`; 197 changed lines.
- Independent README/config/compatibility review: PASS. README SDK snippet compiled without executing it; example TOML loaded through the actual placeholder CLI.
- Root/decision help passed with a missing explicit config, without credentials or network calls.
- Full fresh tests/race, tidy-diff, vet/build, and whitespace checks passed; local HTTP fixtures only, no paid requests.
- Every resumed PR respects the 450-line hard cap. Soft-cap exceptions record deterministic endpoint/retry/input safety coverage.
- Final source/module/CLI behavior and remaining skill/version/update follow-ups are documented. Cleanup follows successful merge, preserving the private spec and unrelated files.

## Skill foundation 13

- Parent: `d13b424`; branch: `feature/13-skill`; 377 changed lines including plan and this ledger.
- Soft-cap exception: readable fixed-URL transport, bounded content validation, and before/after callback cancellation coverage; below hard 450.
- Independent review/repeated tests/race/vet passed; root full tests/race, tidy-diff, vet/build, and whitespace checks passed.
- Empty-front-matter panic found during root review was corrected and regression-tested. No credentials/cookies/redirects, body limits and closure verified.
- Command registration waits for ticket 14, preserving the confirmed local-install default. Fixtures only; no paid requests or real downloads in QA.

## Skill targets 14

- Parent: reviewed foundation `e269f98`, merged by PR 19; branch: `feature/14-skill-targets`; 449 changed lines including docs.
- Soft-cap exception for rooted filesystem safety, injected write/sync/close/publication failures, and race-safe no-overwrite tests; hard 450 respected.
- Independent QA and root full tests/race, tidy-diff, vet/build, formatting, and credential-free help/online smoke checks passed.
- Default `.agent`, explicit `.agent`/`.claude`, `--force` hint, static symlink rejection, inode-preserving force, staging cleanup, and cwd production bridge covered.
- Native Unix force only; Windows force and nonnative install limitations documented. Prepublication failures preserve prior file; postpublication cancellation/cleanup errors do not trigger unsafe rollback.

## Module migration 15

- Baseline: `eee94f0`; feature branch: `feature/15-module-rename`.
- Scope: module/current imports and consumer examples only; CLI/configuration names unchanged.
- Independent/root tests, race, vet, build, tidy-diff, and formatting passed; 74 changed lines. Separate consumer and all six cross-builds (including both Windows architectures) passed. Dependencies unchanged; private specification stays unstaged.

## Version reporting 16

- Baseline: `5ba89cd`; branch: `feature/16-version`; private specification excluded.
- Root version flag and subcommand must share safe, config-independent output; make metadata and Windows executable suffix checked.
- Independent/root tests, race, vet, build, tidy-diff, formatting and linked native smoke passed; both Windows cross-builds passed. Final delta: 274 lines, below soft cap.

## Release assets 17a

- Baseline: `60c09b0`; branch: `feature/17a-release-assets`; manual semantic-release unchanged.
- Native Windows tests use USERPROFILE; trusted tooling/provenance, no-redirect publishing and manifest-last safety justify the soft-cap exception.
- Independent/root fixture tests, actionlint/shellcheck, Go tests/race/vet/build/tidy, and six actual offline builds/native smoke passed; 410 lines. Publication was not invoked; all three native CI jobs must pass before merge.

## Release checks 17b

- Baseline: `3f6671d`; branch: `feature/17b-release-check`; metadata foundation only; CLI registration split into 17c.
- Soft-cap exception: bounded transport, strict uint64 versions, client isolation, cancellation and safe cause/closure coverage; CLI split into 17c preserves readability.
- Independent/root tests, repeated/race tests, vet/build/tidy and formatting passed; 390 changed lines. Dedicated idle/TLS/dial bounds, caller-controlled total deadline; no real API or paid requests in QA.

## Release-check CLI 17c

- Baseline: `1a72d1b`; branch: `feature/17c-release-check-cli`; 298 changed lines, below soft cap.
- Independent/root full tests, race, repeated update tests, vet/build/tidy, fixtures and formatting passed; native offline smoke and all six cross-builds passed.
- Flag-only positive deadline, caller cancellation, safe errors/short writes, strict comparisons and unknown/absent releases covered. No config/service credentials, binary downloads, executable writes or real release API requests in QA.

## Verified download 18a

- Baseline: `236ad86`; branch: `feature/18a-verified-download`; 446 changed lines, hard cap respected.
- Soft-cap exception for strict manifest/metadata parsing, exact-size bounds, restricted redirects and error/cancellation coverage; replacement and CLI integration stay separate.
- Independent/root full and repeated tests, race, vet/build/tidy, fixtures and formatting passed; native offline help and six cross-builds passed. Fixtures only, no real downloads or release calls; no executable writes.

## Update version guards 18b1

- Baseline: `7920f03`; branch: `feature/18b1-update-version-guard`; 434 changed lines, hard cap respected.
- Soft-cap exception for passive binary/stamp inspection, checksum/version/platform guards, and actual trimmed/native Make regressions. QA found duplicate stamp copies in untrimmed Make output; adding `-trimpath` fixed it without weakening duplicate rejection.
- Independent/root full/repeated tests, race, vet/build/tidy, fixtures and formatting passed; six stripped/trimmed builds and linked Make smoke passed. Guards never execute inspected binaries; no release requests, replacement or publication occurred.

## Atomic replacement 18b2

- Baseline: `6d1a04c`; branch: `feature/18b2-atomic-replace`; 444 changed lines, hard cap respected.
- Soft-cap exception for rooted locking/owner/link/inode checks, staged publication and injected failure/cancellation/cleanup coverage. Independent/root tests, repeated fixtures, race, vet/build/tidy and formatting passed; six cross-builds and Windows test compilation passed.
- Original bytes/inode survive prepublication failures; successful rename retains Installed state on later errors. Returned-path symlink ambiguity, mode-only preservation and cooperative-filesystem limits documented. No user executable touched; optional unchanged-metadata inode-exchange regression follows in 18c.

## Update integration 18c

- Baseline: `9abf4a9`; branch: `feature/18c-update-integration`; 439 changed lines, hard cap respected.
- Soft-cap exception for injected CLI stage/deadline/publication-state coverage, fake HTTP-to-temporary-executable integration, and unchanged-metadata inode exchange. Independent/root full/repeated tests, race, vet/build/tidy, fixtures and formatting passed; native offline smoke and six cross-builds passed.
- Read-only checks remain config/credential independent; automatic installs reject unknown versions/unsupported platforms before effects, never rewrite equal/newer versions, and report successful publication even on later cancellation/cleanup/output errors. No user executable changed, inspected binary executed, real release downloaded or release published.

## MCP tool foundation 19a

- Baseline: `4237ea0`; branch: `feature/19a-mcp-tool`; 449 changed lines, hard cap respected.
- Soft-cap exception for SDK v1.8.0 dependency pins, explicit schemas, duplicate/null/unknown-field rejection, input/output bounds and protocol/cancellation/privacy coverage. Independent/root full/repeated tests, race, vet/build/tidy, fixtures and formatting passed; six all-package cross-builds and both Windows test compilations passed.
- Review caught a duplicate prompt ending in null that retained the earlier string; token-based duplicate rejection fixed it with regressions. Structured results preserve service probabilities and nullable usage; fixed wire errors retain causes only server-side. No HTTP listener, CLI registration, background process, real decision/release request or workflow change in this foundation slice.

## MCP HTTP security 19b1

- Baseline: `e38b68a`; branch: `feature/19b-mcp-http`; 432 changed lines, hard cap respected.
- Soft-cap exception for loopback/authentication guards, bounded SDK transport and incremental SSE error sanitization. Independent source review and root full test/race/vet/build/tidy, six cross-builds, fixtures and formatting passed.
- SDK parser/protocol errors are masked without buffering entire SSE streams. JSON-RPC HTTP-error negotiation, legacy batch rejection and live protocol fixtures are mandatory in 19b2 before CLI exposure; no real decision calls, persistent daemon or workflow edits.

## MCP protocol and admission 19b2a

- Baseline: `cb57af6`; branch: `feature/19b2-mcp-protocol`; 348 changed lines, hard cap respected.
- Legacy batches are rejected before dispatch; at most four decisions run with immediate overload refusal. Sanitized HTTP JSON-RPC errors preserve code/ID and only verified public protocol revisions; live supported-version and future-fallback fixtures passed source review and implementation gates.
- Client cancellation, escaped-output and writer/frame boundary regressions remain mandatory in 19b2b before CLI exposure. No paid API calls, persistent daemon or release/workflow changes.

## MCP HTTP boundaries 19b2b

- Baseline: `fa2d472`; branch: `feature/19b2b-mcp-boundaries`; 237 changed lines, below the soft cap.
- Modern socket disconnect and legacy lifetime cancellation, complete escaped output above 16 MiB, 32 MiB frame limits and terminal writer errors passed source review and implementation gates. Mapped loopback addresses now match actual listener Host values.
- Maximum 8 MiB decision JSON may yield SSE frames above the SDK client's default 16 MiB; clients need a 32 MiB event limit for maximum-size results. No real decisions, persistent daemon or release/workflow changes.

## MCP foreground runtime 19c1

- Baseline: `6b11eb8`; branch: `feature/19c1-mcp-runtime`; 324 changed lines, hard cap respected.
- Soft-cap exception for owned-listener startup failures, safe live readiness, independent decision/lifetime contexts, active-call cancellation and bounded graceful shutdown. Independent/root repeated and full gates, race, vet/build/tidy, fixtures, formatting and six cross-builds passed.
- Ready callbacks and decision services must cooperate with context cancellation; arbitrary blocking callbacks cannot be forcibly interrupted. The caller owns service cleanup. CLI exposure follows in 19c2; no paid calls or persistent daemon.

## MCP foreground command 19c2

- Baseline: `1acea95`; branch: `feature/19c2-mcp-cli`; 295 changed lines, below the soft cap.
- Lazy token/address/config/service resolution, separate credentials, owned cleanup, endpoint-only output and fixed safe errors passed independent/root full and repeated tests, race, vet/build/tidy, fixtures, formatting and six cross-builds.
- QA found readiness ignored short writes; full-length checking and cause/cleanup regression fixed it. README documents unreleased scope, setup, protocol/batch support, 32 MiB client event setting, cancellation and cooperative callback/writer limits. No paid API calls or persistent daemon.

## MCP runtime ownership 19d1a

- Baseline: `5abca68`; 405 source/test lines plus this plan/QA, hard cap respected. Independent source review, repeated tests/race, full gates and all six cross-builds passed.
- The lease never adopts or removes preexisting, exchanged or nonempty locks. Store handles stay open until lease cleanup.
- This is a mode/owner-only Unix foundation, with Windows unsupported. Darwin ACL and Windows privacy gates remain mandatory before state, key or token-file exposure. No daemon, paid calls or release changes.

## MCP private files 19d1b

- 355 source/test lines plus plan/QA, under the hard cap. Independent source audit, repeated/race coverage and root full gates passed.
- Allowlisted, bounded reads and exclusive writes validate pinned descriptors. Partial/short/write/sync/close failures preserve exchanged files and withhold unreadable secrets.
- Darwin ACL and Windows explicit-owner/DACL slices still precede state exposure. No paid requests or persistent service started.

## MCP explicit-owner creation 19d2a1

- 299 source/test/dependency lines before documentation; hard cap respected. Independent ownership/ACL/lifetime audit and root full gates passed; six cross-builds and both Windows test compilations passed.
- Windows exclusive creation is relative to a pinned root and gives the current user explicit ownership and a protected user-only DACL. Directories use directory-specific desired access. Unix files use exclusive read/write handles for later publication.
- Native Windows CI is mandatory before merge; storage remains unsupported on Windows until 19d2a2. No user service, real decision or release was started.
- Soft-cap exception: native Windows exposed a Unix-only rename assumption. The fixture now verifies both permitted rename and exact sharing-violation anchoring, without skipping or weakening privacy checks. All exact-head checks must rerun for the corrected fixture.

## MCP Windows storage privacy 19d2a2

- 340 source/test lines plus plan/QA, hard cap respected. Exact opened-handle current-user ownership, one protected user-only root DACL, inheritance, reparse and hardlink checks enable Windows storage.
- Independent review and root full gates, six cross-builds and both Windows test compilations passed. Native Windows exact-head fixtures are required before merge.
- Existing unsafe permissions are never repaired. This remains a storage foundation without background CLI exposure, paid requests or release changes.

## MCP Darwin ACL privacy 19d2b

- 193 source/test lines plus plan/QA, below the soft cap. Independent native ACL fixtures, repeated tests/race and root full gates passed; all six cross-builds passed.
- Every extended ACL is refused through the pinned file descriptor. Unknown, unavailable, truncated, malformed or nonempty extended-security results fail closed; no path-based repair or permission weakening.
- Linux mode/owner checks remain unchanged. Token/state exposure follows platform privacy completion; no paid requests, user daemon or release changes.

## MCP read-only runtime storage 19d1c

- Independent source review and repeated/race QA passed. Root full tests, race, vet/build/tidy, formatting, fixtures and six cross-builds passed on the reviewed parent. Native Linux/macOS/Windows exact-head CI is required before merge; hard cap respected.
- Inspection opens existing validated storage without mkdir, lease acquisition, repair or cleanup. Missing paths remain absent; borrowed closure preserves ownership. No paid calls, user daemon or release changes.

## MCP strict instance state 19d3a

- Independent source review and repeated/race QA passed. Root full tests, race, vet/build/tidy, formatting, fixtures and six cross-builds passed on the reviewed parent. Native Linux/macOS/Windows exact-head CI is required before merge; hard cap respected.
- Strict bounded metadata and private-key reads reject malformed, unsafe and partial state. Presence alone never proves liveness; no process IDs or credentials enter public metadata. No paid calls, user daemon or release changes.

## MCP publication link privacy 19d3a2

- Independent source review and repeated/race QA passed. Root full tests, race, vet/build/tidy, formatting, fixtures and six cross-builds passed on the reviewed parent. Native Linux/macOS/Windows exact-head CI is required before merge; hard cap respected.
- Pinned validation preserves all owner, mode, ACL and reparse checks for exactly one or two aliases. A two-link count is not ownership: publication must verify both owned names. No paid calls, user daemon or release changes.
- Native Windows caught a fixture attempting ACL mutation through a production read/write handle. The fixture now acquires independent DACL access to its own temporary path; production rights and pinned privacy assertions are unchanged. Corrected exact-head native CI must pass before merge.

## MCP owned state publication 19d3b

- Independent source review and repeated/race QA passed. Root full tests, race, vet/build/tidy, formatting, fixtures and six cross-builds passed on the reviewed parent. Native Linux/macOS/Windows exact-head CI is required before merge; hard cap respected.
- Atomic no-overwrite publication and cleanup certify every owned alias, exact contents and full privacy even during two-link staging. Independent aggregate-focused QA matches frozen feature files; unsafe exchanged or modified state is preserved. No paid calls, user daemon or release changes.
