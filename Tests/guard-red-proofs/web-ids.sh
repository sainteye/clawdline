#!/bin/bash
# guard: tools/check-web-ids.py
# defect: an element the page looks up at load time and does not contain
# expect: looked up but not defined
#
# The id goes into the registry with a leading comma so that the array the guard's pattern reads
# stays the shape it reads. If the registry is ever written differently the substitution stops
# applying, both arms become the same tree, and the runner reports that the guard passed a tree
# with the defect in it — loudly, and pointing here.
set -euo pipefail
ARM="$1"
DIR="$2"

cp -R "$GUARD_BASE/Resources/web" "$DIR/web"
REGISTRY="$DIR/web/app/js/core/dom.js"
ANCHOR='].forEach(function (id) { els[id] = document.getElementById(id); });'
if [ "$ARM" = broken ]; then
  if ! grep -qF "$ANCHOR" "$REGISTRY"; then
    echo "red proof could not find the els registry in ${REGISTRY##*/}; the mutation was not applied" >&2
    exit 3
  fi
  python3 - "$REGISTRY" "$ANCHOR" <<'PY'
import sys
path, anchor = sys.argv[1], sys.argv[2]
text = open(path).read()
open(path, "w").write(text.replace(anchor, ', "red-proof-absent-id"' + anchor, 1))
PY
fi

exec env CLAWDLINE_WEB_ROOT="$DIR/web" python3 "$GUARD_REPO/tools/check-web-ids.py"
