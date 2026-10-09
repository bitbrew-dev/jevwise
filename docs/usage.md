# CLI usage

[Documentation index](../README.md#documentation)

The examples assume `jevwise` is installed on PATH. For a source build, use `bin/jevwise` instead.

## Decisions

```sh
export TYPESAFE_API_KEY='<your-key>'
jevwise decide --prompt 'Which task should I tackle first?' \
  --option 'Fix the bug' --option 'Write documentation'

printf '%s' 'Which task should I tackle first?' | jevwise decide --stdin \
  --option 'Fix the bug' --option 'Write documentation'
```

- Select exactly one of `--prompt` or `--stdin`. Stdin is read only when requested.
- Repeat `--option` at least twice. Labels must be distinct and nonblank after trimming.
- Commas remain part of a label; prompts preserve their original text and newlines.
- Stdin is limited to 1 MiB. Generic blocking input readers cannot be interrupted during a read; cancellation is checked before and after it.
- Stdout contains one JSON object with `model`, `choice`, `confidence`, `probabilities`, and optional `usage`. Values come from Jev, not local calculations.
- Errors go to stderr and cause a nonzero exit. Arbitrary service/input error text is not printed.
- `--timeout` covers the service operation, including retries and waits, after input is read.
- `--provider codex` and `--provider claude` return a clear not-implemented error without credentials or subprocesses.

## Skill setup

```sh
jevwise skill          # Default: .agent/skills/typesafe-ai/SKILL.md
jevwise skill --agent  # Explicit default target
jevwise skill --claude # .claude/skills/typesafe-ai/SKILL.md
jevwise skill --force  # Replace an existing regular skill file
jevwise skill --online # Print the upstream GitHub URL only
```

- Downloads the latest [upstream skill](https://github.com/typesafe-ai/skills/blob/main/skills/typesafe-ai/SKILL.md) unchanged, relative to the current project directory.
- Local installation is the default; `--local` selects it explicitly. `--online` and `--local`, or `--agent` and `--claude`, cannot be combined. Online mode rejects installation flags and performs no download or write.
- Hidden `--jev` selects the only supported skill. No decision credentials/configuration are read or sent. `--timeout` sets a `10s` default deadline for fetching and checks before file publication; filesystem calls are not forcibly interrupted.
- Downloads require HTTP 200 and valid UTF-8 Markdown/front matter, are limited to 1 MiB, and never follow redirects. Invalid downloads leave existing skills untouched.
- Existing files require `--force`. Static symlinks and nonregular destinations are refused; staging files are cleaned and complete bytes published without truncating the old inode.
- Native Unix supports force replacement. Windows force replacement and JS/WASI/Plan 9 installation are unsupported; filesystems without hardlinks fail safely.
- Filesystem operations assume ordinary native filesystems without hostile mounts/directory renames. Failure/cancellation after publication may mean the skill is already installed; no unsafe rollback is attempted.
