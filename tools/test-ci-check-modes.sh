#!/bin/sh
# The legacy check answers from the pinned manifest, with or without a source.
#
# It used to need ~/code/clawdline present and exit 2 without it, and CI passed
# --allow-missing-source, which skipped the check entirely — so the 85-file lock
# was never verified anywhere. It is verified everywhere now, and this holds the
# two halves that matter: a missing source does not stop the check, and a
# changed copy is caught whether or not a source is there to compare against.
set -eu

root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
tmp=$(mktemp -d "${TMPDIR:-/tmp}/clawdline-ci-modes.XXXXXX")
trap 'rm -rf "$tmp"' EXIT HUP INT TERM
missing="$tmp/no-legacy-source"

# 1. No source: the copies are still checked, and the absence is named rather
#    than passed over in silence.
set +e
CLAWDLINE_WEB_SOURCE_TREE="$missing" "$root/tools/check-legacy-css.sh" >"$tmp/nosrc.out" 2>&1
nosrc=$?
set -e
if [ "$nosrc" -ne 0 ]; then
  echo "a missing source returned $nosrc, want 0" >&2; cat "$tmp/nosrc.out" >&2; exit 1
fi
if ! grep -Fq 'source unavailable (optional)' "$tmp/nosrc.out"; then
  echo "a missing source was not named:" >&2; cat "$tmp/nosrc.out" >&2; exit 1
fi
if ! grep -Fq 'copies match the pinned manifest' "$tmp/nosrc.out"; then
  echo "the copies were not checked without a source:" >&2; cat "$tmp/nosrc.out" >&2; exit 1
fi

# 2. The old spelling still works, for a CI job that has not been updated yet.
CLAWDLINE_WEB_SOURCE_TREE="$missing" "$root/tools/check-legacy-css.sh" \
  --allow-missing-source >"$tmp/flag.out" 2>&1
if ! grep -Fq 'copies match the pinned manifest' "$tmp/flag.out"; then
  echo "--allow-missing-source stopped the check:" >&2; cat "$tmp/flag.out" >&2; exit 1
fi

# 3. A changed copy is caught with no source present. Nothing in the repository
#    is edited: the check runs against a copy of the tree.
work="$tmp/tree"
mkdir -p "$work/tools" "$work/web/console/src/legacy" "$work/web/console/public/strings"
cp -R "$root/web/console/src/legacy/." "$work/web/console/src/legacy/"
cp -R "$root/web/console/public/strings/." "$work/web/console/public/strings/"
cp "$root/tools/check-legacy-css.sh" "$work/tools/"
printf '\n' >>"$work/web/console/src/legacy/js/core/esc.js"
set +e
( cd "$work" && CLAWDLINE_WEB_SOURCE_TREE="$missing" tools/check-legacy-css.sh ) >"$tmp/drift.out" 2>&1
drift=$?
set -e
if [ "$drift" -ne 1 ]; then
  echo "an edited copy returned $drift, want 1" >&2; cat "$tmp/drift.out" >&2; exit 1
fi
if ! grep -Fq 'copy changed' "$tmp/drift.out"; then
  echo "an edited copy was not named:" >&2; cat "$tmp/drift.out" >&2; exit 1
fi

echo "CI check modes: the pinned copies are checked with or without a source, and an edited copy is caught either way"
