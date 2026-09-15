#!/bin/bash
# guard: tools/check-web-strings.py
# prevents: a branch that added a sentence following the catalog guard's only remedy — re-export from the running app, which is the installed build of other source — and writing that build's words over the branch's own
# defect: one key /v1/strings sends missing from Resources/web/strings/zh-Hant.json, as a branch that added the sentence leaves it
# expect: by hand — one key per line, in sorted position
#
# Review 2e03ef42 finding F6: the red message said only "re-export it from a build of this source", a
# child may not run ./build.sh, and the implementer of the guard edited the JSON by hand anyway. The
# message now names that edit and the literal to add; this proof keeps it from being dropped. The
# key deleted is one the source defines as a plain literal, found by reading Copy+Chinese.swift, so
# the literal to add is really there to be named.
set -euo pipefail
ARM="$1"
DIR="$2"

cp -R "$GUARD_BASE/Resources/web" "$DIR/web"
if [ "$ARM" = broken ]; then
  python3 - "$DIR/web/strings/zh-Hant.json" "$GUARD_BASE/Sources/Copy+Chinese.swift" <<'PY'
import json, sys
from pathlib import Path
path = Path(sys.argv[1])
catalog = json.loads(path.read_text())
source = Path(sys.argv[2]).read_text()
traditional = source[source.index("struct TraditionalChinese: Copy {"):source.index("struct SimplifiedChinese: Copy {")]
key = next((name for name in sorted(catalog)
            if name.startswith("web") and isinstance(catalog[name], str) and "\\" not in catalog[name]
            and ("    let %s = %s\n" % (name, json.dumps(catalog[name], ensure_ascii=False))) in traditional), None)
if key is None:
    sys.exit("red proof found no catalog key whose value is a TraditionalChinese literal; the mutation was not applied")
del catalog[key]
path.write_text(json.dumps(catalog, ensure_ascii=False, sort_keys=True, indent=2, separators=(",", ": ")) + "\n")
print("red proof deleted the hosted catalog key %s" % key)
PY
fi

exec env CLAWDLINE_WEB_ROOT="$DIR/web" python3 "$GUARD_REPO/tools/check-web-strings.py"
