#!/bin/bash
# guard: tools/check-web-ids.py
# prevents: an element id renamed on one side of the pair — the page or the registry that looks it up at load time — leaving a lookup the page cannot answer
# defect: an id the registry looks up, renamed in index.html
# expect: looked up but not defined
#
# A rename, not an insertion. Nobody adds an id to the registry for an element they never wrote;
# what happens is that an element is renamed in the page and the registry keeps the old spelling,
# or the other way round. The mutation has to be the failure the guard exists for, or what it
# proves is only that the guard is not completely dead.
#
# The id is read out of the registry rather than named here: a pinned spelling would break silently
# the day that element is renamed for real, which is the same defect one level up.
set -euo pipefail
ARM="$1"
DIR="$2"

cp -R "$GUARD_BASE/Resources/web" "$DIR/web"
if [ "$ARM" = broken ]; then
  python3 - "$DIR/web" <<'PY'
import re, sys
from pathlib import Path
web = Path(sys.argv[1])
registry = (web / "app" / "js" / "core" / "dom.js").read_text()
match = re.search(
    r"\[(?P<body>(?:\s*['\"][^'\"]+['\"]\s*,?)+)\]"
    r"\s*\.forEach\s*\(\s*function\s*\(\s*id\s*\)\s*\{\s*"
    r"els\s*\[\s*id\s*\]\s*=\s*document\.getElementById\s*\(\s*id\s*\)",
    registry, re.DOTALL)
if match is None:
    sys.exit("red proof could not find the els registry in dom.js; the mutation was not applied")
index_path = web / "index.html"
index = index_path.read_text()
for name in re.findall(r"['\"]([^'\"]+)['\"]", match.group("body")):
    attribute = 'id="%s"' % name
    if attribute in index:
        index_path.write_text(index.replace(attribute, 'id="%s-renamed"' % name, 1))
        print("red proof renamed %s in index.html" % name)
        break
else:
    sys.exit("red proof found no registry id defined in index.html; the mutation was not applied")
PY
fi

exec env CLAWDLINE_WEB_ROOT="$DIR/web" python3 "$GUARD_REPO/tools/check-web-ids.py"
