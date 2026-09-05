#!/bin/bash
# guard: tools/check-guards-go-red.sh
# defect: a guard added to tools/ with no red proof beside it
# expect: has no red proof
#
# The dogfood. A mechanism that catches "this guard cannot fail" and cannot itself fail would be
# the next instance of exactly what it is for.
#
# Both arms are a fixture tree carrying one made-up guard; the difference is whether anything in
# Tests/guard-red-proofs/ names it. The runner is pointed at the fixture with CLAWDLINE_GUARD_ROOT,
# which also stops it running the fixture's proofs — what is under test here is the register, and
# executing a fixture's scripts is not part of it.
#
# The fixture's own header lines are written with printf rather than a heredoc so that this file
# contains exactly one line beginning "# guard:" — its own.
set -euo pipefail
ARM="$1"
DIR="$2"

mkdir -p "$DIR/tools" "$DIR/Tests/guard-red-proofs"
printf '#!/bin/bash\nexit 0\n' > "$DIR/tools/check-nothing-at-all.sh"

if [ "$ARM" = clean ]; then
  {
    printf '#!/bin/bash\n'
    printf '# %s: tools/check-nothing-at-all.sh\n' guard
    printf '# %s: nothing at all, which is what this fixture is\n' defect
    printf '# %s: a sentence no run of this proof ever prints\n' expect
    printf 'exit 1\n'
  } > "$DIR/Tests/guard-red-proofs/nothing-at-all.sh"
fi

exec env CLAWDLINE_GUARD_ROOT="$DIR" bash "$GUARD_REPO/tools/check-guards-go-red.sh"
