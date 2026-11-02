# Provider quota footer

The console shares one account-quota footer across its pages. **Settings → Browser** has independent
**Show quota bar on desktop** and **Show quota bar in the mobile Session list** switches. Both
default on and are saved separately in that browser's local storage. Existing saved visibility
is the fallback for each switch until it is explicitly set. Turning a layout off unmounts its
footer and stops its reads; it does not change dispatch policy. Providers without a usable numeric
window are hidden. A real zero is a usable reading. The footer is available from API level 28.

The footer reads the currently selected machine, including in the all-machines view; its detail
names that source machine. It never adds percentages across machines, projects or Sessions.
Switching the Cloud reader discards the previous machine's footer state. Desktop windows show a
progress bar, percentage and reset countdown, with middle dots between one provider's windows. Desktop keeps all windows
together; readings that exceed a provider's available width end with an ellipsis instead of
overlapping the next provider. The local footer starts with the
provider icons, while Cloud may identify the selected machine by name. Every known window remains available in the details dialog. Below 900 CSS pixels,
the mobile layout shows each available provider's compact window (defaulting to all
models' 7d window for Gemini rather than model-specific Argon, and the tightest window
for other providers), truncating long readings with an ellipsis, only in the Session
list. Session conversations and other pages unmount the footer and stop its reads, leaving no
reserved bottom space. The mobile list reserves the full measured footer height, including its
home-indicator clearance from `--indicator` (the console does not use `viewport-fit=cover`, so
`env(safe-area-inset-bottom)` alone can be zero on iOS). Changing viewport size does not change
the saved browser preference.

## Sources and ownership

| Provider | Read source | Login ownership |
| --- | --- | --- |
| Claude Code | Existing `limits.Reader`: status-line `rate-limits.json`; when that has no fresh window, Claude Code's own `get_usage` control request | No credential access or login change: Claude Code reads and refreshes its own sign-in |
| Codex | Existing `limits.Reader`: quota keys in recent rollouts | No credential access or login change |
| Gemini | Configured native ACP profile: fixed `/usage` command, five-hour and weekly Gemini windows | Reuses the selected personal OAuth profile with AI Credits off; does not borrow user conversations |
| Legacy Gemini CLI (without native configuration) | Existing `~/.gemini/oauth_creds.json`; `loadCodeAssist`, then `retrieveUserQuota` | Reads the saved access token; login remains owned by that CLI |
| Grok CLI | Existing `$GROK_HOME/auth.json` or `~/.grok/auth.json`; CLI billing credits, optionally its monthly view | Uses the xAI issuer entry; an expired token requires running Grok CLI |

These are quota metadata reads, never model generations or chat prompts. The implementation does
not copy CLI credentials, generate model output, redeem credits or switch accounts.
The native Gemini reader opens and closes a temporary ACP connection for the fixed
metadata command, with no user Session slot or transcript entry. Its existing
process owner observes daemon shutdown and cleanup failures. The native report may list a separate
**Gemini 4 Argon** weekly bucket alongside the general Gemini group. Both the
Session limits and account footer retain this bucket as **Argon 7d**, with its own
percentage and reset time. A healthy general Gemini balance does not imply Argon
quota remains; a zero remaining Argon balance is 100% used. Unreadable or duplicate
recognized bucket values remain unknown rather than silently dropping the constraint. Provider responses and
credential files are capped at 1 MiB per read. Only numeric quota windows, timestamps, source names
and typed problem codes cross the local API and encrypted Cloud transport; tokens, emails, account
identifiers, provider response bodies and filesystem paths do not.

`GET /v1/provider-quotas`, carried over Cloud as `provider-quotas`, returns one machine snapshot.
It is separate from `/v1/orchestrator/assistants`: adding quota-only providers does not imply they
can be dispatched. The browser polls while visible, normally every 30 seconds. Gemini and Grok
refresh asynchronously, one in flight per provider, with a 15-minute minimum interval shared by
all browsers on that daemon. A complete refresh has a ten-second deadline, no automatic retries,
and respects a longer `Retry-After`. The native reader replaces the legacy credential/API path when configured.
The Refresh button rereads this cache and the local files;
it does not bypass provider cooldowns.

Failed refreshes keep the last successful windows marked outdated. A changed credential identity
clears the previous account's cached reading, and an in-flight answer for it cannot publish into
the new identity. Missing login files clear that provider. A missing numeric response is unknown,
not zero. Grok's omitted scalar zero is inferred only with an explicitly weekly period and matching,
valid billing dates; a computable monthly budget stays monthly. Quota percentages that have passed
their reset time remain marked outdated until the next usable observation.

## Claude without a status line

Claude Code passes plan windows only to a configured `statusLine.command`, so a machine with no
status line used to show no Claude reading at all. When `rate-limits.json` has no fresh window
(missing, empty, every window reset, or too old), a footer read asks Claude Code itself: the
daemon starts the installed `claude` in print mode with stream-json input and output, writes one
experimental `get_usage` control request with `skip_behaviors`, reads the one matching reply, and
stops the process. It never sends a prompt, so no model request is made and no quota is spent.

The process runs in an empty scratch directory with `--setting-sources ""` (no user, project or
local settings, so none of the person's hooks or plugins run), `--strict-mcp-config` (no MCP
servers) and `--no-session-persistence` (no transcript). `--bare` is not used because it also
skips the keychain where Claude Code keeps its sign-in. Clawdline never reads the token.

The probe shares the provider readers' single flight, ten-second deadline and 15-minute floor.
Doctor inspection never starts it. A fresh status-line reading is never replaced or probed. The
row then names its source as the native CLI. Unavailable plan windows (an API key, Bedrock,
Vertex or a signed-out CLI) report `no_reading`; an older Claude Code without `get_usage`
reports `unavailable`. Only the 5h and 7d windows are carried. The footer is the only consumer;
dispatch availability and Session info still read the status line alone.

Measured on 2026-11-02 with Claude Code 2.1.316: an answer in 0.06–0.65 s and about 2 s of
process lifetime, total cost 0 and API time 0 ms. Clawdline listed no additional Session during a
15-second hold, and Claude Code wrote no transcript. Claude Code did update two of its own cache
fields in `~/.claude.json` (`cachedGrowthBookFeaturesAt` and `cachedUsageUtilization`). A control
run without the probe changed nothing in the same time. The control request is labelled
experimental by Anthropic's Agent SDK and may change; a changed shape degrades to `unavailable`.

## Doctor evidence

[Doctor](doctor.md) records bounded local quota transitions and exports a sanitized
report on explicit request. It does not change these sources or refresh policies.
Missing numeric providers remain hidden in the footer, but remain visible in Doctor.

## Verification

Synthetic transport tests cover both metadata sequences, single-flight requests, cooldowns,
unauthorized responses, stale retention, account changes, token expiry, response size and real zero
versus absent fields. The browser model tests cover default-on storage, hidden empty providers,
compact priorities and expired windows. HTTP and Cloud tests cover the read-only contract and the
machine operation. Manual browser acceptance uses isolated synthetic fixtures, never a user's
Gemini or Grok credentials. Live provider compatibility requires a signed-in account for that
provider; a synthetic fixture is not a live-account receipt.

The reference investigation used [Orca commit ad4952e](https://github.com/stablyai/orca/tree/ad4952e1dabcfff0871fa7a227cb00f0ac5d269b/src/main/rate-limits).
This implementation reuses Clawdline's existing Claude/Codex observations and implements its own
Go metadata readers; it does not import Orca's account-management or agent-control behavior.
Orca reads Claude's OAuth token and calls the undocumented usage endpoint directly. The Claude
fallback above instead follows [t3code commit 47f2c8d](https://github.com/pingdotgg/t3code/tree/47f2c8d2/apps/server/src/provider),
which asks Claude Code over its control protocol and leaves the credential with Claude Code.
