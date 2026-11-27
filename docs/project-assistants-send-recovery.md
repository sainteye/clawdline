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
| The write may have crossed | network loss, a non-OK reply, an abort or grant change *after* the transport was invoked | `pa_outcome_unknown`; the original message stays in the transcript with a recovery action |
| The source returned a receipt, then a later check failed | the outer `authorized()` post-check | The source's receipt is kept as the outcome |
| The write answer has no valid receipt for this command | response shape and command identity checks in Chat | Keeps the original intent as `pa_outcome_unknown`, including the raw answer; only the person can choose a retry |

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
row says **Received by the Bot’s computer** (「主機已收到訊息」); a read failure keeps both that row
and its typed error, including after reload. A new send can proceed after the
previous admission receipt arrives. Until the corresponding message is visible,
its receipt stays in the journal. Plain text and quoted text use the known
conversation during a read interruption. The host resolves the quote and
checks its scope and memory access at admission. Attachments still require
their existing upload or execution-fence checks.

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

## Recovering from the original message

An uncertain send stays in the transcript while the composer can take a new
draft. Normal conversation sync looks up that send's original key through the
read-only `send-receipt` operation (source API 112). The machine reads one
receipt and its linked user turn in the same SQLite snapshot and checks Bot,
Project, and conversation identity. A matching receipt reattaches the bubble
to the user message. A missing receipt leaves the outcome unconfirmed.

**Retry sending** sends the original payload with the original command key.
**Restore to composer** brings back text, quote and available attachments. It
appears when attachments are missing or the original payload needs editing.
A separate draft is kept and can be switched back into the composer. The person
can reattach missing files or explicitly continue without them.

A recovery edit competes for the original command key. Its separate attempt ID
correlates the Cloud response and identifies the winning variant on lookup. The
serialized admission and SQLite command receipt allow only one variant to
create a turn. If the original arrives first, the transcript shows the original
and the editor keeps the amended text. A receipt proves admission, regardless
of how the agent's run ended.

The local journal is written before sending. Text, quote, attachment counts and
upload references stay in the existing account-scoped journal. Picture data and
files use browser IndexedDB, scoped by account, Bot, conversation, and attempt.
The console removes those bytes when the corresponding message is observed or
the recovery attempt is replaced. Legacy records with no attachment bytes
still surface the restoration action. Sources below API 112 can retry the
original request using its existing command key.

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

- `TestProjectAssistantSendRecoveryHasOneWinner` exercises the authenticated
  HTTP source, SQLite, and Cloud receipt read. It tests both request orders
  and concurrent original and edited sends with the same command key.
- `web/console/tools/project-assistants-send-recovery.e2e.mjs` drives the live
  Chat and source client in a browser against a synthetic source. The matrix
  covers automatic reconciliation after a lost reply, intact picture retry
  after reload, restoration of a message with missing bytes, preservation of
  both drafts when the original wins, and zero full Bot exports. The existing
  `--read-recovery` and `--legacy` modes cover interrupted reads and drafts
  that predate command IDs.
- `web/console/src/cloud/project-assistants.test.ts` exercises refusals before
  transport, authority changes, lost responses, and relay refusals.

Permission answers use the separate host-owned permission flow. A pending
card keeps its typed draft and shows a refusal or uncertain outcome on the
card. The chat and other permission cards stay available. A new press reads
the live offer before attempting a write, and the source accepts an answer
only while the offer is pending. Browser storage is local to that account
and browser on the device.
