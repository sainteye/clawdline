#!/bin/bash
# Every guard in `tools/` has to have been seen to fail.
#
# A check nobody has watched go red is worth less than no check at all, because it makes people
# believe somebody is looking. This repository has caught that twice in one evening: a claims
# comparison in `tools/git-hooks/pre-commit` that is identically true inside a linked worktree, and
# a `stale > worst` comparison that was an identity in the situation it was written for. Both were
# green. Both were green the way a working guard is green.
#
# So: for each `tools/check-*` there is a proof in `Tests/guard-red-proofs/` that puts one defect in
# front of it and requires it to say so — and, because a guard that is red for its own reasons would
# satisfy that trivially, the proof is a **differential**:
#
#   the broken arm exits non-zero and its output contains the sentence the defect should produce,
#   and the clean arm's output does not contain that sentence.
#
# Not "the clean arm is green". The architecture guard is legitimately red for minutes at a time
# while a seal window is open, and a mechanism that could not be used during those minutes would be
# switched off during exactly the changes that need it. What the pair proves is narrower and enough:
# *this sentence appears when the defect is there and not when it is not.* Where the clean arm was
# already red for an unrelated reason the run says so, naming it, rather than passing in silence.
#
# **And the meta check below is the point of the file as much as the proofs are.** A guard added
# with no proof beside it is the ninth instance of the family this was written for, so a
# `tools/check-*` that no proof names fails this — and this file matches `tools/check-*` itself, so
# it is held to its own rule by its own list. `Tests/guard-red-proofs/self-meta.sh` is the proof
# that it goes red, run against a fixture tree carrying a guard nobody registered.
#
# Usage:
#   tools/check-guards-go-red.sh            # meta check, then every proof
#   tools/check-guards-go-red.sh --meta     # the meta check alone, which compiles and runs nothing
#
# CLAWDLINE_GUARD_ROOT points the meta check at another tree; in that mode the proofs are not run,
# because what is under test there is the meta check and not somebody else's fixtures.
set -euo pipefail
cd "$(dirname "$0")/.."

REPO="$PWD"
GUARD_ROOT="${CLAWDLINE_GUARD_ROOT:-$REPO}"
PROOF_DIR="$GUARD_ROOT/Tests/guard-red-proofs"
EXEMPT_MARKER="red-proof-exempt:"
# A marker with nothing after it is a silencer, not an exemption. Same floor as the curl guard's.
EXEMPT_MIN_REASON=16

failures=0
notes=0

fail() {
  echo "guard red proofs: $1" >&2
  failures=$((failures + 1))
}

# ---- The meta check ------------------------------------------------------------------------
# Two directions, because a registry rots both ways: a guard nothing proves, and a proof that
# names a guard which is no longer there.

guards=()
while IFS= read -r g; do
  [ -n "$g" ] && guards+=("$g")
done < <(find "$GUARD_ROOT/tools" -maxdepth 1 -type f -name 'check-*' 2>/dev/null | sort)

if [ "${#guards[@]}" -eq 0 ]; then
  # The empty-scan refusal every guard here owes: passing because there was nothing to look at is
  # the failure this whole file is about.
  echo "guard red proofs: no tools/check-* found under $GUARD_ROOT — the finder, not the tree." >&2
  exit 2
fi

proofs=()
if [ -d "$PROOF_DIR" ]; then
  while IFS= read -r p; do
    [ -n "$p" ] && proofs+=("$p")
  done < <(find "$PROOF_DIR" -maxdepth 1 -type f -name '*.sh' 2>/dev/null | sort)
fi

# guard path (repo-relative) -> the proof that names it
proved=""
for proof in ${proofs[@]+"${proofs[@]}"}; do
  named=$(sed -n 's/^# guard:[[:space:]]*//p' "$proof" | head -1)
  expect=$(sed -n 's/^# expect:[[:space:]]*//p' "$proof" | head -1)
  defect=$(sed -n 's/^# defect:[[:space:]]*//p' "$proof" | head -1)
  base=${proof##*/}
  if [ -z "$named" ] || [ -z "$expect" ] || [ -z "$defect" ]; then
    fail "$base declares no '# guard:', '# expect:' or '# defect:' header; all three are the proof"
    continue
  fi
  if [ ! -f "$GUARD_ROOT/$named" ]; then
    fail "$base proves $named, which is not in this checkout — a proof for a guard that left reads as cover and is none"
    continue
  fi
  proved="$proved
$named"
done

for guard in "${guards[@]}"; do
  relative=${guard#"$GUARD_ROOT"/}
  if printf '%s\n' "$proved" | grep -qxF "$relative"; then
    continue
  fi
  # The marker has to be a comment of its own, not the word appearing in a string: a guard that
  # merely talks about exemptions must not be able to exempt itself by talking about them.
  reason=$(sed -n "s/^[[:space:]]*#[[:space:]]*$EXEMPT_MARKER[[:space:]]*//p" "$guard" | head -1)
  if [ -n "$reason" ] && [ "${#reason}" -ge "$EXEMPT_MIN_REASON" ]; then
    echo "  exempt  $relative — $reason"
    continue
  fi
  if [ -n "$reason" ]; then
    fail "$relative carries a $EXEMPT_MARKER marker with no reason behind it"
  else
    fail "$relative has no red proof. Add one to Tests/guard-red-proofs/, or write '# $EXEMPT_MARKER <why this cannot be proved to fail>' into the guard itself"
  fi
done

if [ "${1:-}" = "--meta" ] || [ -n "${CLAWDLINE_GUARD_ROOT:-}" ]; then
  if [ "$failures" -gt 0 ]; then
    echo "guard red proofs: $failures problem(s) in the register of proofs" >&2
    exit 1
  fi
  echo "guard red proofs: ${#guards[@]} guards, every one of them registered"
  exit 0
fi

# ---- Running the proofs --------------------------------------------------------------------
# One copy of the tracked tree, made once and cloned per arm. Several guards read the whole
# repository and locate it from their own path, so the only way to put a defect in front of them is
# to give them a tree with the defect in it.

WORK=$(mktemp -d "${TMPDIR:-/tmp}/clawdline-red-proofs.XXXXXX")
cleanup_red_proofs() { rm -rf "$WORK"; }
trap cleanup_red_proofs EXIT

GUARD_BASE="$WORK/base"
mkdir -p "$GUARD_BASE"
git ls-files -z | tar -cf - --null -T - | ( cd "$GUARD_BASE" && tar -xf - )
export GUARD_BASE GUARD_REPO="$REPO"

run_arm() {
  # $1 proof, $2 arm, $3 dir. Prints the output; returns the guard's status.
  mkdir -p "$3"
  bash "$1" "$2" "$3" 2>&1
}

for proof in ${proofs[@]+"${proofs[@]}"}; do
  base=${proof##*/}
  named=$(sed -n 's/^# guard:[[:space:]]*//p' "$proof" | head -1)
  expect=$(sed -n 's/^# expect:[[:space:]]*//p' "$proof" | head -1)
  defect=$(sed -n 's/^# defect:[[:space:]]*//p' "$proof" | head -1)
  [ -n "$named" ] && [ -n "$expect" ] && [ -n "$defect" ] || continue

  broken_out=$(run_arm "$proof" broken "$WORK/${base%.sh}-broken") && broken_status=0 || broken_status=$?
  clean_out=$(run_arm "$proof" clean "$WORK/${base%.sh}-clean") && clean_status=0 || clean_status=$?

  if [ "$broken_status" = 0 ]; then
    fail "$named passed a tree with $defect in it and exited 0 — it cannot see the thing it is for"
    continue
  fi
  if ! printf '%s\n' "$broken_out" | grep -qF -- "$expect"; then
    fail "$named went red on $defect, but not for that: it never said \"$expect\". First line: $(printf '%s\n' "$broken_out" | head -1)"
    continue
  fi
  if printf '%s\n' "$clean_out" | grep -qF -- "$expect"; then
    fail "$named says \"$expect\" on the clean arm too, so this proof separates nothing"
    continue
  fi
  if [ "$clean_status" != 0 ]; then
    # Not a failure. The pair still separates the defect from its absence; what it cannot also
    # report is "and otherwise this tree is fine", so it says that out loud instead of implying it.
    notes=$((notes + 1))
    echo "  red     $named — $defect (differential: the clean arm is already red for another reason: $(printf '%s\n' "$clean_out" | head -1))"
  else
    echo "  red     $named — $defect"
  fi
done

if [ "$failures" -gt 0 ]; then
  echo "guard red proofs: $failures of ${#proofs[@]} proof(s) did not hold" >&2
  exit 1
fi

echo "guard red proofs: ${#guards[@]} guards, ${#proofs[@]} proved to go red$([ "$notes" -gt 0 ] && printf ' (%d over an already-red control)' "$notes")"
