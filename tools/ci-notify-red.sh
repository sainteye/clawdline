#!/bin/sh
# tools/ci-notify-red.sh <job result>… — tell the person once when main turns red.
#
# The last CI job runs this after every push to main with the results of all
# the other jobs. When this run failed and the run before it on main
# succeeded, it POSTs once to the URL in CLAWDLINE_CI_NOTIFY_URL; red after
# red, green after anything, and a cancelled run send nothing, so a broken main
# is said once, not on every push. On 2026-10-07 main went red and nobody was
# told for two days.
#
# "The run before it" is the completed, not cancelled or skipped, run of this
# workflow on main with the highest run number below this one's. Runs can
# finish out of order, so the most recently finished run is not necessarily
# the predecessor; and when a later run on main has already completed, this
# run (a re-run of an older push) no longer says what main is, and sends
# nothing.
#
# Two kinds of receiver:
#
# - With CLAWDLINE_CI_NOTIFY_TOKEN set, the URL is a Bot routine webhook
#   (`POST /hooks/routines/{id}`, docs/project-assistants-routine-events.md).
#   The key goes in `Authorization: Bearer`, the body is the event
#   {"source":"github","event":"ci.red","repo":…,"branch":"main",
#   "workflow":"CI","conclusion":"failure","runId":"<id>","runUrl":…}, and
#   the URL must be https without a query, user or the key in it.
# - Without it, the body is what a Clawdline Cloud schedule webhook takes
#   ({"deliver_within_seconds"}); that URL is itself the credential.
#
# Both are repository secrets, never in this public repository. The URL and
# the key reach curl on its standard input, not its arguments, and neither is
# printed. Idempotency-Key is ci-red-<run id>, so a re-run of the job, or a
# retry after a busy or uncertain answer, does not send twice. Without a URL
# it prints a notice and succeeds.
#
# Environment: GH_TOKEN, GITHUB_REPOSITORY, GITHUB_RUN_ID, GITHUB_RUN_NUMBER,
# GITHUB_REF, GITHUB_SERVER_URL (as Actions sets them),
# CLAWDLINE_CI_NOTIFY_URL, CLAWDLINE_CI_NOTIFY_TOKEN. CLAWDLINE_GH,
# CLAWDLINE_CURL, CLAWDLINE_JQ and CLAWDLINE_SLEEP name the binaries (the test
# stubs them).
# Exit: 0 nothing to send, sent, or no URL; 1 the send, the read or the
# configuration failed.
set -u
gh_bin=${CLAWDLINE_GH:-gh}
curl_bin=${CLAWDLINE_CURL:-curl}
jq_bin=${CLAWDLINE_JQ:-jq}
sleep_bin=${CLAWDLINE_SLEEP:-sleep}
run_id=${GITHUB_RUN_ID:?}
repo=${GITHUB_REPOSITORY:?}
branch=main

case "$run_id" in '' | *[!0-9]*) echo "::error::GITHUB_RUN_ID is not a number"; exit 1 ;; esac
case "$repo" in
  */*/* | /* | */ | *[!A-Za-z0-9._/-]*) echo "::error::GITHUB_REPOSITORY is not owner/name"; exit 1 ;;
  */*) ;;
  *) echo "::error::GITHUB_REPOSITORY is not owner/name"; exit 1 ;;
esac
# Only a public github.com server yields a link; anything else is left out
# rather than forwarded.
run_url=
if [ "${GITHUB_SERVER_URL:-https://github.com}" = https://github.com ]; then
  run_url="https://github.com/$repo/actions/runs/$run_id"
fi

if [ "${GITHUB_REF:-refs/heads/$branch}" != "refs/heads/$branch" ]; then
  echo "Nothing to send: this run is not on $branch."
  exit 0
fi

this=success
for result in "$@"; do
  case "$result" in
    success | skipped) ;;
    cancelled) this=cancelled; break ;;
    *) this=failure ;;
  esac
done
echo "This run ($run_id): $this"
[ "$this" = failure ] || { echo "Nothing to send: this run did not fail."; exit 0; }

# Order by the run number when Actions gives one, else by the run id.
order=number current=${GITHUB_RUN_NUMBER:-}
if [ -z "$current" ]; then order=databaseId current=$run_id; fi
case "$current" in *[!0-9]*) echo "::error::GITHUB_RUN_NUMBER is not a number"; exit 1 ;; esac

errors=$(mktemp "${TMPDIR:-/tmp}/ci-notify-red.XXXXXX") || exit 1
reply=$(mktemp "${TMPDIR:-/tmp}/ci-notify-red.XXXXXX") || exit 1
trap 'rm -f "$errors" "$reply"' EXIT HUP INT TERM

if ! runs=$("$gh_bin" run list --repo "$repo" --branch "$branch" --workflow ci.yml --status completed -L 50 \
    --json databaseId,number,conclusion 2>"$errors"); then
  echo "::error::could not read the previous CI run on $branch: $(head -c 300 "$errors")"
  exit 1
fi
if ! verdict=$(printf '%s' "$runs" | "$jq_bin" -r --arg key "$order" --argjson cur "$current" '
    [.[] | select(.conclusion != "cancelled" and .conclusion != "skipped" and .conclusion != null and .conclusion != "")] as $done
    | ([$done[] | select(.[$key] > $cur)] | length) as $newer
    | ([$done[] | select(.[$key] < $cur)] | max_by(.[$key]) | .conclusion // "none") as $previous
    | "\($newer) \($previous)"' 2>"$errors"); then
  echo "::error::could not read the previous CI run on $branch: $(head -c 300 "$errors")"
  exit 1
fi
newer=${verdict%% *} previous=${verdict#* }
echo "Previous completed run on $branch: $previous"
if [ "$newer" != 0 ]; then
  echo "Nothing to send: $newer later run(s) on $branch already completed, so this run no longer says what $branch is."
  exit 0
fi
[ "$previous" = success ] || { echo "Nothing to send: $branch was already $previous before this run."; exit 0; }

where=${run_url:-run $run_id}
url=${CLAWDLINE_CI_NOTIFY_URL:-}
token=${CLAWDLINE_CI_NOTIFY_TOKEN:-}
if [ -z "$url" ]; then
  echo "::notice::$branch went from green to red in $where, and CLAWDLINE_CI_NOTIFY_URL is not set, so nobody was told."
  exit 0
fi
# The URL and the key go into a curl configuration line, so neither may
# contain a quote, a backslash, a space or a control character. Neither is
# printed, even when it is refused.
case "$url" in
  https://*) ;;
  *) echo "::error::CLAWDLINE_CI_NOTIFY_URL is not an https URL; nothing was sent."; exit 1 ;;
esac
if [ -n "$(printf '%s' "$url" | LC_ALL=C tr -d '\041\043-\133\135-\176')" ]; then
  echo "::error::CLAWDLINE_CI_NOTIFY_URL has characters a URL does not; nothing was sent."
  exit 1
fi
if [ -n "$token" ]; then
  if [ "${#token}" -lt 32 ] || [ "${#token}" -gt 512 ] || [ -n "$(printf '%s' "$token" | LC_ALL=C tr -d 'A-Za-z0-9._~+/=-')" ]; then
    echo "::error::CLAWDLINE_CI_NOTIFY_TOKEN is not a webhook key (32 to 512 letters, digits or ._~+/=-); nothing was sent."
    exit 1
  fi
  hostpart=${url#https://}; hostpart=${hostpart%%/*}
  case "$url" in
    *'?'* | *'#'*) echo "::error::CLAWDLINE_CI_NOTIFY_URL has a query; a Bot webhook URL has none. Nothing was sent."; exit 1 ;;
    *"$token"*) echo "::error::CLAWDLINE_CI_NOTIFY_URL contains the key; keep the key only in CLAWDLINE_CI_NOTIFY_TOKEN. Nothing was sent."; exit 1 ;;
  esac
  case "$hostpart" in
    '' | *@*) echo "::error::CLAWDLINE_CI_NOTIFY_URL has no host, or a user in it; nothing was sent."; exit 1 ;;
  esac
  body="{\"source\":\"github\",\"event\":\"ci.red\",\"repo\":\"$repo\",\"branch\":\"$branch\",\"workflow\":\"CI\",\"conclusion\":\"failure\",\"runId\":\"$run_id\""
  [ -z "$run_url" ] || body="$body,\"runUrl\":\"$run_url\""
  body="$body}"
  kind="Bot webhook"
else
  body='{"deliver_within_seconds":3600}'
  kind="schedule webhook"
fi

# send — one POST; sets status (HTTP code, 000 when nothing came back).
send() {
  : >"$reply"
  status=$({
    printf 'url = "%s"\n' "$url"
    [ -z "$token" ] || printf 'header = "Authorization: Bearer %s"\n' "$token"
  } | "$curl_bin" -sS --config - --proto =https --max-redirs 0 \
      --connect-timeout 10 --max-time 30 -X POST \
      -H 'Content-Type: application/json' \
      -H "Idempotency-Key: ci-red-$run_id" \
      ${run_url:+-H "X-Clawdline-CI-Run: $run_url"} \
      --data "$body" -o "$reply" -w '%{http_code}' 2>/dev/null)
  case "$status" in [0-9][0-9][0-9]) ;; *) status=000 ;; esac
}

# A busy routine, an uncertain outcome, a server error or no answer is
# retried with the same Idempotency-Key, at most four times in all.
echo "$branch went from green to red; sending one notification to the $kind."
attempt=1
while :; do
  send
  code=$(LC_ALL=C sed -n 's/.*"code"[[:space:]]*:[[:space:]]*"\([a-z_]\{1,64\}\)".*/\1/p' "$reply" | head -n 1)
  case "$status" in
    2??)
      if LC_ALL=C grep -q '"ignored"[[:space:]]*:[[:space:]]*true' "$reply"; then
        echo "::warning::the $kind accepted the notification but ignored it: its routine's event filter does not match ci.red on $repo $branch."
      else
        echo "Notification accepted."
      fi
      exit 0 ;;
    409) case "$code" in webhook_busy | webhook_outcome_unknown) retry=yes ;; *) retry=no ;; esac ;;
    000 | 5??) retry=yes ;;
    *) retry=no ;;
  esac
  if [ "$retry" = no ] || [ "$attempt" -ge 4 ]; then
    # The URL and the key are credentials: say that it failed, not where.
    echo "::error::the notification was not accepted after $attempt attempt(s): HTTP $status${code:+ $code}."
    exit 1
  fi
  echo "The $kind answered HTTP $status${code:+ $code}; retrying with the same Idempotency-Key."
  "$sleep_bin" $((attempt * 3))
  attempt=$((attempt + 1))
done
