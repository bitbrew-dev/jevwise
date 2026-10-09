# Jevwise

A Go SDK for TypeSafe AI and the `jevwise` decision CLI.

## Installation

### Homebrew

```sh
brew tap benbenbang/forge
brew install jevwise
```

- On Homebrew versions requiring explicit tap trust, or to avoid formula-name collisions, use `brew install benbenbang/forge/jevwise`. This selects and trusts only that formula: see [Homebrew tap trust](https://docs.brew.sh/Tap-Trust).
- Manage Homebrew installations with `brew upgrade jevwise`, not the standalone self-updater or install script's `--force` option.

### GitHub install script (macOS/Linux)

Latest stable release, installed as `~/.local/bin/jevwise`:

```sh
curl -fsSL https://raw.githubusercontent.com/bitbrew-dev/jevwise/main/scripts/install.sh | sh -s --
# Pin a release and choose a writable directory:
curl -fsSL https://raw.githubusercontent.com/bitbrew-dev/jevwise/main/scripts/install.sh | sh -s -- --version v1.2.3 --dir "$HOME/bin"
# From a checkout, without piping remote shell code:
sh scripts/install.sh --version latest --dir "$HOME/.local/bin"
```

- `--version latest` is the default; pinned versions must be canonical stable `vX.Y.Z` tags. Latest selection uses GitHub's public release redirect, not the anonymous REST API quota. Private releases require manual authenticated downloads.
- `--dir PATH` selects the installation directory, including relative paths. The default is `$HOME/.local/bin`, not `/usr/local/bin`. Add it to `PATH` yourself if needed.
- Requires macOS/Linux amd64/arm64, POSIX `sh`, curl **8.4+**, standard Unix utilities and `sha256sum` or `shasum`. Windows installation remains manual.
- Existing files are preserved unless `--force` is supplied. Replacement requires a regular, nonsymlink executable owned by the current user. A pinned install with `--force` can downgrade; use your package manager for managed installations.
- Exact platform assets are verified against bounded `SHA256SUMS` before same-directory publication. Legacy `jev_...` assets are supported only when the manifest lacks the new name, never after a failed download/hash check. Older binaries retain their original branding and behavior even when installed as `jevwise`.
- The script uses no sudo, modifies no shell/config/agent settings, and never executes the downloaded binary. Restart running MCP servers after replacement. Cooperating installers/updaters share a lock; stale locks require manual inspection.
- Trust the repository, GitHub/TLS, local tools and installation parents. Final directory symlinks and world-writable directories are refused; hostile concurrent writers and power-loss durability are not supported. Checksums detect corruption, not a compromised publisher.
- Piping executes remote code before you can inspect it. Prefer downloading and reviewing the script first, and pin its URL to a trusted commit/tag when reproducibility matters. The script itself is not authenticated by the binary checksum. The public URL becomes available when this change lands on `main`.

### Manual GitHub download

Download a binary and `SHA256SUMS` from [GitHub Releases](https://github.com/bitbrew-dev/jevwise/releases). Choose your platform and verify the SHA-256 checksum before installation.

| Platform | Architectures | Asset pattern |
| --- | --- | --- |
| Linux | amd64, arm64 | `jevwise_vX.Y.Z_linux_ARCH` |
| macOS | amd64, arm64 | `jevwise_vX.Y.Z_darwin_ARCH` |
| Windows | amd64, arm64 | `jevwise_vX.Y.Z_windows_ARCH.exe` |

- Install Unix binaries as `jevwise` in a directory on PATH and make them executable. Windows users install as `jevwise.exe` in a directory on PATH.
- Releases through v1.2.0 retain legacy `jev_...` filenames and original branding. No `jev` alias is installed; verify ownership before removing an old binary belonging to another project.
- Checksums detect corruption, not a compromised publisher. See [release assets and verification](docs/releases.md) for details.
- Prefer building from source? Follow the [development guide](docs/development.md#build-from-source).

## Documentation

| Guide | Contents |
| --- | --- |
| [CLI usage](docs/usage.md) | Decisions and skill setup |
| [Configuration](docs/configuration.md) | Precedence, API keys, local config and editor |
| [Go SDK](docs/sdk.md) | Quickstart, request options and retries |
| [Development](docs/development.md) | Source builds, platform builds and tests |
| [Debugging](docs/debug.md) | Debug logging and safe diagnostics |
| [Updates](docs/updates.md) | Release checks, self-update and replacement safety |
| [Releases](docs/releases.md) | Assets, verification and publication |
| [Local MCP](docs/mcp.md) | Foreground/background mode, credentials and limits |
| [Agent onboarding](docs/mcp-agents.md) | Connect Codex or Claude Code safely |
