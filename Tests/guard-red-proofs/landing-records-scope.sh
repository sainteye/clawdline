#!/bin/bash
# guard: tools/check-landing-records.py
# prevents: a suite failing over a landing record the run in front of it is not permitted to write — the shape that killed two runs on 2026-09-06, one of them an isolated child that died over another root's landing in the base repository and could not have settled it if it had understood the message
# defect: an unrecorded landing in the repository this run is standing in, from a checkout that is allowed to close it
# expect: and this run can settle them
#
# **The same debt, the same registry, one variable.** Both arms build one repository, one delivery
# branch merged into `main`, and one registry row with no `landing` key on it — the fixture from
# `landing-records.sh`, unchanged and identically broken in both arms. What differs is where the
# guard is run from: the repository itself, or a linked worktree of it. That is the second of the
# two conditions the failing scope now asks, and this pair is the only place it is held.
#
# The arm names are the harness's and read backwards here, so: `broken` is the tree with the defect
# in front of a runner that can act on it, and `clean` is the same defect in front of a runner that
# cannot. Nothing about the registry, the branch or the merge changes between them.
#
# **What this pair cannot hold, said out loud.** The harness asserts that the broken arm goes red
# for this sentence and that the clean arm does not say it. It cannot assert what the clean arm
# *does* say — and "exits 0 quietly" is precisely the defect `tools/git-hooks/pre-commit` carries a
# paragraph about. `Tests/landing-records-scope.mjs` is where that half is held: it runs both arms
# and requires the worktree one to name the checkout, the repository, `--git-common-dir`, who can
# settle the row, and that it is not the green of a run that found nothing.
set -euo pipefail
ARM="$1"
DIR="$2"

REPO="$DIR/repo"
STORE="$DIR/store"
WORKTREE="$DIR/linked-checkout"
TASK="66666666-7777-8888-9999-aaaaaaaaaaaa"
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

git -C "$REPO" checkout -q main
git -C "$REPO" merge -q --no-ff -m "land the delivery" "clawdline/task/$TASK"

# No `landing` key in either arm: the debt is the constant here, not the variable.
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
                 "base": "$BASE", "head": "$HEAD_COMMIT"}}
 ]}
JSON

export CLAWDLINE_REMOTE_DIR="$STORE"
GUARD=$(cd "${GUARD_REPO:-.}" && pwd)/tools/check-landing-records.py

if [ "$ARM" = broken ]; then
  # Standing in the repository the debt is in, in its main worktree: both conditions met.
  cd "$REPO"
else
  # A linked worktree of the same repository — made here rather than faked, because the whole
  # question is what git answers for `--git-common-dir`, and a hand-built `.git` file would be this
  # proof asserting its own idea of the answer. It belongs to the fixture repository this script
  # created three commands ago and dies with the harness's temporary directory.
  git -C "$REPO" worktree add --quiet --detach "$WORKTREE" main
  cd "$WORKTREE"
fi
exec python3 "$GUARD" --since 2020-01-01
