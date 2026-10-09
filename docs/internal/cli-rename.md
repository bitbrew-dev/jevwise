# Executable rename: jev to jevwise

## Plan and scope

- [x] 25 - Rename command/help/version output, default Make and native-CI filenames, actionable MCP hints and public command examples. Test name/help/completion/version and Make output. Keep release tooling's native-version check compatible with existing tags. Parent: `cebb336`.
- [x] 26 - Rename future release assets to `jevwise_...`; teach the updater to prefer the new exact name and use legacy names only when the new name is absent. Preserve size/checksum/redirect/version guards. Add offline publisher/downloader regression tests and migration notes. Parent: `b683738` (25).
- Both scopes approved by the user. One PR at a time; feature branches stacked linearly on `virtual-main`, worktrees under `.agent/worktrees`; soft 300 / hard 450 aggregate changed lines including docs/tests.
- Keep SDK import paths, `cmd` entry-point path, Jev provider/model/hidden skill flag, `TS_JEV_*` / `TYPESAFE_*`, `ts-jev` config path, runtime state and update stamps unchanged. No `jev` alias, user-binary deletion, release/tag/visibility changes or Windows dependency upgrade.

## Migration and QA

- Existing published v1.0.0, v1.1.0 and v1.2.0 retain their `jev` command/artifact family. Never rewrite published binaries or manifests. New names apply to the next release and current source builds.
- Old clients know only `jev_...` assets: document manual installation under `jevwise` for the first renamed release. Never rename/remove another vendor's `jev` binary automatically; the updater replaces its current executable path, not its filename.
- Require root and independent QA: normal/race tests, tidy/vet/build/format/diff, offline Python fixtures, six builds and credential-free smoke. Installed-agent probes remain opt-in; no paid decisions or ordinary user configuration edits.
- Require all five exact-head CI successes before an approved merge, then verify merged tree/main/virtual-main and rerun checks. Preserve private spec and user artifacts; remove only merged task-owned worktrees/branches/temp files.

- 25 local QA passed: root fresh normal/race/tidy/vet/build/format/diff, 11 Python fixtures and six Make builds; independent full tests plus focused race checks repeated three times. Command-only smoke is credential-free. CI/publication/merge pending.

- 26 local QA passed: root fresh normal/race/tidy/vet/build/format/diff, 11 offline Python fixtures, six Make builds and credential-free native smoke; independent full tests, updater race repeated five times and final updater/CLI race repeated three times. Documentation structure, links and shell syntax passed.
- Publisher integration also built six real binaries and a matching manifest from clean 25 source using synthetic version metadata, without tags, releases or uploads. Legacy-only manifests and primary failure/no-downgrade paths are covered; CI/publication/merge pending.
