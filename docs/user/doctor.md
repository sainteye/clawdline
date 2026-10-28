# Check and report a problem

Open **Settings → Status → Doctor → Run check**. Doctor shows Clawdline's running
version, Claude Code and Codex CLI versions, quota readings and recent recorded
failures. No model message is sent and no sign-in token is collected.

Expand **Preview the full report** to review every collected field. Download the
JSON report if you want to keep it or submit it from another device. In Cloud,
optionally describe what happened, attach screenshots, review the private-submission consent and press
**Submit private report**. Follow the returned link to read replies. Do not include
passwords, API keys or private conversations in the description.

If the affected machine is unavailable, sign in at
[Doctor report upload](https://app.clawdline.com/help/doctor) and choose a previously
downloaded Doctor report. Loading a file does not upload it: submission still
requires your explicit action and consent. Reports and descriptions are stored in
Cloud for you, Clawdline support and its answering Agent; they are not public
community posts.

If submission is not confirmed, use **Check the same submission again**. It keeps
the original report and request key after reloading, rather than submitting a new
report. Use the same account that started the submission.

For a terminal-only machine, `clawdline doctor --json` prints the same report from
the running daemon. Redirect its output to a JSON file for later upload. Plain
`clawdline doctor` retains the basic local installation check.

Missing quota data is unknown, not zero. Claude's quota depends on a status-line
cache; Doctor does not inspect its login credential or observe the upstream usage
request. Recent history covers only times Clawdline read quota data, retains up to
24 changes for three days and reports gaps. A report may help locate the failing
stage without proving the original cause.
