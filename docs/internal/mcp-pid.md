# MCP diagnostic PID follow-up

## Plan and scope

- [x] 21 - Record the actual child's positive PID in schema-2 private `state.json`; accept legacy schema-1 records without a PID. Keep strict bounded decoding and authenticated instance control.
- Display the recorded PID for inspection only, after authenticated `jev mcp status`. Stop must not read process identity from the PID, signal it, or automatically remove stale state.
- Reuse owned state publication and cleanup; no separate PID file. Test strict PID decoding, legacy state, real child PID versus launcher, status/authentication and cleanup.
- One feature PR, soft 300 / hard 450 changed lines including tests/docs. Use `git ch` and sequential passing-CI squash merge. Preserve removed obsolete docs, private spec, release tags and workflows; no release dispatch or real user daemon.

## QA

- Branch `feature/21-mcp-pid`, parent `b6b6098`; 197 aggregate changed lines including tests and this note.
- Independent QA passed: strict schema/PID validation, legacy compatibility, authenticated display, unchanged stop authority, repeated/race synthetic-child lifecycle and owned cleanup.
- Root and QA full gates passed: tests, race, tidy-diff, vet, build, formatting, 10 offline Python fixtures; root also built all six Linux/macOS/Windows amd64/arm64 targets.
- Require all five exact-head CI checks, including native Linux/macOS/Windows, before squash merge; repeat full root gates after merge and remove task-owned artifacts only.
