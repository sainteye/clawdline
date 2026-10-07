#!/usr/bin/env bash
# check-shell-expansions.sh: two expansions that macOS's /bin/bash 3.2 turns
# into an unbound-variable error under `set -u`, which bash 5 on Linux accepts.
#
# 1. `$name` followed directly by a non-ASCII character, as in
#    "building $goos/$goarch…": under a UTF-8 locale bash reads those bytes as
#    part of the name. On 2026-10-07 the v0.10.0 release build stopped on that
#    line and, because of its EXIT trap, reported success. Write `${name}…`.
# 2. A bare "${list[@]}": bash 3.2 calls an empty array unset. The same day
#    the publication gate stopped on `"${extra[@]}"`, which is empty for every
#    stable release. Write ${list[@]+"${list[@]}"}.
set -euo pipefail
cd "$(dirname "$0")/.."

found=$(git ls-files -z -- '*.sh' 'install.sh' | xargs -0 perl -ne '
  if (!/^\s*#/) {
    print "$ARGV:$.: non-ASCII after a name: $_" if /\$[A-Za-z_][A-Za-z0-9_]*[^\x00-\x7F]/;
    print "$ARGV:$.: bare array expansion: $_" if /(?<!\+)"\$\{[A-Za-z_][A-Za-z0-9_]*\[@\]\}"/;
  }
  close ARGV if eof;
' || true)
if [ -n "$found" ]; then
  printf '%s\n' "$found" >&2
  echo "check-shell-expansions: write \${name} before a non-ASCII character, and \${list[@]+\"\${list[@]}\"} for an array" >&2
  exit 1
fi
echo "check-shell-expansions: no expansion that bash 3.2 reads as unbound"
