#!/bin/bash
# tools/check.sh [--full] [--web] [--profile]
# tools/check.sh --quick <package>... | --plan
#
# Every check AGENTS.md requires before a commit, in one command, ending with
# one line per check and an exit code: 0 every check passed, 1 at least one
# did not, 2 this script was called wrongly, 75 the compile slot or memory was
# not had within heavy's --max-wait and nothing was checked (run it again),
# or 76 when a queued check was handed to a callback (end this turn and wait
# for its notice). A
# check that answers "could not tell" (check-private.sh's 3, for one) is not a
# pass, so it counts as failed.
#
# Roots and children each used to write their own loop over the list in
# AGENTS.md; this is that loop, kept beside the list so the two change
# together. Every check runs even after one fails, so one run says everything
# that is wrong. The dependency preparation step may populate ignored
# web/node_modules in a linked worktree.
#
# The local gate runs changed-package tests, affected web tests, type checks
# and changed-file privacy checks. Caller compilation is optional (--callers).
# Full runtime suites, cross-platform vet/builds, whole-tree privacy and bundles
# run once for integration/CI, not after every local edit. Unknown/tooling inputs
# fail closed to full checks. --full forces integration; --profile retains timings.
# --quick runs explicitly named Go packages for development, never the gate.
# See docs/checks.md.
#
# Ordinary check/test scripts do not reserve the native compile slot. Explicit
# application/native builds must wrap only their build step with tools/heavy.sh.
set -uo pipefail
cd "$(dirname "$0")/.." || exit 2

web=auto
full=no
quick=no
profile=no
callers=no
plan_only=no
packages=()
run_pattern=""
for arg in "$@"; do
  case "$arg" in
    --web) web=yes ;;
    --full) full=yes ;;
    --quick) quick=yes ;;
    --profile) profile=yes ;;
    --callers) callers=yes ;;
    --run=*) run_pattern=${arg#--run=} ;;
    --plan) plan_only=yes ;;
    ./*) packages+=("$arg") ;;
    -h | --help) sed -n '2,21p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "usage: tools/check.sh [--full] [--web] [--profile] | --quick <package>... | --plan" >&2; exit 2 ;;
  esac
done

if [ "$quick" = yes ]; then
  if [ "${#packages[@]}" -eq 0 ] || [ "$full" != no ] || [ "$web" != auto ] || [ "$profile" != no ] || [ "$plan_only" != no ] || [ "$callers" != no ]; then
    echo '--quick requires explicit ./package paths and cannot be combined with gate options' >&2; exit 2
  fi
elif [ "${#packages[@]}" -ne 0 ] || [ -n "$run_pattern" ]; then
  echo 'package arguments require --quick' >&2; exit 2
fi
# The resolver needs TypeScript before choosing scope in a fresh worktree.
# Keep --plan stdout machine-readable and fail preparation before any gate runs.
if [ "$quick" != yes ]; then
  node tools/ensure-web-deps.mjs >&2 || exit 2
fi
if [ "$plan_only" = yes ]; then
  args=(); [ "$full" = no ] && [ "$profile" = no ] || args+=(--full)
  [ "$web" = auto ] || args+=(--web)
  exec node tools/check-scope.mjs ${args[@]+"${args[@]}"}
fi

if [ "$quick" = yes ]; then
  echo 'DEVELOPMENT CHECK ONLY: this does not satisfy the pre-commit gate.'
  run_args=(); [ -z "$run_pattern" ] || run_args+=("-run=$run_pattern")
  exec go test ${run_args[@]+"${run_args[@]}"} ${packages[@]+"${packages[@]}"}
fi

tmp=${TMPDIR:-/tmp}
logs=$(mktemp -d "${tmp%/}/clawdline-check.XXXXXX") || exit 2
scope_args=(); [ "$full" = no ] && [ "$profile" = no ] || scope_args+=(--full)
node tools/check-scope.mjs --out "$logs/scope.json" ${scope_args[@]+"${scope_args[@]}"} || exit 2
scope_full=$(node tools/check-scope.mjs --field "$logs/scope.json" full) || exit 2
case "$scope_full" in true) go_checks=yes; web=yes ;; false) go_checks=no ;; *) echo 'check: invalid scope plan' >&2; exit 2 ;; esac
if [ "$web" = auto ]; then
  scope_web=$(node tools/check-scope.mjs --field "$logs/scope.json" web) || exit 2
  case "$scope_web" in true) web=scoped ;; false) web=no ;; *) exit 2 ;; esac
fi
echo "check plan: $(node tools/check-scope.mjs --field "$logs/scope.json" reason)"
go_tests=() go_compile=() private_paths=() web_tests=()
node tools/check-scope.mjs --field "$logs/scope.json" go.runs --nul > "$logs/go-tests.list" || exit 2
node tools/check-scope.mjs --field "$logs/scope.json" go.compile --nul > "$logs/go-compile.list" || exit 2
node tools/check-scope.mjs --field "$logs/scope.json" paths --nul > "$logs/paths.list" || exit 2
node tools/check-scope.mjs --field "$logs/scope.json" webTests --nul > "$logs/web-tests.list" || exit 2

while IFS= read -r -d '' path; do go_compile+=("$path"); done < "$logs/go-compile.list"
while IFS= read -r -d '' path; do private_paths+=("$path"); done < "$logs/paths.list"
while IFS= read -r -d '' path; do web_tests+=("$PWD/$path"); done < "$logs/web-tests.list"
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

run_web_tests() { (cd web/console && node --experimental-strip-types --test ${web_tests[@]+"${web_tests[@]}"}); }
selected() {
  local value
  value=$(node tools/check-scope.mjs --field "$logs/scope.json" "guards.$1") || exit 2
  case "$value" in true) return 0 ;; false) return 1 ;; *) echo "invalid guard selection" >&2; exit 2 ;; esac
}
run_go_module() {
  local package=$1 pattern=$2 code
  go test -json "-run=$pattern" "$package" > "$logs/module-events.jsonl" 2>&1
  code=$?
  if [ "$code" -ne 0 ]; then cat "$logs/module-events.jsonl"; return "$code"; fi
  node tools/check-profile.mjs "$logs/module-events.jsonl" "$logs/module-summary.json" --require-tests || { cat "$logs/module-events.jsonl"; return 2; }
}
if selected format; then check "gofmt -l internal cmd" gofmt_silent; fi
if [ "$go_checks" = yes ]; then
  check "go vet ./..." go vet ./...
  check "go build ./..." env HEAVY_HANDOFF=0 tools/heavy.sh go build ./...
  check "linux/amd64 build ./cmd/clawdline" env HEAVY_HANDOFF=0 tools/heavy.sh env GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o /dev/null ./cmd/clawdline
  check "windows/amd64 build ./cmd/clawdline" env HEAVY_HANDOFF=0 tools/heavy.sh env GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o /dev/null ./cmd/clawdline
  check "windows go vet ./..." env GOOS=windows go vet ./...
else
  summary+=("SKIP  Go vet and cross-builds  (local scope; integration/CI runs them)")
fi
# Integration runs all packages; local checks select changed packages.
# The explicit uncached guard covers directory walks that Go cannot cache safely.
if [ "$profile" = yes ]; then
  check "go test ./... (profile, uncached)" go test -json -count=1 ./...
  events="$logs/$((${#summary[@]} - 1)).log"
  check "Go test timing report" node tools/check-profile.mjs "$events" "$logs/go-profile.json"
elif [ "$scope_full" = true ]; then
  check "go test ./..." go test ./...
  check "repository copy guards (uncached)" go test -count=1 ./internal/config
else
  while IFS= read -r -d '' package && IFS= read -r -d '' pattern; do
    if [ -n "$pattern" ]; then check "Go module: $package (paired tests)" run_go_module "$package" "$pattern"; else check "Go package: $package" go test "$package"; fi
  done < "$logs/go-tests.list"
  if [ "${#go_compile[@]}" -gt 0 ]; then
    if [ "$callers" = yes ]; then check "caller compilation (runtime tests in integration/CI)" go test '-run=^$' ${go_compile[@]+"${go_compile[@]}"}; else summary+=("SKIP  caller compilation and runtime tests  (${#go_compile[@]} packages listed by --plan; integration/CI or --callers)"); fi
  fi
  check "repository copy guards (uncached)" go test -count=1 ./internal/config
  if selected capacity; then check "capacity registration and copy guards" go test -count=1 ./internal/domain/capacity '-run=^(TestEveryBoundIsRegistered|TestRegisterRowsAnswerTheFourQuestions|TestEveryRowExplainsItselfToAPerson)$'; fi
fi
if selected contract; then check "contract-gen -check" go run ./tools/contract-gen -check; else summary+=("SKIP  contract-gen -check  (inputs unchanged; full CI retains coverage)"); fi
if selected legacy; then check "check-legacy-css.sh" tools/check-legacy-css.sh; else summary+=("SKIP  check-legacy-css.sh  (inputs unchanged; full CI retains coverage)"); fi
check "web dependencies" node tools/ensure-web-deps.mjs
if selected borders; then check "check-rounded-side-borders.mjs" node web/console/tools/check-rounded-side-borders.mjs; else summary+=("SKIP  check-rounded-side-borders.mjs  (inputs unchanged; full CI retains coverage)"); fi
if selected machine; then
  if [ "$scope_full" = true ]; then check "check-machine-words.sh (whole tree)" tools/check-machine-words.sh
  else check "check-machine-words.sh (changed files)" tools/check-machine-words.sh -- ${private_paths[@]+"${private_paths[@]}"}; fi
fi
if selected shell; then check "check-shell-expansions.sh" tools/check-shell-expansions.sh; else summary+=("SKIP  check-shell-expansions.sh  (inputs unchanged; full CI retains coverage)"); fi
if [ "$scope_full" = true ]; then
  check "check-private.sh (whole tree)" tools/check-private.sh
elif [ "${#private_paths[@]}" -gt 0 ]; then
  check "check-private.sh (changed files; whole tree in integration)" tools/check-private.sh -- ${private_paths[@]+"${private_paths[@]}"}
fi
check "check-console-tests.mjs" node tools/check-console-tests.mjs
check "check-mirror-tests.mjs" node tools/check-mirror-tests.mjs
if selected tooling; then
check "test-notify-release-success.sh" sh tools/test-notify-release-success.sh
if [ "$(uname -s)" = Linux ]; then
  check "test-heavy-linux-cache.sh" bash tools/test-heavy-linux-cache.sh
  check "test-linux-dev-build.sh" bash tools/test-linux-dev-build.sh
fi
check "check planning and profiling tests" node --test tools/check-plan.test.mjs tools/check-profile.test.mjs tools/check-scope.test.mjs
check "check modes integration" bash tools/test-check-modes.sh
check "machine-word delta guard" bash tools/test-machine-words-changes.sh
check "heavy handoff capability" bash tools/test-heavy-handoff.sh
if [ "$(uname -s)" = Darwin ]; then
  check "Swift staged app watcher" sh tools/test-staged-app-watcher.sh
  check "Swift native shell language" sh tools/test-shell-language.sh
  check "Swift pairing permission before the code" sh tools/test-shell-pairing-permission.sh
fi
fi
# The release evidence checks (docs/security/release-evidence.md) run in
# every full check and whenever their scripts or the workflows that call them
# change; nothing else exercises their failure cases before a release.
release_evidence=$scope_full
for path in ${private_paths[@]+"${private_paths[@]}"}; do
  case "$path" in tools/release/*|tools/check.sh|.github/workflows/release.yml|.github/workflows/dmg.yml|.github/workflows/tmux.yml) release_evidence=true ;; esac
done
if [ "$release_evidence" = true ]; then check "release evidence (supply-chain.test.sh)" bash tools/release/supply-chain.test.sh
if [ "$(uname -s)" = Darwin ]; then
  check "DMG install harness (synthetic images)" bash tools/release/test-dmg-install-smoke.sh
fi
else summary+=("SKIP  release evidence  (tools/release and the release workflows unchanged; full checks run it)"); fi
if [ "$web" = yes ]; then
  check "web: npm run check" sh -c 'cd web && npm run deps && npm run check'
  check "web: npm run build" sh -c 'cd web && npm run build'
  check "web: npm test" sh -c 'cd web && npm test'
elif [ "$web" = scoped ]; then
  check "web: incremental type-check" sh -c 'cd web && node node_modules/typescript/bin/tsc --noEmit -p console --incremental --tsBuildInfoFile .clawdline-check.tsbuildinfo'
  check "web: source catalog" node web/console/tools/check-source-catalog.mjs
  check "web: catalogs" node web/console/tools/check-catalogs.mjs
  check "web: semantic risk keys" node web/console/tools/semantic-risk-keys.mjs --check
  check "web: semantic screen groups" node web/console/tools/group-semantic-keys.mjs --check
  check "web: refusal copy" node web/console/tools/check-refusal-text.mjs
  if [ "${#web_tests[@]}" -gt 0 ]; then check "web: affected tests" run_web_tests; else summary+=("SKIP  web runtime tests  (no affected test imports; integration/CI runs the full suite)"); fi
  summary+=("SKIP  web bundle  (integration, CI and release)")
else
  summary+=("SKIP  web checks  (nothing under web/ changed; --web runs them)")
fi

echo
printf '%s\n' ${summary[@]+"${summary[@]}"}
if [ "$failed" -eq 0 ]; then
  if [ "$profile" = yes ]; then echo "check: profile logs retained at $logs"; else rm -rf "$logs"; fi
  echo "check: every check passed"
else
  echo "check: FAILED; each failed check's whole output is in $logs"
fi
exit "$failed"
