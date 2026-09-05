#!/bin/bash
# guard: tools/check-version-strings.py
# defect: the app's own version typed into a document nothing keeps in step
# expect: nothing keeps in step
#
# The version is read out of the fixture rather than written here. A literal would be a third
# place this repository's version is typed into — caught by this very guard, in this very file.
set -euo pipefail
ARM="$1"
DIR="$2"

cp -R "$GUARD_BASE/." "$DIR/"
if [ "$ARM" = broken ]; then
  VERSION=$(sed -n 's|.*CFBundleShortVersionString</key><string>\([^<]*\)<.*|\1|p' "$DIR/build.sh" | head -1)
  if [ -z "$VERSION" ]; then
    echo "red proof could not read the stamped version out of build.sh; the mutation was not applied" >&2
    exit 3
  fi
  echo "Clawdline $VERSION is what this page was written against." >> "$DIR/docs/waiting.md"
fi

git -C "$DIR" init -q
git -C "$DIR" add -A

exec env CLAWDLINE_VERSION_SCAN_ROOT="$DIR" python3 "$GUARD_REPO/tools/check-version-strings.py"
