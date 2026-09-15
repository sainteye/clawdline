#!/bin/bash
# guard: tools/check-web-strings.py
# prevents: the hosted console's static Traditional Chinese catalog falling behind the strings a Mac sends, so app.clawdline.com shows English or the key's name where a translated sentence exists
# defect: one key /v1/strings sends deleted from Resources/web/strings/zh-Hant.json
# expect: sent by /v1/strings but missing from the hosted catalog
#
# The shape it came in: on 2026-09-15 the catalog was 25 keys behind Copy+Chinese.swift because a
# feature added sentences and nobody re-exported. One missing key is the smallest instance of that.
set -euo pipefail
ARM="$1"
DIR="$2"

cp -R "$GUARD_BASE/Resources/web" "$DIR/web"
if [ "$ARM" = broken ]; then
  python3 - "$DIR/web/strings/zh-Hant.json" <<'PY'
import json, sys
from pathlib import Path
path = Path(sys.argv[1])
catalog = json.loads(path.read_text())
key = next((name for name in sorted(catalog) if name.startswith("web")), None)
if key is None:
    sys.exit("red proof found no web key in the hosted catalog; the mutation was not applied")
del catalog[key]
path.write_text(json.dumps(catalog, ensure_ascii=False, sort_keys=True, separators=(",", ":")) + "\n")
print("red proof deleted the hosted catalog key %s" % key)
PY
fi

exec env CLAWDLINE_WEB_ROOT="$DIR/web" python3 "$GUARD_REPO/tools/check-web-strings.py"
