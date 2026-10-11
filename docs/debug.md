# Debugging

[Documentation index](../README.md#documentation)

## Debug logging

```sh
jevwise --debug update --check
jevwise --debug mcp
jevwise mcp --verbose # Same logging as --debug
```

- Debug logging is opt-in via `--debug` or its `--verbose` alias, uses `github.com/phuslu/log`, and writes JSON to stderr. Normal stdout stays unchanged; logs can be redirected with `2>debug.log`.
- Covers command/stage outcomes and timing, config loading, API attempts/retries/status codes, release verification/replacement, skills, MCP requests/lifecycle and authenticated management.
- Never logs credentials, prompts/options/results, user-supplied URLs/paths, raw headers/bodies, or raw error causes. Release checks may log their fixed public GitHub endpoint and strictly numeric rate-limit metadata. Treat timing/status diagnostics as operationally sensitive when sharing logs.
- Detached background mode traces the launcher only; child output remains isolated. There is no persistent debug log file. Use foreground MCP for request diagnostics, then restart without `--debug` when finished.
- Logging is best-effort. Write failures do not change operation results. No global logger or SDK configuration is changed.

## MCP startup failures

- Either flag enables diagnostics; `--debug=false --verbose` still logs. They are flags only, not config settings. No flag means no debug trace.
- Foreground startup logs classify missing/invalid upstream API keys, unsupported providers, client configuration errors and failed/empty factories, without showing values or raw causes. Known credential failures also print actionable errors without enabling debugging.
- Set a Jev API key through `TS_JEV_API_KEY`, `TYPESAFE_API_KEY` or TOML `api_key`. The separate MCP agent bearer token is not the upstream key; see [configuration](configuration.md) and [MCP credentials](mcp.md#credentials-state-and-safety).
- If neither flag is listed by `jevwise mcp --help`, check `jevwise version` and which binary is on PATH. Older published binaries may predate logging even when the current source supports it. For Homebrew-managed installs, use `brew upgrade jevwise` once a release containing these changes is available, not the standalone updater.
