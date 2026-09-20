#!/bin/bash
# No sentence this build shows a person may call the machine a Mac.
#
# This daemon runs on macOS, Linux and Windows. A sentence that says "this Mac"
# is wrong on two of the three, and the place it is most wrong is the one a
# person reads on a phone, away from the machine, when something has already
# gone wrong. 146 such sentences were found here on 2026-09-20, and the hardest
# one was not a sentence at all: `machineName` returned the literal "Mac" for a
# machine with no name, and published it to the account's machine list.
#
# **It reads string literals, not comments.** A comment is the codebase talking
# to itself and often names a real Mac — a measurement taken on one, an iTerm2
# or AppleScript path that exists nowhere else. Separating those by machine is
# guesswork. A string literal is either shown to somebody or compared against
# something that is, and that is a question with an answer.
#
# **It does not read the copies.** `web/console/src/legacy/`,
# `public/strings/zh-Hant.json` and the files that declare themselves copies of
# the retired Swift app's `Copy+Chinese.swift` are that app's words, checked
# byte for byte by check-legacy-css.sh. Changing one there is drift, not a fix,
# so this guard stays out of them — and the list of what it skips is the list of
# places this repository cannot reach, which is worth reading on its own.
#
# A line where Mac is the right word — the platform this machine really is, a
# user-agent string, a host name in a fixture — goes in tools/machine-words.allow
# with the reason. Three answers: 0 clean, 1 a sentence to fix, 2 could not be
# checked.
set -uo pipefail
cd "$(dirname "$0")/.."

ALLOW="tools/machine-words.allow"
SCAN="tools/machine-words.py"
[ -f "$ALLOW" ] || { echo "cannot check: no allow list at $ALLOW" >&2; exit 2; }
[ -f "$SCAN" ] || { echo "cannot check: no scanner at $SCAN" >&2; exit 2; }

git ls-files -z -- '*.go' '*.ts' '*.tsx' | python3 "$SCAN" "$ALLOW"
