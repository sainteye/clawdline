#!/bin/sh
# tools/wait-hosted-console.sh <sha> [--deadline 15m] [--strict-catalogs]
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

usage() { echo "usage: tools/wait-hosted-console.sh <sha> [--deadline 15m] [--strict-catalogs]" >&2; exit 2; }
[ $# -ge 1 ] || usage
sha=$1; shift
deadline=15m
strict_catalogs=
while [ $# -gt 0 ]; do
  case "$1" in
    --deadline) [ $# -ge 2 ] || usage; deadline=$2; shift 2 ;;
    --strict-catalogs) strict_catalogs=--strict; shift ;;
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
assets_tmp=
cleanup() {
  rc=$?
  [ -z "$assets_tmp" ] || rm -rf "$assets_tmp"
  if [ "$rc" -eq 1 ]; then sh tools/notify-release-failure.sh cloud "$full"; fi
}
trap cleanup EXIT

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

assets_tmp=$(mktemp -d "${TMPDIR:-/tmp}/clawdline-hosted-assets.XXXXXX") || exit 1
curl -fsS --max-time 20 "$origin/" -o "$assets_tmp/index.html" || exit 1
bundle=$(grep -o 'assets/main-[^"]*\.js' "$assets_tmp/index.html" | head -n 1)
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

# The same word also exists in the error page produced by a truthy, invalid
# declaration. Validate the compiled configuration, not only the branch name.
curl -fsS --max-time 20 "$origin/$bundle" | node web/console/tools/check-hosted-build.mjs "$stamp" || exit 1

# A missing CSS asset can return the SPA HTML with HTTP 200. The generated
# entry's styles must actually be CSS before this deployment is accepted.
node web/console/tools/check-hosted-assets.mjs --list "$origin" "$assets_tmp/index.html" > "$assets_tmp/styles" || exit 1
while IFS= read -r style; do
  curl -fsS --max-time 20 -D "$assets_tmp/headers" "$origin$style" -o "$assets_tmp/body" || exit 1
  node web/console/tools/check-hosted-assets.mjs --style "$style" "$assets_tmp/headers" "$assets_tmp/body" || exit 1
done < "$assets_tmp/styles"

# Check the same versioned paths that CloudGate fetches during startup.
if [ -f web/console/public/catalogs/en.json ]; then
  python3 tools/check-hosted-catalogs.py "$origin" --build "$stamp" $strict_catalogs || exit 1
fi
tools/notify-release-success.sh cloud "$stamp"
exit 0
