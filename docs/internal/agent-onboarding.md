# Agent onboarding follow-up

## Plan and scope

- [x] 23 - Add a public Codex/Claude Code MCP setup guide: separate credentials, environment-backed bearer configuration, literal loopback endpoint, discovery checks, troubleshooting and cleanup. Link it from README; no client settings are installed automatically.
- [x] 24 - Add opt-in installed-client discovery smoke tests against a synthetic production MCP handler with temporary agent homes/configuration, no model turns or paid API calls, bounded deadlines and owned cleanup. Depends on 23; split further if needed to preserve line limits.
- One PR at a time on `virtual-main`, using `.agent/worktrees`; soft 300 / hard 450 aggregate changed lines per PR including tests/docs. Preserve private spec, removed obsolete docs, user configuration, code outside tests, workflows and release tags.

## QA

- Ticket 23 parent `78699eb`: current official MCP documentation fetched through Context7 and official OpenAI/Claude pages; local Codex 0.160.1 and Claude Code 2.1.292 CLI help checked.
- Ticket 23 root and independent QA passed: local links/anchors, JSON/TOML examples, shell syntax, Bash/Zsh token loading with no ending/LF/CRLF, security and cleanup caveats; 135 aggregate changed lines. Require all five exact-head CI checks before squash merge.
- Ticket 24 must demonstrate actual authenticated MCP discovery with zero decision calls; normal test runs must not require installed clients. Recheck merged changes and remove only task-owned worktrees/branches/artifacts.
- Ticket 24 branch `feature/24-agent-smoke`, parent `b746c97`: use explicit opt-in, temporary per-client homes/settings and a scrubbed environment. Require installed clients; no auto-install, user configuration changes, model threads/turns, release actions or real upstream requests.
- Codex probes its app-server MCP catalog using file-only credential storage and a loopback-only synthetic model provider. Claude probes connection health using bare mode and disabled nonessential traffic. Verify observed authenticated MCP requests, not just successful command exits; retain no raw client output in reports.
- Ticket 24 root and independent QA passed: each ran three normal and two race live-client repetitions on macOS with Codex 0.160.1 / Claude Code 2.1.292, plus full default-skip local gates and six platform builds. Zero tool calls, decisions and model-backend requests; 291 aggregate changed lines. Require five exact-head CI checks before merge. Managed host policies remain applicable; these probes are not an OS sandbox.
