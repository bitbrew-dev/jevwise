#!/bin/sh
# Public release installer. Requires trusted local tools and installation parents.
set -eu
LC_ALL=C
export LC_ALL

main() {
  version=latest; directory=${HOME:+$HOME/.local/bin}; force=0
  while [ "$#" -gt 0 ]; do
    case "$1" in
      --version|--dir)
        [ "$#" -ge 2 ] && [ -n "$2" ] || fail 'Missing option value'
        if [ "$1" = --version ]; then version=$2; else directory=$2; fi
        shift 2 ;;
      --force) force=1; shift ;;
      --help|-h)
        printf '%s\n' 'Usage: sh install.sh [--version latest|vX.Y.Z] [--dir PATH] [--force]' \
          'Default: latest release into ~/.local/bin. No automatic sudo.'
        return ;;
      *) fail 'Unknown option; use --help' ;;
    esac
  done
  for tool in curl awk mktemp uname mkdir rm rmdir cat chmod mv ln id stat find; do
    command -v "$tool" >/dev/null 2>&1 || fail "Required tool missing: $tool"
  done
  curl -q --version | awk 'NR==1 {split($2,v,"."); ok=(v[1]>8 || v[1]==8 && v[2]>=4)} END {exit !ok}' || fail 'curl 8.4+ is required for bounded downloads'
  case $(uname -s) in Darwin) system=darwin ;; Linux) system=linux ;; *) fail 'Installer supports macOS/Linux only' ;; esac
  case $(uname -m) in x86_64|amd64) arch=amd64 ;; arm64|aarch64) arch=arm64 ;; *) fail 'Unsupported architecture' ;; esac
  if command -v sha256sum >/dev/null 2>&1; then hash=sha256sum
  elif command -v shasum >/dev/null 2>&1; then hash=shasum
  else fail 'sha256sum or shasum is required'; fi
  [ "$version" = latest ] || valid_version "$version" || fail 'Version must be a canonical stable vX.Y.Z tag'
  [ -n "$directory" ] || fail 'Set HOME or provide --dir'
  case "$directory" in /*) ;; *) directory=$(pwd -P)/$directory ;; esac
  while [ "$directory" != / ] && [ "${directory%/}" != "$directory" ]; do directory=${directory%/}; done
  [ ! -L "$directory" ] || fail 'Install directory must not be a symlink'
  (umask 022; mkdir -p "$directory") || fail 'Cannot create install directory; choose a writable --dir'
  directory=$(cd "$directory" && pwd -P)
  [ -z "$(find "$directory" -prune -perm -0002)" ] || fail 'Install directory must not be world-writable'
  target=$directory/jevwise; stage=; work=; lock=$directory/.jevwise.jev-update-lock; locked=0
  trap cleanup 0
  trap 'exit 130' INT
  trap 'exit 143' TERM
  (umask 077; mkdir "$lock") || fail 'Update lock exists or directory is not writable; inspect before retrying'
  locked=1
  if [ -e "$target" ] || [ -L "$target" ]; then
    [ ! -L "$target" ] && [ -f "$target" ] || fail 'Destination must be a regular file, not a symlink'
    [ "$force" = 1 ] || fail 'Destination exists; use --force only after verifying ownership'
    case "$system" in darwin) owner=$(stat -f %u "$target") ;; linux) owner=$(stat -c %u "$target") ;; esac
    [ "$owner" = "$(id -u)" ] || fail 'Existing executable is owned by another user'
  fi
  case ${TMPDIR:-/tmp} in /*) ;; *) fail 'TMPDIR must be absolute' ;; esac
  umask 077
  work=$(mktemp -d "${TMPDIR:-/tmp}/jevwise-install.XXXXXXXX")
  repo=https://github.com/bitbrew-dev/jevwise
  if [ "$version" = latest ]; then
    status=$(curl -q --silent --head --globoff --proto '=https' --connect-timeout 10 --max-time 60 \
      --dump-header "$work/headers" --output /dev/null --write-out '%{http_code}' "$repo/releases/latest") || fail 'Latest release lookup failed'
    case "$status" in 301|302|303|307|308) ;; *) fail 'No latest public release redirect is available; try --version' ;; esac
    location=$(redirect)
    case "$location" in "$repo/releases/tag/"*) version=${location#"$repo/releases/tag/"} ;; *) fail 'Unexpected latest release location' ;; esac
    valid_version "$version" || fail 'Latest release tag is not a stable vX.Y.Z version'
  fi
  base=$repo/releases/download/$version
  fetch "$base/SHA256SUMS" "$work/manifest" 65536
  primary=jevwise_${version}_${system}_${arch}; legacy=jev_${version}_${system}_${arch}
  selection=$(awk -v p="$primary" -v l="$legacy" '
    {if (NF!=2 || length($1)!=64 || $1~/[^0-9a-fA-F]/ || $2~/[^A-Za-z0-9_.-]/ || $0!=$1"  "$2) bad=1
     if ($2==p) {pc++; ph=tolower($1)}; if ($2==l) {lc++; lh=tolower($1)}}
    END {if (bad || pc>1 || lc>1) exit 1; if (pc==1) print p,ph; else if (lc==1) print l,lh; else exit 1}
  ' "$work/manifest") || fail 'Manifest is invalid, ambiguous or lacks this platform'
  # Both fields are restricted to safe ASCII above; no shell evaluation occurs.
  asset=${selection%% *}; expected=${selection#* }
  fetch "$base/$asset" "$work/download" 67108864
  [ -s "$work/download" ] || fail 'Downloaded executable is empty'
  if [ "$hash" = sha256sum ]; then actual=$(sha256sum < "$work/download")
  else actual=$(shasum -a 256 < "$work/download"); fi
  actual=${actual%% *}
  [ "$actual" = "$expected" ] || fail 'SHA-256 mismatch; no executable installed'
  stage=$(mktemp "$directory/.jevwise-install.XXXXXXXX")
  cat "$work/download" > "$stage"
  chmod 755 "$stage"
  # Cooperating installers/updaters share the lock; hostile concurrent writers are unsupported.
  if [ -L "$target" ] || { [ -e "$target" ] && [ ! -f "$target" ]; }; then
    fail 'Destination changed to an unsafe entry'
  fi
  if [ "$force" = 1 ]; then mv -f "$stage" "$target"; stage=
  else ln "$stage" "$target" || fail 'Destination appeared or publication failed'; fi
  printf 'Installed Jevwise %s at %s\n' "$version" "$target"
  printf '%s\n' 'Add the install directory to PATH if needed; restart any running MCP server.'
}

fail() { printf 'jevwise installer: %s\n' "$*" >&2; exit 1; }

valid_version() {
  printf '%s\n' "$1" | awk '
    NR==1 && /^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$/ {
      sub(/^v/,""); split($0,a,"."); ok=1
      for (i=1;i<=3;i++) if (length(a[i])>20 || length(a[i])==20 && "x"a[i]>"x18446744073709551615") ok=0
    } END {exit !(NR==1 && ok)}'
}

redirect() {
  awk 'tolower($1)=="location:" {sub(/\r$/,""); sub(/^[^:]*:[ \t]*/,""); value=$0; n++}
    END {if(n!=1) exit 1; print value}' "$work/headers" || fail 'Missing or ambiguous redirect'
}

fetch() {
  original=$1; url=$1; output=$2; limit=$3; hops=0
  while :; do
    status=$(curl -q --silent --globoff --proto '=https' --connect-timeout 10 --max-time 60 \
      --max-filesize "$limit" --dump-header "$work/headers" --output "$output" --write-out '%{http_code}' "$url") || fail 'Download failed (network, TLS, timeout or size limit)'
    case "$status" in
      200) return ;;
      301|302|303|307|308)
        [ "$hops" -lt 5 ] || fail 'Too many download redirects'
        url=$(redirect)
        case "$url" in *'#'*) fail 'Redirect fragment refused' ;; esac
        case "$url" in
          "$original"|https://release-assets.githubusercontent.com/*|https://objects.githubusercontent.com/*) ;;
          *) fail 'Download redirect host refused' ;;
        esac
        hops=$((hops + 1)) ;;
      [0-9][0-9][0-9]) fail "Release download failed (HTTP $status)" ;;
      *) fail 'Invalid download status' ;;
    esac
  done
}

cleanup() {
  result=$?
  trap - 0
  if [ -n "$stage" ]; then rm -f "$stage" || result=1; fi
  if [ -n "$work" ]; then
    rm -f "$work/headers" "$work/manifest" "$work/download" || result=1
    rmdir "$work" || result=1
  fi
  if [ "$locked" = 1 ]; then rmdir "$lock" || result=1; fi
  exit "$result"
}

main "$@"
