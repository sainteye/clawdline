#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
tmp=$(mktemp -d "${TMPDIR:-/tmp}/clawdline-ci-modes.XXXXXX")
trap 'rm -rf "$tmp"' EXIT HUP INT TERM
missing="$tmp/no-legacy-source"

set +e
CLAWDLINE_WEB_SOURCE_TREE="$missing" "$root/tools/check-legacy-css.sh" >"$tmp/strict.out" 2>&1
strict=$?
set -e
if [ "$strict" -ne 2 ]; then
  echo "strict legacy check returned $strict, want 2" >&2
  cat "$tmp/strict.out" >&2
  exit 1
fi

CLAWDLINE_WEB_SOURCE_TREE="$missing" "$root/tools/check-legacy-css.sh" \
  --allow-missing-source >"$tmp/ci.out" 2>&1
if ! grep -Fq 'legacy: skipped: comparison source is not present' "$tmp/ci.out"; then
  echo "CI legacy check did not name its skip:" >&2
  cat "$tmp/ci.out" >&2
  exit 1
fi

echo "CI check modes: a missing legacy source is strict locally and an explicit skip only when requested"
