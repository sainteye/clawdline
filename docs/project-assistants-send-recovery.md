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
| The write answer has no valid receipt for this command | response shape and command identity checks in Chat | Keeps the original intent as `pa_outcome_unknown`, including the raw answer; never resends |

The "not sent" mark is a non-enumerable property set by `notSent()` and read
by `provablyNotSent()` (`web/console/src/project-assistants/client.ts`), so a
failure's envelope is unchanged. Before this change a grant that went offline
or unpaired *after* a send crossed the transport surfaced as `pa_offline` /
`pa_unpaired`, which the chat treated as definitive and released — a possible
duplicate if the person sent again. Writes now report outcome unknown in that
case and keep the transport's evidence in `AssistantTransportFailure.response`.

## Admission and visible results

An accepted receipt does not prove that the conversation has been observed.
Chat retains the accepted command and text in the existing journal until a
user message in the same Bot, Project and conversation names the receipt's
turn. Another message with identical text cannot replace it. Until then the
row says **Sent · updating conversation…**; a read failure keeps both that row
and its typed error, including after reload. The composer may hold the next
draft, but another send waits until the accepted message is observed.

An explicit send starts a fresh snapshot read even if the browser reports a
hidden document. Foreground and explicit demands coalesce behind an active
read instead of disappearing. An older snapshot whose frontier precedes
admission cannot end observation of the newer turn. Generation changes still
require a validated recovery snapshot; they do not erase the accepted text.
A permission/trace collection crossing that boundary propagates its typed
`pa_stale_generation` before recovery locks and aborts its other reads. It
must not abort itself first and swallow the boundary as external cancellation.
Initial entry and subsequent refreshes share one coordinator, which permits
one recovery snapshot after a validated snapshot's metadata changes fence.
Another failure stays typed and visible; it does not start a recovery loop.

Only that accepted turn may continue using the existing event cadence while
hidden. A terminal event requests one final consistent snapshot before this
observation stops, so a terminal event arriving ahead of a message does not
strand the answer behind the event cursor. Leaving the Chat, pagehide or
revocation stops its reads. Ordinary hidden polling remains paused. None of
this changes execution fences, permissions, transport deadlines, retry policy
or read acknowledgements: fetching a hidden result does not mark it read.

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

Outcomes: *delivered* reattaches — the restored copy leaves the composer and
the chat refreshes onto the existing turn. A confirmed admitted receipt keeps
its pending row and journal until that turn's message is observed, including
when the following read fails; *failed* releases
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
pictures and the source's `unknown` or validated admitted receipt, if any. It never holds picture
data, tokens or grants. Unreadable originals are preserved as opaque local
bytes when the person continues; they are never interpreted as commands. An admitted record restores its pending row until its canonical message is read. For an uncertain record, a full reload reads a valid record back: the box shows the held
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
from the old draft or submitted. This action cancels an in-progress check of
that send. A later check settles an aside send only on a delivered or
recorded-failed receipt. The person can also remove its local note, including
after a failed check, without contacting the source or clearing the new draft.
For an unreadable record, the same control preserves its original bytes in
the journal before emptying the box, and leaves a dismissible note. Dismissing
the note does not erase those bytes. If storage cannot preserve the original,
the record and lock remain. There is no
time or idle heuristic anywhere.

## Legacy plain drafts without a command identity

The future-send journal cannot identify a draft saved by an earlier version.
On entry, Chat captures the nonempty plain draft and its quote before any
asynchronous source read. If the first validated, scope-matching snapshot
reports an unknown turn and there is no held command, the draft is kept out
of the send path while the console preserves it. Body equality is never
delivery evidence.

`UnverifiedDrafts` stores the original text, quote, and full Bot/Project/
conversation scope under `clawdline.bot-unverified.<draft key>`. The transcript
shows the original in a single message bubble with **Old draft · Delivery
unconfirmed** below it. The normal composer can be used as soon as the console
has saved the original and cleared the matching plain-draft slot. Any newer
draft in memory or on disk is retained. If storage refuses, the old draft stays
in the transcript, the composer stays locked, and **Save old draft** retries
the local save. An undecodable record remains untouched. The original survives
reload independently from subsequent drafts.

The old draft has no command or receipt to check. Saving it performs no
export, query, send, replay, cancel, or acknowledgement. A subsequent explicit
send receives a fresh UUID and still passes the source's admission policy.

## Compared with Codync

Codync iOS at `e7148ed2f89eef94f50d75a19cb55aad848d97f0` transfers a
composer draft to a local user row with a `clientNonce` and clears the composer
(`BotStore+Sending.swift`:47–101). The status and actions sit under that
message bubble (`Thread/ChatRows.swift`:152–195). An explicit resend of a
local message reuses its nonce (`BotStore+Sending.swift`:273–285) and the host
returns an existing message for that nonce (`host/src/api/bots.rs`:253–268).
The iOS mirror keeps pending local rows when it saves (`BotStore+Cache.swift`:42–66).
Clawdline follows the chat row and independent composer model for old drafts.
Its legacy drafts predate the journal and have no nonce to reuse.

## Evidence

- `TestProjectAssistantChatObservationBrowser` injects hidden visibility into
  the actual Bots/Chat and captured source client, then crosses the closed
  Cloud decoder, authenticated HTTP Host and SQLite. A local protocol provider
  holds execution preflight until the first post-admission snapshot is captured,
  changes the binding generation before metadata reads, emits a tool trace and waits for the harness
  before finishing. No provider CLI, model API or production Bot is used.
  The old Chat loses its accepted row before any canonical read. The matrix
  covers English desktop and Traditional Chinese narrow widths, a busy older
  read and foreground demand, terminal-before-message recovery, a typed read
  failure across reload, leaving, revocation, unknown and malformed write
  answers without replay, and an explicit receipt check followed by a failed
  read. The outer encrypted carrier is simulated; this is
  not production pairing or UI acceptance. Run with
  `CLAWDLINE_CHAT_OBSERVATION_BROWSER_DIR=/task-owned/evidence-directory go test ./internal/transport/http -run '^TestProjectAssistantChatObservationBrowser$' -count=1 -v`.

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

The `--legacy` browser mode seeds synthetic historical source facts and a
saved plain draft without a held journal. The narrow scenario checks a single
copy in the scrollable transcript, an immediately empty composer, separate
drafts after reload, one explicit new UUID send, zero automatic exports, and
account isolation. Add `--refuse-storage` to fault the initial save, verify the
lock, and recover through the local save control. State tests inject failures
at each persistence stage, verify conversation scope and protect newer drafts.
The fixture uses a synthetic source and does not exercise native execution
or production Cloud admission.

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
