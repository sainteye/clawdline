#!/bin/bash
# guard: tools/check-web-strings.py
# prevents: a Traditional Chinese sentence reworded in Copy+Chinese.swift while the hosted catalog keeps the old words, so app.clawdline.com says something the Mac no longer says
# defect: one hosted catalog value changed away from its TraditionalChinese string literal
# expect: a different value in the hosted catalog than TraditionalChinese
#
# Six values had drifted that way on 2026-09-15 with the key sets identical, which a key comparison
# cannot see. The value changed here is one the source defines as a plain literal, found by reading
# Copy+Chinese.swift rather than named, so a renamed key cannot make this proof mutate nothing.
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
    sys.exit("red proof found no catalog value that is a TraditionalChinese literal; the mutation was not applied")
catalog[key] = catalog[key] + "（舊的說法）"
path.write_text(json.dumps(catalog, ensure_ascii=False, sort_keys=True, separators=(",", ":")) + "\n")
print("red proof changed the hosted catalog value of %s" % key)
PY
fi

exec env CLAWDLINE_WEB_ROOT="$DIR/web" python3 "$GUARD_REPO/tools/check-web-strings.py"
