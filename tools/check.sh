#!/bin/bash
# tools/check.sh [--web]
#
# Every check AGENTS.md requires before a commit, in one command, ending with
# one line per check and an exit code: 0 every check passed, 1 at least one
# did not, 2 this script was called wrongly, 75 the compile slot or memory was
# not had within heavy's --max-wait and nothing was checked (run it again). A
# check that answers "could not tell" (check-private.sh's 3, for one) is not a
# pass, so it counts as failed.
#
# Roots and children each used to write their own loop over the list in
# AGENTS.md; this is that loop, kept beside the list so the two change
# together. Every check runs even after one fails, so one run says everything
# that is wrong. Nothing here writes to the checkout.
#
# The web checks run when anything under web/ differs from where this branch
# left main, committed or not, or when --web is given.
#
# The whole script runs once inside tools/heavy.sh, which is what AGENTS.md
# asks for: a `heavy` inside a `heavy` runs directly, so the steps below that
# AGENTS.md writes with tools/heavy.sh share this one slot.
set -uo pipefail
cd "$(dirname "$0")/.." || exit 2

web=auto
for arg in "$@"; do
  case "$arg" in
    --web) web=yes ;;
    -h | --help) sed -n '2,21p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "usage: tools/check.sh [--web]" >&2; exit 2 ;;
  esac
done

# heavy.sh without a daemon runs the command directly and sets nothing, so the
# script marks its own second run rather than trusting CLAWDLINE_HEAVY alone.
if [ -z "${CLAWDLINE_HEAVY:-}" ] && [ -z "${CLAWDLINE_CHECK_WRAPPED:-}" ]; then
  export CLAWDLINE_CHECK_WRAPPED=1
  HEAVY_REASON="${HEAVY_REASON:-tools/check.sh}" exec tools/heavy.sh "$0" "$@"
fi

if [ "$web" = auto ]; then
  web=no
  base=$(git merge-base HEAD main 2>/dev/null) || base=""
  if [ -z "$base" ]; then
    echo "check: no merge base with main, so the web checks run" >&2
    web=yes
  elif ! git diff --quiet "$base" -- web || [ -n "$(git status --porcelain -- web)" ]; then
    web=yes
  fi
fi

tmp=${TMPDIR:-/tmp}
logs=$(mktemp -d "${tmp%/}/clawdline-check.XXXXXX") || exit 2
summary=()
failed=0

# check <name> <command…> runs one check, keeps its output, and shows the end
# of it only when the check failed.
check() {
  local name=$1; shift
  local log="$logs/${#summary[@]}.log" began=$SECONDS code
  echo "== $name"
  "$@" >"$log" 2>&1
  code=$?
  if [ "$code" -eq 0 ]; then
    summary+=("PASS  $name  ($((SECONDS - began))s)")
  else
    tail -n 40 "$log" | sed 's/^/   | /'
    summary+=("FAIL  $name  (exit $code, $((SECONDS - began))s, log $log)")
    failed=1
  fi
}

# gofmt answers 0 whatever it lists; a file it lists is the failure.
gofmt_silent() { local out; out=$(gofmt -l internal cmd) || return $?; [ -z "$out" ] || { echo "$out"; return 1; }; }

check "gofmt -l internal cmd" gofmt_silent
check "go vet ./..." go vet ./...
check "go build ./..." go build ./...
check "linux/amd64 build ./cmd/clawdline" env GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o /dev/null ./cmd/clawdline
check "windows/amd64 build ./cmd/clawdline" env GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o /dev/null ./cmd/clawdline
check "windows go vet ./..." env GOOS=windows go vet ./...
check "go test ./..." go test ./...
check "contract-gen -check" go run ./tools/contract-gen -check
check "check-legacy-css.sh" tools/check-legacy-css.sh
check "check-machine-words.sh" tools/check-machine-words.sh
check "check-private.sh" tools/check-private.sh
check "check-private.sh -history -new" tools/check-private.sh -history -new
if [ "$web" = yes ]; then
  check "web: npm run check" sh -c 'cd web && npm run deps && npm run check'
  check "web: npm run build" sh -c 'cd web && npm run build'
else
  summary+=("SKIP  web checks  (nothing under web/ changed; --web runs them)")
fi

echo
printf '%s\n' "${summary[@]}"
if [ "$failed" -eq 0 ]; then
  rm -rf "$logs"
  echo "check: every check passed"
else
  echo "check: FAILED; each failed check's whole output is in $logs"
fi
exit "$failed"
