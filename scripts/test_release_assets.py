"""Offline release fixtures: API/fetch/build calls mocked; Git config parsed locally."""
import json
import base64
import io
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch
import release_assets as release

TAG, COMMIT = "v1.2.3", "a" * 40
STABLE = {"tag_name": TAG, "id": 42, "draft": False, "prerelease": False,
          "html_url": f"https://github.com/{release.REPOSITORY}/releases/tag/{TAG}"}
META = {"tag": TAG, "commit": COMMIT, "date": "2026-10-07T00:00:00Z", "release_id": 42}
UPSTREAM = {"full_name": release.REPOSITORY, "private": False}
ENV = {"GITHUB_REPOSITORY": release.REPOSITORY, "EVENT_NAME": "workflow_dispatch", "GITHUB_REF": "refs/heads/main"}


class ReleaseFixtures(unittest.TestCase):
    def test_api_hosts_credentials_method_and_redirect_refusal(self):
        with patch.dict(os.environ, {"GH_TOKEN": "fixture-token"}), patch.object(release.urllib.request, "build_opener") as opener:
            opener.return_value.open.side_effect = [io.BytesIO(b'{}'), io.BytesIO(b'{}')]
            release.api("")
            release.api("releases/42/assets?name=fixture", b"binary")
            requests = [call.args[0] for call in opener.return_value.open.call_args_list]
            self.assertEqual(requests[0].full_url, "https://api.github.com/repos/bitbrew-dev/jevwise")
            self.assertEqual(requests[0].get_method(), "GET")
            self.assertTrue(requests[1].full_url.startswith("https://uploads.github.com/"))
            self.assertEqual(requests[1].get_method(), "POST")
            self.assertEqual(requests[1].get_header("Authorization"), "Bearer fixture-token")
            self.assertEqual(requests[1].get_header("Content-type"), "application/octet-stream")
            handler = opener.call_args.args[0]()
            self.assertIsNone(handler.redirect_request(None, None, 302, "redirect", {}, "https://evil.example"))

    def test_canonical_version_and_asset_contract(self):
        for tag in ("v0.0.0", TAG, "v18446744073709551615.0.0"):
            self.assertEqual(release.tag_valid(tag), tag)
        for tag in ("v01.2.3", "1.2.3", "v1.2.3-rc1", "v1.2.3\n", "v1.2.3/../x", "v18446744073709551616.0.0", None):
            with self.assertRaises(ValueError):
                release.tag_valid(tag)
        names = release.asset_names(TAG)
        self.assertEqual(len(set(names)), 6)
        self.assertEqual(names[-2:], ["jev_v1.2.3_windows_amd64.exe", "jev_v1.2.3_windows_arm64.exe"])

    @patch.dict(os.environ, {"GH_TOKEN": "fixture-token"})
    @patch.object(release, "run", side_effect=["", "", COMMIT, "", "1791331200"])
    @patch.object(release, "api", side_effect=[UPSTREAM, STABLE])
    def test_plan_latest_fallback_and_ancestry(self, api, run):
        result = release.plan(ENV)
        self.assertEqual(result, META)
        api.assert_any_call("releases/latest")
        self.assertIn(["git", "merge-base", "--is-ancestor", COMMIT, "refs/remotes/origin/main"], [call.args[0] for call in run.call_args_list])

    @patch.dict(os.environ, {"GH_TOKEN": "fixture-token"})
    def test_plan_guards_and_release_identity(self):
        for change in ({"GITHUB_REPOSITORY": "fork/repo"}, {"GITHUB_REF": "refs/heads/other"}, {"EVENT_NAME": "push"}, {"INPUT_TAG": "bad"}):
            with patch.object(release, "api", return_value=UPSTREAM), self.assertRaises(ValueError):
                release.plan(dict(ENV, **change))
        for change in ({"draft": True}, {"prerelease": True}, {"id": True}, {"html_url": "https://evil.example"}):
            with self.assertRaises(ValueError):
                release.stable_release(dict(STABLE, **change))
        for outputs in (["dirty"], ["", "", COMMIT, subprocess.CalledProcessError(1, "git")]):
            with patch.object(release, "api", side_effect=[UPSTREAM, STABLE]), patch.object(release, "run", side_effect=outputs), self.assertRaises((ValueError, subprocess.CalledProcessError)):
                release.plan(ENV)

    @patch.dict(os.environ, {"GH_TOKEN": "fixture-token"})
    def test_public_and_private_published_releases_keep_source_guards(self):
        events = [dict(ENV, INPUT_TAG=TAG), dict(ENV, EVENT_NAME="release",
                  EVENT_ACTION="published", EVENT_TAG=TAG)]
        for private in (False, True):
            for environment in events:
                with self.subTest(private=private, event=environment["EVENT_NAME"]):
                    with patch.object(release, "api", side_effect=[dict(UPSTREAM, private=private), STABLE]), \
                            patch.object(release, "run", side_effect=["", "", COMMIT, "", "1791331200"]) as run:
                        self.assertEqual(release.plan(environment), META)
                    fetch = run.call_args_list[1]
                    self.assertEqual(fetch.args[0], ["git", "fetch", "--no-tags",
                                     f"https://github.com/{release.REPOSITORY}.git",
                                     "refs/heads/main:refs/remotes/origin/main",
                                     f"refs/tags/{TAG}:refs/tags/{TAG}"])
                    self.assertIn(["git", "merge-base", "--is-ancestor", COMMIT,
                                   "refs/remotes/origin/main"], [call.args[0] for call in run.call_args_list])
        for repository in ({}, dict(UPSTREAM, full_name="fork/repo")):
            with patch.object(release, "api", return_value=repository), \
                    patch.object(release, "run") as run, self.assertRaises(ValueError):
                release.plan(ENV)
            run.assert_not_called()
        with patch.object(release, "api") as api, self.assertRaises(ValueError):
            release.plan(dict(ENV, GITHUB_REPOSITORY="fork/repo"))
        api.assert_not_called()

    def test_fetch_credentials_are_ephemeral_fixed_url_and_not_traced(self):
        environment = {"GH_TOKEN": "fixture-token", "GITHUB_TOKEN": "other-fixture-token",
                       "PATH": "/fixture/bin", "GIT_TRACE": "1", "GIT_TRACE_CURL": "1",
                       "GIT_CONFIG_PARAMETERS": "inherited-config",
                       "GIT_CONFIG_COUNT": "99", "GIT_CONFIG_KEY_98": "inherited-key",
                       "GIT_CONFIG_VALUE_98": "inherited-secret"}
        with patch.dict(os.environ, environment, clear=True), patch.object(release, "run") as run:
            before = dict(os.environ)
            release.fetch_source(TAG)
            self.assertEqual(dict(os.environ), before)
            release.run(["git", "status", "--porcelain"])
            self.assertNotIn("env", run.call_args.kwargs)
        fetch = run.call_args_list[0]
        self.assertNotIn("fixture-token", " ".join(fetch.args[0]))
        env = fetch.kwargs["env"]
        settings = [(env[f"GIT_CONFIG_KEY_{n}"], env[f"GIT_CONFIG_VALUE_{n}"])
                    for n in range(int(env["GIT_CONFIG_COUNT"]))]
        header = f"http.https://github.com/{release.REPOSITORY}.git.extraheader"
        self.assertEqual(settings[:2], [(header, ""), (header, "AUTHORIZATION: basic " +
                         base64.b64encode(b"x-access-token:fixture-token").decode("ascii"))])
        self.assertIn((f"http.https://github.com/{release.REPOSITORY}.git.followRedirects", "false"), settings)
        self.assertIn((f"http.https://github.com/{release.REPOSITORY}.git.sslVerify", "true"), settings)
        self.assertIn(("credential.helper", ""), settings)
        self.assertIn((f"credential.https://github.com/{release.REPOSITORY}.git.helper", ""), settings)
        self.assertIn(("fetch.recurseSubmodules", "false"), settings)
        self.assertEqual(env["GIT_CONFIG_GLOBAL"], os.devnull)
        self.assertEqual(env["GIT_CONFIG_NOSYSTEM"], "1")
        self.assertEqual(env["GIT_TERMINAL_PROMPT"], "0")
        for key in ("GH_TOKEN", "GITHUB_TOKEN", "GIT_TRACE", "GIT_TRACE_CURL",
                    "GIT_CONFIG_PARAMETERS", "GIT_CONFIG_KEY_98", "GIT_CONFIG_VALUE_98"):
            self.assertNotIn(key, env)

    def test_actual_git_url_settings_override_inherited_config_without_persistence(self):
        with patch.dict(os.environ, {"GH_TOKEN": "fixture-token"}), patch.object(release, "run") as run:
            release.fetch_source(TAG)
        env = run.call_args.kwargs["env"]
        url = f"https://github.com/{release.REPOSITORY}.git"
        with tempfile.TemporaryDirectory() as temp:
            config = Path(temp) / "config"
            inherited = (f'[http "{url}"]\n followRedirects = true\n sslVerify = false\n extraheader = stale-header\n'
                         f'[credential "{url}"]\n helper = stale-helper\n')
            config.write_text(inherited)
            # Model inherited URL-specific settings while exercising Git's real
            # runtime config parser. There is no fetch, init or config write.
            env["GIT_CONFIG_GLOBAL"] = str(config)
            def setting(key, target=url):
                return subprocess.run(["git", "config", "--get-urlmatch", key, target],
                                      cwd=temp, env=env, capture_output=True, text=True)
            self.assertEqual(setting("http.followRedirects").stdout.strip(), "false")
            self.assertEqual(setting("http.sslVerify").stdout.strip(), "true")
            self.assertEqual(setting("credential.helper").stdout.strip(), "")
            self.assertEqual(setting("credential.helper").returncode, 0)
            self.assertEqual(setting("http.extraheader").stdout.splitlines(), [
                             "AUTHORIZATION: basic " + base64.b64encode(b"x-access-token:fixture-token").decode("ascii")])
            for other in ("https://github.com/another/repo.git", "https://evil.example/repo.git",
                          url + "-other"):
                self.assertEqual(setting("http.extraheader", other).returncode, 1)
            self.assertEqual(config.read_text(), inherited)

    def test_fetch_failures_never_persist_credentials(self):
        for token in (None, "", "bad\ntoken", "bad token", "nonascii-\u00e9"):
            with patch.dict(os.environ, {} if token is None else {"GH_TOKEN": token}, clear=True), \
                    patch.object(release, "run") as run, self.assertRaises(ValueError):
                release.fetch_source(TAG)
            run.assert_not_called()
        with patch.dict(os.environ, {"GH_TOKEN": "fixture-token"}, clear=True), \
                patch.object(release, "run", side_effect=subprocess.CalledProcessError(1, "git")) as run:
            before = dict(os.environ)
            with self.assertRaises(subprocess.CalledProcessError):
                release.fetch_source(TAG)
            self.assertEqual(dict(os.environ), before)
            self.assertEqual(run.call_count, 1)
        with patch.object(release, "run") as run, self.assertRaises(ValueError):
            release.fetch_source("bad")
        run.assert_not_called()

    def test_builds_six_targets_without_token_and_stable_checksums(self):
        with tempfile.TemporaryDirectory() as temp, patch.dict(os.environ, {}, clear=True):
            directory = Path(temp) / "assets"
            def build_run(arguments, cwd=None, env=None):
                if arguments[:3] == ["git", "rev-parse", "HEAD"]:
                    return COMMIT
                if arguments[0] == "git":
                    return ""
                if arguments[:2] == ["go", "env"]:
                    return {"GOHOSTOS": "darwin", "GOHOSTARCH": "arm64"}[arguments[2]]
                if arguments[:2] == ["go", "build"]:
                    self.assertEqual(env["CGO_ENABLED"], "0")
                    self.assertIn("-trimpath", arguments)
                    self.assertIn(f"{release.BUILDINFO}.UpdateStamp=JEVWISE_UPDATE_V1[{TAG}]JEVWISE_UPDATE_END", arguments[arguments.index("-ldflags") + 1])
                    Path(arguments[arguments.index("-o") + 1]).write_bytes((env["GOOS"] + env["GOARCH"]).encode())
                    return ""
                self.assertTrue(arguments[0].endswith("darwin_arm64"))
                return f"jev {TAG}\ncommit: {COMMIT}\n"
            with patch.object(release, "run", side_effect=build_run) as run:
                release.build(META, Path(temp), directory)
            self.assertEqual(sum(call.args[0][:2] == ["go", "build"] for call in run.call_args_list), 6)
            manifest = (directory / "SHA256SUMS").read_text()
            self.assertEqual(manifest, release.checksums(directory, release.asset_names(TAG)))
            self.assertEqual(manifest.splitlines(), sorted(manifest.splitlines(), key=lambda line: line.split("  ")[1]))
            with patch.dict(os.environ, {"GH_TOKEN": "secret"}), patch.object(release, "run", side_effect=[COMMIT, ""]), self.assertRaises(ValueError):
                release.build(META, Path(temp), Path(temp) / "blocked")

    def test_publication_preflight_identity_pagination_and_manifest_last(self):
        with tempfile.TemporaryDirectory() as temp:
            directory = Path(temp)
            names = release.asset_names(TAG)
            for name in names:
                (directory / name).write_bytes(name.encode())
            (directory / "SHA256SUMS").write_text(release.checksums(directory, names))
            pages = [[{"name": "unrelated"}] * 100, []]
            confirmations = [{"name": name, "size": len((directory / name).read_bytes()), "state": "uploaded"}
                             for name in names + ["SHA256SUMS"]]
            with patch.object(release, "api", side_effect=[STABLE, {"sha": COMMIT}, *pages, *confirmations]) as api:
                release.publish(META, directory)
            uploads = [call for call in api.call_args_list if len(call.args) == 2]
            self.assertEqual([call.args[0] for call in uploads], [f"releases/42/assets?name={name}" for name in names + ["SHA256SUMS"]])
            for broken in ({}, {"state": "new"}, {"size": 1}, {"name": "wrong"}, {"digest": "sha256:wrong"}):
                result = dict(confirmations[0], **broken) if broken else {}
                with patch.object(release, "api", side_effect=[STABLE, {"sha": COMMIT}, [], result]) as api, self.assertRaises(ValueError):
                    release.publish(META, directory)
                self.assertEqual(sum(len(call.args) == 2 for call in api.call_args_list), 1)
            with patch.object(release, "api", side_effect=[STABLE, {"sha": COMMIT}, [], confirmations[0], RuntimeError("upload failed")]) as api, self.assertRaises(RuntimeError):
                release.publish(META, directory)
            self.assertNotIn("SHA256SUMS", api.call_args_list[-1].args[0])
            for collision in names + ["SHA256SUMS"]:
                with patch.object(release, "api", side_effect=[STABLE, {"sha": COMMIT}, [{"name": collision}]]) as api, self.assertRaises(ValueError):
                    release.publish(META, directory)
                self.assertFalse(any(len(call.args) == 2 for call in api.call_args_list))
            with patch.object(release, "api", side_effect=[dict(STABLE, id=99)]), self.assertRaises(ValueError):
                release.publish(META, directory)
            (directory / names[0]).write_bytes(b"tampered")
            with patch.object(release, "api", side_effect=[STABLE, {"sha": COMMIT}]), self.assertRaises(ValueError):
                release.publish(META, directory)


if __name__ == "__main__":
    unittest.main()
