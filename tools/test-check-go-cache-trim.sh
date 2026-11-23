#!/bin/bash
# tools/check.sh starts one `clawdline go-cache trim` in the background when it
# exits, whatever its outcome, so direct `go test` and `go vet` runs also bring
# the default shared Go cache back under its registered budget. It never
# changes check.sh's exit status, never waits for the trim, and never trims an
# explicit GOCACHE or from inside the compile slot (tools/heavy.sh trims there).
#
# The wiring runs against a fake clawdline. With CHECK_TRIM_E2E_BIN set to a
# built clawdline, it also fills a default cache under a throwaway HOME with
# sparse entries over the budget and requires check.sh to bring it under.
set -euo pipefail
cd "$(dirname "$0")/.." || exit 1
real_go=$(command -v go)
scratch=$(mktemp -d)
trap 'rm -rf -- "$scratch"' EXIT
mkdir -p "$scratch/repo/tools" "$scratch/bin" "$scratch/home"
cp tools/check.sh "$scratch/repo/tools/"
cat > "$scratch/bin/go" <<'MOCK'
#!/bin/bash
if [ "$1" = env ]; then exec "$REAL_GO" "$@"; fi
printf 'go %s\n' "$*" >> "$CALLS"
if [ "$1" = test ] && [ "${FAIL_TESTS:-0}" = 1 ]; then exit 7; fi
exit 0
MOCK
cat > "$scratch/fake-clawdline" <<'MOCK'
#!/bin/bash
if [ "$#" -eq 0 ] || [ "$*" = go-cache ]; then
  [ "${TRIM_CAPABILITY:-1}" = 1 ] && echo 'usage: clawdline go-cache trim [--dry-run] [--json]'
  exit 2
fi
if [ "$1 ${2:-}" = 'go-cache trim' ]; then
  sleep "${TRIM_SLEEP:-0}"
  printf 'trim GOCACHE=%s\n' "${GOCACHE:-}" >> "$CALLS"
  exit "${TRIM_EXIT:-0}"
fi
exit 2
MOCK
chmod +x "$scratch/bin/go" "$scratch/fake-clawdline"
export PATH="$scratch/bin:$PATH" TMPDIR="$scratch" CALLS="$scratch/calls" REAL_GO="$real_go" HOME="$scratch/home"
unset GOCACHE XDG_CACHE_HOME CLAWDLINE_HEAVY CLAWDLINE_CALLBACK_TASK_ID CLAWDLINE_CHECK_GO_CACHE_TRIM XDG_DATA_HOME
cd "$scratch/repo"

# wait_for_trim <count>: the background trim writes its line after check.sh returned.
wait_for_trim() {
  local i
  for i in $(seq 1 100); do
    [ "$(grep -c '^trim ' "$CALLS" 2>/dev/null || true)" -ge "$1" ] && return 0
    sleep 0.1
  done
  echo "expected $1 trim(s); calls were:" >&2; cat "$CALLS" >&2; return 1
}
no_trim() { sleep 0.5; if grep -q '^trim ' "$CALLS"; then echo "unexpected trim ($1)" >&2; cat "$CALLS" >&2; exit 1; fi; }

export CLAWDLINE="$scratch/fake-clawdline"

# A passing check trims once; a slow trim does not hold check.sh or its output pipe.
: > "$CALLS"
began=$SECONDS
out=$(TRIM_SLEEP=3 bash tools/check.sh --quick ./internal/example)
[ $((SECONDS - began)) -lt 3 ] || { echo 'check.sh waited for the trim' >&2; exit 1; }
grep -q 'DEVELOPMENT CHECK ONLY' <<<"$out"
wait_for_trim 1
grep -Fxq 'trim GOCACHE=' "$CALLS"

# A failing check keeps its own status and still trims; a failed trim changes nothing.
: > "$CALLS"
if FAIL_TESTS=1 TRIM_EXIT=2 bash tools/check.sh --quick ./internal/example > /dev/null 2>&1; then exit 1; else [ "$?" = 7 ]; fi
wait_for_trim 1
# A refused invocation (usage error) keeps 2 and still trims.
: > "$CALLS"
if bash tools/check.sh --quick > /dev/null 2>&1; then exit 1; else [ "$?" = 2 ]; fi
wait_for_trim 1

# No trim: explicit GOCACHE, inside the compile slot, opted out, or a clawdline without go-cache.
: > "$CALLS"; GOCACHE="$scratch/explicit" bash tools/check.sh --quick ./internal/example > /dev/null; no_trim explicit
: > "$CALLS"; CLAWDLINE_HEAVY=held bash tools/check.sh --quick ./internal/example > /dev/null; no_trim heavy
: > "$CALLS"; CLAWDLINE_CHECK_GO_CACHE_TRIM=0 bash tools/check.sh --quick ./internal/example > /dev/null; no_trim opted-out
: > "$CALLS"; TRIM_CAPABILITY=0 bash tools/check.sh --quick ./internal/example > /dev/null; no_trim capability

if [ -z "${CHECK_TRIM_E2E_BIN:-}" ]; then
  echo 'check go-cache trim wiring: ok (set CHECK_TRIM_E2E_BIN to a built clawdline for the budget run)'
  exit 0
fi

# End to end: 25 sparse 1 GiB entries, used a year ago, in this HOME's default cache.
export CLAWDLINE="$CHECK_TRIM_E2E_BIN"
cache=$(go env GOCACHE)
case "$cache" in "$scratch"/*) ;; *) echo "default cache escaped the throwaway HOME: $cache" >&2; exit 1 ;; esac
[ -z "$(go env -changed GOCACHE)" ]
mkdir -p "$cache/00"
printf 'This directory holds cached build artifacts from the Go build system.\n' > "$cache/README"
for i in $(seq 1 25); do
  f="$cache/00/$(printf '%064x' "$i")-d"
  : > "$f"; truncate -s 1073741824 "$f"
  touch -t "$(printf '2025%02d010000' $(( (i - 1) / 3 + 1 )))" "$f"
done
budget=$((20 << 30))
before=$(find "$cache/00" -type f | wc -l | tr -d ' ')
[ "$before" = 25 ]
: > "$CALLS"
bash tools/check.sh --quick ./internal/example > /dev/null
for i in $(seq 1 300); do
  left=$(find "$cache/00" -type f | wc -l | tr -d ' ')
  [ "$left" -le 20 ] && break
  sleep 0.1
done
left=$(find "$cache/00" -type f | wc -l | tr -d ' ')
echo "check go-cache trim end to end: $before GiB before, $left GiB after (budget $((budget >> 30)) GiB)"
[ "$left" -le 20 ] || { echo 'the cache stayed over budget' >&2; exit 1; }
# Trimming is oldest-used first (trim_test.go covers the order in full).
[ ! -f "$cache/00/$(printf '%064x' 1)-d" ]
echo 'check go-cache trim wiring and budget run: ok'
