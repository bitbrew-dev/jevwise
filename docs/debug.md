# Debugging

[Documentation index](../README.md#documentation)

## Debug logging

```sh
jevwise --debug update --check
jevwise --debug mcp
```

- Debug logging is opt-in via `--debug`, uses `github.com/phuslu/log`, and writes JSON to stderr. Normal stdout stays unchanged; logs can be redirected with `2>debug.log`.
- Covers command/stage outcomes and timing, config loading, API attempts/retries/status codes, release verification/replacement, skills, MCP requests/lifecycle and authenticated management.
- Never logs credentials, prompts/options/results, user-supplied URLs/paths, raw headers/bodies, or raw error causes. Release checks may log their fixed public GitHub endpoint and strictly numeric rate-limit metadata. Treat timing/status diagnostics as operationally sensitive when sharing logs.
- Detached background mode traces the launcher only; child output remains isolated. There is no persistent debug log file. Use foreground MCP for request diagnostics, then restart without `--debug` when finished.
- Logging is best-effort. Write failures do not change operation results. No global logger or SDK configuration is changed.
