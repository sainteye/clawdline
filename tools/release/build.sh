#!/bin/bash
# Build every artifact of one release, and its unsigned manifest
# (docs/releasing.md). Signing is a separate step, so the key is never needed
# on a machine that only builds.
#
# usage: tools/release/build.sh <version> <out-dir> [--channel stable|beta|dev]
#          [--base-url URL] [--min-version vX.Y.Z] [--no-app] [--skip-tests]
#          [--test-key <base64 public key>] [--tmux-from <dir>] [--host-only]
#          [--tmux-installed]
#
# --channel dev is a dev build (`clawdline dev install`, docs/updates.md): a
# vX.Y.Z-dev.<commit> version, unsigned, installed only on the machine that
# built it. --host-only builds this machine's os/arch alone. --tmux-installed,
# for a dev build only, says the --tmux-from binaries are the ones an
# installed release carries: signed ad hoc after their tmux.json was written,
# so their hash is not the one it records. They must still be the pinned tmux.
#
# <out-dir> must not exist. It ends up holding:
#   clawdline_<v>_<os>_<arch>.tar.gz   `clawdline`, `dist/`, and the tmux the
#                                      release carries (`libexec/tmux`,
#                                      `licenses/`, `libexec/tmux.json`), the
#                                      layout `current` points at
#   Clawdline-<v>-macos-arm64.tar.gz   the app bundle (macOS hosts with swiftc)
#   SHA256SUMS                         for manual checks and older installers
#   manifest.json                      what `tools/release sign` signs
#   <archive>.spdx.json                the SBOM of each archive (not for dev)
#
# The tmux each archive carries is built by tools/release/tmux/build.sh
# (pinned, hash-checked sources; Docker for the Linux two). --tmux-from <dir>
# takes them already built instead, from <dir>/<os>-<arch>/, as that script
# leaves them; each one's tmux.json must name the pinned tmux.
#
# --test-key compiles one more trusted key into the binaries. It is accepted
# only for a -test.N version, which the release workflow refuses to publish, so
# no public release can trust a key that is not in internal/adapters/release.
set -euo pipefail
cd "$(dirname "$0")/../.."

die() { echo "release build: $*" >&2; exit 2; }

[ "$#" -ge 2 ] || die "usage: tools/release/build.sh <version> <out-dir> [options]"
version=$1 out=$2
shift 2
channel=stable base_url="" min_version="" app=1 tests=1 test_key="" tmux_from="" host_only=0 tmux_installed=0
while [ "$#" -gt 0 ]; do
  case "$1" in
    --channel) channel=${2:?}; shift ;;
    --base-url) base_url=${2:?}; shift ;;
    --min-version) min_version=${2:?}; shift ;;
    --no-app) app=0 ;;
    --skip-tests) tests=0 ;;
    --test-key) test_key=${2:?}; shift ;;
    --tmux-from) tmux_from=${2:?}; shift ;;
    --host-only) host_only=1 ;;
    --tmux-installed) tmux_installed=1 ;;
    *) die "unknown option $1" ;;
  esac
  shift
done

[[ "$version" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.]+)?$ ]] || die "$version is not vX.Y.Z[-pre]"
case "$channel" in stable|beta|dev) ;; *) die "--channel is stable, beta or dev" ;; esac
[ "$channel" = stable ] && [[ "$version" == *-* ]] && die "a pre-release version belongs on the beta channel"
[ "$channel" = dev ] && [[ ! "$version" =~ -dev\.[0-9a-f]+$ ]] && die "a dev build is vX.Y.Z-dev.<commit>"
[ "$channel" != dev ] && [[ "$version" =~ -dev\. ]] && die "a -dev. version is built with --channel dev"
[ "$tmux_installed" = 1 ] && { [ "$channel" = dev ] && [ -n "$tmux_from" ]; } || [ "$tmux_installed" = 0 ] ||
  die "--tmux-installed is for a dev build with --tmux-from"
if [ -n "$test_key" ]; then
  [[ "$version" =~ -test\.[0-9]+$ ]] || die "--test-key is only for a vX.Y.Z-test.N version"
fi
[[ "$version" =~ -test\. ]] && [ -z "$test_key" ] && die "a -test version must carry --test-key"
[ ! -e "$out" ] || die "$out already exists"
[ -z "$(git status --porcelain)" ] || die "the checkout has changes; a release is built from a commit"

commit=$(git rev-parse HEAD)
committed_at=$(TZ=UTC git show -s --format=%cd --date=format-local:%Y-%m-%dT%H:%M:%SZ "$commit")
[ -n "$base_url" ] || base_url="https://github.com/sainteye/clawdline/releases/download/$version"

# A session the daemon opened carries its production switches; they are inputs
# to a running service, not to a build (tools/build-linux-user-release.sh).
while IFS='=' read -r name _; do
  case "$name" in CLAWDLINE_NEXT_*) unset "$name" ;; esac
done < <(env)
unset VITE_HOSTED_CONSOLE CLAWDLINE_WEB_SOURCE || true

mkdir -p "$out"
out=$(cd "$out" && pwd -P)
work=$(mktemp -d)
# finished: macOS's /bin/bash 3.2 exits 0 after an unbound-variable error when
# an EXIT trap runs, so a build that stopped half-way reported success
# (v0.10.0, 2026-10-07). A run that did not reach its last line fails.
finished=0
# shellcheck source=tools/release/codesign.sh
. "$(dirname "$0")/codesign.sh"
trap 's=$?; codesign_end; rm -rf -- "$work"; if [ "$finished" != 1 ] && [ "$s" = 0 ]; then exit 1; fi' EXIT
# official_mac: an official release built on a Mac is signed with the
# hardened runtime and notarized (codesign.sh). The notary credentials are
# checked here, before anything is compiled, so a release that could not be
# notarized stops at once instead of after the build.
official_mac=0
if [ "$(uname -s)" = Darwin ]; then
  codesign_begin
  if [ "$channel" != dev ] && [ -z "$test_key" ]; then
    codesign_require_developer_id
    official_mac=1
    export CLAWDLINE_CODESIGN_HARDENED=1
    [ -n "${CLAWDLINE_NOTARY_KEY:-}" ] && [ -n "${CLAWDLINE_NOTARY_KEY_ID:-}" ] && [ -n "${CLAWDLINE_NOTARY_ISSUER:-}" ] ||
      die "an official macOS release is notarized: set CLAWDLINE_NOTARY_KEY, CLAWDLINE_NOTARY_KEY_ID and CLAWDLINE_NOTARY_ISSUER"
  fi
fi

if [ "$tests" = 1 ]; then
  ( umask 022; go test ./... )
  go vet ./...
fi

echo "building the console…"
( cd web && npm ci --ignore-scripts --silent && npm run build --silent )
dist=web/console/dist
main=$(find "$dist/assets" -maxdepth 1 -type f -name 'main-*.js' -print)
[ "$(printf '%s\n' "$main" | sed '/^$/d' | wc -l | tr -d ' ')" = 1 ] || die "the console does not hold exactly one main bundle"
if grep -q 'CloudGate' "$main"; then die "the console is the hosted build (CloudGate); unset VITE_HOSTED_CONSOLE"; fi

ldflags="-s -w -X main.releaseVersion=$version -X main.releaseCommit=$commit"
[ -z "$test_key" ] || ldflags="$ldflags -X github.com/sainteye/clawdline/internal/adapters/release.extraKey=$test_key"

# tar_into <archive> <dir>: the directory's contents at the archive's root.
tar_into() {
  # --no-xattrs: GNU tar on Linux would otherwise print a notice per macOS attribute it skips.
  COPYFILE_DISABLE=1 tar --no-xattrs -C "$2" -czf "$1" .
}

# carry_tmux <goos> <goarch> <stage>: the tmux this release carries, built or
# taken from --tmux-from, into libexec/ with its licenses beside it.
. tools/release/tmux/sources.sh
carry_tmux() {
  local goos=$1 goarch=$2 stage=$3 built
  if [ -n "$tmux_from" ]; then
    built="$tmux_from/$goos-$goarch"
  else
    built="$work/tmux/$goos-$goarch"
    tools/release/tmux/build.sh "$goos" "$goarch" "$built"
  fi
  [ -x "$built/tmux" ] || die "no tmux for $goos/$goarch in $built"
  grep -q "\"version\": \"$TMUX_VERSION\", \"sha256\": \"$TMUX_SHA256\"" "$built/tmux.json" ||
    die "$built/tmux.json does not name the pinned tmux $TMUX_VERSION"
  # A tmux from --tmux-from crossed a machine or a CI job: it must still be the
  # binary its tmux.json was written for.
  local got
  got=$(shasum -a 256 "$built/tmux" | cut -d' ' -f1)
  [ "$tmux_installed" = 1 ] || grep -q "\"binary_sha256\": \"$got\"" "$built/tmux.json" ||
    die "$built/tmux is $got, not the binary its tmux.json describes"
  mkdir -p "$stage/libexec" "$stage/licenses"
  cp "$built/tmux" "$stage/libexec/tmux"
  cp "$built/tmux.json" "$stage/libexec/tmux.json"
  cp "$built"/licenses/*.txt "$stage/licenses/"
  # Signed like the daemon beside it (codesign.sh): an arm64 Mac does not run
  # an unsigned binary.
  if [ "$goos" = darwin ] && command -v codesign >/dev/null; then
    codesign_path "$stage/libexec/tmux"
  fi
}

targets="linux/amd64 linux/arm64 darwin/arm64 darwin/amd64"
[ "$host_only" = 0 ] || targets="$(go env GOOS)/$(go env GOARCH)"
for target in $targets; do
  goos=${target%/*} goarch=${target#*/}
  stage="$work/$goos-$goarch"
  mkdir -p "$stage"
  carry_tmux "$goos" "$goarch" "$stage"
  echo "building ${goos}/${goarch}…"
  CGO_ENABLED=0 GOOS=$goos GOARCH=$goarch go build -trimpath -buildvcs=true -ldflags "$ldflags" \
    -o "$stage/clawdline" ./cmd/clawdline
  # The linker signs a darwin binary ad hoc; it is signed again with the
  # build's certificate so its identity survives updates (codesign.sh).
  if [ "$goos" = darwin ] && command -v codesign >/dev/null; then
    codesign_path "$stage/clawdline"
  fi
  if [ "$goos" = darwin ] && [ "$official_mac" = 1 ]; then
    codesign_notarize "clawdline_${version}_${goos}_${goarch}" "$stage/clawdline" "$stage/libexec/tmux"
  fi
  cp -R "$dist" "$stage/dist"
  printf '{"stamp":"%s","committed_at":"%s","version":"%s"}\n' "$commit" "$committed_at" "$version" >"$stage/dist/BUILD.json"
  tar_into "$out/clawdline_${version}_${goos}_${goarch}.tar.gz" "$stage"
done

if [ "$app" = 1 ]; then
  if [ "$(uname -s)" != Darwin ] || ! command -v swiftc >/dev/null; then
    die "the app bundle needs a Mac with swiftc; pass --no-app to build without it"
  fi
  [ -z "$test_key" ] || die "a test release is built with --no-app: the bundle's daemon would not trust the test key"
  appdir="$work/app"
  mkdir -p "$appdir"
  CLAWDLINE_RELEASE_VERSION=$version CLAWDLINE_WEB_SOURCE="$PWD/$dist" CLAWDLINE_TMUX_DIR="${tmux_from:-$work/tmux}/darwin-arm64" \
    tools/package-macos.sh --build-only "$appdir"
  if [ "$official_mac" = 1 ]; then
    bundle=$(find "$appdir" -maxdepth 1 -name '*.app' -print)
    [ -n "$bundle" ] || die "the app build left no .app in $appdir"
    codesign_notarize "Clawdline-${version}-macos-arm64" "$bundle"
  fi
  tar_into "$out/Clawdline-${version}-macos-arm64.tar.gz" "$appdir"
fi

( cd "$out" && shasum -a 256 ./*.tar.gz | sed 's# \./# #' >SHA256SUMS )
args=(-version "$version" -commit "$commit" -committed-at "$committed_at" -channel "$channel" -base-url "$base_url")
[ -z "$min_version" ] || args+=(-min-version "$min_version")
go run ./tools/release manifest ${args[@]+"${args[@]}"} "$out"
# A release carries an SBOM per archive (docs/security/release-evidence.md);
# a dev build stays on the machine that built it and needs none.
[ "$channel" = dev ] || tools/release/sbom.sh generate "$out"
# DMG is a delivery container, not a new updater artifact kind. Its evidence
# binds the unsigned manifest bytes; the workflow signs those same bytes next.
if [ "$official_mac" = 1 ] && [ "$app" = 1 ]; then
  bash tools/release/dmg.sh pack "$out" "$out" "$version" "$commit" "$commit" .github/workflows/release.yml
fi
if [ "$channel" = dev ]; then
  echo "dev build $version ($commit) built in $out"
else
  echo "release $version ($commit) built in $out; sign it with: go run ./tools/release sign -key <file> $out"
fi
finished=1
