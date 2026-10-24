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
together; overflowing providers remain horizontally scrollable. The local footer starts with the
provider icons, while Cloud may identify the selected machine by name. Every known window remains available in the details dialog. Below 900 CSS pixels,
the mobile layout shows the two tightest providers and a **+N** details button only in the Session
list. Session conversations and other pages unmount the footer and stop its reads, leaving no
reserved bottom space. The mobile list reserves the full measured footer height, including its
home-indicator clearance from `--indicator` (the console does not use `viewport-fit=cover`, so
`env(safe-area-inset-bottom)` alone can be zero on iOS). Changing viewport size does not change
the saved browser preference.

## Sources and ownership

| Provider | Read source | Login ownership |
| --- | --- | --- |
| Claude Code | Existing `limits.Reader`: status-line `rate-limits.json` | No credential access or login change |
| Codex | Existing `limits.Reader`: quota keys in recent rollouts | No credential access or login change |
| Gemini CLI | Existing `~/.gemini/oauth_creds.json`; `loadCodeAssist`, then `retrieveUserQuota` | Reads the saved access token; an expired token requires running Gemini CLI |
| Grok CLI | Existing `$GROK_HOME/auth.json` or `~/.grok/auth.json`; CLI billing credits, optionally its monthly view | Uses the xAI issuer entry; an expired token requires running Grok CLI |

These are quota metadata reads, never model generations or chat prompts. The implementation does
not write CLI credentials, run agents, redeem credits or switch accounts. Provider responses and
credential files are capped at 1 MiB per read. Only numeric quota windows, timestamps, source names
and typed problem codes cross the local API and encrypted Cloud transport; tokens, emails, account
identifiers, provider response bodies and filesystem paths do not.

`GET /v1/provider-quotas`, carried over Cloud as `provider-quotas`, returns one machine snapshot.
It is separate from `/v1/orchestrator/assistants`: adding quota-only providers does not imply they
can be dispatched. The browser polls while visible, normally every 30 seconds. Gemini and Grok
refresh asynchronously, one in flight per provider, with a 15-minute minimum interval shared by
all browsers on that daemon. A complete refresh has a ten-second deadline, no automatic retries,
and respects a longer `Retry-After`. The Refresh button rereads this cache and the local files;
it does not bypass provider cooldowns.

Failed refreshes keep the last successful windows marked outdated. A changed credential identity
clears the previous account's cached reading, and an in-flight answer for it cannot publish into
the new identity. Missing login files clear that provider. A missing numeric response is unknown,
not zero. Grok's omitted scalar zero is inferred only with an explicitly weekly period and matching,
valid billing dates; a computable monthly budget stays monthly. Quota percentages that have passed
their reset time remain marked outdated until the next usable observation.

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
