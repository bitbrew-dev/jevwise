# Release binaries and publishing

[Documentation index](../README.md#documentation)

The release-assets workflow attaches raw CLI binaries and `SHA256SUMS` to an existing stable [GitHub release](https://github.com/bitbrew-dev/jevwise/releases). It does not create versions or releases.

- Publishing supports the upstream repository whether public or private. Actions authenticates planning and upload; source-fetch credentials are temporary and never persisted or passed to builds.
- CLI release checks and self-update remain anonymous/public-only. For private releases, download assets through authenticated GitHub access and install manually.
- To retry asset publication without creating another release, run the release-assets workflow on `main` with the existing release tag. Existing assets are never overwritten or automatically resumed.

| Platform | Architectures | Asset pattern |
| --- | --- | --- |
| Linux | amd64, arm64 | `jevwise_vX.Y.Z_linux_ARCH` |
| macOS | amd64, arm64 | `jevwise_vX.Y.Z_darwin_ARCH` |
| Windows | amd64, arm64 | `jevwise_vX.Y.Z_windows_ARCH.exe` |

- Future releases use the patterns above; existing v1.0.0, v1.1.0 and v1.2.0 keep their legacy `jev_...` names and are not rewritten.
- Download the exact OS/architecture asset. Install it as `jevwise` (`jevwise.exe` on Windows); Unix users must make it executable. Verify `jevwise version` before use.
- For the first renamed release, use the install script or install manually: older `jev` clients cannot find `jevwise_...` assets. Confirm binary ownership before removing an old copy; never remove another project's `jev`. Regenerate shell completions with `jevwise completion SHELL` and review saved scripts or agent commands.
- The new updater prefers `jevwise_...`, using `jev_...` only when the new exact platform name is absent from release metadata, never after a verification/download failure. It replaces its current executable path without renaming that path or installing a legacy alias.
- `SHA256SUMS` hashes the exact raw binary bytes. Verify the hash before use. Checksums detect corruption, not a compromised publisher; trust remains the repository and GitHub/TLS.
- The internal update downloader verifies exact platform asset names, metadata sizes and SHA-256 before returning bytes. Binary/manifest limits are 64 MiB/64 KiB; downloads use fixed release URLs and bounded HTTPS redirects to allowlisted GitHub hosts, without credentials or cookies.
- Release/Make builds embed a passive update stamp that survives stripping and path trimming. Version guards inspect Go module/platform metadata, recheck the candidate checksum, and reject equal/older or mismatched on-disk versions without executing binaries. Stamps are self-declared metadata, not signatures.
- Your manual semantic-release workflow still owns release creation. Asset publication runs after a published release, with a manual tag-based fallback. No release is created during development QA.
- Publication rejects existing target assets and uploads the checksum manifest last. If publication fails midway, inspect and remove incomplete assets manually before retrying; no automatic overwrite/delete/resume occurs.
- Native CI tests Linux, macOS, and Windows CLI execution. Windows arm64 is cross-built, not claimed as natively executed.
