#!/bin/bash
# guard: Tests/scratch-tool.mjs
# prevents: a snapshot run that leaves its repository copy and test binary behind when it ends — the 106 tmp.* directories and 1,871 MB the mktemp -d recipes in AGENTS.md had left in one user's temporary directory by 2026-09-11, because nothing in those recipes ever removed what they made
# defect: tools/scratch.sh with its EXIT cleanup trap deleted
# expect: left an entry behind
#
# The mutation is the one line every exit path depends on. `snapshot-run` removes its entry from its
# EXIT trap — the command's own exit, a refusal while copying, and the `exit` each signal handler ends
# with all arrive there — so deleting that line is how "it cleans up however the run ends" actually
# breaks: a rewrite that arms the trap after an early `exit`, or a new way out that bypasses it. The
# suite then has to say, of a run that succeeded, that an entry was left behind.
#
# Only the two files the suite uses are copied. It finds the tool beside itself and builds its own
# repositories and scratch root under mkdtemp; nothing else in the tree is read.
set -euo pipefail
ARM="$1"
DIR="$2"

mkdir -p "$DIR/tools" "$DIR/Tests"
cp "$GUARD_BASE/tools/scratch.sh" "$DIR/tools/scratch.sh"
cp "$GUARD_BASE/Tests/scratch-tool.mjs" "$DIR/Tests/scratch-tool.mjs"

if [ "$ARM" = broken ]; then
  pattern='^[[:space:]]*trap scratch_cleanup EXIT[[:space:]]*$'
  found=$(grep -c "$pattern" "$DIR/tools/scratch.sh" || true)
  if [ "$found" != 1 ]; then
    echo "red proof expected one 'trap scratch_cleanup EXIT' line in tools/scratch.sh and found ${found:-0}; the mutation was not applied"
    exit 3
  fi
  grep -v "$pattern" "$DIR/tools/scratch.sh" > "$DIR/scratch.sh.mutated" || true
  mv "$DIR/scratch.sh.mutated" "$DIR/tools/scratch.sh"
  left=$(grep -c "$pattern" "$DIR/tools/scratch.sh" || true)
  if [ "$left" != 0 ]; then
    echo "red proof could not delete the cleanup trap from tools/scratch.sh; the mutation was not applied"
    exit 3
  fi
fi

exec node "$DIR/Tests/scratch-tool.mjs"
