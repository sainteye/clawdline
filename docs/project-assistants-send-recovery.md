# Recovering an uncertain Bot send

A Bot message can leave the console without the console learning what
happened to it: the response is lost, the connection drops, or the grant
changes while the request is in flight. Until 2026-11-16 every such case held
the message in a disabled composer with "Outcome unknown; no automatic retry"
and offered no way out short of reloading the page. Some of those cases had
provably sent nothing at all.

## What the console now distinguishes

| Evidence | Where it comes from | What the chat does |
|---|---|---|
| Refused before any transport | `ProjectAssistantSourceClient.request` pre-checks (grant, abort, scope), `authorized()` pre-check, the Cloud relay's checks before its encrypted request | Releases the hold; the draft returns editable; the refusal code is shown |
| The source answered with a typed refusal | the response body | Releases the hold, as before |
| The write may have crossed | network loss, a non-OK reply, an abort or grant change *after* the transport was invoked | `pa_outcome_unknown`; the hold and the locked draft stay |
| The source returned a receipt, then a later check failed | the outer `authorized()` post-check | The source's receipt is kept as the outcome |

The "not sent" mark is a non-enumerable property set by `notSent()` and read
by `provablyNotSent()` (`web/console/src/project-assistants/client.ts`), so a
failure's envelope is unchanged. Before this change a grant that went offline
or unpaired *after* a send crossed the transport surfaced as `pa_offline` /
`pa_unpaired`, which the chat treated as definitive and released — a possible
duplicate if the person sent again. Writes now report outcome unknown in that
case and keep the transport's evidence in `AssistantTransportFailure.response`.

## The explicit check

While a send is held, the footer explains that the message stays locked and is
never resent, and offers **Check with the machine**. One click is one
authenticated read of the existing export route — never a poll, retry or
acknowledgement. `inspectHeldSend` (`view-state.ts`) trusts only:

- an export whose Bot, Project and conversation match the chat's captured scope;
- exactly one receipt with the same Bot, operation `send` and idempotency key;
- for an admitted receipt (`accepted`, `executing`, `delivered`, `observed`,
  `read`, or a source `unknown` whose turn exists), the turn named by
  `resource_id` with the same idempotency key in the same conversation.

Outcomes: *delivered* reattaches — the hold clears, the restored copy leaves
the composer, and the chat refreshes onto the existing turn; *failed* releases
the draft for editing; *unknown*, *missing* and anything invalid keep the hold.
A missing receipt is never read as non-delivery: a Cloud request may still
arrive. A held permission answer is host-owned and is never inspected as a
message send.

## Evidence

- `web/console/src/cloud/project-assistants.test.ts` — the `send recovery:`
  tests: pre-transport refusals, authority changes after transport, lost
  responses, Cloud relay pre-send refusals.
- `web/console/src/project-assistants/view-state.test.ts` — the inspection
  matrix: same key on a different Bot, wrong operation, Project/conversation
  mismatch, duplicate receipts, missing turn, failed, unknown, delivered.
- `web/console/tools/project-assistants-send-recovery.e2e.mjs`
  (`npm run e2e:bots-send-recovery`) — the production Chat and source client
  in a real browser through `clawdline browser`, with a synthetic in-page
  source. Against 8378f470f the pre-transport step shows the unknown banner
  with a locked draft and zero transport requests; with this change all five
  steps pass: refusal returns the draft, an explicit send works, a lost
  response reattaches after one export read with no second send (also after
  leaving and reopening the chat), a missing receipt stays held, and a held
  draft cannot be edited or resent.

## Not covered

- The hold lives in host memory: a full page reload drops it, and the draft
  restored to storage becomes editable again. Persisting the hold would need
  its own storage bound and image handling.
- A send whose receipt never appears stays held until reload; there is no
  separate "continue with a new message" control.
- The Cloud relay's own request layer cannot yet say whether an encrypted
  request left the page, so a relay failure after it is invoked stays unknown.
- Translations other than English and Taiwan Traditional Chinese still carry
  the earlier "check the Bot list before trying again" wording for
  `failUnknown` and fall back to English for the new keys.
