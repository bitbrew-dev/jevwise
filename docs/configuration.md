# Configuration

[Documentation index](../README.md#documentation)

## CLI configuration

Precedence: **changed flags > nonblank environment > TOML > defaults**. These names apply to the CLI; the standalone Go SDK retains its `TYPESAFE_API_KEY` fallback.

Settings are grouped into tables. `[general]` selects the provider, which reads its settings from `[provider.<name>]` (`jev`, `gemini`, `codex` or `claude`); other provider tables are ignored.

```toml
[general]
provider = "jev"
debug = false
editor = "vim"

[mcp]
host = "127.0.0.1"
port = 8080

[provider.jev]
api_key = "$JEV_API_KEY"
base_url = "https://api.typesafe.ai"
model = "jev-latest"
timeout = "10s"
```

| TOML key | Flag | Environment | Default |
| --- | --- | --- | --- |
| `general.provider` | `--provider` | `TS_JEV_PROVIDER` | `jev` |
| `general.debug` | `--debug`, `--verbose` | `TS_JEV_DEBUG` | `false` |
| `general.editor` | `config edit --editor` | Not read | `vim` |
| `mcp.host` | `--listen` (with port) | `TS_JEV_MCP_BASE_URL` | `127.0.0.1` |
| `mcp.port` | `--listen` (with host) | `TS_JEV_MCP_PORT` | `8080` |
| `provider.<name>.api_key` | `--api-key` | See provider table below | Required for Jev and Gemini |
| `provider.<name>.base_url` | `--base-url` | See provider table below | See provider table below |
| `provider.<name>.model` | `--model` | See provider table below | See provider table below |
| `provider.<name>.timeout` | `--timeout` | `TS_JEV_TIMEOUT` | `10s` |

Provider environment names and defaults. Each provider reads only its own names, so a Jev key or URL never reaches Gemini, and the reverse:

| Provider | API key environment | Base URL environment, default | Model environment, default |
| --- | --- | --- | --- |
| `jev` (also `codex`, `claude`) | `JEV_API_KEY`; aliases: `JEVWISE_API_KEY`, `TS_JEV_API_KEY`, `TYPESAFE_API_KEY` | `TS_JEV_BASE_URL`, then `TYPESAFE_BASE_URL`; `https://api.typesafe.ai` | `TS_JEV_MODEL`, then `TYPESAFE_DEFAULT_MODEL`; `jev-latest` |
| `gemini` | `GOOGLE_API_KEY`, then `GEMINI_API_KEY` (as in google-genai) | `GOOGLE_GEMINI_BASE_URL`; `https://generativelanguage.googleapis.com` | None; `gemini-decision-1` |

- Changed flags apply to the selected provider. Other environment names are unchanged from the flat layout, so `TS_JEV_MCP_BASE_URL` still sets `mcp.host`.
- The `gemini` provider name is reserved in this release: selecting it reports "provider is not implemented" until the Gemini decision service is wired in.
- `mcp.host` is a literal loopback IP without scheme or port (`127.0.0.1`, `::1`); `http://`, `localhost` and non-loopback hosts fail. `mcp.port` is an integer from 1 to 65535 (`port = 8080` or `"8080"`). `jevwise mcp` and `jevwise mcp install` read only these two keys for the address; a changed `--listen` overrides both, so `jevwise mcp --listen 127.0.0.1:9090` equals `jevwise mcp` with `port = 9090` under `[mcp]`.

### Upgrading a flat configuration

Files from earlier releases put every key at the top level (`api_key = ...`, `mcp_port = ...`). They are no longer read: commands that load settings fail and ask you to migrate. Convert the selected file once:

```sh
jevwise config migrate                 # selected local or global file
jevwise config migrate --config PATH   # an explicit file
```

- Keys move to their tables unchanged: `$NAME` references stay literal and values are not validated, so you can fix them afterwards with `jevwise config edit`. `mcp_base_url` becomes `mcp.host` and `mcp_port` becomes `mcp.port`.
- The original is kept beside the file as `<file>.flat.bak` (owner-only). It may contain your API key: delete it once the migrated file is correct. Migration refuses to run if that backup exists, and refuses links, unknown keys, files mixing both layouts, and values other than strings, integers or booleans; errors name keys, never values.
- Comments are not carried over. A file already using tables is left unchanged.
- `config edit`, `version` and `mcp status`/`stop` still run on a flat file, so it can be repaired or migrated.
- Explicit file: `--config /path/to/config.toml`. Missing or invalid explicit files fail.
- Otherwise: current-directory `.jevwise.toml`, then `$XDG_CONFIG_HOME/jevwise/jevwise.toml` or `$HOME/.config/jevwise/jevwise.toml`. Only a missing global file is allowed. The selected local file replaces rather than merges with global settings; no ancestor-directory search or visible-name fallback.
- On Windows, the default global path is `%USERPROFILE%\.config\jevwise\jevwise.toml` when `XDG_CONFIG_HOME` is unset.
- Quoted `--config "~/.config/jevwise/jevwise.toml"` paths expand the current user's home directory for init, view, edit and runtime loading. `~otheruser` and shell/environment evaluation of paths are not supported. An XDG path beginning with `~/` is supported too.
- Trust the current directory before running decision/MCP commands: local settings can change the upstream destination. Environment/changed flags still win. Invalid, oversized (over 1 MiB), linked or nonregular local files fail without global fallback; help/version do not load them, except that `version` and other commands read only the `debug` key and ignore an invalid or unreadable file.
- Start with [config.example.toml](../config.example.toml). Prefer environment credentials rather than command-line secrets.
- TOML `provider.<name>.api_key` supports `$NAME`/`${NAME}`, for example `api_key = "$JEV_API_KEY"` or `api_key = "${JEV_MCP_API_KEY}"`. Runtime loading expands references and trims the result; undefined variables become empty.
- API environment names above are checked left to right, skipping blank values. `JEV_API_KEY` is for the upstream API; `JEVWISE_MCP_TOKEN` is separate local MCP authentication. Never reuse the same credential.
- TOML `provider.<name>.api_key` supports `$NAME`/`${NAME}`, for example `api_key = "${MY_JEV_KEY}"`. Runtime loading expands references and trims the result; undefined variables become empty.
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
jevwise config migrate       # Convert a flat file to grouped tables
```

- Init never overwrites existing files, directories or links. It writes a template, not your environment credentials; Unix file permissions are requested as `0600`, new parent directories as `0700`. Use a trusted parent with safe ownership/ACLs: mode bits alone do not guarantee privacy, including inherited Unix ACLs. Windows users must manage ACLs separately.
- `--config PATH` also selects an init destination; do not combine it with `--local` or pass a blank path. View prints the `[general]`, `[mcp]` and selected `[provider.<name>]` tables. It ignores environment/decision flag overrides, unknown keys, other provider tables and comments; invalid files fail safely. It is not a dump of effective runtime settings. Only the `api_key` field is automatically redacted; do not store credentials in other displayed settings.
- Set `editor = "nvim"` (or another executable name/path) under `[general]` to change the editor. Selection is `config edit --editor` > selected file's `general.editor` (or a flat file's `editor`) > `vim` on PATH; `$EDITOR`/`$VISUAL` are not read. No editor is bundled and no shell is used. Arguments are not parsed: use a trusted wrapper executable if options such as `--wait` are needed.
- Edit forwards terminal input/output and waits for the direct editor process. It uses the command context, not the decision timeout; missing editors/nonzero exits fail safely. Invalid TOML falls back to Vim for repair; wrong-typed editor settings or oversized files can be repaired using `--editor`. It refuses missing files, directories and final symlinks.
- Go-copy pipe waits are bounded after process exit/cancellation. Only the direct editor process is managed, not wrapper descendants; arbitrary blocking custom readers/writers cannot be forcibly interrupted.
- Trust local editor settings and wrappers before editing: they select a program to execute. Editor terminal output intentionally may show the file you are editing. macOS/Linux support assumes an installed editor; no Windows editor availability/behavior is promised.
- Add `.jevwise.toml` to your project's ignore rules before storing credentials. Prefer environment API keys.
