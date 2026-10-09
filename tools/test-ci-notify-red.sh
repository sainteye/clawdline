#!/bin/sh
# Prove that the CI notice is sent once, on green to red only, and never
# without its URL. gh and curl are stubs; nothing leaves this machine.
set -eu

root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
tmp=$(mktemp -d "${TMPDIR:-/tmp}/clawdline-ci-notify.XXXXXX")
trap 'rm -rf "$tmp"' EXIT HUP INT TERM

printf '#!/bin/sh\ncat "%s/previous.txt"\n' "$tmp" >"$tmp/gh"
printf '#!/bin/sh\necho "$*" >>"%s/sent.txt"\n' "$tmp" >"$tmp/curl"
chmod +x "$tmp/gh" "$tmp/curl"

# run <previous conclusion> <url> <job result>…
run() {
  printf '%s\n' "$1" >"$tmp/previous.txt"; url=$2; shift 2
  : >"$tmp/sent.txt"
  env CLAWDLINE_GH="$tmp/gh" CLAWDLINE_CURL="$tmp/curl" GITHUB_RUN_ID=7 \
    GITHUB_REPOSITORY=example/repo CLAWDLINE_CI_NOTIFY_URL="$url" \
    "$root/tools/ci-notify-red.sh" "$@" >"$tmp/out.txt" 2>&1
}
fail() { echo "ci notify: $1" >&2; cat "$tmp/out.txt" >&2; exit 1; }
sent() { [ -s "$tmp/sent.txt" ] || fail "$1: nothing was sent"; }
unsent() { [ ! -s "$tmp/sent.txt" ] || fail "$1: something was sent"; }

run success https://hook.example.invalid/x success failure success
sent "green to red"
grep -Fq 'Idempotency-Key: ci-red-7' "$tmp/sent.txt" || fail "the key is not the run id"
grep -Fq 'deliver_within_seconds' "$tmp/sent.txt" || fail "the body is not the webhook's"

run failure https://hook.example.invalid/x failure success
unsent "red to red"
run success https://hook.example.invalid/x success success skipped
unsent "green to green"
run success https://hook.example.invalid/x failure cancelled
unsent "a cancelled run"
run success "" failure
unsent "no URL"
grep -Fq '::notice::' "$tmp/out.txt" || fail "a missing URL was not named"

echo "ci notify: sent once on green to red, never otherwise, and a missing URL is a notice"
