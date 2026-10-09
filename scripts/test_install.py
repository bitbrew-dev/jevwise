"""Installer fixtures never download or execute release binaries."""
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest

SCRIPT = Path(__file__).with_name("install.sh").read_bytes()
PAYLOAD = b"fixture binary, not executable code\n"
# One fixture program supplies curl, uname and portable ownership responses.
STUB = r'''
import hashlib, json, os, pathlib, sys
name = pathlib.Path(sys.argv[0]).name
args = sys.argv[1:]
if name == "uname":
    print(os.environ["SYSTEM"] if args == ["-s"] else os.environ["ARCH"])
    sys.exit()
if name == "stat":
    print(os.environ.get("OWNER", str(os.getuid())))
    sys.exit()
assert args[0] == "-q"
if args == ["-q", "--version"]:
    print("curl " + os.environ.get("CURL_VERSION", "8.7.1") + " fixture")
    sys.exit()
assert "--location" not in args and "-L" not in args
assert args[args.index("--proto") + 1] == "=https"
assert "--connect-timeout" in args and "--max-time" in args
url, scenario = args[-1], os.environ.get("SCENARIO", "ok")
assert url.startswith("https://")
with open(os.environ["CALLS"], "a") as log:
    log.write(json.dumps(args) + "\n")
header = pathlib.Path(args[args.index("--dump-header") + 1])
output = pathlib.Path(args[args.index("--output") + 1])
repo = "https://github.com/bitbrew-dev/jevwise"
status, location, body = 200, None, b""
if scenario == "network":
    sys.exit(7)
if url.endswith("/releases/latest"):
    status, location = 302, repo + "/releases/tag/v1.2.3"
    if scenario == "latest_bad": location += "-rc1"
    if scenario == "latest_host": location = "https://evil.example/tag/v1.2.3"
    if scenario == "rate": status, location = 403, None
else:
    tag = url.split("/download/")[1].split("/")[0] if "/download/" in url else "v1.2.3"
    system = {"Darwin": "darwin", "Linux": "linux"}[os.environ["SYSTEM"]]
    arch = {"x86_64": "amd64", "amd64": "amd64", "aarch64": "arm64", "arm64": "arm64"}[os.environ["ARCH"]]
    asset = "jevwise_" + tag + "_" + system + "_" + arch
    payload = b"fixture binary, not executable code\n"
    digest = hashlib.sha256(payload).hexdigest()
    if url.endswith("SHA256SUMS"):
        body = (digest + "  " + asset + "\n").encode()
        if scenario == "legacy": body = body.replace(b"jevwise_", b"jev_")
        if scenario == "missing": body = body.replace(asset.encode(), b"other_asset")
        if scenario == "duplicate": body += body
        if scenario == "malformed": body = b"not a manifest\n"
        if scenario in ("both", "both_fail"): body += body.replace(b"jevwise_", b"jev_")
    else:
        body = payload
        if scenario in ("mismatch", "both_fail"): body += b"tampered"
        if scenario == "empty": body = b""
        if scenario == "http": status = 404
        if scenario == "size": sys.exit(63)
        if scenario.startswith("redirect") and url.startswith(repo):
            status, body = 302, b""
            location = "https://release-assets.githubusercontent.com/fixture/binary?token=fake"
            if scenario == "redirect_evil": location = "https://evil.example/binary"
            if scenario == "redirect_http": location = "http://release-assets.githubusercontent.com/binary"
            if scenario == "redirect_fragment": location += "#fragment"
header.write_text("HTTP/1.1 " + str(status) + " Fixture\r\n" +
                  ("Location: " + location + "\r\n" if location else "") +
                  ("Location: " + location + "\r\n" if scenario == "ambiguous" and location else ""))
if "--head" not in args:
    limit = int(args[args.index("--max-filesize") + 1])
    assert limit == (65536 if url.endswith("SHA256SUMS") else 67108864)
    assert len(body) <= limit
    output.write_bytes(body)
print(status, end="")
'''


@unittest.skipUnless(os.name == "posix", "POSIX installer; Windows installs are manual")
class InstallerFixtures(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="jevwise-installer-test-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.bin = self.root / "tools"
        self.bin.mkdir()
        for name in ("awk", "mktemp", "mkdir", "rm", "rmdir", "cat", "chmod", "mv", "ln", "id", "find", "shasum"):
            executable = shutil.which(name)
            self.assertIsNotNone(executable, name)
            (self.bin / name).symlink_to(executable)
        for name in ("curl", "uname", "stat"):
            stub = self.bin / name
            stub.write_text("#!" + sys.executable + "\n" + STUB)
            stub.chmod(0o755)
        self.home, self.tmp = self.root / "home", self.root / "tmp"
        self.home.mkdir(); self.tmp.mkdir()
        self.env = {"PATH": str(self.bin), "HOME": str(self.home), "TMPDIR": str(self.tmp),
                    "SYSTEM": "Linux", "ARCH": "x86_64", "CALLS": str(self.root / "calls")}
        self.target = self.home / ".local/bin/jevwise"

    def run_install(self, *args, ok=True, shell="sh", **env):
        result = subprocess.run([shutil.which(shell), "-s", "--", *args], input=SCRIPT,
                                cwd=self.root, env=dict(self.env, **env), capture_output=True, timeout=15)
        self.assertEqual(result.returncode == 0, ok, result.stderr.decode())
        self.assertEqual(list(self.tmp.iterdir()), [])
        self.assertEqual(list(self.home.rglob(".jevwise-install.*")), [])
        self.assertEqual(list(self.home.rglob(".jevwise.jev-update-lock")), [])
        return result

    def calls(self):
        path = self.root / "calls"
        return [json.loads(line)[-1] for line in path.read_text().splitlines()] if path.exists() else []

    def test_latest_platforms_and_piped_shells(self):
        for shell in ("sh", "dash"):
            if not shutil.which(shell): continue
            for system, arch in (("Linux", "x86_64"), ("Linux", "aarch64"), ("Darwin", "amd64"), ("Darwin", "arm64")):
                with self.subTest(shell=shell, system=system, arch=arch):
                    self.run_install(shell=shell, SYSTEM=system, ARCH=arch)
                    self.assertEqual(self.target.read_bytes(), PAYLOAD)
                    self.assertEqual(self.target.stat().st_mode & 0o777, 0o755)
                    self.target.unlink()
        self.assertTrue(any(url.endswith("/releases/latest") for url in self.calls()))

    def test_pinned_relative_custom_path_and_sha256sum(self):
        sha = shutil.which("sha256sum")
        if sha: (self.bin / "sha256sum").symlink_to(sha)
        self.run_install("--version", "v0.0.1", "--dir", "custom bin", HOME="")
        directory = self.root / "custom bin"
        self.assertEqual((directory / "jevwise").read_bytes(), PAYLOAD)
        self.assertEqual(sorted(p.name for p in directory.iterdir()), ["jevwise"])
        self.assertFalse(any(url.endswith("/releases/latest") for url in self.calls()))
        self.assertTrue(all("/download/v0.0.1/" in url for url in self.calls()))

    def test_help_and_invalid_inputs_do_not_fetch(self):
        self.run_install("--help")
        for args in (("--unknown",), ("--version",), ("--dir", ""),
                     *(("--version", v) for v in ("1.2.3", "v01.2.3", "v1.2.3-rc1", "v1.2.3\n", "v1.2.3/../x", "v18446744073709551616.0.0"))):
            with self.subTest(args=args): self.run_install(*args, ok=False)
        self.run_install(ok=False, HOME="")
        self.run_install(ok=False, CURL_VERSION="8.3.0")
        self.run_install(ok=False, SYSTEM="Windows")
        self.run_install(ok=False, ARCH="riscv64")
        self.assertEqual(self.calls(), [])
        self.assertFalse(self.target.exists())

    def test_failures_preserve_existing_binary_and_cleanup(self):
        self.target.parent.mkdir(parents=True)
        self.target.write_bytes(b"original")
        self.run_install(ok=False)
        self.assertEqual(self.calls(), [])
        for scenario in ("network", "rate", "latest_bad", "latest_host", "ambiguous", "missing", "duplicate", "malformed",
                         "mismatch", "empty", "http", "size", "both_fail", "redirect_evil", "redirect_http", "redirect_fragment"):
            with self.subTest(scenario=scenario):
                self.run_install("--force", ok=False, SCENARIO=scenario)
                self.assertEqual(self.target.read_bytes(), b"original")
        self.assertFalse(any("/jev_v" in url for url in self.calls()))
        self.run_install("--force", SCENARIO="redirect_ok")
        self.assertEqual(self.target.read_bytes(), PAYLOAD)

    def test_legacy_manifest_selection(self):
        self.run_install(SCENARIO="legacy")
        self.assertEqual(self.target.read_bytes(), PAYLOAD)
        self.assertTrue(self.calls()[-1].endswith("/jev_v1.2.3_linux_amd64"))
        self.target.unlink()
        self.run_install(SCENARIO="both")
        self.assertTrue(self.calls()[-1].endswith("/jevwise_v1.2.3_linux_amd64"))

    def test_unsafe_files_and_stale_lock_are_preserved(self):
        self.target.parent.mkdir(parents=True)
        victim = self.root / "victim"
        victim.write_bytes(b"untouched")
        self.target.symlink_to(victim)
        self.run_install("--force", ok=False)
        self.assertEqual(victim.read_bytes(), b"untouched")
        self.target.unlink(); self.target.mkdir()
        self.run_install("--force", ok=False)
        self.target.rmdir(); self.target.write_bytes(b"original")
        self.run_install("--force", ok=False, OWNER=str(os.getuid() + 1))
        self.assertEqual(self.target.read_bytes(), b"original")
        lock = self.target.parent / ".jevwise.jev-update-lock"
        lock.mkdir()
        result = subprocess.run([shutil.which("sh"), "-s", "--", "--force"], input=SCRIPT,
                                cwd=self.root, env=self.env, capture_output=True, timeout=15)
        self.assertNotEqual(result.returncode, 0)
        self.assertTrue(lock.is_dir())
        self.assertEqual(self.calls(), [])
        lock.rmdir()
        link = self.root / "linked dir"
        link.symlink_to(self.target.parent)
        self.run_install("--dir", str(link), ok=False)
        self.run_install("--dir", str(link) + "/", ok=False)
        self.target.parent.chmod(0o777)
        self.run_install("--force", ok=False)
        self.target.parent.chmod(0o755)


if __name__ == "__main__":
    unittest.main()
