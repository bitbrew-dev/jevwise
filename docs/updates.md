# Release checks and updates

[Documentation index](../README.md#documentation)

```sh
jevwise update --check
jevwise update --check --timeout 20s
jevwise update --timeout 60s # Install on supported standalone Linux/macOS builds
```

- `--debug update --check` shows the fixed `bitbrew-dev/jevwise` endpoint, HTTP status, rate-limit remaining/reset/retry seconds and failure category. Anonymous checks can be rate-limited: wait for the limit to reset. A 403 without rate-limit evidence is reported as an HTTP failure, not assumed to be rate limiting.
- `--check` checks the latest stable GitHub release without downloading a binary or changing files. No background checks, API credentials, or decision configuration are used.
- `--timeout` is a positive flag-only deadline for the whole update, default `10s`; decision timeout environment/config values do not affect updates. Increase it for slow downloads.
- Reports newer/current releases or no published release. Development/unknown builds report the latest release without inventing a version comparison.
- Accepts canonical stable `vX.Y.Z` tags only; prereleases/drafts and malformed or oversized responses are refused.
- Without `--check`, downloads and verifies a strictly newer release before replacement. Equal/newer installed versions are not rewritten or downgraded. Development, unknown and unstamped builds require manual installation.
- Automatic installation supports Linux/macOS amd64/arm64 only. Windows still supports version reporting and release checks, but updates are manual. Use your package manager for managed installations.
- After success, start a new invocation to use the new binary. An error saying the update was installed means publication succeeded but cancellation, cleanup or reporting failed: inspect `jevwise version` and any lock before retrying; no rollback occurs.

## Replacement safety

- Automatic replacement is limited to standalone Linux/macOS binaries. Windows updates remain manual; the updater never uses sudo or elevates privileges.
- A per-executable `.<binary-name>.jev-update-lock` directory serializes cooperating updates. Crashed/stale locks require manual inspection and removal, never automatic deletion.
- Guards reject unsafe returned-path targets and changed versions. Only permission bits are preserved, not group ownership, ACLs or xattrs. A complete same-directory staging file is permissioned, synced and closed before renaming; failures before publication preserve the original. Errors after publication do not roll it back.
- Filesystem checks assume ordinary cooperative local storage, not hostile concurrent writers, mount changes or power-loss durability. Context cancellation is checked at safe boundaries; filesystem calls are not interruptible.
- Use your package manager for managed installs. `os.Executable` may resolve a symlink launch to its physical target, so the updater cannot reliably identify every symlink-invoked or package-managed installation.
