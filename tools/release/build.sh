#!/bin/bash
# Build every artifact of one release, and its unsigned manifest
# (docs/releasing.md). Signing is a separate step, so the key is never needed
# on a machine that only builds.
#
# usage: tools/release/build.sh <version> <out-dir> [--channel stable|beta]
#          [--base-url URL] [--min-version vX.Y.Z] [--no-app] [--skip-tests]
#          [--test-key <base64 public key>]
#
# <out-dir> must not exist. It ends up holding:
#   clawdline_<v>_<os>_<arch>.tar.gz   `clawdline` and `dist/`, the layout
#                                      `current` points at
#   Clawdline-<v>-macos-arm64.tar.gz   the app bundle (macOS hosts with swiftc)
#   SHA256SUMS                         for install.sh, which cannot read JSON
#   manifest.json                      what `tools/release sign` signs
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
channel=stable base_url="" min_version="" app=1 tests=1 test_key=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --channel) channel=${2:?}; shift ;;
    --base-url) base_url=${2:?}; shift ;;
    --min-version) min_version=${2:?}; shift ;;
    --no-app) app=0 ;;
    --skip-tests) tests=0 ;;
    --test-key) test_key=${2:?}; shift ;;
    *) die "unknown option $1" ;;
  esac
  shift
done

[[ "$version" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.]+)?$ ]] || die "$version is not vX.Y.Z[-pre]"
case "$channel" in stable|beta) ;; *) die "--channel is stable or beta" ;; esac
[ "$channel" = stable ] && [[ "$version" == *-* ]] && die "a pre-release version belongs on the beta channel"
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
trap 'rm -rf -- "$work"' EXIT

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

ldflags="-s -w -X main.releaseVersion=$version"
[ -z "$test_key" ] || ldflags="$ldflags -X github.com/sainteye/clawdline/internal/adapters/release.extraKey=$test_key"

# tar_into <archive> <dir>: the directory's contents at the archive's root.
tar_into() {
  COPYFILE_DISABLE=1 tar -C "$2" -czf "$1" .
}

for target in linux/amd64 linux/arm64 darwin/arm64 darwin/amd64; do
  goos=${target%/*} goarch=${target#*/}
  stage="$work/$goos-$goarch"
  mkdir -p "$stage"
  echo "building $goos/$goarch…"
  CGO_ENABLED=0 GOOS=$goos GOARCH=$goarch go build -trimpath -buildvcs=true -ldflags "$ldflags" \
    -o "$stage/clawdline" ./cmd/clawdline
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
  CLAWDLINE_RELEASE_VERSION=$version CLAWDLINE_WEB_SOURCE="$PWD/$dist" tools/package-macos.sh --build-only "$appdir"
  tar_into "$out/Clawdline-${version}-macos-arm64.tar.gz" "$appdir"
fi

( cd "$out" && shasum -a 256 ./*.tar.gz | sed 's# \./# #' >SHA256SUMS )
args=(-version "$version" -commit "$commit" -committed-at "$committed_at" -channel "$channel" -base-url "$base_url")
[ -z "$min_version" ] || args+=(-min-version "$min_version")
go run ./tools/release manifest "${args[@]}" "$out"
echo "release $version ($commit) built in $out; sign it with: go run ./tools/release sign -key <file> $out"
