<!-- clawdline-doc: kind=record audience=both -->
# Cloud operations reference

For maintainers tracing the 2026-09 operation mapping, this record comes from the [Cloud wire specification](../cloud-wire.md).
The current operation catalog in code is authoritative.

### 10.3 27 種操作

`Sources/CloudAppBridge.swift` 的 `case "…"`。寫入類（:3107-3465）：
`send`、`answer`/`key`、`start`、`resume`、`end`、`focus`、`shell-kill`、`board-command`、
`timeline-command`、`schedule-create`/`schedule-update`、`schedule-delete`/`schedule-run`、
`snippet-create`/`snippet-update`、`snippet-delete`、`snippet-order`、
`schedule-webhook-bind-v1`、`push-subscribe`、`push-unsubscribe`、`push-test`、`voice`、
`diagnostics.report`、`diagnostics.events`、`dispatch`。

唯讀類（:3828-4090）：`transcript`、`info`、`agent`、`shell`、`skills`、`git`、`screen`、`image`、
`documents`、`document`、`board`、`board.items`、`timeline`、`places`、
`project-worktree-lifecycle`(-refresh)、`past-sessions`、`schedules`、`snippets`、`schedule`、
`push-key`。

Go 版目前有對應本機功能的約 7 種（`docs/remote.md`）。**這一波（地基）一種都不做**；第三階段開始接，
逐項在[實作紀錄 §10.5](cloud-wire-implementation-2026-09.md)。**數字是量出來的，不要抄**：`cloudops.Vocabulary()` 與 `Implemented()` 都是從 catalog 推
出來的，2026-09-20 量到詞彙 37 個字、接上 28 個、其餘回具名的拒絕（8 個 `unknown_command`、
`dispatch` 回 `cloud_dispatch_unpinned`），`Divergences()` 5 筆。

As of 2026-09-23, the hosted Work v2 console carries explicit words. The read words are
`work.v2.item`, `work.v2.items`, `work.v2.search`, `work.v2.proposals`,
`work.v2.session-todos`, and the binary `work.v2.image`; the person-only mutation words are
`work.v2.create`, `work.v2.edit`, `work.v2.assign`, `work.v2.persona-suggestion`, `work.v2.remind`, `work.v2.cancel`,
`work.v2.image-create`, `work.v2.image-delete`, `work.v2.proposal-resolve`,
`work.v2.todo-create`, `work.v2.todo-image-create`, and `work.v2.todo-action`. Each mutation is
routed with the paired-device actor marker and the viewer's
request id as its idempotency key. The bridge therefore cannot accidentally exercise an Agent-only
or machine-only authority merely because Cloud execution happens through the daemon's in-process
router. The console's carried-word list and the daemon vocabulary are checked together by tests.

As of 2026-09-26 an eighteenth, `work.v2.complete`, carries the person's manual completion the same
way `work.v2.cancel` does: `{type, session, request, id, item}` routed to
`POST /v1/work/v2/items/<id>/complete` with the paired-device marker, `item` being
`{expected_version, note?}`.

As of 2026-10-02, `work.v2.seen` carries the person's read receipt for an item's deploying or
done phase: `{type, session, request, id, item:{phase}}` routed to
`POST /v1/work/v2/items/<id>/seen` with the paired-device marker, `phase` being `deploying` or
`done`. The bridge passes `item` through unjudged; the local route checks the phase and that the
actor is a person or device, and records which occurrence of that phase was seen.

As of 2026-09-28, `work.v2.persona-suggestion` carries the explicit AI role-classification press as
`{type, session, request, id, item:{expected_version}}` to
`POST /v1/work/v2/items/<id>/persona-suggestion`. The request id remains its Idempotency-Key, and
the paired-device actor marker means the machine applies the same send permission as a browser on
the machine. The explicit press needs no separate Board AI consent. The item text is read by the
local route, never placed in the Cloud command envelope.

Also as of 2026-09-28, the hosted Settings page carries the two machine-wide model defaults through
two narrow words: the read-only `default-models` routes to `GET /v1/settings/default-models`, and
`default-models-update` carries `{changes}` to that route's POST under the viewer request id and
paired-device actor. Both the Cloud decoder and the local route accept only
`codex_default_model` and `claude_default_model`; the full `/v1/settings` snapshot never crosses the
relay. The read answer also includes the same provider model catalog the daemon uses for its model
picker, so the hosted page offers choices instead of accepting arbitrary text. Reading needs only
the paired line. Saving is a command, so it also needs the machine's
Cloud-command switch and the paired device's Send capability.

As of 2026-09-29, that page also carries the two future-assignment gate defaults through two
narrow words: `work-gate-settings` reads `GET /v1/settings/work-gates`, and
`work-gate-settings-update` carries `{changes}` to its POST. The Cloud decoder and local route
accept only `planning_gate` and `verify_gate`, both boolean or null; neither the full settings
snapshot nor another settings key crosses. Reading needs the paired line. Saving additionally
needs the machine's Cloud-command switch and the paired device's Send capability. The screen says
that a successful assignment captures the pair, so changing it never looks retroactive.

As of 2026-10-07, the Settings page's **立即更新** crosses as `update-apply`, which carries nothing
but its request id to `POST /v1/update/apply` with body `{}`: a phone can start the update of the
newest release of the machine's channel, but cannot name a version or force one. It is a command,
so it needs the machine's Cloud-command switch and the paired device's Send capability. Its
progress is read through the existing `update` word.

Also as of 2026-09-28, `board-command` carries the Settings page's Board mode command as
`{command}` to `POST /v1/board`; the AI-consent form remains accepted for older clients but is no
longer shown or required. The command's own `requestId` is both the Cloud
request id and the local route's Idempotency-Key, so retrying an uncertain save cannot apply it
twice. It crosses as the paired device, preserving the local route's Send and Admin checks; item
writes remain separate Work v2 commands.

As of 2026-09-26 the token bill crosses too, as three read words: `usage.session`, `usage.task` and
`usage.item`, each `{type, session, request, id}` on the machine reply channel and routed to
`GET /v1/usage/{sessions,tasks,items}/<id>`. The id is refused before the route when the route
itself would refuse it (letters, digits, `-`, `_`, `.`, at most 200, never `..`); everything else —
a bill's `not_yet_read` or `transcript_missing` reason inside a 200, an unknown id's 404 — is the
route's own answer. These rows are not in the table below, which is a measurement of 2026-09-18
against the running daemon; these words were tested against the bridge, not measured there.

Also as of 2026-09-26, things waiting to be verified (docs/verifications.md) cross as seven words:
`verification.list` and `verification.get` are machine reads like the bill's; `verification.create`,
`verification.note`, `verification.criterion`, `verification.close` and `verification.delete` are
commands carrying the paired-device marker, each with the route's JSON body whole under
`verification` (at most 64 KiB), the id checked before the route (letters, digits and `-`, at most
64), a criterion's index an integer 0–11, and delete's `force` a required boolean that becomes
`?force=1`. There is no word that deletes more than one record. Not in the measured table either.
