#!/bin/sh
# Prove that the CI notice is sent once, on green to red on main only, to
# either receiver, and never without its URL; that the Bot webhook gets the
# exact event its routine matches with the key only in a bearer header; and
# that neither the URL nor the key is ever an argument or printed. gh, curl
# and sleep are stubs (jq is real); nothing leaves this machine.
set -eu

root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
tmp=$(mktemp -d "${TMPDIR:-/tmp}/clawdline-ci-notify.XXXXXX")
trap 'rm -rf "$tmp"' EXIT HUP INT TERM

url=https://hook.example.invalid/hooks/routines/r1
key=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef

printf '#!/bin/sh\necho "$*" >"%s/gh-args.txt"\ncat "%s/runs.json"\n' "$tmp" "$tmp" >"$tmp/gh"
# The curl stub records its arguments and its standard input (the
# configuration), one call per file, writes the next canned reply to -o and
# prints its status for -w.
cat >"$tmp/curl" <<EOF
#!/bin/sh
n=\$(( \$(cat "$tmp/calls" 2>/dev/null || echo 0) + 1 )); echo \$n >"$tmp/calls"
printf '%s\n' "\$@" >"$tmp/args.\$n"
cat >"$tmp/stdin.\$n"
out=
while [ \$# -gt 0 ]; do [ "\$1" = -o ] && out=\$2; shift; done
line=\$(sed -n "\${n}p" "$tmp/replies.txt"); [ -n "\$line" ] || line=\$(tail -n 1 "$tmp/replies.txt")
printf '%s' "\${line#* }" >"\$out"
printf '%s' "\${line%% *}"
EOF
printf '#!/bin/sh\necho "$*" >>"%s/slept.txt"\n' "$tmp" >"$tmp/sleep"
chmod +x "$tmp/gh" "$tmp/curl" "$tmp/sleep"

# runs <json array of completed runs> — what gh run list answers.
runs() { printf '%s' "$1" >"$tmp/runs.json"; }
# replies <status body>… — what successive POSTs answer.
replies() { printf '%s\n' "$@" >"$tmp/replies.txt"; }
green='[{"databaseId":6,"number":41,"conclusion":"success"}]'

# run <url> <key> <job result>… (GITHUB_RUN_ID 7, run number 42, ref main
# unless REF is set)
run() {
  u=$1; k=$2; shift 2
  rm -f "$tmp"/args.* "$tmp"/stdin.* "$tmp/calls" "$tmp/slept.txt"
  status=0
  env CLAWDLINE_GH="$tmp/gh" CLAWDLINE_CURL="$tmp/curl" CLAWDLINE_SLEEP="$tmp/sleep" \
    GITHUB_RUN_ID=7 GITHUB_RUN_NUMBER=42 GITHUB_REF="${REF:-refs/heads/main}" \
    GITHUB_REPOSITORY=example/repo GITHUB_SERVER_URL=https://github.com \
    CLAWDLINE_CI_NOTIFY_URL="$u" CLAWDLINE_CI_NOTIFY_TOKEN="$k" \
    "$root/tools/ci-notify-red.sh" "$@" >"$tmp/out.txt" 2>&1 || status=$?
}
fail() { echo "ci notify: $1" >&2; cat "$tmp/out.txt" >&2; exit 1; }
calls() { cat "$tmp/calls" 2>/dev/null || echo 0; }
sent() { [ "$(calls)" = "${2:-1}" ] || fail "$1: expected ${2:-1} send(s), got $(calls)"; }
unsent() { [ "$(calls)" = 0 ] || fail "$1: something was sent"; [ "$status" = 0 ] || fail "$1: exit $status"; }
# Neither secret is an argument or in the output, whatever happened.
sealed() {
  for f in "$tmp"/args.* "$tmp/out.txt"; do
    [ -e "$f" ] || continue
    ! grep -Fq "$key" "$f" || fail "$1: the key is in $(basename "$f")"
    ! grep -Fq "hook.example.invalid" "$f" || fail "$1: the URL is in $(basename "$f")"
  done
}
arg() { grep -Fxq -- "$2" "$tmp/args.${3:-1}" || fail "$1: no argument $2"; }

# The Bot webhook: green to red sends the exact event, bearer on stdin.
runs "$green"; replies '200 {"run_id":"x"}'
run "$url" "$key" success failure success success
[ "$status" = 0 ] || fail "bot green to red: exit $status"
sent "bot green to red"; sealed "bot green to red"
arg "bot" 'Idempotency-Key: ci-red-7'
arg "bot" '{"source":"github","event":"ci.red","repo":"example/repo","branch":"main","workflow":"CI","conclusion":"failure","runId":"7","runUrl":"https://github.com/example/repo/actions/runs/7"}'
arg "bot" '--config'; arg "bot" '-'; arg "bot" '--proto'; arg "bot" '=https'; arg "bot" '--max-time'
grep -Fxq "url = \"$url\"" "$tmp/stdin.1" || fail "bot: the URL is not in curl's configuration"
grep -Fxq "header = \"Authorization: Bearer $key\"" "$tmp/stdin.1" || fail "bot: no bearer header"
grep -Fq '"runId":"7"' "$tmp/args.1" || fail "bot: runId is not a string"
grep -Fq 'Notification accepted.' "$tmp/out.txt" || fail "bot: acceptance not said"
grep -Fq 'number' "$tmp/gh-args.txt" || fail "bot: the run number was not read"

# Busy, then accepted: retried with the same key, after a pause.
replies '409 {"code":"webhook_busy"}' '200 {"run_id":"x"}'
run "$url" "$key" failure
[ "$status" = 0 ] || fail "busy then accepted: exit $status"
sent "busy then accepted" 2; sealed "busy"
arg "busy retry" 'Idempotency-Key: ci-red-7' 2
[ -s "$tmp/slept.txt" ] || fail "busy: retried without a pause"

# Refused: one attempt, exit 1, the code named, no secret printed.
replies '401 {"code":"webhook_unauthorized"}'
run "$url" "$key" failure
[ "$status" = 1 ] || fail "refused: exit $status"
sent "refused"; sealed "refused"
grep -Fq 'HTTP 401 webhook_unauthorized' "$tmp/out.txt" || fail "refused: the code was not named"

# Always uncertain: four attempts, then exit 1.
replies '000 '
run "$url" "$key" failure
[ "$status" = 1 ] || fail "no answer: exit $status"
sent "no answer" 4; sealed "no answer"

# Accepted but ignored: the routine filter does not match; a warning.
replies '200 {"ignored":true}'
run "$url" "$key" failure
[ "$status" = 0 ] || fail "ignored: exit $status"
grep -Fq '::warning::' "$tmp/out.txt" || fail "an ignored event was not a warning"

# The legacy schedule URL alone: the schedule body, no Authorization.
replies '200 {}'
run "$url" "" success failure success
sent "schedule green to red"; sealed "schedule"
arg "schedule" 'Idempotency-Key: ci-red-7'
arg "schedule" '{"deliver_within_seconds":3600}'
! grep -Fq 'Authorization' "$tmp/stdin.1" || fail "schedule: a bearer was sent without a key"

# Nothing is sent but on green to red on main, to either receiver.
for k in "$key" ""; do
  runs '[{"databaseId":6,"number":41,"conclusion":"failure"}]'
  run "$url" "$k" failure success; unsent "red to red"
  runs "$green"
  run "$url" "$k" success success skipped; unsent "green to green"
  run "$url" "$k" failure cancelled; unsent "a cancelled run"
  REF=refs/heads/feature; run "$url" "$k" failure; REF=; unsent "a branch other than main"
  # Out of order: run 43 finished red before this run 42, and 40 was green.
  # 41 is the predecessor; the most recently finished run is not.
  runs '[{"databaseId":9,"number":43,"conclusion":"failure"},{"databaseId":6,"number":41,"conclusion":"success"}]'
  run "$url" "$k" failure; unsent "a later run already completed"
  # Cancelled and skipped predecessors are passed over to the green before.
  runs '[{"databaseId":6,"number":41,"conclusion":"cancelled"},{"databaseId":5,"number":40,"conclusion":"skipped"},{"databaseId":4,"number":39,"conclusion":"failure"},{"databaseId":3,"number":38,"conclusion":"success"}]'
  run "$url" "$k" failure; unsent "red behind a cancelled run"
  # A re-run of this same run (number 42) is not its own predecessor.
  runs '[{"databaseId":7,"number":42,"conclusion":"success"},{"databaseId":4,"number":41,"conclusion":"failure"}]'
  run "$url" "$k" failure; unsent "this run's earlier attempt"
done
runs '[{"databaseId":6,"number":41,"conclusion":"cancelled"},{"databaseId":3,"number":40,"conclusion":"success"}]'
replies '200 {}'
run "$url" "$key" failure; sent "green behind a cancelled run"

# No URL: a notice, nothing sent.
runs "$green"
run "" "$key" failure; unsent "no URL"
grep -Fq '::notice::' "$tmp/out.txt" || fail "a missing URL was not named"

# A bad configuration is refused before anything is sent, and not echoed.
for bad in "http://hook.example.invalid/hooks/routines/r1" \
           "https://hook.example.invalid/hooks/routines/r1?key=$key" \
           "https://u@hook.example.invalid/hooks/routines/r1" \
           "https://hook.example.invalid/hooks/\"r1"; do
  run "$bad" "$key" failure
  [ "$status" = 1 ] || fail "bad URL accepted: exit $status"
  [ "$(calls)" = 0 ] || fail "bad URL: something was sent"
  sealed "bad URL"
done
run "$url" 'short' failure
[ "$status" = 1 ] && [ "$(calls)" = 0 ] || fail "a short key was used"
run "$url" "$key\"x" failure
[ "$status" = 1 ] && [ "$(calls)" = 0 ] || fail "a key with a quote was used"
! grep -Fq "$key" "$tmp/out.txt" || fail "a refused key was printed"

echo "ci notify: green to red on main sends once to the Bot or schedule webhook, never otherwise; secrets stay off arguments and output"
