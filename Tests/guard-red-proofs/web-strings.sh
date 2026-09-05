#!/bin/bash
# guard: tools/check-web-strings.py
# defect: a module reading a translated string that i18n does not define
# expect: read but not defined in T
set -euo pipefail
ARM="$1"
DIR="$2"

cp -R "$GUARD_BASE/Resources/web" "$DIR/web"
if [ "$ARM" = broken ]; then
  echo 'export function redProofProbe() { return T.redProofStringNobodyDefined; }' \
    >> "$DIR/web/app/js/core/dom.js"
fi

exec env CLAWDLINE_WEB_ROOT="$DIR/web" python3 "$GUARD_REPO/tools/check-web-strings.py"
