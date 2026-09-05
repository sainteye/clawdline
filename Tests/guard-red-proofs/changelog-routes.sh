#!/bin/bash
# guard: Tests/changelog-facts.mjs
# prevents: the CHANGELOG's Unreleased block promising an HTTP route the shipped server does not answer — the failure tools/release.sh opens with, where the README described a product the only downloadable build did not contain
# defect: a route named in Unreleased that Sources/RemoteServer.swift does not serve
# expect: answers no such path
# known-blind: a route counts as answered if ANY run of its literal segments longer than three characters appears anywhere in RemoteServer.swift, so `/v1/orchestrator` alone answers for every route beneath it. Measured on 2026-09-05 in a disposable copy of this tree: `/v1/orchestrator/tasks/:id/nothing-answers-this` is green, and renaming `/v1/orchestrator/tasks` to `/v1/GONE/tasks` in the source leaves it green as well. Every route anybody actually adds lives under a namespace that already exists, so the guard is blind to its whole subject. Do not repair the guard from here: it is the specimen this mechanism was sharpened against, and root asked for it to be pointed at before it is touched.
#
# Three arms, because "the proof did not hold" is two different findings and they must not be
# confused. `easy` is the crude mutation — a route under a top-level name the source has never
# heard of — and it goes red, which is what makes `broken` staying green a statement about the
# guard rather than about this script.
set -euo pipefail
ARM="$1"
DIR="$2"

cp -R "$GUARD_BASE/." "$DIR/"

case "$ARM" in
  broken) ROUTE="/v1/orchestrator/tasks/:id/nothing-answers-this" ;;
  easy)   ROUTE="/v1/nothing-answers-this" ;;
  *)      ROUTE= ;;
esac

if [ -n "$ROUTE" ]; then
  python3 - "$DIR/CHANGELOG.md" "$ROUTE" <<'PY'
import sys
from pathlib import Path
path, route = Path(sys.argv[1]), sys.argv[2]
text = path.read_text()
heading = "\n## Unreleased\n"
if heading not in text:
    sys.exit("red proof found no '## Unreleased' heading; the mutation was not applied")
at = text.index(heading) + len(heading)
path.write_text(text[:at] + "\n- Invented by a red proof: `GET %s`.\n" % route + text[at:])
PY
fi

exec node "$DIR/Tests/changelog-facts.mjs"
