# Config command plan

- 27: centralize discovery: explicit `--config` > current-directory `jevwise.toml` > existing XDG/HOME `ts-jev/config.toml`; selected files replace, not merge. Preserve flags/environment precedence and credential-free help. Bound reads and reject nonregular local files. Never search ancestor directories.
- 28: add tested file helpers for initialization and redacted viewing; init defaults global, `--local` creates `./jevwise.toml`, explicit paths supported, conflicting selectors rejected. Publish a mode-restricted template in trusted parent storage without overwriting any existing file/link; view shows known file settings/defaults, not environment overrides or arbitrary extra keys/comments.
- 29: wire `config init`/`view` commands and CLI tests/docs.
- 30: add `config edit`, default installed `vim` on PATH for macOS/Linux; configurable `editor` executable name/path, no shell or embedded editor. Forward terminal streams, test with injected/fake processes, safe errors and cancellation. No Windows editor support promise; preserve cross-builds.
- Worktrees under `.agent/worktrees`, linear `virtual-main` branches; soft 300/hard 450 aggregate lines each. Local commits via approved git ch/OpenAI/luna; no push/merge/tag/release without approval.
- Local auto-load requires trusting the current directory, especially its base URL. Do not copy environment API keys into templates or show them in view; add local file to project ignore rules before storing credentials. Existing provider/SDK/config namespace and Windows dependency deferral remain unchanged.
- Root and independent QA: fresh normal/race tests, tidy/vet/build, offline Python fixtures, formatting/diff checks, six platform builds and synthetic credential-free CLI smoke. Never launch a real editor/agent or use normal user config during tests. Private spec remains unstaged.

- 27 local QA passed: root normal/race/tidy/vet/build, 11 offline Python fixtures, formatting/diff/docs checks and six platform builds; independent full tests and config race repeated 20 times. No network/model/editor calls.

- Implementation split adjusted to four small branches so tests and documentation remain below the 300-line target; behavior unchanged.

- 28 local QA passed: root fresh full gates and six builds; independent full tests and config race repeated 20 times. Initialization preserves existing entries and cleans staging; file view ignores environment and redacts only api_key. Mode bits are not ACL enforcement, so require trusted parent ownership/ACLs in public CLI docs.

- 29 local QA passed after correcting blank selectors and short output writes: root fresh full gates, six builds and isolated native init/view smoke; independent full tests and CLI config race repeated 20 times. Public docs qualify ACL/privacy assumptions and field-only redaction.
- Follow-up outside this scope: shared Cobra help-template write errors may be printed directly to stderr; harden the shared HelpFunc separately, not just config commands.

- 30 local QA passed: root fresh full gates/six builds and fake-editor native smoke (PATH/default/custom spaced path/repair/override/help); independent final full tests and editor/config race repeated 10 times. Bounded inherited-pipe waits and owned-helper completion are regression-tested. This cohesive security regression justifies a small soft-300 exception; hard 450 remains satisfied. No real editor, API, agent, push, tag or release used.
