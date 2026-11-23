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

### The survey interval

`bots.survey_ms` was 50 ms from the first Bot runtime commit (91d6f6308,
2026-11-12), with no measurement recorded. The runtime doc already said it "is
not a guarantee against unobserved fast reparenting". What it buys is narrow:
group signals already reach every descendant that stays in the Agent's process
group, whatever the interval. The survey matters only for a descendant that
leaves the group (`setsid`) *and* whose parent exits between two censuses. A
typical daemonizing double fork does both within a few milliseconds, faster
than either 50 or 250 ms; a long-lived parent is caught by either.

Measured on this machine (about 640 processes), one Agent's survey alone, 10 s
per row, CPU from `getrusage` including the Go runtime's idle cost:

| Interval | Censuses per second | CPU per census | Share of one core | CPU per hour |
|---|---|---|---|---|
| 50 ms (before) | 20 | 0.71 ms | 1.43% | 51 s |
| 250 ms (now) | 4 | 1.16 ms | 0.46% | 17 s |
| 1 s | 1 | 2.47 ms | 0.25% | 9 s |

The cost scales with each live Agent, and the census rate is what turned the
sysctl race into a routine failure. The interval is now 250 ms: five times
fewer censuses and a third of the CPU, while the window it narrows stays a
fraction of a second. An event-driven survey was considered and not adopted:
macOS removed kqueue `NOTE_TRACK`, so `NOTE_FORK` says that a watched process
forked but not which child, and every new descendant must still be found by a
census before it can be watched; Linux would need a different mechanism. It
would not close the escape window, only move it.
