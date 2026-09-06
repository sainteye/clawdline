#!/bin/bash
# guard: tools/check-landing-records.py
# prevents: a delivery reaching the target branch while its landing record is never written, so the work is finished and nothing anywhere says so — the shape that produced 22 hand-written records in one evening, 14 of them for code that had been sitting in main for days
# defect: a task whose merged delivery has no landing record
# expect: and no record was written
#
# **The mutation is the missing record, not a missing file.** Both arms build the same repository
# and the same registry: one task, one delivery branch, merged into `main`. The only difference is
# the `landing` key — present and `landed` on the clean arm, absent on the broken one. That is the
# failure in its own terms: the work landed, and the one sentence that says so was never written.
#
# Deleting the guard, emptying the registry or pointing it at nothing would all go red too, and
# none of them is this. Nobody deletes the registry; people finish a delivery, merge it, and move
# on to the next thing.
#
# `--since` is passed on purpose rather than left at the built-in cutoff. The cutoff is a date and
# the grace period is six hours, so "after the cutoff and older than the grace" is a window that
# moves; a proof that only holds on some days is not a proof. The fixture's commits are dated 2021
# and the cutoff is 2020, which puts the finding in the fatal set on every day this ever runs.
set -euo pipefail
ARM="$1"
DIR="$2"

REPO="$DIR/repo"
STORE="$DIR/store"
TASK="11111111-2222-3333-4444-555555555555"
mkdir -p "$REPO" "$STORE"

export GIT_AUTHOR_DATE="2021-01-01T00:00:00Z" GIT_COMMITTER_DATE="2021-01-01T00:00:00Z"
export GIT_AUTHOR_NAME="red proof" GIT_AUTHOR_EMAIL="red@proof.invalid"
export GIT_COMMITTER_NAME="red proof" GIT_COMMITTER_EMAIL="red@proof.invalid"

git -C "$REPO" init -q
git -C "$REPO" symbolic-ref HEAD refs/heads/main
echo base > "$REPO/file.txt"
git -C "$REPO" add file.txt
git -C "$REPO" commit -q -m "base"
BASE=$(git -C "$REPO" rev-parse HEAD)

git -C "$REPO" checkout -q -b "clawdline/task/$TASK"
echo delivered >> "$REPO/file.txt"
git -C "$REPO" commit -q -am "the delivery"
HEAD_COMMIT=$(git -C "$REPO" rev-parse HEAD)

# The landing itself: the delivery is now in `main`, exactly as the broker's own
# `merge-base --is-ancestor` would find it.
git -C "$REPO" checkout -q main
git -C "$REPO" merge -q --no-ff -m "land the delivery" "clawdline/task/$TASK"

LANDING=
if [ "$ARM" != broken ]; then
  LANDING=",\"landing\":{\"state\":\"landed\",\"commit\":\"$HEAD_COMMIT\",\"target\":\"main\",\"landed_at\":1609459200,\"since\":1609459200}"
fi

cat > "$STORE/orchestrator.json" <<JSON
{"version": 1,
 "tasks": [
   {"id": "$TASK",
    "state": "success",
    "title": "a delivery that reached main",
    "created": 1609459200,
    "finished_at": 1609459200,
    "claims": [],
    "root_label": "the root that has since gone home",
    "project_dir": "$REPO",
    "worktree": {"repository": "$REPO", "path": "$DIR/checkout",
                 "branch": "clawdline/task/$TASK",
                 "base": "$BASE", "head": "$HEAD_COMMIT"}$LANDING}
 ]}
JSON

export CLAWDLINE_REMOTE_DIR="$STORE"
exec python3 "${GUARD_REPO:-.}/tools/check-landing-records.py" --since 2020-01-01
