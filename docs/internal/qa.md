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
- Remaining implementation tickets have not been marked complete. No remote publication or `main` merge has happened.

## Package layout change

- User requested `pkg/` instead of a root SDK package. Canonical SDK path is now `pkg/typesafe` with import `github.com/benbenbang/ts-jev-go-sdk/pkg/typesafe`.
- Move source and tests together without changing behavior. Completed feature branches retain their historical snapshots; unfinished branches adopt the new package before further implementation.
- Use rename-aware diffs for the feature cap: unchanged file moves count as zero added/deleted lines.
- Fresh `go test -count=1 ./...`, `go test -race -count=1 ./...`, `go vet ./...`, and CLI build/help passed with localhost-only fixture permission. The reviewer also checked every moved file against its parent by SHA256.

## Merge-phase handoff

- Remote and PR publication are pending user setup.
- Publish only the oldest unmerged feature PR at a time, with prerequisites already in its base.
- Retain task-owned worktrees under `/private/tmp/feature-x/` until PRs are merged without errors.
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
