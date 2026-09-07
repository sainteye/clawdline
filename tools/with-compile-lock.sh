#!/bin/bash
# Run a focused compiler/check under the same machine lock as test.sh. The source
# remains test.sh's marked protocol: this wrapper does not create a second lock.
set -euo pipefail
board_repo=$(cd "$(dirname "$0")/.." && pwd)
if [ "$#" -eq 0 ]; then
  echo "usage: bash tools/with-compile-lock.sh <command> [arguments...]" >&2
  exit 2
fi
board_lock_source=$(node - "$board_repo/test.sh" <<'JS'
const fs = require('fs');
const text = fs.readFileSync(process.argv[2], 'utf8');
const start = '# >>> clawdline suite lock >>>';
const end = '# <<< clawdline suite lock <<<';
const a = text.indexOf(start), b = text.indexOf(end);
if (a < 0 || b <= a || text.indexOf(start, a + start.length) >= 0) {
  throw new Error('compile_lock_source_invalid');
}
process.stdout.write(text.slice(a + start.length, b));
JS
)
export CLAWDLINE_SUITE_LOCK_HOLDER="${CLAWDLINE_SUITE_LOCK_HOLDER:-focused-check}"
export CLAWDLINE_SUITE_LOCK_TREE="${CLAWDLINE_SUITE_LOCK_TREE:-$board_repo}"
LOG="${TMPDIR:-/tmp}/clawdline-focused-$$.log"
eval "$board_lock_source"
clawdline_suite_lock_phase compiling
"$@"
