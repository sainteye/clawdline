#!/bin/bash
# guard: tools/check-architecture-boundaries.sh
# defect: Tests/main.swift grown past the ceiling it is held to
# expect: maximum is 500
#
# This guard finds its tree from its own path, so the only way to hand it a defect is to hand it
# a tree. The ceiling on Tests/main.swift is the first thing it measures, which matters here: on a
# base with an open seal window the clean arm is already red further down, and a check that never
# ran could not have been shown to fail.
set -euo pipefail
ARM="$1"
DIR="$2"

cp -R "$GUARD_BASE/." "$DIR/"
if [ "$ARM" = broken ]; then
  awk 'BEGIN { for (i = 0; i < 600; i++) print "// a line nobody reviewed" }' >> "$DIR/Tests/main.swift"
fi

exec bash "$DIR/tools/check-architecture-boundaries.sh"
