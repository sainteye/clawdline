#!/bin/bash
# Check the byte-for-byte copies against the hashes pinned when they were copied.
#
# The retired application's source tree is optional evidence. When present, its
# current hashes are compared with the same pins and any later source movement is
# reported, but it is not allowed to redefine what the copies should contain.
#
# Three answers, not two. 0 is "every copy matches its pin", 1 is "a copy
# drifted", and 2 is "the pinned comparison could not be completed". A missing
# optional source tree is named, but it cannot make a complete pinned check
# indeterminate.
set -uo pipefail
cd "$(dirname "$0")/.."

case "${1:-}" in
  "" | --allow-missing-source) ;;
  *) echo "usage: tools/check-legacy-css.sh [--allow-missing-source]" >&2; exit 2 ;;
esac

SRC="${CLAWDLINE_WEB_SOURCE_TREE:-${HOME:-}/code/clawdline/Resources/web}"
DST="web/console/src/legacy"
MANIFEST="$DST/MANIFEST.json"

cannot_check() {
  echo "cannot check pinned copies: $*" >&2
  exit 2
}

[ -f "$MANIFEST" ] || cannot_check "no manifest at $MANIFEST"
command -v python3 >/dev/null 2>&1 || cannot_check "python3 is not available"

if command -v shasum >/dev/null 2>&1; then
  sha256_16() { shasum -a 256 "$1" | cut -c1-16; }
elif command -v sha256sum >/dev/null 2>&1; then
  sha256_16() { sha256sum "$1" | cut -c1-16; }
else
  cannot_check "neither shasum nor sha256sum is available"
fi

entries=$(mktemp "${TMPDIR:-/tmp}/clawdline-legacy-lock.XXXXXX") || \
  cannot_check "could not create a temporary manifest listing"
trap 'rm -f "$entries"' EXIT
trap 'exit 2' HUP INT TERM

if ! python3 - "$MANIFEST" "$DST" >"$entries" <<'PY'
import json
import re
import sys

manifest_path, destination = sys.argv[1:]
try:
    with open(manifest_path, encoding="utf-8") as source:
        manifest = json.load(source)
except (OSError, json.JSONDecodeError) as error:
    raise SystemExit(f"cannot read pinned manifest: {error}")

groups = (
    ("css", lambda name: (f"{destination}/{name}", f"app/css/{name}")),
    ("js", lambda name: (f"{destination}/{name}", f"app/{name}")),
    (
        "strings",
        lambda name: (
            f"web/console/{name}",
            f"strings/{name.rsplit('/', 1)[-1]}",
        ),
    ),
)
for group, paths in groups:
    pins = manifest.get(group)
    if not isinstance(pins, dict):
        raise SystemExit(f"pinned manifest has no {group} map")
    for name, checksum in sorted(pins.items()):
        if not isinstance(name, str) or not name or any(c in name for c in "\t\r\n"):
            raise SystemExit(f"pinned manifest has an invalid {group} path")
        if not isinstance(checksum, str) or not re.fullmatch(r"[0-9a-f]{16}", checksum):
            raise SystemExit(f"pinned manifest has an invalid checksum for {group}/{name}")
        local, retired = paths(name)
        print("\t".join((local, retired, checksum)))
PY
then
  cannot_check "the manifest could not be turned into a file list"
fi

copy_drift=0
checked=0
source_changed=0
copy_unreadable=0
source_present=0
[ -d "$SRC" ] && source_present=1

while IFS=$'\t' read -r local retired want; do
  checked=$((checked + 1))
  if [ ! -f "$local" ]; then
    echo "copy missing: $local" >&2
    copy_drift=$((copy_drift + 1))
  elif ! have=$(sha256_16 "$local"); then
    echo "cannot hash copy: $local" >&2
    copy_unreadable=$((copy_unreadable + 1))
  elif [ "$have" != "$want" ]; then
    echo "copy changed: $local ($want -> $have)" >&2
    copy_drift=$((copy_drift + 1))
  fi

  if [ "$source_present" -eq 1 ]; then
    if [ ! -f "$SRC/$retired" ]; then
      echo "source no longer has: $retired" >&2
      source_changed=$((source_changed + 1))
    elif ! source_have=$(sha256_16 "$SRC/$retired"); then
      echo "source could not be read: $retired" >&2
      source_changed=$((source_changed + 1))
    elif [ "$source_have" != "$want" ]; then
      echo "source changed since copy: $retired ($want -> $source_have)" >&2
      source_changed=$((source_changed + 1))
    fi
  fi
done <"$entries"

[ "$checked" -gt 0 ] || cannot_check "the manifest names no copied files"
if [ "$copy_unreadable" -gt 0 ]; then
  cannot_check "$copy_unreadable of $checked copy file(s) could not be hashed"
fi
if [ "$copy_drift" -gt 0 ]; then
  echo "$copy_drift of $checked copied file(s) drifted from the pinned manifest" >&2
  exit 1
fi

if [ "$source_present" -eq 0 ]; then
  echo "source unavailable (optional): $SRC; pinned copies were still checked" >&2
  echo "legacy: $checked copies match the pinned manifest"
elif [ "$source_changed" -gt 0 ]; then
  echo "legacy: $checked copies match the pinned manifest; $source_changed source file(s) changed later"
else
  echo "legacy: $checked copies and the optional source tree match the pinned manifest"
fi
