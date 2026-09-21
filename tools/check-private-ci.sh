#!/bin/bash
# Run every public privacy rule in CI without pretending that CI owns the
# private word list. The tree's exit 3 is preserved as a visible warning.
# History gets a temporary fixed-rule checkpoint at the event's base commit,
# so old findings stay visible but only a finding introduced by this change is
# red. Findings and checker failures still fail the workflow.
set -uo pipefail
cd "$(dirname "$0")/.."

tools/check-private.sh
tree_status=$?
case "$tree_status" in
  0) echo "privacy: working tree complete" ;;
  3)
    echo "::warning title=Privacy guard undetermined::The working tree passed the fixed rules, but CI has no private-word list. This is not publication approval."
    ;;
  *) echo "privacy: working tree failed with exit $tree_status" >&2; exit "$tree_status" ;;
esac

base=${CLAWDLINE_CI_BASE:-}
case "$base" in
  ""|0000000000000000000000000000000000000000) base=HEAD^ ;;
esac
if ! git cat-file -e "$base^{commit}" 2>/dev/null; then
  base=HEAD^
fi

tmp=$(mktemp -d "${TMPDIR:-/tmp}/clawdline-private-ci.XXXXXX") || exit 2
trap 'rm -rf "$tmp"' EXIT HUP INT TERM
words="$tmp/fixed-rules-only"
checkpoint="$tmp/history-checkpoint"
# The list needs one word so the fixed rules run, and it must be a word this
# project never says. Written in halves because a whole one would be a word
# this project says — in the file that defines it, which is where the scan
# below found it on CI's first run.
printf '%s%s\n' 'clawdline-ci-private-word-sentinel' '-not-used-by-the-project' >"$words"

CLAWDLINE_PRIVATE_WORDS="$words" tools/check-private.sh \
  -history -revs="$base" -checkpoint="$checkpoint" >"$tmp/baseline.out" 2>&1
baseline_status=$?
case "$baseline_status" in
  0|1) tail -n 8 "$tmp/baseline.out" ;;
  *) cat "$tmp/baseline.out"; exit "$baseline_status" ;;
esac

CLAWDLINE_PRIVATE_WORDS="$words" tools/check-private.sh \
  -history -new -revs=HEAD -checkpoint="$checkpoint" >"$tmp/change.out" 2>&1
change_status=$?
case "$change_status" in
  0) tail -n 10 "$tmp/change.out" ;;
  *) cat "$tmp/change.out"; exit "$change_status" ;;
esac

echo "::warning title=Private-word history rule unavailable::History was checked incrementally with every fixed rule. The uncommitted private-word list is still required before publication."
