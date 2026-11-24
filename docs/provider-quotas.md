# Provider quota footer

The console shares one account-quota footer across its pages. **Settings → General** has independent
**Show quota bar on desktop** and **Show quota bar in the mobile Session list** switches. Both
default on and are saved separately in that browser's local storage. Existing saved visibility
is the fallback for each switch until it is explicitly set. Turning a layout off unmounts its
footer and stops its reads; it does not change dispatch policy. Providers without a usable numeric
window are hidden. A real zero is a usable reading. The footer is available from API level 28.

The footer shell remains visible while its first read is pending, when no provider has a usable
reading, and after a failed read. It shows the corresponding loading, empty, unavailable or offline
message, keeps its measured bottom space, and retains the Refresh action. Readings and their
absence do not own shell visibility. A failed refresh with a previous reading still shows that
reading as outdated. The footer's `data-reading-state` distinguishes loading, empty, failed,
offline and ready states for inspection without exposing response content.

Bots saved in the footer and the view launcher follow the footer's visibility. They never fall
back to floating when the bar is hidden by a browser preference, an unsupported machine or mobile
navigation. A Bot explicitly dragged out of the footer retains its independent floating placement.
The view manager remains reachable from the sidebar when the footer is hidden. Dock availability
is owned by the mounted footer, independently of whether an overlay temporarily removes its icons.

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
| Claude Code | Existing `limits.Reader`: status-line `rate-limits.json` and Claude Code's own `get_usage` control request | No credential access or login change: Claude Code reads and refreshes its own sign-in |
| Codex | Existing `limits.Reader`: quota keys in recent rollouts | No credential access or login change |
| Gemini | Public CLI: fixed `--settings '{"useG1Credits":false}' --print /usage`; legacy ACP 1.4.1: fixed `/usage` command | Native CLI owns the default personal sign-in; Clawdline checks its explicit personal/no-Credits policy and does not borrow user conversations |
| Legacy Gemini CLI (without native configuration) | Existing `~/.gemini/oauth_creds.json`; `loadCodeAssist`, then `retrieveUserQuota` | Reads the saved access token; login remains owned by that CLI |
| Grok CLI | Existing `$GROK_HOME/auth.json` or `~/.grok/auth.json`; CLI billing credits, optionally its monthly view | Uses the xAI issuer entry; an expired token requires running Grok CLI |

These are quota metadata reads, never model generations or chat prompts. The implementation does
not copy CLI credentials, generate model output, redeem credits or switch accounts.
The public Gemini CLI answers the fixed print-mode command directly. Its
tab-separated numeric report is parsed without creating an ACP Session or sending
`session/prompt`. Only the default public CLI configurations are supported;
custom account/profile arguments return unknown rather than querying a different
account. The legacy ACP reader opens a temporary connection only for the captured
`antigravity-acp` 1.4.1 runtime; other runtimes refuse the prompt-based probe before
writing. Both readers observe daemon shutdown and the existing output/time budgets.
The native report may list a separate
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
Both refresh buttons use `GET /v1/provider-quotas/refresh` (`provider-quotas-refresh` over Cloud,
API level 115). It rereads local records immediately and lets each provider check run when its
cooldown permits. Codex updates when its CLI writes
a new reading. While a manual check is in progress the browser polls the ordinary snapshot more
frequently and reports when it sees a newer observation, a failed check or unchanged readings.
On older daemons the buttons reread the ordinary snapshot and explain the version requirement.

Failed refreshes keep the last successful windows marked outdated. A changed credential identity
clears the previous account's cached reading, and an in-flight answer for it cannot publish into
the new identity. Missing login files clear that provider. A missing numeric response is unknown,
not zero. Grok requires an explicit percentage or a numeric used/limit pair. A dated weekly period
alone remains unknown: the [official CLI billing codec](https://github.com/xai-org/grok-build/blob/2bdd1d6a6369de0e8c68132ea4539e9abd9e14a8/crates/codegen/xai-grok-shell/src/extensions/billing.rs#L55)
represents `credit_usage_percent` as optional. This does not reinterpret an explicit zero.
Computable monthly budgets stay monthly. Quota percentages that have passed
their reset time remain marked outdated until the next usable observation.

## Claude account readings

There is one Claude reading per machine: `limits.Reader.Quota`. Dispatch availability, the
assistants route, Session info and its Status Line corner, Cloud's assistant choice and the
footer all use it. A status line can rewrite `rate-limits.json` with data an open Session
already holds, including data from before a plan upgrade. A newer file timestamp therefore
does not give it priority over a direct account response. The status-line file is the fallback
while no direct response has a live window.

For active reads, the daemon starts the installed `claude` in print mode with stream-json input and output, writes one
experimental `get_usage` control request with `skip_behaviors`, reads the one matching reply, and
stops the process. It never sends a prompt, so no model request is made and no quota is spent.

The process runs in an empty scratch directory with `--setting-sources ""` (no user, project or
local settings, so none of the person's hooks or plugins run), `--strict-mcp-config` (no MCP
servers) and `--no-session-persistence` (no transcript). `--bare` is not used because it also
skips the keychain where Claude Code keeps its sign-in. Clawdline never reads the token.

A reading never waits for an ask. Asks run one at a time, at most once every 15 minutes
(`claudeusage.RefreshSecondsLimit`), each stopped after ten seconds and 1 MiB of output; all
three are registered capacity rows. Any active reader may start one. Doctor's inspection uses
`Observe` and starts no checks. Active reads recheck at this interval even when the status-line
file looks recent. The direct answer ages by the same freshness line as the file. When it answers, the footer names the native CLI as the source and Session info's
sentence ends "asked Claude Code". When it cannot, the reason stays the record's (`no_record`,
for example) and the sentence adds what the ask found: no Claude Code installed, no plan windows
(an API key, Bedrock, Vertex or signed out), or no answer (an older Claude Code without
`get_usage`). A failed check keeps the last good answer, which then ages. A response confirming
no plan clears the previous direct answer. Only 5h and 7d are
carried; the model-scoped Fable weekly window is not.

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
