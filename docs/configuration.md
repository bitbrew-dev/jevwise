# Configuration

[Documentation index](../README.md#documentation)

## CLI configuration

Precedence: **changed flags > nonblank environment > TOML > defaults**. These names apply to the CLI; the standalone Go SDK retains its `TYPESAFE_API_KEY` fallback.

| Setting | Flag | Environment | Default |
| --- | --- | --- | --- |
| API key | `--api-key` | `JEV_API_KEY`; aliases: `JEVWISE_API_KEY`, `TS_JEV_API_KEY`, `TYPESAFE_API_KEY` | Required for Jev |
| Base URL | `--base-url` | `TS_JEV_BASE_URL`, then `TYPESAFE_BASE_URL` | `https://api.typesafe.ai` |
| Model | `--model` | `TS_JEV_MODEL`, then `TYPESAFE_DEFAULT_MODEL` | `jev-latest` |
| Provider | `--provider` | `TS_JEV_PROVIDER` | `jev` |
| Operation timeout | `--timeout` | `TS_JEV_TIMEOUT` | `10s` |

- Explicit file: `--config /path/to/config.toml`. Missing or invalid explicit files fail.
- Otherwise: current-directory `.jevwise.toml`, then `$XDG_CONFIG_HOME/jevwise/jevwise.toml` or `$HOME/.config/jevwise/jevwise.toml`. Only a missing global file is allowed. The selected local file replaces rather than merges with global settings; no ancestor-directory search or visible-name fallback.
- On Windows, the default global path is `%USERPROFILE%\.config\jevwise\jevwise.toml` when `XDG_CONFIG_HOME` is unset.
- Quoted `--config "~/.config/jevwise/jevwise.toml"` paths expand the current user's home directory for init, view, edit and runtime loading. `~otheruser` and shell/environment evaluation of paths are not supported. An XDG path beginning with `~/` is supported too.
- Trust the current directory before running decision/MCP commands: local settings can change the upstream destination. Environment/changed flags still win. Invalid, oversized (over 1 MiB), linked or nonregular local files fail without global fallback; help/version do not load them.
- Start with [config.example.toml](../config.example.toml). Prefer environment credentials rather than command-line secrets.
- TOML `api_key` supports `$NAME`/`${NAME}`, for example `api_key = "$JEV_API_KEY"` or `api_key = "${JEV_MCP_API_KEY}"`. Runtime loading expands references and trims the result; undefined variables become empty.
- API environment names above are checked left to right, skipping blank values. `JEV_API_KEY` is for the upstream API; `JEVWISE_MCP_TOKEN` is separate local MCP authentication. Never reuse the same credential.
- TOML `api_key` supports `$NAME`/`${NAME}`, for example `api_key = "${MY_JEV_KEY}"`. Runtime loading expands references and trims the result; undefined variables become empty.
- Custom environment names work through TOML references, for example `api_key = "${JEV_MCP_API_KEY}"`. Export the variable in the process environment before starting Jevwise. Exporting `JEV_MCP_API_KEY` alone is not an automatic API-key override; nonblank `TS_JEV_API_KEY`/`TYPESAFE_API_KEY` still take precedence over the file.
- Local discovery never loads `config.toml`, `configs.toml`, YAML files or ancestor configs. Only `.jevwise.toml` is a local candidate; visible `jevwise.toml` is not auto-loaded. Legacy `ts-jev/config.toml` is no longer discovered: migrate manually or select it with `--config`. An explicit `--config PATH` can still select another trusted filename.
- `JEV_MCP_API_KEY` is your chosen upstream-key variable when referenced by `api_key`; it is not the separate MCP agent bearer token (`JEV_MCP_TOKEN` or `--token-file`).
- Environment and `--api-key` values stay literal. File viewing never expands references or requires a key; Jev service initialization requires a nonblank key.

### Initialize, inspect and edit

```sh
jevwise config init          # Global XDG/HOME config, even if local exists
jevwise config init --local  # ./.jevwise.toml, automatically selected in this cwd
jevwise config view          # Selected file/defaults, API key redacted
jevwise config view --config /path/to/config.toml
jevwise config edit          # Selected local/global file, vim by default
jevwise config edit --editor nvim # One-shot override, also for repairs
```

- Init never overwrites existing files, directories or links. It writes a template, not your environment credentials; Unix file permissions are requested as `0600`, new parent directories as `0700`. Use a trusted parent with safe ownership/ACLs: mode bits alone do not guarantee privacy, including inherited Unix ACLs. Windows users must manage ACLs separately.
- `--config PATH` also selects an init destination; do not combine it with `--local` or pass a blank path. View ignores environment/decision flag overrides, unknown keys and comments; invalid files fail safely. It is not a dump of effective runtime settings. Only the `api_key` field is automatically redacted; do not store credentials in other displayed settings.
- Set `editor = "nvim"` (or another executable name/path) in TOML to change the editor. Selection is `config edit --editor` > selected file's `editor` > `vim` on PATH; `$EDITOR`/`$VISUAL` are not read. No editor is bundled and no shell is used. Arguments are not parsed: use a trusted wrapper executable if options such as `--wait` are needed.
- Edit forwards terminal input/output and waits for the direct editor process. It uses the command context, not the decision timeout; missing editors/nonzero exits fail safely. Invalid TOML falls back to Vim for repair; wrong-typed editor settings or oversized files can be repaired using `--editor`. It refuses missing files, directories and final symlinks.
- Go-copy pipe waits are bounded after process exit/cancellation. Only the direct editor process is managed, not wrapper descendants; arbitrary blocking custom readers/writers cannot be forcibly interrupted.
- Trust local editor settings and wrappers before editing: they select a program to execute. Editor terminal output intentionally may show the file you are editing. macOS/Linux support assumes an installed editor; no Windows editor availability/behavior is promised.
- Add `.jevwise.toml` to your project's ignore rules before storing credentials. Prefer environment API keys.
