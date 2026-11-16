#!/bin/bash
# Exercise the real check entrypoint against a small Git fixture and fake
# compilers. A skipped build must not silently become skipped Go tests.
set -euo pipefail
cd "$(dirname "$0")/.." || exit 1
real_node=$(command -v node)
real_python=$(command -v python3)
scratch=$(mktemp -d)
trap 'rm -rf -- "$scratch"' EXIT
mkdir -p "$scratch/repo/tools" "$scratch/repo/web/console/tools" "$scratch/bin"
cp tools/check.sh tools/check-plan.mjs tools/check-scope.mjs tools/check-profile.mjs "$scratch/repo/tools/"
for name in check-legacy-css.sh check-machine-words.sh check-shell-expansions.sh check-private.sh test-notify-release-success.sh test-heavy-linux-cache.sh test-linux-dev-build.sh test-machine-words-changes.sh test-heavy-handoff.sh test-check-modes.sh test-staged-app-watcher.sh test-shell-language.sh test-shell-pairing-permission.sh; do
  printf '#!/bin/sh\nprintf "%%s\\n" "%s" >> "$CALLS"\n' "$name" > "$scratch/repo/tools/$name"
  chmod +x "$scratch/repo/tools/$name"
done
cat > "$scratch/repo/tools/heavy.sh" <<'MOCK'
#!/bin/bash
printf 'heavy %s\n' "$*" >> "$CALLS"
if [ "${1:-}" = --compile ]; then shift; fi
exec "$@"
MOCK
chmod +x "$scratch/repo/tools/heavy.sh"
mkdir -p "$scratch/repo/tools/release"
printf '#!/bin/sh\nprintf "supply-chain\\n" >> "$CALLS"\n' > "$scratch/repo/tools/release/supply-chain.test.sh"
printf '#!/bin/sh\nprintf "dmg-install-harness\\n" >> "$CALLS"\n' > "$scratch/repo/tools/release/test-dmg-install-smoke.sh"
cat > "$scratch/bin/go" <<'MOCK'
#!/bin/bash
printf 'go %s\n' "$*" >> "$CALLS"
if [ "$1" = list ]; then root=$(pwd -P); printf '{"Dir":"%s/internal/example","ImportPath":"fixture/example"}\n{"Dir":"%s/internal/caller","ImportPath":"fixture/caller","Imports":["fixture/example"]}\n' "$root" "$root"; exit 0; fi
if [ "${FAIL_TESTS:-0}" = 1 ] && [ "$1" = test ]; then exit 7; fi
if [ "$1 $2" = 'test -json' ]; then
  printf '%s\n' '{"Action":"pass","Package":"fixture","Elapsed":1}'
fi
MOCK
cat > "$scratch/bin/node" <<'MOCK'
#!/bin/bash
case "$1" in
  tools/ensure-web-deps.mjs)
    [ "${FAIL_DEPS:-0}" != 1 ] || exit 9
    : > "$CALLS.deps"
    exit 0 ;;
  tools/check-plan.mjs|tools/check-scope.mjs)
    [ -f "$CALLS.deps" ] || exit 31
    if [ "${BROKEN_PLAN:-0}" = 1 ]; then printf 'invalid\n'; exit 0; fi
    if [ "${HANG_STEP:-}" != '' ] && [ "${2:-}" = --out ]; then
      exec "$REAL_NODE" -e 'require("fs").writeFileSync(process.argv[1],JSON.stringify({full:false,reason:"hanging test fixture",go:{runs:[],compile:[]},paths:[],web:process.env.HANG_STEP==="web",webTests:["web/console/hang.test.mjs"],guards:{format:false,capacity:false,contract:false,legacy:false,borders:false,machine:false,shell:false,tooling:process.env.HANG_STEP==="tooling"}}))' "$3"
    fi
    exec "$REAL_NODE" "$@" ;;
  tools/check-profile.mjs) exec "$REAL_NODE" "$@" ;;
esac
if [ "${HANG_STEP:-}" != '' ] && [[ " $* " = *' --test '* ]]; then
  args=()
  deadline_seen=no
  for arg in "$@"; do
    # Exercise the real runner with a shorter deadline in this fixture only.
    if [ "$arg" = --test-timeout=120000 ]; then arg=--test-timeout=150; deadline_seen=yes; fi
    args+=("$arg")
  done
  [ "$deadline_seen" = yes ] || { echo 'Node test deadline missing' >&2; exit 41; }
  exec "$REAL_NODE" ${args[@]+"${args[@]}"}
fi
exit 0
MOCK
cat > "$scratch/bin/npm" <<'MOCK'
#!/bin/bash
printf 'npm %s\n' "$*" >> "$CALLS"
if [ "${HANG_NPM:-0}" = 1 ] && [ "$*" = test ]; then
  echo 'starting console/hang.test.mjs' >&2
  exec "$REAL_NODE" console/hang.test.mjs
fi
MOCK
cat > "$scratch/bin/python3" <<'MOCK'
#!/bin/bash
if [ "${1:-}" = -c ]; then
  script=$2
  shift 2
  # Keep the wrapper and cleanup real, shortening only the fixture deadline.
  if [ "${HANG_STEP:-}" != '' ] || [ "${HANG_NPM:-0}" = 1 ]; then
    shift
    set -- 0.5 "$@"
  fi
  exec "$REAL_PYTHON" -c "$script" "$@"
fi
[ "$*" = tools/test-workflow-eval.py ] || exit 32
printf 'workflow-evaluation\n' >> "$CALLS"
exit "${FAIL_WORKFLOW:-0}"
MOCK
printf '#!/bin/sh\nexit 0\n' > "$scratch/bin/gofmt"
chmod +x "$scratch/bin/"*
export PATH="$scratch/bin:$PATH" TMPDIR="$scratch" CALLS="$scratch/calls" REAL_NODE="$real_node" REAL_PYTHON="$real_python" CLAWDLINE_CHECK_WRAPPED=1
unset CLAWDLINE_HEAVY
cd "$scratch/repo"
git init -q -b main
git config user.name Test
git config user.email test@example.invalid
mkdir docs
printf '# Fixture\n' > docs/fixture.md
git add .
git commit -qm 'Initial fixture'
git checkout -qb task
printf 'Documentation edit\n' >> docs/fixture.md
: > "$CALLS"
bash tools/check.sh > "$scratch/docs.log"
! grep -Fxq 'go test ./...' "$CALLS"
grep -Fxq 'go test -count=1 ./internal/config' "$CALLS"
grep -Fxq 'workflow-evaluation' "$CALLS"
! grep -q '^heavy\|^go build\|^go vet\|^npm' "$CALLS"
mkdir -p internal/example
printf 'package example\n' > internal/example/example.go
: > "$CALLS"
bash tools/check.sh > "$scratch/module.log"
grep -Fxq 'go test fixture/example' "$CALLS"
! grep -Fxq 'go test ./...' "$CALLS"
! grep -Fxq 'go test -run=^$ fixture/caller' "$CALLS"
: > "$CALLS"
bash tools/check.sh --callers > "$scratch/callers.log"
grep -Fxq 'go test -run=^$ fixture/caller' "$CALLS"
rm -- internal/example/example.go
# A change to the native shell alone is enough to run its Swift tests on a Mac.
mkdir -p shell/darwin
printf 'let pairing = 1\n' > shell/darwin/Pairing.swift
: > "$CALLS"
bash tools/check.sh > "$scratch/shell.log"
if [ "$(uname -s)" = Darwin ]; then grep -Fxq 'test-shell-pairing-permission.sh' "$CALLS"; fi
rm -r shell
rmdir internal/example internal
: > "$CALLS"
bash tools/check.sh --full > "$scratch/full.log"
grep -Fxq 'go vet ./...' "$CALLS"
grep -Fxq 'go build -o /dev/null ./cmd/clawdline' "$CALLS"
grep -Fxq 'heavy go build ./...' "$CALLS"
! grep -q '^heavy .*test\|^heavy .*vet' "$CALLS"
grep -Fxq 'npm run build' "$CALLS"
grep -Fxq 'supply-chain' "$CALLS"
# The native shell's Swift tests run in every full check on a Mac, and only there.
if [ "$(uname -s)" = Darwin ]; then
  grep -Fxq 'test-shell-pairing-permission.sh' "$CALLS"
  grep -Fxq 'test-shell-language.sh' "$CALLS"
  grep -Fxq 'dmg-install-harness' "$CALLS"
elif grep -Fxq 'test-shell-pairing-permission.sh' "$CALLS"; then
  exit 1
fi
: > "$CALLS"
bash tools/check.sh --quick ./internal/example > "$scratch/quick.log"
[ "$(cat "$CALLS")" = 'go test ./internal/example' ]
grep -q 'DEVELOPMENT CHECK ONLY' "$scratch/quick.log"
: > "$CALLS"
bash tools/check.sh --quick --run=TestExample ./internal/example > /dev/null
[ "$(cat "$CALLS")" = 'go test -run=TestExample ./internal/example' ]
for args in '--quick' '--quick --full ./internal/example' './internal/example'; do
  if bash tools/check.sh $args > /dev/null 2>&1; then exit 1; else [ "$?" = 2 ]; fi
done
if FAIL_TESTS=1 bash tools/check.sh --quick ./internal/example > /dev/null 2>&1; then exit 1; else [ "$?" = 7 ]; fi
if FAIL_TESTS=1 bash tools/check.sh > /dev/null 2>&1; then exit 1; else [ "$?" = 1 ]; fi
if FAIL_WORKFLOW=7 bash tools/check.sh > /dev/null 2>&1; then exit 1; else [ "$?" = 1 ]; fi
: > "$CALLS"
bash tools/check.sh --profile > "$scratch/profile.log"
grep -Fxq 'go test -json -count=1 ./...' "$CALLS"
grep -q 'profile logs retained' "$scratch/profile.log"
: > "$CALLS"
if BROKEN_PLAN=1 bash tools/check.sh > /dev/null 2>&1; then exit 1; else [ "$?" = 2 ]; fi
[ ! -s "$CALLS" ]
rm -f "$CALLS.deps"
bash tools/check.sh --plan > "$scratch/plan.json"
test -f "$CALLS.deps"
if FAIL_DEPS=1 bash tools/check.sh --plan > /dev/null 2>&1; then exit 1; else [ "$?" = 2 ]; fi
[ ! -s "$CALLS" ]
"$REAL_NODE" -e 'const fs=require("fs");const p=JSON.parse(fs.readFileSync(process.argv[1]));if(p.full || p.web)process.exit(1)' "$scratch/plan.json"
# A broken gate deadline must fail this regression test, not hang CI itself.
run_hanging_gate() {
  "$REAL_PYTHON" -c '
import os, signal, subprocess, sys
child = subprocess.Popen(["bash", "tools/check.sh", *sys.argv[1:]], start_new_session=True)
try:
    sys.exit(child.wait(timeout=10))
except subprocess.TimeoutExpired:
    os.killpg(child.pid, signal.SIGKILL)
    child.wait()
    print("check modes: gate failed to enforce its own deadline", file=sys.stderr)
    sys.exit(125)
' "$@"
}
# Real hanging test files must fail through both direct node --test steps.
for mode in async sync; do
  if [ "$mode" = async ]; then
    printf 'import test from "node:test"; test("deliberate hang", async()=>{await new Promise(()=>setInterval(()=>{},1000))})\n' > web/console/hang.test.mjs
  else
    printf 'import test from "node:test"; test("deliberate sync hang", ()=>{while(true){}})\n' > web/console/hang.test.mjs
  fi
  if HANG_STEP=web run_hanging_gate > "$scratch/hang-$mode.log" 2>&1; then exit 1; else [ "$?" = 1 ]; fi
  grep -q 'FAIL  web: affected tests' "$scratch/hang-$mode.log"
  grep -q 'hang.test.mjs' "$scratch/hang-$mode.log"
  grep -q 'test step exceeded\|testTimeoutFailure\|timed out' "$scratch/hang-$mode.log"
  cat "$scratch/hang-$mode.log"
done
cp web/console/hang.test.mjs tools/check-plan.test.mjs
touch tools/check-profile.test.mjs tools/check-scope.test.mjs
if HANG_STEP=tooling run_hanging_gate > "$scratch/hang-tooling.log" 2>&1; then exit 1; else [ "$?" = 1 ]; fi
grep -q 'FAIL  check planning and profiling tests' "$scratch/hang-tooling.log"
grep -q 'check-plan.test.mjs' "$scratch/hang-tooling.log"
cat "$scratch/hang-tooling.log"
# Nested npm runners have an overall bound that also kills their descendants.
printf 'setInterval(()=>{},1000)\n' > web/console/hang.test.mjs
if HANG_NPM=1 run_hanging_gate --full > "$scratch/hang-npm.log" 2>&1; then exit 1; else [ "$?" = 1 ]; fi
grep -q 'test step exceeded' "$scratch/hang-npm.log"
grep -q 'FAIL  web: npm test  (exit 124' "$scratch/hang-npm.log"
grep -q 'console/hang.test.mjs' "$scratch/hang-npm.log"
cat "$scratch/hang-npm.log"
echo 'check modes integration: PASS'
