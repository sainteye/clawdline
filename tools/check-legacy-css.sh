#!/bin/bash
# Compare everything copied from the Swift app's console against its source.
#
# A copy that cannot notice its source changed is not a copy, it is a fork
# nobody declared. This says so out loud while the two are still meant to match.
#
# Three answers, not two. 0 is "they match", 1 is "they drifted", and 2 is
# "this could not be checked" — a missing source must never be reported as the
# green one.
set -uo pipefail
cd "$(dirname "$0")/.."

allow_missing=0
case "${1:-}" in
  "") ;;
  --allow-missing-source) allow_missing=1 ;;
  *) echo "usage: tools/check-legacy-css.sh [--allow-missing-source]" >&2; exit 2 ;;
esac

SRC="${CLAWDLINE_WEB_SOURCE_TREE:-$HOME/code/clawdline/Resources/web}"
DST="web/console/src/legacy"
MANIFEST="$DST/MANIFEST.json"

[ -f "$MANIFEST" ] || { echo "no manifest at $MANIFEST" >&2; exit 1; }
if [ ! -d "$SRC" ]; then
  if [ "$allow_missing" -eq 1 ]; then
    echo "legacy: skipped: comparison source is not present at $SRC"
    exit 0
  fi
  echo "cannot check: no source tree at $SRC" >&2
  exit 2
fi

drift=0
checked=0
while IFS=$'\t' read -r local remote want; do
  checked=$((checked + 1))
  if [ ! -f "$SRC/$remote" ]; then
    echo "gone from source: $remote" >&2; drift=$((drift + 1)); continue
  fi
  have=$(shasum -a 256 "$SRC/$remote" | cut -c1-16)
  if [ "$have" != "$want" ]; then
    echo "source changed: $remote ($want -> $have)" >&2; drift=$((drift + 1)); continue
  fi
  if ! cmp -s "$SRC/$remote" "$local"; then
    echo "copy differs from source: $local" >&2; drift=$((drift + 1))
  fi
done < <(python3 -c '
import json, sys
m = json.load(open(sys.argv[1]))
dst = sys.argv[2]
for name, sha in sorted(m["css"].items()):
    print("\t".join([dst + "/" + name, "app/css/" + name, sha]))
for name, sha in sorted(m["js"].items()):
    print("\t".join([dst + "/" + name, "app/" + name, sha]))
for name, sha in sorted(m["strings"].items()):
    local = "web/console/" + name
    print("\t".join([local, "strings/" + name.rsplit("/", 1)[-1], sha]))
' "$MANIFEST" "$DST")

[ "$checked" -gt 0 ] || { echo "checked nothing; a zero-file run is a failure, not a pass" >&2; exit 1; }
if [ "$drift" -gt 0 ]; then
  echo "$drift of $checked copied file(s) drifted from $SRC" >&2
  exit 1
fi
echo "legacy: $checked files match $SRC"
