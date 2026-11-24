# Safety mechanisms that latch a transient failure into a permanent state (audit, 2026-11-24)

Audited tree: `71e7a4ac3` (branch `main` at dispatch). Scope: `internal/`, `cmd/`
and `web/console/src` (plus the `web/core` stream it uses). This is a read-only
inventory: no product code was changed. Evidence comes from reading the code
at that revision and from read-only searches of this machine's daemon logs
(`daemon.log` and its four rotations, 2026-11-19 to 2026-11-24). Machine,
Bot and viewer identifiers are omitted.

**Revision, 2026-11-24 (after review by the reliability QA Bot).** Checked
against local `main` at `f49bf1dfc`, which contains the ENOMEM fix
`92064ecd2`; `origin/main` (`87ee68023`) did not yet contain it when this was
written. Changes: the ENOMEM status in P1-1 is corrected, P1-1 and P1-2 are
one piece of work with a census-window acceptance rule, the `Reconcile` fix in
P1-4 no longer revokes after a count of failures, the survey interval row says
what `main` runs, the dispatch list follows the reviewed order, and the
name-matching commit guard is withdrawn in favour of behavioural tests.

## The pattern

A check exists to keep something safe: an Agent process must be proved
stopped, a credential must not be reused while its owner is unclear, a
content key must not seal after a failed rotation. When the check cannot
reach an answer it fails closed. That is correct. The defect is what happens
next: the "could not decide" answer is **stored as if it were a final
answer**, and nothing asks the question again. A one-off `ENOMEM`, a `ps`
that hit its deadline, a 5xx, a full queue for one burst, or a daemon restart
then becomes a state that only a restart (or, for durable rows, nothing at
all) can clear.

Three shapes recur:

1. **Sticky first error.** `sync.Once`, a `closeOnce`, a cached `(value, err)`
   or a `disabled`/`broken`/`final` flag keeps the first failure and returns it
   forever.
2. **"Unknown" with no exit.** A state named `unknown`, `cleanup_unknown`,
   `owner_lost` or `halted` is entered on uncertainty, and every action that
   could resolve it refuses while in it.
3. **Retry that cannot succeed.** A retry loop exists, but the thing it
   retries returns the stored result instead of re-checking, or the loop
   stops after a fixed budget with no later trigger.

Known incidents of this family:

- 2026-11-23 EPERM: `kill(-pgid)` answered EPERM while the group was exiting
  and was read as live; `Process.Close`'s `closeOnce` kept the error. Fixed by
  `c787f78ab` (`reverify`, `retryRetained`; `docs/incidents/bot-cleanup-eperm-2026-11-23.md`).
- 2026-11-24 ENOMEM: `sysctl(KERN_PROC_ALL)` raced between size query and copy;
  the 50 ms survey ran it ~72,000 times an hour and the first failure was
  stored as a final `descendant_inventory` gap. The sizing fix `92064ecd2` is on
  `main` (an ancestor of `f49bf1dfc`; it was not yet on `71e7a4ac3`, the
  audited tree). It removes the race, not the latch: a census that still
  fails stays a final inventory gap.
- 2026-11-23 Beacon: `execution_effect_unknown` was described to the person as
  "some operations may have completed" (d6aeb2e5) — the same over-extension of
  "not known" into a claim, here in copy rather than state.

## Risk levels

- **P1** — observed on this machine, or reachable by an ordinary event
  (a daemon restart, a dev install, a network blip).
- **P2** — needs a plausible but less common transient (EIO, ENOSPC, SQLITE_BUSY,
  a 5xx from a proxy, a burst).
- **P3** — theoretical, or the impact is cosmetic.

## Findings, by risk

### P1-1. A single failed process census during a Bot turn is a final cleanup failure

- **Where:** `internal/adapters/projectassistantacp/process.go:552-573`
  (`survey`), `:541-549` (`setCleanupError`), `process_unix.go:82-87`
  (`captureOwned` wraps any census error as `descendant_inventory`),
  `process.go:166-189` (`reverifiablePhases` excludes it), `:406-462`
  (`Close` inside `closeOnce`), `:468-475` (`reverify` returns early when
  `cleanupFinal`).
- **Plain description:** every `DescendantPoll` (50 ms) the survey takes a
  full process census. Any error — `ENOMEM` from the sysctl race, `ps` killed
  by its deadline on Linux (and on macOS before `cbac1db9d`), `EIO` from a
  short or changing snapshot, a deadline on a loaded machine — is recorded as
  `descendant_inventory`, which is final. `Close` then returns it, and
  `reverify` will never look again.
- **Why "final" is too strong:** the reason given is that a descendant that was
  never seen cannot be proved stopped. But the survey already has a 50 ms blind
  window between successful ticks; one failed tick widens that window to
  100 ms. It does not make the inventory unknowable. A later complete census
  still sees every descendant that kept our process group or a traced parent;
  what it cannot see is a descendant that both `setsid`-escaped and lost its
  traced parent inside the gap, which is the same risk the 50 ms interval
  already accepts.
- **Triggers:** `ENOMEM` (sysctl size race), `context deadline exceeded` from
  `ps` under load, `EIO`/`io.ErrShortBuffer` from the census guards, process
  churn (many short-lived children, e.g. builds run by the Agent).
- **What the person sees:** the Bot refuses every message with "could not
  confirm that the Agent had shut down" (`failCleanup`,
  `web/console/src/project-assistants/words.ts:119`); Bot-to-Bot requests end
  in `execution_effect_unknown:owned_cleanup`. Only a restart clears it.
- **Evidence (logs):**
  - 2026-11-23 19:34:03, 19:39:13, 19:48:20 (twice): `descendant_inventory:
    signal: killed` followed by `context deadline exceeded` — the `ps` census
    hit its deadline. Logged "keeps its owner until Clawdline restarts".
  - 2026-11-23 23:33:20–23:34:02, 2026-11-24 00:01:43–00:02:14, 00:59:31,
    02:16:22–02:16:53: `descendant_inventory: cannot allocate memory`.
  - 20 `cannot allocate memory` and 24 `descendant_inventory` lines in total.
- **Status on `main` (`f49bf1dfc`):** the ENOMEM sizing fix `92064ecd2` is
  on `main`, with its attempt bound registered as
  `bots.process_census_attempts`. As its own message says, "a census that
  still fails stays a final inventory gap", and the Linux/BSD census still
  shells out to `ps` under `CleanupTimeout` (`process_unix.go:36-58`). The
  latch itself is unchanged.
- **Suggested fix (together with P1-2; already in progress as
  the census-window task):**
  distinguish *a census that failed* from *a census that proved a gap*. Record
  a failed survey tick as a reverifiable `survey_gap` carrying the time window
  it covers, not as `descendant_inventory`. A count of consecutive failures
  is not the deciding fact; what the window could have hidden is. When the
  gap is rechecked (in `reverify`, and again when the retained owner is
  acquired for the next turn), take a complete fresh census and **refuse only
  if a process born inside the window, running as the same uid, without our
  process group or a traced parent (an orphan that could be ours), is still
  alive; otherwise release.** Keep `root_identity` and `owned_descendants`
  (capacity) final; those are real proofs, not transient answers.

### P1-2. The retained-owner retry loop cannot clear a final error, and stops after 31 s

- **Where:** `internal/app/projectassistant/lifecycle.go:26-89`
  (`retain`, `retryRetained`), `runtime.go:149-170` (refusal while retained),
  capacity `bots.cleanup_retries` = 5, `bots.cleanup_retry_ms` = 1000.
- **Plain description:** `retryRetained` re-runs `retryCleanup` five times with
  doubling delay. Each retry calls `Process.Close`, which, after the first
  call, only runs `reverify` — and `reverify` returns immediately for a final
  error. So for every `descendant_inventory` failure the five retries are
  guaranteed to fail, and after the last one nothing ever retries again.
- **Triggers:** any P1-1 failure; any other final cleanup error.
- **What the person sees:** the same refusal as P1-1. The chat copy says
  Clawdline re-checks for about 30 seconds, which is true but cannot succeed.
- **Evidence:** 2026-11-24 02:16:22 → 02:16:53: six "retries cleanup" lines
  each with the same `cannot allocate memory`, then silence; 2026-11-23
  23:33:20 → 23:34:02 and 2026-11-24 00:01:43 → 00:02:14 show the same shape,
  ending in "keeps its owner until cleanup is confirmed".
- **Suggested fix (one piece of work with P1-1; already in
  progress as the census-window task):** (a) Do not schedule futile retries: when the stored error is
  final, say so in the log and the Bot status instead of retrying. (b) Once
  P1-1 makes census failures reverifiable, keep a slow retry after the fast
  budget (bounded, with backoff, registered in `capacity`) rather than
  stopping, and **re-check when the retained owner is acquired** for the next
  message instead of refusing on the stored result. When the retries give up,
  tell the person what they can do next. Never resend the earlier turn; keep
  its `execution_effect_unknown` outcome.

### P1-3. A daemon restart turns in-flight login profiles and connector credentials into permanent `unknown` rows

- **Where:** `internal/adapters/botloginprofiles/registry.go:61` (startup:
  every `accepted`/`running`/`removing` row → `unknown`), `:455-540` (about
  a dozen error paths → `unknown`), `:610-621` (`Quarantine`), refusals at
  `:349-350`, `:398-399`, `:411`, count at `:305`.
  `internal/adapters/botconnectorvault/registry.go:85-90` (startup: `accepted`
  rows and `ready` rows with a hold → `unknown`), `:466-475`, `:498-506`
  (`quarantine` / `Quarantine`), refusals at `:293-294`, `:316-317`, `:403`.
  Callers: `projectassistantacp/model_configuration.go:312,328`,
  `mcp_stdio.go:72-75,179-183`, `mcp.go:156-157`.
- **Plain description:** these rows are durable (SQLite). Once `unknown`,
  prepare, check, start and remove all refuse; remove of a connector
  credential only moves it to `cleanup_unknown`, which also refuses. No code
  path leaves either state, so a restart does not help either. Hold rows are
  not swept and still count against `MaxLoginSecretTargets`.
- **Triggers:** any daemon restart (including a dev install) while a sign-in,
  check or connector execution is running; a vault `Get` that fails once
  during `check` (`:491-493`, turns a healthy signed-in profile `unknown`);
  `EAGAIN` when starting the native process; a cleanup timeout feeding
  `Quarantine` (that is, P1-1 again).
- **What the person sees:** a login profile shown as unknown that cannot be
  checked, re-keyed or removed; a connector that answers
  `connector_credential` OutcomeUnknown for that Bot forever; eventually a
  capacity refusal for new holds.
- **Evidence:** none observed — the tables on this machine have no rows. Ranked
  P1 because a daemon restart is an ordinary event and the state has no exit
  at all.
- **Suggested fix:** keep "do not reuse the old signed-in receipt" (that is the
  safety meaning), but give `unknown` a resolving action: `check` should be
  allowed on an `unknown` profile and should settle it from a fresh native
  status (signed in → `signed_in` with a new receipt; signed out →
  `signed_out`); `remove` should be allowed once no live owned process holds
  the profile (proved by the same census the Bot runtime uses). At startup,
  delete holds whose owning process cannot exist any more (the daemon that
  created them has exited). A connector credential in `cleanup_unknown`
  should be removable after the same proof.

### P1-4. A restart while a Bot dev server runs leaves it `Unknown` and unremovable

- **Where:** `internal/app/botdevserver/registry.go:98-102` (reload: any active
  attempt → `Unknown "owner_lost"`, `reserved=true`, counts toward
  `MaximumBotServerProcesses`), refusals at `:290-291` (`server_attempt`),
  `:362-363`, `:481-482`; `owner.go:250-252` (`cleanup_unknown` on Close
  timeout), `owner.go:160-162` (`journal_unknown` on a failed save).
  Separately, `revoke.go:37-66`: every 1 s routine tick runs `Reconcile`, and
  **any** check error — including a transient read of the shared place
  registry or the store — stops the server, with "no auto restart on a later
  successful check".
- **Triggers:** a daemon restart or dev install while a server runs; a slow
  process-tree shutdown; an SQLite write error; for `Reconcile`, one failed
  file or DB read during the 1 s tick.
- **What the person sees:** a dev server stuck in "unknown" that can be
  neither started, stopped nor deleted; with enough of them, `server_processes`
  CapacityFull. For `Reconcile`, a server that stops by itself and stays
  stopped.
- **Evidence:** no `owner_lost` or `cleanup_unknown` in the logs. Ranked P1 on
  ease (restart).
- **Suggested fix:** on reload, an `owner_lost` attempt is exactly a case where
  a census can decide: the journal holds the PID and start time, so look for
  that identity and its group; if absent, mark the attempt stopped and release
  the slot; if present, signal it under the existing owned-identity rules.
  In `Reconcile`:
  - revoke only on an *authoritative* denial: the scope is gone, the Bot was
    removed, or the rule changed;
  - mark every read error as *degraded* and confirm again on the next tick;
    a count of read errors never becomes a revocation;
  - when the server has been degraded for too long, show the person "could
    not confirm" and what they can do next, rather than stopping the server
    for them.

### P1-5 (closed). EPERM from an exiting process group

`c787f78ab` fixed this: `?E` members count as stopped, a reused group is not
ours, and `signal_*` phases are reverifiable. Kept here as the reference
example. Evidence: four "until Clawdline restarts … signal_group_live:
operation not permitted" lines on 2026-11-23 (17:50, 18:23, 19:07, 19:48).

### P2-1. One failed Cloud line Open stops all retries

- **Where:** `cmd/clawdline/cloud_update.go:35-36`: `reload` sets
  `g.started = true` even when `g.open()` fails; `resume` (`:39-60`) then
  exits.
- **Triggers:** any transient failure inside `cloudtransport.Open`
  (`internal/transport/cloud/link.go:633+`): settings or key read `EIO`,
  spool open, `keys.PendingRevocation()` read.
- **What the person sees:** "cloud: the line is off: …" and no phone or
  `app.clawdline.com` access until settings are saved again or Clawdline
  restarts.
- **Evidence:** 121 "the line is off" lines, all with "The Cloud connection is
  not available in this daemon." — that is the `!g.ready()` branch, which
  returns *before* `started` is set and does retry. No occurrence of the
  latched branch was found.
- **Suggested fix:** set `started` only on a successful open; keep the ticker
  retrying a failed open with backoff.

### P2-2. Cloud content key halts on a transient store error and only a later revocation or restart recovers

- **Where:** `internal/domain/cloud/key_epoch.go:139-142,175-180`;
  `internal/transport/cloud/rekey_runtime.go:98-105,131-144`;
  `internal/adapters/cloud/transport.go:1090-1094`. The comment at
  `transport.go:1149-1150` says "the next tick asks again: Recover is what
  reopens it", but no tick calls `Recover`; only a later revocation or the
  link's Open does (`link.go:874-880`).
- **Triggers:** `EIO`/`ENOSPC`/fsync failure during a viewer revocation; a
  transiently unreadable revocation journal (`rekey_runtime.go:98-103` halts
  deliberately on a read error).
- **What the person sees:** all Cloud traffic stops ("content_key_halted").
- **Evidence:** no `content_key_halted` in the logs.
- **Suggested fix:** keep sealing stopped while halted (that is the safety
  meaning), but schedule `Recover` with bounded backoff from the spool tick,
  which the comment already assumes; correct the comment either way.

### P2-3. Bot network Cloud rail marks itself broken after one full queue

- **Where:** `internal/adapters/botnetwork/cloud_rail.go:97-110`. `broken` is
  never cleared; `Ready()` stays false and pending requests fail each second.
  The rail is rebuilt only when the line context ends or `current` fails
  (`internal/transport/http/project_assistants_network_cloud.go:283-311`).
- **Triggers:** a momentary burst larger than `MaxNetworkCloudFrames`.
- **What the person sees:** `bot_cloud_unavailable` for Bot-to-Bot traffic
  across machines until a Cloud reload or restart.
- **Evidence:** none in the logs.
- **Suggested fix:** a full queue refuses that frame (typed backpressure to its
  sender); `broken` should mean the rail lost order, and should trigger a
  rail rebuild, not a permanent not-ready.

### P2-4. The console locks its language to English after one failed catalog fetch

- **Where:** `web/console/src/catalog.ts:179-188` (`bootCatalog` caches the
  resolved fallback in module-level `boot`). The retry written for this case
  (`strings-retry.ts`, `legacy/bridge.ts:207-228` `loadStrings`) is not called
  outside e2e files and fills a different table.
- **Triggers:** one network blip, 5xx or timeout during boot (phone on mobile
  data, Cloud cold start).
- **What the person sees:** the whole console in English for the life of the
  tab.
- **Suggested fix:** cache only a successful catalog; on failure, show English
  and retry in the background (bounded backoff plus `online`/`visibilitychange`),
  then swap the active catalog.

### P2-5. The session list stops updating after the local event stream is refused

- **Where:** `web/console/src/useFleet.ts:22-31`, `web/core/src/fleet.ts:73-96`,
  `web/core/src/stream.ts:47-63`. `onError` only sets `live:false`; nothing
  reopens an `EventSource` that the browser closed after a non-200 answer
  (a 500 during a daemon restart, a proxy 502/503, 401/403).
- **What the person sees:** the connection light shows "retrying" while the list
  no longer updates live; pressing it refreshes once but does not reopen the
  stream. Reload fixes it. (`bar/fleet.ts` restarts its stream on show; the
  terminal SSE in `TerminalView.tsx:340-363` reconnects correctly.)
- **Suggested fix:** when the `EventSource` reaches `CLOSED`, reopen with
  backoff, and on `online`/`visibilitychange`, as `TerminalView` already does.
  The same applies to `session/ScreenPanel.tsx:158-171` and
  `session/prompt-suggestions.ts:56-64` (P3; they keep a 15 s poll).

### P2-6. Phone identity pairing aborts on one failed poll

- **Where:** `web/console/src/cloud/health-owner/identity-pair.ts:59-88`. The
  poll may run 600 times, but `call` throws on any fetch error, timeout, 429
  or 5xx, and one throw ends the pairing.
- **What the person sees:** pairing fails as "unavailable"; the person starts
  over and scans the QR code again.
- **Suggested fix:** inside the poll, treat network errors, timeouts, 429 and
  5xx as "not ready yet" (honour `Retry-After`); keep typed refusals (expired,
  claimed, stale) terminal.

### P2-7. The Bots roster stream stops on `pa_unsupported` during a reconnect

- **Where:** `web/console/src/project-assistants/live-roster.ts:154`
  (`stopped` includes `pa_unsupported`); `web/console/src/cloud/project-assistants.ts:851-860`
  raises `pa_unsupported` when the retained machine descriptor is a snapshot
  that does not list the command yet ("During reconnect the retained
  descriptor can lag behind the machine").
- **Triggers:** a Cloud reconnect, before the machine's fresh descriptor
  arrives.
- **Evidence:** 95 viewer reports `cloud.bots.roster.failed code=pa_unsupported`
  with `api_level=null stage=admission` (2026-11-22: 8, 2026-11-23: 84,
  2026-11-24: 3), arriving about once a minute, so some outer caller does
  retry; whether the visible roster recovers without a remount was not
  confirmed.
- **Suggested fix:** while the descriptor is a snapshot, answer
  `pa_offline`-like "not known yet" (retryable) rather than `pa_unsupported`;
  reserve `pa_unsupported` for a fresh descriptor that lacks the command.

### P2-8. Remote Control close gives up on a race between `kill(pid,0)` and the start-time read

- **Where:** `internal/adapters/clauderemote/controls.go:117-131` (100 ms poll),
  `internal/adapters/process/claude_remote_unix.go:45-61`. If the process exits
  between `kill(pid,0)` and `ProcessStart`, the start time reads as zero and
  the call returns "process generation unreadable"; one error ends the close
  with "worker exit could not be verified".
- **What the person sees:** a Remote Control session that was archived but is
  reported as unverified; the copy says not to retry automatically.
- **Suggested fix:** when the start time is unreadable, re-run `kill(pid,0)`:
  `ESRCH` now means gone. Only return an error when the process is still
  present and its generation still cannot be read on the next tick.

### P2-9. Browser profile state error at startup has no exit

- **Where:** `internal/app/browserbridge/profiles.go:61-137,380-390`
  (`SetStopped` refuses to clear an error that is not a write failure),
  `internal/transport/http/browser_profiles.go:18-25`; one level down,
  `tabs_state.go:31-32,69-75`, `bridge.go:852`.
- **Triggers:** a transient `EIO`/`EMFILE` reading `browser-profiles.json`, the
  audit journal or the tab state at startup.
- **What the person sees:** every Agent browser command refused with
  `StateWriteFailed`, `state_error` in status, until restart.
- **Suggested fix:** keep refusing to overwrite bytes that were read and found
  corrupt or from a future schema (that is the real safety rule), but treat a
  failed *read* as retryable: re-read on the next status call or on the
  person's resume.

### P2-10. Push store open error is cached for the life of the daemon

- **Where:** `internal/transport/http/push.go:74-86` (`pushStores` keeps
  `{store:nil, err}`).
- **What the person sees:** every `/v1/push/*` route refuses; notifications to
  the phone stop.
- **Suggested fix:** do not store a failed open; retry on the next request
  (bounded).

### P2-11. Console Cloud lists give up after a fixed budget with no online/visibility trigger

- **Where:** `web/console/src/pages/terminal/cloud-project.ts:88-93,155-172`
  (stops after ~1 minute), `session/cloud-terminal-all.ts:90` (stops after
  ~7 s), `cloud/CloudGate.tsx:563-577` (`rosterFailed`).
- **What the person sees:** "Try again" or `roster_failed` after a phone moved
  between networks.
- **Suggested fix:** keep the button, and also retry on `online`,
  `visibilitychange` and `pageshow`, as `legacy/js/net/cloud-boot.js` and
  pairing recovery in `CloudGate.tsx:1167-1178` already do.

### P3 findings

| Where | What latches | Suggested fix |
|---|---|---|
| `internal/domain/apiperformance/storage.go:203-213` | One failed history write (including `SQLITE_BUSY`, `busy_timeout=0` at `:84`) disables persistence until restart; deliberate ("Restart is the explicit recovery boundary"). | Retry on the next snapshot with backoff; a non-zero busy timeout. |
| `internal/adapters/cloud/pinned.go:506-509` | A read error is kept while the file's size and mtime are unchanged, so a one-off `EIO` refuses inbound viewer envelopes. | Cache only parsed content; on error, re-read after a short bounded interval. |
| `web/console/src/legacy/devices-bridge.ts:184-199` | `/v1/diagnostics` failure cached as platform `null`; Setup Guide drops the macOS app step. | Do not cache a failure. |
| `web/console/src/session/Snippets.tsx:259-273,362-367` | One failed playbook-source read disables Start; any project resolve error is worded as an older machine. | Separate transport failure (retry) from mismatch. |
| `internal/adapters/terminal/owned/server.go:143-156`, `resolve.go:363-369` | "tmux is not installed/too old" cached for process life; a timed-out `tmux -V` memoised until the binary changes. | Do not memoise a timeout; re-check "not installed" on a later request. |
| `internal/transport/http/update.go:182-205` | A transient `os.Executable` failure in `updateOnce` leaves the daemon on the BUILD.json checker for life. | Retry selection on the next check. |
| `internal/app/orchestrator/callback.go:677-690`, `internal/transport/http/work_v2.go:1562-1567` | A 10 s pid-file wait and a 2 s conversation-id wait each mark the effect failed and final. | Mark as unknown-and-recheckable, not failed. |
| `web/console/src/legacy/skills-bridge.ts:80-90`, `personas.ts:62-75`, `session/skill-feed-core.ts:166`, `session/VideoCards.tsx:32-33` | A non-OK answer is cached as an empty or unsupported result (5 minutes, or until remount). | Cache only typed answers. |

### High-frequency polling of racy or external calls (< 1 s)

| Loop | Interval | Calls | On failure |
|---|---|---|---|
| `projectassistantacp/process.go:554` survey | 50 ms on `main` (`bots.survey_ms`); a 250 ms proposal still needs measurement, see the census-window task | full process census (sysctl on macOS, `ps` elsewhere) | stored as final — P1-1 |
| `clauderemote/controls.go:117` | 100 ms | `kill(0)` + sysctl start time | ends close — P2-8 |
| `adapters/cloud/transport.go:1107` spool drain | 100 ms | socket writes | ends pass; halted key — P2-2 |
| `transport/http/project_assistants_roster.go:79` | 250 ms | store reads | stream cancelled, client reconnects (fine) |
| `app/terminals/stream.go:213` | 250 ms (+33 ms gap) | tmux capture | one "Unreachable" frame, clears (fine) |
| `adapters/terminal/farewell.go:176` | 150 ms | tmux + tty sysctl | one-shot `close_unreadable` refusal (person retries) |
| `iterm_close_darwin.go:65` | 100 ms | tty sysctl | "left open", no retry (P3) |
| `projectassistanthost/host.go:876` routine tick | 1000 ms | place-registry file + DB | revokes dev server — P1-4 |

The 50 ms survey is the outlier: it multiplies a rare race into a near-certain
one over a long turn. Its interval should be justified against what it
protects (catching a `setsid` escape) and measured; a census per 50 ms per
running Bot is the reason the ENOMEM race was hit at all. `c18ff9376` changed
it to 250 ms on the branch of a task that was sent back, so `main` still runs
50 ms; whether 250 ms is safe is part of the census-window task and needs measurement
first.

### Checked and found fine

Once-guards that cache only success or cannot fail (`listsOnce`, `doctorOnce`,
`usageOnce`, `limitsOnce`, `routeStatsOnce`, embedded copy); Once used only as a
close or stop guard; per-execution poison in `mcp_stdio.go:139-176`; errors
that reset on the next success (updater `Checker.lastErr`, logs `lastErr`,
cloud `Roster.lastErr`, session recaps); `connectionhealth.StateBlocked`
(recomputed each scan); terminal SSE reconnect; the console Cloud connection
loop (parks and resumes on visibility/online); `Engine.disabled` (cleared when
the host engine is rebuilt).

## Suggested dispatch list (reviewed order)

1. **Census failure is a recheckable window, rechecked at acquire** (P1-1 and
   P1-2; already in progress as the census-window task). Done when: a failure-injection test
   makes one census fail mid-turn; a later recheck refuses while an orphan
   born inside the window with the same uid is still alive, and releases the
   Bot otherwise; acquiring the retained owner for the next message re-checks
   instead of returning the stored error; a final error produces no futile
   retries, and a reverifiable one keeps a bounded retry with backoff that
   tells the person the next step when it gives up.
2. **P1-3: login profiles and connector credentials can leave `unknown`.**
   Done when: `check` settles an `unknown` profile from fresh native status,
   `remove` works once no owned process holds it, stale holds from an exited
   daemon are swept at startup, and a restart-mid-sign-in test ends in a
   usable profile.
3. **P1-4: dev servers survive a daemon restart.** Done when: reload resolves
   `owner_lost` by the journalled PID and start time (absent → stopped, slot
   released), and `Reconcile` revokes only on an authoritative denial, marks
   read errors degraded and rechecks next tick, and shows "could not confirm"
   with a next step instead of stopping the server; tests inject a read error
   and a restart.
4. **P2-1 and P2-10: the Cloud line and the push store retry by themselves.**
   Done when: a failed Open keeps retrying with backoff, and a failed push
   store open is not cached; each with a failure-injection test.
5. **P2-2 and P2-3: a halted content key and an overflowed rail recover.**
   Done when: a halted key schedules `Recover` with bounded backoff, and a
   rail that overflowed is rebuilt; each with a failure-injection test.
6. **Console: P2-4 language catalog, P2-5 fleet stream, P2-6 pairing polls,
   P2-7 roster.** Done when: each recovers without a reload, tested with an
   injected failure followed by success; the screens are accepted by Lens.
7. **P2-8 Remote Control and P2-9 browser profiles re-check.** Done when: an
   exit between `kill(0)` and the start-time read counts as gone, and a
   transient read error of browser-profile state is confirmed again later
   instead of latched.
8. **P3 sweep, then the rule.** Done when: each P3 row either retries or is
   documented as an intentional restart boundary with the reason; finally
   the draft rule below goes into `docs/working-rules.md`, written as the
   behavioural-test pattern only, with no name-scanning gate.

## Draft rule for `AGENTS.md` / `docs/working-rules.md`

> **A safety check that cannot decide must be able to decide later.** When a
> check meets a transient condition — `unknown`, `EPERM`, `ENOMEM`, `EAGAIN`,
> `EIO`, a timeout, a cancelled context, a 5xx, a full queue, a daemon
> restart — it fails closed for *this* attempt and records the answer as
> *undecided*, with the next moment it will be checked again. Only definite
> counter-evidence (a live owned process seen in a complete census, a typed
> refusal, a corrupt file that was actually read) may lock a final state.
> Every state that blocks the person must name the event that leaves it; if
> the only exit is a restart, that is a defect unless the code says why and a
> test pins it. A retry must re-run the check, never return the stored
> result.

The rule in practice:

- **Five answers, five treatments.** A check answers *unknown* (could not
  look), *transient* (looked and hit a condition expected to pass), *definite*
  (a positive, complete answer), *denied* (an authoritative refusal) or
  *empty* (looked completely and found nothing). Only *definite*, *denied*
  and *empty* may settle a state; *unknown* and *transient* keep the
  operation fail-closed for this attempt and schedule a recheck.
- **A receipt replays only a settled answer.** An idempotency receipt or a
  cached result stores *definite*, *denied* or *empty*; it never replays
  *unknown* or *transient* as if it were final.
- **Refresh before declaring a credential expired.** A credential that looks
  stale is refreshed once before the state says it has expired.
- **A full table evicts.** Rows that hold capacity (holds, attempts, retained
  owners) have an eviction path, so a stuck row cannot fill the bound.
- **Retries are bounded and back off,** and when they give up the person is
  told what they can do next.

### How to test it

The only evidence that a check does not latch is a behavioural test: inject
one failure into the dependency, then let it succeed, and assert that the
blocked operation succeeds **without a restart**. The existing seams
(`censusOverride`, `signal`, the `get typeof fetch` parameters in the
console) already allow this.

A name-matching commit guard (fields called `final`, `broken`, `disabled` and
so on, requiring a `latch:` annotation) was considered and withdrawn: it would
misfire often, it cannot say which harm a hit causes, and it would become a
gate that blocks `main`. If a scan is useful, it may only run as a
non-blocking inventory report.
