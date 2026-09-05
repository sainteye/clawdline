#!/bin/bash
# guard: tools/check-curl-status.py
# prevents: a request whose HTTP status is never looked at, so a 401 or a 409 comes back through curl's exit code as the answer the caller was hoping for
# defect: a shell script that reads only curl's exit status
# expect: reads curl's exit status
#
# The smallest possible tree: one script, one call, and the flag that is the whole difference
# between the two arms — plus one instruction file, which is not scenery. The guard scans a second
# domain now (the briefings and guides this app hands to agents) and refuses a scan that finds none
# of them, so without this file the clean arm would be red for a structural reason and the pair
# would be separating that instead of the defect. `Tests/guard-red-proofs/curl-status-briefing.sh`
# is where that second domain is actually proved.
set -euo pipefail
ARM="$1"
DIR="$2"

mkdir -p "$DIR/Resources/skill-guides"
{
  echo '# A guide an assistant reads and types'
  echo
  echo '```bash'
  echo 'curl --fail-with-body -sS "http://127.0.0.1:$PORT/v1/health"'
  echo '```'
} > "$DIR/Resources/skill-guides/guide.md"

{
  echo '#!/bin/bash'
  echo '# A probe against a port nothing is listening on. It is never run; it is read.'
  if [ "$ARM" = broken ]; then
    echo 'curl -s http://127.0.0.1:1/v1/health'
  else
    echo 'curl -fs http://127.0.0.1:1/v1/health'
  fi
} > "$DIR/probe.sh"

# The guard lists its subjects with `git ls-files`, so the fixture has to be a repository. It
# never needs a commit: the index is what `ls-files` reads.
git -C "$DIR" init -q
git -C "$DIR" add -A

exec env CLAWDLINE_CURL_SCAN_ROOT="$DIR" python3 "$GUARD_REPO/tools/check-curl-status.py"
