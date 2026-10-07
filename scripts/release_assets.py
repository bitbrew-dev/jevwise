"""Trusted-main release tooling: plan, build immutable source, publish without replacement."""
import argparse
import base64
import datetime
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import urllib.request

REPOSITORY = "bitbrew-dev/jevwise"
PLATFORMS = [(system, arch) for system in ("darwin", "linux", "windows") for arch in ("amd64", "arm64")]
BUILDINFO = "github.com/bitbrew-dev/jevwise/internal/buildinfo"


def tag_valid(tag):
    if not isinstance(tag, str) or not re.fullmatch(r"v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)", tag):
        raise ValueError("a canonical stable version tag is required")
    if any(len(part) > 20 or int(part) > 2**64 - 1 for part in tag[1:].split(".")):
        raise ValueError("version components exceed uint64")
    return tag


def asset_names(tag):
    return [f"jev_{tag_valid(tag)}_{system}_{arch}" + (".exe" if system == "windows" else "")
            for system, arch in PLATFORMS]


def api(route, payload=None):
    host = "https://uploads.github.com" if payload is not None else "https://api.github.com"
    request = urllib.request.Request(f"{host}/repos/{REPOSITORY}/{route}".rstrip("/"), data=payload,
                                    headers={"Authorization": "Bearer " + os.environ["GH_TOKEN"],
                                             "Accept": "application/vnd.github+json",
                                             "Content-Type": "application/octet-stream"})
    # GitHub API endpoints must not redirect a privileged credential.
    class NoRedirect(urllib.request.HTTPRedirectHandler):
        def redirect_request(self, request, fp, code, message, headers, new_url):
            return None
    with urllib.request.build_opener(NoRedirect).open(request, timeout=60) as response:
        return json.load(response)


def run(arguments, cwd=None, env=None):
    return subprocess.run(arguments, cwd=cwd, env=env, check=True, capture_output=True, text=True).stdout.strip()


def fetch_source(tag):
    tag_valid(tag)
    token = os.environ.get("GH_TOKEN", "")
    if not re.fullmatch(r"[!-~]+", token):
        raise ValueError("release planning requires an authenticated token")
    url = f"https://github.com/{REPOSITORY}.git"
    authorization = base64.b64encode(("x-access-token:" + token).encode("ascii")).decode("ascii")
    # Credentials exist only in this child's environment, never argv/config.
    # Ignore inherited tracing/config overrides and prevent credential redirects.
    env = {key: value for key, value in os.environ.items()
           if not key.startswith("GIT_") and key not in ("GH_TOKEN", "GITHUB_TOKEN")}
    settings = [(f"http.{url}.extraheader", ""),
                (f"http.{url}.extraheader", "AUTHORIZATION: basic " + authorization),
                (f"http.{url}.followRedirects", "false"), (f"http.{url}.sslVerify", "true"),
                ("credential.helper", ""), (f"credential.{url}.helper", ""),
                ("fetch.recurseSubmodules", "false")]
    env.update(GIT_CONFIG_COUNT=str(len(settings)), GIT_CONFIG_NOSYSTEM="1",
               GIT_CONFIG_GLOBAL=os.devnull, GIT_TERMINAL_PROMPT="0")
    for index, (key, value) in enumerate(settings):
        env[f"GIT_CONFIG_KEY_{index}"] = key
        env[f"GIT_CONFIG_VALUE_{index}"] = value
    run(["git", "fetch", "--no-tags", url, "refs/heads/main:refs/remotes/origin/main",
         f"refs/tags/{tag}:refs/tags/{tag}"], env=env)


def stable_release(release):
    tag = tag_valid(release.get("tag_name"))
    if release.get("draft") is not False or release.get("prerelease") is not False or type(release.get("id")) is not int or release["id"] <= 0:
        raise ValueError("an existing published stable release is required")
    if release.get("html_url") != f"https://github.com/{REPOSITORY}/releases/tag/{tag}":
        raise ValueError("release repository mismatch")
    return tag


def plan(environment):
    if environment.get("GITHUB_REPOSITORY") != REPOSITORY:
        raise ValueError("publishing is restricted to the upstream repository")
    if api("").get("full_name") != REPOSITORY:
        raise ValueError("authenticated repository identity mismatch")
    event = environment.get("EVENT_NAME")
    if event == "workflow_dispatch" and environment.get("GITHUB_REF") == "refs/heads/main":
        tag = environment.get("INPUT_TAG", "")
    elif event == "release" and environment.get("EVENT_ACTION") == "published":
        tag = environment.get("EVENT_TAG", "")
        tag_valid(tag)
    else:
        raise ValueError("unsupported event or manual ref")
    release = api("releases/tags/" + tag_valid(tag) if tag else "releases/latest")
    resolved = stable_release(release)
    if tag and resolved != tag:
        raise ValueError("release tag mismatch")
    if run(["git", "status", "--porcelain"]):
        raise ValueError("trusted tooling checkout must be clean")
    fetch_source(resolved)
    commit = run(["git", "rev-parse", "--verify", f"refs/tags/{resolved}^{{commit}}"])
    if not re.fullmatch(r"[0-9a-f]{40}", commit):
        raise ValueError("invalid commit identity")
    run(["git", "merge-base", "--is-ancestor", commit, "refs/remotes/origin/main"])
    epoch = int(run(["git", "show", "-s", "--format=%ct", commit]))
    date = datetime.datetime.fromtimestamp(epoch, datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    result = {"tag": resolved, "commit": commit, "date": date, "release_id": release["id"]}
    if environment.get("GITHUB_OUTPUT"):
        with open(environment["GITHUB_OUTPUT"], "a", encoding="utf-8") as output:
            output.write(f"commit={commit}\n")
    return result


def checksums(directory, names):
    return "".join(hashlib.sha256((directory / name).read_bytes()).hexdigest() + "  " + name + "\n"
                   for name in sorted(names))


def build(metadata, source, directory):
    names = asset_names(metadata["tag"])
    if run(["git", "rev-parse", "HEAD"], cwd=source) != metadata["commit"] or run(["git", "status", "--porcelain"], cwd=source):
        raise ValueError("source must be the clean validated tag commit")
    if os.environ.get("GH_TOKEN") or os.environ.get("GITHUB_TOKEN"):
        raise ValueError("builds must not inherit publishing tokens")
    directory.mkdir(parents=True, exist_ok=False)
    flags = "-s -w " + " ".join(f"-X {BUILDINFO}.{key}={metadata[field]}"
                                 for key, field in (("Version", "tag"), ("Commit", "commit"), ("Date", "date")))
    flags += f" -X {BUILDINFO}.UpdateStamp=JEVWISE_UPDATE_V1[{metadata['tag']}]JEVWISE_UPDATE_END"
    for (system, arch), name in zip(PLATFORMS, names):
        env = dict(os.environ, CGO_ENABLED="0", GOOS=system, GOARCH=arch)
        run(["go", "build", "-mod=readonly", "-trimpath", "-ldflags", flags, "-o", str(directory / name), "./cmd"], cwd=source, env=env)
    host = (run(["go", "env", "GOHOSTOS"]), run(["go", "env", "GOHOSTARCH"]))
    native = directory / names[PLATFORMS.index(host)]
    if not run([str(native), "version"]).startswith("jev " + metadata["tag"] + "\ncommit: " + metadata["commit"] + "\n"):
        raise ValueError("linked release metadata mismatch")
    with (directory / "SHA256SUMS").open("w", encoding="ascii", newline="\n") as manifest:
        manifest.write(checksums(directory, names))


def publish(metadata, directory):
    tag = tag_valid(metadata["tag"])
    release = api("releases/tags/" + tag)
    if stable_release(release) != tag or release["id"] != metadata["release_id"] or api("commits/" + tag).get("sha") != metadata["commit"]:
        raise ValueError("release or tag changed before publication")
    names = asset_names(tag)
    if (directory / "SHA256SUMS").read_text(encoding="ascii") != checksums(directory, names):
        raise ValueError("checksum manifest mismatch")
    page = 1
    while True:
        assets = api(f"releases/{release['id']}/assets?per_page=100&page={page}")
        if any(asset.get("name") in names + ["SHA256SUMS"] for asset in assets):
            raise ValueError("release assets already exist; no overwrites or automatic resume")
        if len(assets) < 100:
            break
        page += 1
    for name in names + ["SHA256SUMS"]:
        payload = (directory / name).read_bytes()
        uploaded = api(f"releases/{release['id']}/assets?name={name}", payload)
        digest = "sha256:" + hashlib.sha256(payload).hexdigest()
        if uploaded.get("name") != name or uploaded.get("size") != len(payload) or uploaded.get("state") != "uploaded" or uploaded.get("digest", digest) not in (None, digest):
            raise ValueError("upload confirmation mismatch; manifest publication stopped")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("operation", choices=("plan", "build", "publish"))
    parser.add_argument("--plan", default="plan.json")
    parser.add_argument("--source", default="source")
    parser.add_argument("--output", default="release-assets")
    args = parser.parse_args()
    try:
        if args.operation == "plan":
            print(json.dumps(plan(os.environ)))
        else:
            metadata = json.loads(Path(args.plan).read_text(encoding="utf-8"))
            directory = Path(args.output).resolve()
            if args.operation == "build":
                build(metadata, Path(args.source).resolve(), directory)
            else:
                publish(metadata, directory)
    except Exception as error:
        raise SystemExit("release operation failed: " + type(error).__name__) from None
