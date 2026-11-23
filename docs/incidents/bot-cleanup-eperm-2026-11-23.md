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
