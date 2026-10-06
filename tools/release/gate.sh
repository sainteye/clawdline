#!/bin/bash
# The publication gate for a drafted release (docs/releasing.md). Run on a
# machine that has the private word list (.git/info/private-words); CI has
# none, so CI can draft a release but never decide it may be published.
#
# usage: tools/release/gate.sh <version> [--publish] [--dir <local release dir>]
#
# 1. downloads the draft's assets (or reads --dir, for a local test release),
# 2. verifies manifest.sig.json with the keys compiled into this checkout and
#    every artifact's size and sha256,
# 3. unpacks every archive and runs tools/check-private.sh -dir over all of it,
#    binaries included,
# 4. with --publish, and only when all of that passed, un-drafts the release.
set -euo pipefail
cd "$(dirname "$0")/../.."

[ "$#" -ge 1 ] || { echo "usage: tools/release/gate.sh <version> [--publish] [--dir D]" >&2; exit 2; }
version=$1; shift
publish=0 dir=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --publish) publish=1 ;;
    --dir) dir=${2:?}; shift ;;
    *) echo "unknown option $1" >&2; exit 2 ;;
  esac
  shift
done
[ "$publish" = 0 ] || [ -z "$dir" ] || { echo "--publish is for a drafted GitHub release, not --dir" >&2; exit 2; }

work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT
if [ -z "$dir" ]; then
  draft=$(gh release view "$version" -R sainteye/clawdline --json isDraft -q .isDraft)
  [ "$draft" = true ] || { echo "$version is not a draft; nothing to gate" >&2; exit 1; }
  gh release download "$version" -R sainteye/clawdline --dir "$work/assets"
  dir="$work/assets"
fi

extra=()
[[ "$version" =~ -test\.[0-9]+$ ]] && extra=(-pub "${CLAWDLINE_TEST_RELEASE_PUB:?a -test release is verified with CLAWDLINE_TEST_RELEASE_PUB}")
go run ./tools/release verify "${extra[@]}" "$dir"

mkdir -p "$work/unpacked"
for archive in "$dir"/*.tar.gz; do
  d="$work/unpacked/$(basename "$archive" .tar.gz)"
  mkdir -p "$d"
  tar -xzf "$archive" -C "$d"
done
cp "$dir/manifest.json" "$dir/manifest.sig.json" "$dir/SHA256SUMS" "$work/unpacked/"

set +e
tools/check-private.sh -dir "$work/unpacked"
rc=$?
set -e
case "$rc" in
  0) echo "privacy gate: clean" ;;
  3) echo "privacy gate: UNDETERMINED (no private word list); not publishing" >&2; exit 3 ;;
  *) echo "privacy gate: FAILED ($rc); the draft stays a draft" >&2; exit 1 ;;
esac

if [ "$publish" = 1 ]; then
  latest=()
  [[ "$version" == *-* ]] || latest=(--latest)
  gh release edit "$version" -R sainteye/clawdline --draft=false "${latest[@]}"
  echo "published $version"
else
  echo "gate passed; publish with: tools/release/gate.sh $version --publish"
fi
