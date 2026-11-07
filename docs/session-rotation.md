# Long conversation reminders

The Console suggests a fresh Session when its Codex rollout or Claude Code
transcript reaches 50 MiB, or when a currently delivered Session has been idle
for at least 24 hours. These are reminders only, approved as the default on
2026-11-07. They do not start provider work in the background.

`GET /v1/sessions/{id}/info` (including `parts=summary`) reports `record.bytes`
from the file metadata used by the bounded facts reader. It includes compacted
snapshots and history outside the tail read. Missing or unreadable records omit
the field. No full-file compaction count is inferred from a tail, and neither
provider's conversation files are modified or removed.

The info reply's `rotation` supplies the registered byte and idle thresholds.
The quiet-time rule requires an idle terminal, a current session-scoped delivery
(`milestone_complete` or `work_complete`), a delivery time, and known record
activity. Its clock starts at the later of delivery and last activity. A later
message invalidates the current delivery through the existing work projection;
unknown activity never becomes an idle reminder. The suggestion appears beside
the open conversation, with the file size also in Session info.

**Prepare handoff** is available while idle. It sends a conversation-bound request
through the existing send receipt and pending-message controls. A recorded or
accepted request is not proof that a receiver exists or has read the package.
Failed or uncertain sends retain the normal retry/read-back controls; the notice
never launches a second request automatically. A recorded milestone handoff
in progress or already delivered suppresses further suggestions, including after
a page reload; unreadable broker history offers no new launch. Machine-role Sessions are excluded
because their role needs a separate succession protocol.

The original assistant prepares a milestone summary using `clawdline handoff
--summary <file>`. See [milestone handoffs](handoff.md#milestone-handoffs) for the
five summary sections, evidence and daemon-generated `obligations.md`. Current
worktree/branch/commit, uncommitted work ownership, decisions, constraints,
blockers and next step must remain explicit. The existing broker waits for a
live receiver conversation before moving captured Board items. Children, unread
notices and unsettled landings remain with the sender. A refused or incomplete
handoff keeps those obligations and the original Session intact.

After delivery, the original assistant checks the receiver binding and settles
its retained obligations, then runs `clawdline session close --dry-run`. It
reports the actual result and keeps the old Session open for the person to
inspect and close through the existing version-bound safety gate. The reminder
itself neither bypasses that gate nor promises immediate closeability.

Tests cover both provider size readings, files larger than the bounded tail,
cache invalidation on growth, missing records, the info transport, threshold
boundaries, unknown/future activity, newer receipts, replaced conversations and
machine roles. Existing milestone and Board transfer tests cover the reused
ownership and receiver-read boundary. Browser evidence must distinguish real
provider work from synthetic provider fixtures.


The opt-in Chrome/broker integration runs after `npm run build` in `web`:

```sh
CLAWDLINE_ROTATION_BROWSER=1 go test ./internal/app/orchestrator -run '^TestRotationConsoleUsesRealMilestoneTransfer$' -count=1 -v
```

It uses synthetic provider ports and an isolated real store. A Console press
precedes the real broker's summary publication, receiver package read, durable
receiver binding, Board item transfer and source closeability projection. It
makes no model calls. `CLAWDLINE_SHOTS` retains five desktop/mobile screenshots;
their synthetic provider text is labelled on screen. This does not establish
that a particular live model will write a complete summary on every attempt.
