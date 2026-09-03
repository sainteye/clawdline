#!/bin/bash
set -euo pipefail

# Writes the governance table in docs/architecture-refactor.md from the values the architecture
# guard already holds. Nobody types a governance number into that document any more: five of the six
# are counted from the tree by `tools/check-architecture-boundaries.sh`, the sixth is
# `expected_swift_receipt` in `test.sh`, and this splices what the guard renders between the markers
# in the doc. The guard then compares the committed block against the same rendering on every run,
# so the doc is a projection of those sources rather than a second hand-edited copy of them.
#
# What this deliberately does NOT do: touch `expected_swift_receipt` or its witness in `test.sh`.
# Those two are set by a person, together, from a suite run. A generator that could also write them
# would be a generator that silences the only guard standing over them, and a check that is always
# satisfied guards nothing.

# `cd ""` returns 0 in both bash and zsh and stays put, so `|| exit 1` alone does not protect this;
# `:?` is what makes an unset or empty path stop the script rather than run it somewhere else.
tools_dir=$(dirname -- "$0")
cd "${tools_dir:?script directory not resolved}/.." || exit 1
repo_root=$(pwd)
echo "generate-governance-table: writing in $repo_root"

doc=docs/architecture-refactor.md
marker_open='<!-- clawdline-governance-table:v1 -->'
marker_close='<!-- /clawdline-governance-table:v1 -->'

[ -f "$doc" ] || { echo "generate-governance-table: $repo_root/$doc does not exist" >&2; exit 1; }
grep -qxF "$marker_open" "$doc" \
  || { echo "generate-governance-table: $doc has no '$marker_open' line to write between" >&2; exit 1; }
grep -qxF "$marker_close" "$doc" \
  || { echo "generate-governance-table: $doc has no '$marker_close' line to write between" >&2; exit 1; }

table=$(mktemp "${TMPDIR:-/tmp}/clawdline-governance-table.XXXXXX")
rendered=$(mktemp "${TMPDIR:-/tmp}/clawdline-governance-doc.XXXXXX")
trap 'rm -f "$table" "$rendered"' EXIT

bash tools/check-architecture-boundaries.sh --emit-governance-table > "$table"
[ -s "$table" ] \
  || { echo "generate-governance-table: the guard rendered an empty table; refusing to write it" >&2; exit 1; }

# The table goes to awk as a file rather than through `-v`, which cannot carry a newline: a `-v`
# holding the whole block fails with `awk: newline in string` on every implementation reached here.
# A blank line on each side of it, because a markdown table pressed against an HTML comment is not a
# table; the guard drops blank lines from both sides before comparing, so that much is presentation
# and not part of the contract.
awk -v opener="$marker_open" -v closer="$marker_close" -v table="$table" '
  $0 == opener {
    print
    print ""
    while ((getline line < table) > 0) print line
    close(table)
    print ""
    inside = 1
    next
  }
  $0 == closer { inside = 0 }
  !inside      { print }
' "$doc" > "$rendered"

if cmp -s "$rendered" "$doc"; then
  echo "generate-governance-table: $doc already carries this tree's table"
  exit 0
fi

cat "$rendered" > "$doc"
echo "generate-governance-table: wrote this tree's table into $doc"
cat "$table"
