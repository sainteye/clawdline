#!/usr/bin/env bash
# check-shell-expansions.sh: no shell script names a variable as `$name`
# followed directly by a non-ASCII character, as in "building $goos/$goarch…".
#
# Under a UTF-8 locale bash reads those bytes as part of the name. With
# `set -u` that is an unbound variable, and macOS's /bin/bash 3.2 then exits 0
# when the script has an EXIT trap: on 2026-10-07 the v0.10.0 release build
# stopped on that line, reported success, and only the signing step after it
# noticed that nothing had been built. Write `${name}…` instead.
set -euo pipefail
cd "$(dirname "$0")/.."

found=$(git ls-files -z -- '*.sh' 'install.sh' | xargs -0 perl -ne '
  print "$ARGV:$.: $_" if !/^\s*#/ && /\$[A-Za-z_][A-Za-z0-9_]*[^\x00-\x7F]/;
  close ARGV if eof;
' || true)
if [ -n "$found" ]; then
  printf '%s\n' "$found" >&2
  echo "check-shell-expansions: write \${name} before a non-ASCII character; bash reads it as part of the name" >&2
  exit 1
fi
echo "check-shell-expansions: every \$name is followed by an ASCII character"
