# Opening an existing Session · 2026-11-20

Selecting a Session should show its latest readable messages as soon as they arrive. Session
selection is a normal navigation action. No production open count or click-to-visible latency
distribution was collected for this change.

## Timing and decision

| Step | Evidence | Decision |
|---|---|---|
| Fetch the latest conversation | Six loopback reads across one Codex and one Claude Session, three each, with 40 entries requested in the `tools-v2` view. Time to first byte ranged from 11 to 25 ms. | Keep the existing request. The sample measures the daemon response on one machine. |
| Refresh the terminal screen and Session row | Six loopback look requests took 233 to 320 ms to first byte. Session selection starts this request in the background. | Keep the refresh. The duration does not add to the time the detail pane takes to open. |
| Make the latest conversation visible | The console kept the body behind its opening skeleton while an automatic history read filled the viewport. A second skeleton could also stay visible for at least 320 ms once shown. | Reveal the first readable entries and continue filling the viewport in place. Remove the second skeleton and its timer. |

At a 390 px mobile viewport in the isolated Chrome fixture, the server delayed the latest page by
250 ms and its predecessor by 1,250 ms. Three fresh tabs per build measured the interval from clicking
the visible Session row to the animation frame that showed the newest message:

| Console | Samples (ms) | Median (ms) |
|---|---|---:|
| Before | 1785, 1782, 1772 | 1782 |
| After | 285, 374, 269 | 285 |

The median fell by 1,497 ms in this scenario. In each fixed run, the latest entry appeared while the
earlier page was still pending, and that earlier entry appeared afterward. The browser regression
asserts that order rather than a time threshold. An empty first page still waits when earlier pages
may have readable entries. Existing read errors, history limits and scroll anchoring still apply.
The main correctness risk is revealing a sparse page during the fill and then moving the reader
when earlier messages arrive. The fixture checks early display and eventual fill. Separate tests
cover the history budget and preserved reading position.

The fixture isolates client-side waiting. These numbers do not measure a paired Cloud Relay,
a physical phone or the time to start a new Session. The Cloud status source also has Session
metadata and transcript checks. Their cost on the visible path needs a paired measurement.

## Reproduce

Build the console with `npm run build` in `web`. Run
`node --test web/console/src/session/jump-to-latest.e2e.ts` from the repository root.
Set `JUMP_REPORT` to collect the samples in JSON. Set `CLAWDLINE_DIST` to the built console
from before the change to run the same probe against the older client. The order assertion
is expected to fail on that client.
