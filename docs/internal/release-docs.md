# Release documentation follow-up

## Plan and scope

- [x] 22 - Correct README MCP availability: v1.1.0 includes MCP, while diagnostic PID/schema-2 support remains unreleased after v1.1.0. Remove the link to deleted compatibility notes without restoring obsolete files.
- Documentation-only branch `feature/22-release-docs` on `virtual-main` at `b3a7d91`; 33 aggregate changed lines including this note, below the soft 300 / hard 450 limits.
- Preserve private spec, code, workflows and existing release tags. No release, asset retry, user daemon or paid API calls; one reviewed PR using the established commit and merge helpers.

## QA

- Root and independent documentation QA passed: release matrix verified against tagged source, remaining local README links target tracked files, security caveats preserved, diff scope and formatting checked.
- Require all five exact-head CI checks, including native Linux/macOS/Windows, before squash merge. Recheck merged documentation and clean task-owned worktree/branch/artifacts afterward.
