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

## Surviving a reload

The hold is journaled before the transport is invoked, under
`clawdline.bot-held.<draft key>` in the storage the console already uses for
drafts (the draft key is account, machine and Bot; the record carries the
send's fence, so its Project and conversation). `HeldOutcomes`
(`view-state.ts`) keeps, per account/Bot, the `active` send in the box and the
sends the person set `aside`. A record holds the original command (same
idempotency key), text, quote id, `upload_ids`, file names, the number of
pictures and the source's `unknown` receipt, if any. It never holds picture
data, tokens or grants. Unreadable originals are preserved as opaque local
bytes when the person continues; they are never interpreted as commands. A full reload reads a valid record back: the box shows the held
text and quote, locked, with **Check with the machine**; nothing is read or
sent until the person clicks. Pictures cannot come back, so the held context
says how many were attached and that they may already have been sent.

The journal retains every set-aside send independently of the source's pending
execution slots. It adds no message-count ceiling. Text still follows the
source's registered 32 KiB message bound (`bots.message_bytes`, pa.MaxMessageBytes).

Failures are never silent. A write that fails (quota, a text beyond the bound)
keeps the hold in this tab and shows that reloading would lose the lock. A
record that cannot be read back — malformed, another Bot's, carrying picture
data — keeps the box locked with an alert, and the raw record is left in place
until the person acts. A permission answer is host-owned: it is not journaled
and is never released by the controls below; a reload still forgets it, as
before.

## Writing a new message

While a send is held, **Write a new message** is the person's explicit choice
to continue. The held send moves aside, still visible with its text and its
own **Check this earlier message** control (the same single export read); the
box starts empty; the next send gets a new command identity. Nothing is copied
from the old draft or submitted. A later check settles an aside send only on a
delivered or recorded-failed receipt; *missing* and *unknown* keep it. For an unreadable record, the same control preserves its original bytes in
the journal before emptying the box, and leaves a dismissible note. Dismissing
the note does not erase those bytes. If storage cannot preserve the original,
the record and lock remain. There is no
time or idle heuristic anywhere.

## Compared with Codync

Codync's desktop renderer (`apps/desktop/src/renderer/store/bot-store.ts` at
9f67eb09) has no unknown state: any send failure marks the local row
`failed`, failed rows are cached across launches (`bot-mirror.ts`
`saveCache`), and **Retry** resends the same text with the same `clientNonce`,
re-uploading files, relying on the host to skip a nonce it already has
(`host/src/remote/relay/delivery.rs`). A send still in flight when the page
goes away is not cached. Clawdline keeps its stronger fence instead of copying
that retry: an uncertain send is never resent, only checked against its exact
receipt, and continuing means a new, empty message.

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
  draft cannot be edited or resent. Against f74ef215c the extended script
  fails its sixth step: after a full reload the box is editable again with
  the uncertain text. With the journal all ten steps pass, adding: a reload
  keeps the send held with no read or send; **Write a new message** gives an
  empty box and exactly one new send under a new key while the earlier one
  stays visible; a lost response survives a reload and reattaches after one
  read while the earlier send's own check reports it still missing; another
  account sees none of it; a revoked grant reads and sends nothing.
- `view-state.test.ts` `send recovery journal:` tests — identity, text, quote
  and references across a reload without picture data; account and source
  isolation; permission answers; corruption and write failure; preserving unreadable bytes and retaining history
  independently of pending execution capacity.

## Not covered

- A held permission answer still lives only in host memory.
- The Cloud relay's own request layer cannot yet say whether an encrypted
  request left the page, so a relay failure after it is invoked stays unknown.
- The journal is per browser storage: another device or browser does not see
  this device's held sends.
- The browser script drives a tab opened from the Session's shell
  (`SEND_RECOVERY_URL`/`SEND_RECOVERY_TAB`); a tab the script opens itself was
  refused as another Session's (`browser_tab_other_session`).
- Translations other than English and Taiwan Traditional Chinese still carry
  the earlier "check the Bot list before trying again" wording for
  `failUnknown` and fall back to English for the new keys.
