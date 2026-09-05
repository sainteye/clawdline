#!/bin/bash
# guard: tools/check-curl-status.py
# prevents: a briefing or a skill guide teaching an agent a command that exits 0 when the server refuses it, so the agent reports a note it never sent and nobody downstream can tell
# defect: one more command added to a child briefing's Swift literal, written the way the others were written before they were fixed
# expect: teaches a command that reads curl's exit status
#
# The mutation is an **addition**, because that is how this comes back. Nobody deletes
# `--fail-with-body` from a working recipe; somebody adds a route, copies the block above it, and
# the copy is older than the fix. The clean arm is the same tree with that one block already
# carrying the flag, so what the pair separates is the flag on a new command and nothing else.
#
# The fixture is three files rather than one, because the guard now refuses an empty scan in three
# places — no tracked files, no curl in shell, no command in any instruction source — and a proof
# whose fixture trips a structural refusal proves the refusal instead of the finding.
set -euo pipefail
ARM="$1"
DIR="$2"

mkdir -p "$DIR/Sources" "$DIR/Resources/skill-guides"

# The shell domain, so the scan of tracked scripts is not empty.
{
  echo '#!/bin/bash'
  echo 'curl --fail-with-body -sS http://127.0.0.1:1/v1/health'
} > "$DIR/probe.sh"

# The instruction domain the guides live in, compliant on both arms.
{
  echo '# A guide an assistant reads and types'
  echo
  echo '```bash'
  echo 'curl --fail-with-body -sS "http://127.0.0.1:$PORT/v1/orchestrator/tasks"'
  echo '```'
} > "$DIR/Resources/skill-guides/guide.md"

# The briefing this app writes into a child's terminal. One recipe on both arms; the broken arm
# has a second one added beneath it.
{
  echo 'let brief = """'
  echo '      Say what you are doing:'
  echo
  echo '      ```bash'
  echo '      curl --fail-with-body -sS -X POST http://127.0.0.1:7717/v1/orchestrator/progress \\'
  echo '        -H "X-Clawdline-Task-Secret: <TASK_SECRET>"'
  echo '      ```'
  if [ "$ARM" = broken ]; then
    echo
    echo '      And to look at what else is running:'
    echo
    echo '      ```bash'
    echo '      curl -s http://127.0.0.1:7717/v1/orchestrator/inflight \\'
    echo '        -H "X-Clawdline-Task-Secret: <TASK_SECRET>"'
    echo '      ```'
  fi
  echo '      """'
} > "$DIR/Sources/ChildBrief.swift"

# The guard lists its subjects with `git ls-files`, so the fixture has to be a repository. It
# never needs a commit: the index is what `ls-files` reads.
git -C "$DIR" init -q
git -C "$DIR" add -A

exec env CLAWDLINE_CURL_SCAN_ROOT="$DIR" python3 "$GUARD_REPO/tools/check-curl-status.py"
