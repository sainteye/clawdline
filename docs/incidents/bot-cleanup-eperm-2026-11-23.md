# A Bot refused every message after `signal_group_live: operation not permitted` (2026-11-23)

## What the person saw

Two Bots stopped accepting messages on the same day (Beacon about 15:20, Lens
17:50). Every new message came back with "This message was not sent. The Agent
process from the previous turn has not been confirmed stopped…", and the only
way out was restarting Clawdline. The daemon log (dev `v3.2.0-dev.9bba41c`)
recorded the Lens case:

```text
17:50:05 project assistant: Agent process cleanup unconfirmed; bot=team-… keeps its owner
until Clawdline restarts: execution_effect_unknown: signal_group_live: operation not permitted
```

The Beacon case predates that log line and left no cause; it is presumed to be
the same, not proved.

## Cause

`operation not permitted` is `EPERM` from `kill(-pgid, sig)` in
`signalOwned` (`internal/adapters/projectassistantacp/process_unix.go`).
After EPERM the cleanup takes a fresh `ps` census and calls
`confirmStoppedGroup`; any member of the group whose state did not start with
`Z` or `X` counted as live, so the result was `signal_group_live`.

On macOS (XNU), `kill(-pgid)` answers EPERM, not success, when every member of
the group is a zombie or has already begun exit. A process that is tearing
itself down after SIGKILL — a Node Agent with a large heap takes a noticeable
time — is shown by BSD `ps` as `?E` ("trying to exit"). Its first letter is
not `Z`, so the census read it as live.

Measured on this machine with a minimal reproduction (a `sh` group whose child
`python3` held about 1.6 GB, SIGKILL to each member, then `kill(-pgid, 0)`
while the child exited):

| Run | `kill(-pgid, 0)` | Census rows in that group at that moment |
|---|---|---|
| 6 of 6 | `EPERM` | leader `Z <defunct>`, child `ppid=1 ?E (Python)` |

The same reproduction also showed that a group containing only an unreaped
zombie answers EPERM to signal 0, TERM and KILL, while a group with a live
set-uid root member (`/usr/bin/top`) still answers success because the other
members were signalable. A set-uid member alone therefore does not explain it.

Disk pressure (data volume 92 %, 78 GB free) was considered and does not take
part: the failure is a signal answer and a process state, neither of which
reads or writes disk.

The live processes of the Lens incident were gone by the time this was
investigated (`ps` at 18:03 showed one unrelated Agent child of the daemon),
so the exact group membership at 17:50 is inferred from the reproduction and
the error phase, not observed.

## Why it needed a restart

`Process.Close` ran inside `closeOnce`, so every later `Close` returned the
first error unchanged. The Engine kept the actor `retained` and
`Engine.acquire` refused every message with `owned_cleanup`. The retained
owner was retried only by `Engine.Close`, which runs at daemon shutdown.

## What changed

- `exitingProcess`/`stoppedProcess`: a member with the BSD `E` flag counts
  as stopped. Linux `ps` does not use that letter.
- `liveOwnedGroup`: a process group id cannot be allocated while that group
  exists, so a leader with our group id and a different start time proves the
  group was reused by someone else; its members are not ours. A leaderless
  remnant of our group still counts.
- A later `Process.Close` re-verifies (`reverify`) a failure whose phase only
  says a signal outcome was unconfirmed (`reverifiablePhases`): a complete
  fresh census, signals to any still-live owned identity, the reaped direct
  child, and no live owned identity or group member. Inventory gaps
  (`descendant_inventory`, `cleanup_survey`, `root_identity`,
  `owned_descendants`) stay final (`cleanupFinal`), because a descendant that
  was never seen cannot be proved stopped.
- `Engine.retryRetained`: a retained owner re-runs the existing verified
  `retryCleanup` on its own, `bots.cleanup_retries` = 5 times, the first
  after `bots.cleanup_retry_ms` = 1 s and doubling (31 s in all). It releases
  only when Close returns without error, never resends the earlier turn, and
  logs whether cleanup was confirmed or remained unconfirmed.
- The chat refusal says Clawdline re-checks for about 30 seconds before
  suggesting a restart.

Accepted, executed and delivered stay distinct: the refused message has no
receipt (`pa_capacity_full` / `owned_cleanup`), and the earlier turn keeps its
`execution_effect_unknown` outcome.

## Not done

There is no "force release" action yet for a cleanup that stays unconfirmed
after the retries. Adding one needs a route that the phone can reach through
Cloud, an authorization decision, and copy that states the risk (a still
running Agent may act alongside the next one). Until then, restarting
Clawdline remains the recovery for that rarer case.

## Recovering a Bot stuck before this change

These steps are for the person; nothing here was run by the investigation.

1. Look at `~/.config/clawdline-next/logs/daemon.log` for the
   `cleanup unconfirmed` line and note the Bot.
2. Optionally confirm no Agent process of that Bot remains
   (`ps -axo pid,ppid,pgid,stat,lstart,comm`): rows with state `Z` or `?E`
   are already exiting.
3. Restart Clawdline on that machine the way it is normally restarted. Shutdown
   retries the retained cleanup, then the new daemon starts with no retained
   owner.
4. Send the message again. The refused message was never accepted, so this
   does not duplicate it. The earlier turn stays marked as an unknown outcome;
   check its effect before asking the Bot to repeat that work.

## 2026-11-24: `descendant_inventory: cannot allocate memory`

### What the person saw

On dev `v3.2.0-dev.87ee680`, which already carried the EPERM change above, an
acceptance request from one Bot to another (`message_bot`) ended with
`execution_effect_unknown:owned_cleanup`. `daemon.log` held 20 lines with
`descendant_inventory: cannot allocate memory`, in three cleanup episodes
(2026-11-23 23:33, 2026-11-24 00:01 and 02:16, three different Bots). In each
episode every one of the five retries over about 31 seconds logged the same
error. The machine was not short of memory (`memory_pressure` 47% free) and
not near a process limit (638 processes, `kern.maxproc` 6000).

### Cause

`cannot allocate memory` is `ENOMEM` from `sysctl(CTL_KERN, KERN_PROC,
KERN_PROC_ALL)` in `processIdentities`
(`internal/adapters/projectassistantacp/process_census_darwin.go`; no `ps` is
forked on macOS). The census asks the kernel for the table size, allocates
exactly that, then copies. The kernel's estimate leaves room for only five
more processes: a probe on this machine read an estimate of 636 entries and
then received 631. When six or more processes start between the two calls, the
copy is incomplete and the kernel answers `ENOMEM`. This is a race, not an
allocation failure.

Measured with a standalone probe of the same two calls (200 or 100 censuses
per row), while shell loops started short-lived processes:

| Load | Gap between calls | `ENOMEM` |
|---|---|---|
| idle | none | 0/200 |
| 8 loops of `/usr/bin/true` | none | 3/200 |
| 8 loops | 10 ms | 3/100 |
| 40 loops of `sh -c 'true & true & wait'` | none | 3/100 |
| 40 loops | 2 ms | 20/100 |
| 40 loops, fresh estimate + 25% headroom, up to 4 attempts | none or 2 ms | 0/100 |

A rare race became a certain failure because of when the census runs. The
descendant survey (`Process.survey`, `process.go`) takes one every
`bots.survey_ms` = 50 ms for the whole life of an Agent, about 72,000 an hour,
while Agents and the daemon themselves start processes. The first `ENOMEM` is
stored by `setCleanupError` as `descendant_inventory`, which is deliberately
not in `reverifiablePhases`: an inventory gap is final (`cleanupFinal`),
because a descendant that started and escaped during the gap could never be
proved stopped. Every later Close therefore returned that one stored error,
which is why all five retries logged the identical line. The retries were not
failing a fresh census each time.

### What changed

- `darwinProcessSnapshot` sizes each copy as the kernel's fresh estimate plus
  25%, capped by the existing census budget (`bots.process_census_bytes`). On
  `ENOMEM` it discards the incomplete copy and repeats both calls, at most
  `bots.process_census_attempts` = 4 times in all. Any other error, an
  oversized table, or a fourth `ENOMEM` still fails the census as before.
- Safety semantics are unchanged. A census that does fail is still a final
  inventory gap; no partial copy is used; no earlier turn is resent. A
  PID-and-start-time check of already known processes was considered as a
  fallback and not adopted: it cannot show that no new descendant started
  and escaped during the gap, so it cannot prove cleanup.
- Tests inject the fault through the reader seam:
  `TestDarwinProcessCensusRetriesASnapshotThatOutgrewItsEstimate` (two
  `ENOMEM` answers, then a real copy) and
  `TestDarwinProcessCensusGivesUpAfterBoundedAttempts` (persistent `ENOMEM`
  makes exactly four copy attempts and still fails). Both failed before the
  change.

Linux and the BSDs still use `ps`, whose own sysctl loop handles growth.

## Design review: a passing failure must not lock a Bot

Both incidents have the same shape. A safety check met a passing condition
(an exiting process answering `EPERM`, a census racing new processes into
`ENOMEM`) and stored it as if the answer could never be known. The safe
outcome, "do not release, do not resend", was right for that moment; storing it
forever was not. The hand-written `reverifiablePhases` list made "final" the
default, so every new phase locked a Bot until someone remembered to add it.

### What changed

`finalCleanupPhases` (`internal/adapters/projectassistantacp/process.go`) is
now the only list, and it names the phases that stay locked, each with its
reason. Every other phase, including one added later, makes a later `Close`
(and so `Engine.retryRetained`) run `reverify` again.
`TestCleanupPhasesAreReverifiableUnlessMarkedFinal` keeps that default.

| Phase | Class | Why |
|---|---|---|
| `descendant_inventory` | re-verifiable | A census failed (`ENOMEM`, `EPERM`, timeout). A later census can succeed; the gap it leaves is checked as below. |
| `cleanup_survey` | re-verifiable | The survey did not stop before the deadline, meaning a census hung. `reverify` first waits for the survey to return. |
| `signal_inventory`, `signal_pid_inventory`, `signal_group_inventory` | re-verifiable | A census around a signal failed; ownership was complete, only the signal's effect is unknown. |
| `signal_pid_live`, `signal_group_live` | re-verifiable | A signal was refused while a census still showed our process. A fresh census and new signals can settle it. |
| `process_cleanup` | re-verifiable | The direct child was not reaped before the deadline; a later wait can see it reaped. |
| `root_identity` | final | The root was never seen while unreaped. Once `Wait` reaps it, its start time, and with it our group's identity, can no longer be learned. |
| `owned_descendants` | final | The Agent made more descendants than the bound records. The bound, not a race, dropped them; a repeated census does not bring them back. |

Re-verification still needs complete evidence: a complete fresh census, new
signals to any live owned identity, the reaped direct child, no live owned
identity or group member, and, when a census had failed, a settled gap.

A failed census opens a *census gap* from the start of the last successful
census to the end of the next one. A gap can hide only a descendant that left
our process group and lost its parent before any census saw it; such a process
is reparented to the orphan reaper. The gap settles when a complete census
shows no live, unowned process born inside the gap (plus 2 s for start-time
resolution) whose oldest such ancestor hangs from the reaper or from an owned
process. macOS always reparents to launchd (pid 1). Linux can reparent to a
subreaper such as `systemd --user`, so there any unowned process born inside
the gap keeps cleanup unknown until it exits. The window is fixed, so later
retries see fewer such processes, never more.

Red before, green after (`go test ./internal/adapters/projectassistantacp`):
`TestProcessCloseReinventoriesAfterATransientCensusFailure` (`ENOMEM`,
`EPERM`, deadline: the second Close returned the first error),
`TestProcessCloseKeepsAnOrphanFromTheGapUnknown` and the default-phase guard
failed; `TestProcessCloseKeepsFinalPhasesLocked` and
`TestDarwinGapSuspectsHangFromLaunchdOrFromUs` hold the other side. With the
gap check disabled, the orphan test fails again.

### The survey interval stays 50 ms

An earlier draft of this change moved `bots.survey_ms` from 50 to 250 ms on a
CPU measurement alone (one Agent, about 640 processes: 20 censuses a second at
1.43% of a core against 4 at 0.46%). Review (Fuse, 2026-11-24) asked for the
census count per hour before and after, the ENOMEM reproduction rate under the
same load, and why the longer blind interval is acceptable. The reproduction
rate was not measured, so the interval stays 50 ms and only the sizing race is
fixed (`bots.process_census_attempts`). kqueue `NOTE_FORK` was considered and
not adopted: macOS has no `NOTE_TRACK`, so every new descendant still needs a
census before it can be watched.

### Review: the gap check must not lock a Bot either (Fuse, 2026-11-24)

The first version of the gap check above was itself a lock. On macOS almost
every XPC service and app hangs from launchd, and this machine had 46 live
launchd children born in the last ten minutes, 20 of them the person's; with a
gap of at least about 4.25 s roughly 28% of gaps met one, five quick retries
then failed, and nothing re-checked afterwards. On Linux every parent counted
as a possible reaper, so a busy machine almost always locked. A gap opened
before the first successful census had no lower bound and never settled.

What changed:

- **Proportional check.** A gap shorter than `bots.unscanned_census_gap_ms`
  (10 s, two cleanup deadlines: one failed census of any kind plus the grace
  wait) is treated like an ordinary survey interval and releases without a
  scan; the survey already accepts a blind interval between polls. Only a
  longer gap is scanned (`gapSettled` in
  `internal/adapters/projectassistantacp/process.go`).
- **Own user only.** The census now reads each process's real user id
  (`Pcred.P_ruid` from sysctl on macOS, `ps -o ruid=` elsewhere). A scan
  ignores other users' processes: an Agent cannot create one without a setuid
  program, which keeps the real user id. Every process that keeps a gap open
  is logged with its pid, parent, user and birth time.
- **Reapers are ancestors.** An orphan goes to init or to the nearest
  subreaper among its ancestors, and every Agent descendant descends from the
  daemon, so `orphanReaperIn` counts only pid 1 and, where subreapers exist,
  the daemon's own ancestors. Making the daemon itself a subreaper
  (`PR_SET_CHILD_SUBREAPER`) was not done: the daemon would then have to reap
  adopted zombies, and a `wait(-1)` loop would steal the exit status of every
  other `os/exec` child the daemon runs.
- **Lower bound.** A gap that opens before the first successful census starts
  at `StartProcess`.
- **Re-checks were meant never to stop** (superseded below). After `bots.cleanup_retries` quick retries, a
  retained Bot keeps re-checking with a doubling delay capped at
  `bots.cleanup_slow_retry_ms` (5 min) until cleanup is confirmed or the
  daemon stops (`retryRetained` in `internal/app/projectassistant/lifecycle.go`).
  The next message also re-checks once in `acquire` when nothing else holds
  the Bot, and is accepted if cleanup is confirmed
  (`internal/app/projectassistant/runtime.go`). Neither resends the earlier
  turn, which keeps `execution_effect_unknown`.

Tests: `TestProcessCloseReleasesPastStrangersAfterAShortGap`,
`TestProcessCloseScansALongGapForOurUserOnly`,
`TestProcessCloseSettlesAGapOpenedBeforeTheFirstCensus`,
`TestProcessCloseReleasesStrangersOutsideTheGap`,
`TestOrphanReapersAreInitAndTheDaemonsAncestors`,
`TestProcessCloseKeepsAnOrphanFromTheGapUnknown` (birth fixed inside the gap,
not at the census moment), `TestRuntimeRetainedCleanupKeepsRecheckingAfterQuickRetries`
and `TestRuntimeNextMessageRechecksRetainedCleanup`. Each failed before the
change. The Engine tests inject the cleanup failure at the provider session's
Close, not at the census; the census path is covered by the adapter tests.

## Second review: re-checking forever is a lock too (Fuse, 2026-11-24)

A long gap scans live processes of our own user born inside it whose oldest
such ancestor hangs from pid 1 or a daemon ancestor. On macOS launchd keeps
starting the person's apps and XPC services with exactly that shape; on Linux
`systemd --user` and a tmux server are daemon ancestors. Re-checks that never
stop therefore made recovery wait for an unrelated process to exit, possibly
for hours.

- **Bounded, then released.** After the quick retries,
  `bots.cleanup_slow_retries` (8: 32, 64, 128, 256 s, then every 300 s, about
  23 min) slow re-checks run. Then the Bot is released without confirmation
  (`releaseUnconfirmed`, cause `degraded`): the daemon log names the pids that
  kept cleanup unconfirmed (`UnconfirmedProcesses`), the abandoned session is
  never reused or signalled again, the earlier turn keeps
  `execution_effect_unknown` and is not resent, and a `cleanup_released`
  notice in the chat says the previous processes could not all be confirmed
  stopped and to check Activity Monitor.
- **Release without confirming.** The refusal line in the Bot chat carries a
  "Release without confirming" button with its risk written beside it. It
  sends `stop` with `release_unconfirmed`; the same release runs at once with
  cause `person`.
- **Unreadable start times.** `ps` now runs with `LC_ALL=C`, since `lstart`
  follows `LC_TIME`. Before, a row whose start time did not parse counted as
  born inside the gap, so one such row kept every long gap open. Now it is
  logged and counts only when its parent is an owned process.
- **Narrowing not adopted.** Filtering by executable name (claude, codex,
  node) is unsound: an Agent can leave any program behind (a shell, python,
  git), and the census deliberately reads no arguments. Session id and process
  group are what a `setsid` escape changes, and those escapes are what the gap
  check exists for. The bounded release covers the remaining false positives.
- The 250 ms interval in the subject of 6e36cb987 was reverted by 3e3ac8852;
  the survey polls every 50 ms.

Tests: `TestRuntimeRetainedCleanupDegradesAfterBoundedSlowRechecks`,
`TestRuntimeReleaseUnconfirmedFreesARetainedBotAtOnce`,
`TestProcessNamesTheOrphanThatKeepsAGapOpen` (a real orphan keeps cleanup
refused until the release) and
`TestProcessCloseDoesNotHoldAGapOnAnUnreadableStartTime`.
