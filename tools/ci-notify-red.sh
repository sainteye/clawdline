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
# The URL is a repository secret, never in this public repository. The body
# is what a Clawdline Cloud schedule webhook takes ({"deliver_within_seconds"}),
# so the URL can be one bound to a trigger-only schedule on the person's
# machine whose task tells their phone; any endpoint that accepts a POST
# works. Idempotency-Key is this run's id, so a re-run of the job does not
# send twice. Without the secret it prints a notice and succeeds.
#
# Environment: GH_TOKEN, GITHUB_REPOSITORY, GITHUB_RUN_ID, GITHUB_SERVER_URL
# (as Actions sets them), CLAWDLINE_CI_NOTIFY_URL. CLAWDLINE_GH and
# CLAWDLINE_CURL name the binaries (the test stubs them).
# Exit: 0 nothing to send, sent, or no URL; 1 the send or the read failed.
set -u
gh_bin=${CLAWDLINE_GH:-gh}
curl_bin=${CLAWDLINE_CURL:-curl}
run_id=${GITHUB_RUN_ID:?}
repo=${GITHUB_REPOSITORY:?}
run_url="${GITHUB_SERVER_URL:-https://github.com}/$repo/actions/runs/$run_id"

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

if ! previous=$("$gh_bin" run list --repo "$repo" --branch main --workflow ci.yml --status completed -L 20 \
    --json databaseId,conclusion \
    --jq ".[] | select(.databaseId != $run_id and .conclusion != \"cancelled\" and .conclusion != \"skipped\") | .conclusion" 2>&1); then
  echo "::error::could not read the previous CI run on main: $previous"
  exit 1
fi
previous=$(printf '%s\n' "$previous" | sed -n 1p)
echo "Previous completed run on main: ${previous:-none}"
[ "$previous" = success ] || { echo "Nothing to send: main was already ${previous:-unknown} before this run."; exit 0; }

if [ -z "${CLAWDLINE_CI_NOTIFY_URL:-}" ]; then
  echo "::notice::main went from green to red in $run_url, and CLAWDLINE_CI_NOTIFY_URL is not set, so nobody was told."
  exit 0
fi
echo "main went from green to red; sending one notification."
if ! answer=$("$curl_bin" --fail-with-body -sS -X POST "$CLAWDLINE_CI_NOTIFY_URL" \
    -H 'Content-Type: application/json' \
    -H "Idempotency-Key: ci-red-$run_id" \
    -H "X-Clawdline-CI-Run: $run_url" \
    --data '{"deliver_within_seconds":3600}' 2>&1); then
  # The URL is a credential: say that it failed, not where it points.
  echo "::error::the notification was not accepted: $(printf '%s' "$answer" | head -c 300)"
  exit 1
fi
echo "Notification accepted."
