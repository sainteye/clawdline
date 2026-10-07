<!-- clawdline-doc: kind=spec audience=agent -->
# Session resource coordination

Clawdfather arranges operations, not ownership of Project work. The Board item owner still implements, verifies, lands and deploys. Every reading used to choose an order carries its source and observation time. A stale or missing Session reading is uncertainty, never evidence that its command finished.

## One coordination protocol

Use the existing broker resources and receipts:

- `heavy_compile` is the exclusive heavy build/test slot. Run work through `tools/heavy.sh` or `clawdline heavy -- <command>`. A queued root command hands itself to a callback by default, ends the model turn, and wakes the Session with a completion notice. The callback owns the lease while the command runs.
- `landing` is exclusive per checkout. Acquire it before a mainline merge, renew while the operation runs, and release after the merge has settled. A grant, rather than a queue position or a notification, authorizes the merge.
- File waits register the owner, waiter, paths and release condition. The owner releases explicitly; a delivered release notice wakes the waiter. Time alone never releases a wait.
- `daemon_restart` is exclusive with compile, landing and other restart leases. Use the operation wrapper so the lease remains held through the restart command. Stop new work, let active important commands reach their own safe point, and verify the console after restart. A Session message is not a substitute for that maintenance boundary.

Clawdfather reads `clawdline sessions --json`, `clawdline leases --json`, open waits, task/callback records, and Machine Bearings before ordering work. The **Coordinate Session resources** panel puts the same observations together, retaining each source time and read error. An absent row on an incomplete scan proves nothing.

## Pause and wake

`clawdline coordination pause --reason "…" --wake "lease:heavy_compile" <conversation-id>…` accepts one durable request per target. Other wake conditions are `lease:landing:<absolute-checkout>`, `lease:daemon_restart`, `wait:<wait-id>`, or a plain condition that the coordinator checks explicitly. The target receives a structured notice. If it is unreachable, busy or showing a menu, the request stays **accepted** with a named delivery error. Inspect with `clawdline coordination status`; use `retry <request-id>` after fixing the cause. Repeating the same accepted request does not send it twice.

The receiving Session completes any important command already in progress. It then runs `clawdline coordination observed <request-id>` and, once it has stopped at a safe turn boundary, `clawdline coordination safe <request-id>` and ends its model turn. The UI says **safe point** only after that receiver receipt. Delivered, observed and safe are distinct facts. No pause request kills a process or abandons an uncommitted operation.

Clawdfather sends `clawdline coordination wake <request-id>` after the condition is met. A typed `wait:` condition requires the release to have reached that waiter. A typed `lease:` condition refuses a wake while another Session holds the resource or is ahead in its queue. A free slot can be taken between the read and the wake, so the receiver must still acquire its lease before work. The wake is delivered as an event; the Session runs `clawdline coordination resumed <request-id>` and verifies its prerequisite. A delivered wake is not an executed command. A replacement Clawdfather may wake a predecessor's parked Session after it is registered as the current coordinator.

## Failure and recovery

The pause row and delivery effect are committed in one broker transaction. An unstarted effect is recovered after daemon restart. A delivery attempted across a crash may be **unknown**; do not automatically type it twice. Inspect the receiver and retry using the same request ID when necessary. Duplicate receipt commands are idempotent. An uncontactable receiver remains accepted with an error, never paused.

An exclusive holder whose renewal lapses keeps its lease while its process or Session is positively live or unreadable. It is replaced only after positive gone evidence; a silent waiter is passed over so it cannot block the queue. A daemon outage is not permission to bypass an exclusive landing or restart boundary. `heavy` retains its documented fail-open recovery path for rebuilding a failed daemon, with a visible warning and no claim of exclusivity. Peers use these same leases, waits and callbacks without a Clawdfather Session.

This is cooperative Session control. The daemon can prove a receipt and guard lease grants, but cannot infer that arbitrary provider work is safe from a terminal line. The Session's safe-point receipt is its explicit statement, and important commands are allowed to finish.
