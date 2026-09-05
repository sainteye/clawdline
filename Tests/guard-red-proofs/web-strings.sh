#!/bin/bash
# guard: tools/check-web-strings.py
# prevents: a translated string renamed on one side of the three-way contract — the modules that read it, the T table that defines it, and what /v1/strings sends — so that the page renders a blank where a sentence should be
# defect: a key renamed in i18n.js and nowhere else
# expect: defined in T but not sent by /v1/strings
#
# A rename rather than a new read, because that is how this breaks: somebody tidies a name in one
# of the three places. It fires all three halves of the contract at once, which is the point — the
# guard's subject is the disagreement, not any one side of it.
set -euo pipefail
ARM="$1"
DIR="$2"

cp -R "$GUARD_BASE/Resources/web" "$DIR/web"
if [ "$ARM" = broken ]; then
  python3 - "$DIR/web/app/js/core/i18n.js" <<'PY'
import re, sys
from pathlib import Path
path = Path(sys.argv[1])
text = path.read_text()
start = text.index("export var T = {")
end = text.index("\n};", start)
body = text[start:end]
match = re.search(r"^(\s*)([A-Za-z_][A-Za-z0-9_]*)(\s*:)", body, re.MULTILINE)
if match is None:
    sys.exit("red proof found no key in the T table; the mutation was not applied")
renamed = body[:match.start()] + match.group(1) + match.group(2) + "Renamed" + match.group(3) \
    + body[match.end():]
path.write_text(text[:start] + renamed + text[end:])
print("red proof renamed the T key %s" % match.group(2))
PY
fi

exec env CLAWDLINE_WEB_ROOT="$DIR/web" python3 "$GUARD_REPO/tools/check-web-strings.py"
