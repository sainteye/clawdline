#!/bin/bash
# Build the tmux one release carries, for one platform (docs/design-decisions.md
# D33). Every source is a pinned tarball checked against its sha256 in
# sources.sh before it is unpacked.
#
# usage: tools/release/tmux/build.sh <goos> <goarch> <out-dir>
#
# <out-dir> must not exist. It ends up holding:
#   tmux                 the executable: linked statically on Linux (musl, so
#                        it runs on any distribution), and on macOS against
#                        only the base system's libraries
#   licenses/*.txt       tmux's, libevent's, utf8proc's and, on Linux,
#                        ncurses's
#   tmux.json            the versions and hashes it was built from, and the
#                        executable's own sha256
#
# darwin/arm64 and darwin/amd64 build on a Mac with Xcode's command line tools.
# linux/amd64 and linux/arm64 build in Docker (the image in sources.sh); a
# platform that is not the host's runs under Docker's emulation and is slow.
#
# Downloads are kept in $CLAWDLINE_TMUX_CACHE (default
# ~/.cache/clawdline/tmux-src), checked again every time they are used.
set -euo pipefail
cd "$(dirname "$0")/../../.."
here=tools/release/tmux
# shellcheck source=sources.sh
. "$here/sources.sh"

die() { echo "tmux build: $*" >&2; exit 2; }

[ "$#" -eq 3 ] || die "usage: tools/release/tmux/build.sh <goos> <goarch> <out-dir>"
goos=$1 goarch=$2 out=$3
case "$goos/$goarch" in
  darwin/arm64 | darwin/amd64 | linux/amd64 | linux/arm64) ;;
  *) die "$goos/$goarch is not a platform a release carries tmux for" ;;
esac
[ ! -e "$out" ] || die "$out already exists"

cache=${CLAWDLINE_TMUX_CACHE:-$HOME/.cache/clawdline/tmux-src}
mkdir -p "$cache"

sha256() {
  if command -v sha256sum >/dev/null; then sha256sum "$1" | cut -d' ' -f1; else shasum -a 256 "$1" | cut -d' ' -f1; fi
}

# fetch <url> <sha256>: the tarball in the cache, downloaded when absent, and
# refused when its hash is not the pinned one.
fetch() {
  local url=$1 want=$2 file
  file="$cache/$(basename "$url")"
  if [ ! -f "$file" ]; then
    curl -fsSL --retry 3 -o "$file.part" "$url"
    mv "$file.part" "$file"
  fi
  local got
  got=$(sha256 "$file")
  if [ "$got" != "$want" ]; then
    rm -f "$file"
    die "$(basename "$url") is $got, not the pinned $want; it was removed"
  fi
  printf '%s\n' "$file"
}

work=$(mktemp -d)
# finished: see tools/release/build.sh — bash 3.2 exits 0 after an unbound
# variable when an EXIT trap runs.
finished=0
trap 's=$?; rm -rf -- "$work"; if [ "$finished" != 1 ] && [ "$s" = 0 ]; then exit 1; fi' EXIT

mkdir -p "$work/src"
for pin in TMUX LIBEVENT UTF8PROC NCURSES; do
  url_var="${pin}_URL" sha_var="${pin}_SHA256"
  [ "$pin" != NCURSES ] || [ "$goos" = linux ] || continue
  cp "$(fetch "${!url_var}" "${!sha_var}")" "$work/src/"
done
cp "$here/inside.sh" "$work/inside.sh"

case "$goos" in
  darwin)
    [ "$(uname -s)" = Darwin ] || die "darwin/$goarch builds on a Mac"
    arch=$goarch
    [ "$arch" != amd64 ] || arch=x86_64
    # Built for the oldest macOS the app supports (Info.plist
    # LSMinimumSystemVersion), against the SDK's own libncurses.
    MACOSX_DEPLOYMENT_TARGET=13.0 TARGET_OS=darwin TARGET_ARCH=$arch \
      TMUX_VERSION=$TMUX_VERSION LIBEVENT_VERSION=$LIBEVENT_VERSION \
      UTF8PROC_VERSION=$UTF8PROC_VERSION \
      /bin/sh "$work/inside.sh" "$work"
    ;;
  linux)
    command -v docker >/dev/null || die "linux/$goarch builds in Docker, and there is no docker here"
    docker run --rm --platform "linux/$goarch" -v "$work:/w" \
      -e TARGET_OS=linux -e TMUX_VERSION="$TMUX_VERSION" -e LIBEVENT_VERSION="$LIBEVENT_VERSION" \
      -e UTF8PROC_VERSION="$UTF8PROC_VERSION" -e NCURSES_VERSION="$NCURSES_VERSION" \
      "$ALPINE_IMAGE" /bin/sh /w/inside.sh /w
    ;;
esac

[ -x "$work/out/tmux" ] || die "the build left no tmux"
mkdir -p "$out"
cp -R "$work/out/." "$out/"
bin_sha=$(sha256 "$out/tmux")
{
  printf '{\n'
  printf '  "goos": "%s",\n  "goarch": "%s",\n' "$goos" "$goarch"
  printf '  "tmux": {"version": "%s", "sha256": "%s"},\n' "$TMUX_VERSION" "$TMUX_SHA256"
  printf '  "libevent": {"version": "%s", "sha256": "%s"},\n' "$LIBEVENT_VERSION" "$LIBEVENT_SHA256"
  printf '  "utf8proc": {"version": "%s", "sha256": "%s"},\n' "$UTF8PROC_VERSION" "$UTF8PROC_SHA256"
  if [ "$goos" = linux ]; then
    printf '  "ncurses": {"version": "%s", "sha256": "%s"},\n' "$NCURSES_VERSION" "$NCURSES_SHA256"
    printf '  "image": "%s",\n' "$ALPINE_IMAGE"
  fi
  printf '  "binary_sha256": "%s"\n}\n' "$bin_sha"
} >"$out/tmux.json"
echo "tmux $TMUX_VERSION for $goos/$goarch in $out ($(wc -c <"$out/tmux" | tr -d ' ') bytes, sha256 $bin_sha)"
finished=1
