#!/bin/sh
# tools/wait-hosted-console.sh <sha> [--deadline 15m]
#
# Waits until the hosted console serves <sha> or a commit after it, then runs
# docs/hosted-console.md's check: the served bundle took the Cloud branch.
# Exit 0 when both hold, 1 when the deadline passes or the check fails, 2 on a
# usage mistake.
#
# It is written to be the command of a callback, so the session that deployed
# ends its turn instead of polling:
#
#   clawdline callback --title "The hosted console serves <sha>" --timeout 20m -- \
#     tools/wait-hosted-console.sh <sha>
#
# CLAWDLINE_CONSOLE_URL points it at another origin (a test's).
set -u

usage() { echo "usage: tools/wait-hosted-console.sh <sha> [--deadline 15m]" >&2; exit 2; }
[ $# -ge 1 ] || usage
sha=$1; shift
deadline=15m
while [ $# -gt 0 ]; do
  case "$1" in
    --deadline) [ $# -ge 2 ] || usage; deadline=$2; shift 2 ;;
    *) usage ;;
  esac
done
case "$deadline" in
  *m) seconds=$(( ${deadline%m} * 60 )) ;;
  *s) seconds=${deadline%s} ;;
  *) usage ;;
esac
origin=${CLAWDLINE_CONSOLE_URL:-https://app.clawdline.com}
full=$(git rev-parse --verify --quiet "$sha^{commit}") || { echo "$sha is not a commit here" >&2; exit 2; }

# reached: the served stamp is <sha> or descends from it.
reached() {
  [ "$1" = "$full" ] && return 0
  git cat-file -e "$1^{commit}" 2>/dev/null || git fetch --quiet origin main 2>/dev/null || true
  git merge-base --is-ancestor "$full" "$1" 2>/dev/null
}

end=$(( $(date +%s) + seconds ))
stamp=
while :; do
  stamp=$(curl -fsS --max-time 20 "$origin/BUILD.json" 2>/dev/null |
    python3 -c 'import json,sys; print(json.load(sys.stdin)["stamp"])' 2>/dev/null) || stamp=
  if [ -n "$stamp" ] && reached "$stamp"; then
    break
  fi
  if [ "$(date +%s)" -ge "$end" ]; then
    echo "after $deadline the hosted console serves ${stamp:-nothing readable}, not $full or later"
    exit 1
  fi
  sleep 20
done
echo "the hosted console serves $stamp, which contains $full"

bundle=$(curl -fsS --max-time 20 "$origin/" | grep -o 'assets/main-[^"]*\.js' | head -n 1)
if [ -z "$bundle" ]; then
  echo "FAILED: the served index names no assets/main-*.js"
  exit 1
fi
gates=$(curl -fsS --max-time 20 "$origin/$bundle" | grep -c CloudGate)
if [ "${gates:-0}" -eq 0 ]; then
  echo "FAILED: the served $bundle has no CloudGate: the build that went up did not take the Cloud branch"
  exit 1
fi
echo "the served $bundle takes the Cloud branch (CloudGate x$gates)"

# The source directory marks builds that advertise the new catalogs. Older
# released builds have no such directory, so their existing CloudGate check
# remains sufficient. Once a build ships catalogs, all of them must be served.
if [ -f web/console/public/catalogs/en.json ]; then
  python3 tools/check-hosted-catalogs.py "$origin" || exit 1
fi
exit 0
