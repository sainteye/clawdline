# A decision a Bot needs from the person during a Bot-to-Bot request (2026-11-23)

Status: investigated and proposed 2026-11-23; direction D has since been built.
§2 describes the code on that date, and its file and line citations are not
kept current. The rest of the page is kept as written. What shipped:

- Bot tools `ask_person`, `list_questions` and `withdraw_question`
  (`internal/adapters/projectassistantacp/mcp.go`). A decision offer
  (`internal/domain/projectassistant/decision.go`) is shown as a card in the
  owner Bot's main chat (`web/console/src/project-assistants/DecisionCard.tsx`);
  the exchange records `needs_person`, and `ExchangePanel` shows that state.
  A withdrawn question returns its reason to the requesting Bot as the reply.
- A `send_message` from a `bot_request` turn is no longer refused: it is posted
  as a peer note in the owner's chat (`postPeerNote` in
  `internal/app/projectassistant/runtime.go`).
- The person's answer quotes the request and the Bot's question, not the
  exchange's reply (`DecisionPrompt`).
- An offer has a fixed 24-hour lifetime (`MaxDecisionOfferSeconds`), and a
  waiting exchange is completed when its offer passes it: the exchange expires
  with the offer, not the other way round. A revoked native binding does not
  withdraw the offer; a hidden Bot refuses the answer (`CheckDecision`).
- In Bot A's chat the pointer is a waiting line plus a separate go button. In
  Bot B's chat, a card whose exchange capsule is not in the loaded timeline
  stays in the permission area instead of under the capsule.
- An answered decision can back a Board item when the `bot_decision_items`
  setting is on (`internal/transport/http/work_v2_bot_decision.go`), although
  §5 recommends no switch for the card itself.

## 1. What the person saw

Bot A (a requesting Bot) used `message_bot` to ask Bot B (an ordinary Bot,
not a team manager) to coordinate a dev rollout. Bot B finished with a reply
that said, in effect, "the only thing left is for the person to authorize this
in this turn." That reply went only to Bot A's chat. In Bot B's own chat the
person saw two "Message from Bot A ›" capsules and "Completed" — no prompt, no
button, no suggested reply. Bot A could only relay the question and ask the
person to go to Bot B and type an authorization by hand. A similar report was
made earlier about another Bot.

The person classified this as a product defect: after two Bots talk, the person
has no way to act on what they said.

## 2. Root cause, with file and line

### 2.1 Where a peer request runs

`AdmitExchange` chooses between two recipients
(`internal/adapters/projectassistantstore/collaboration.go:288`):

- A team-manager recipient (`ManageTeam`) gets a queued turn in its own main
  conversation.
- Every other recipient — the reported case, whose `request_worker_id` was
  `thread-…` — gets a **background worker**:
  `x.RequestWorkerID = backgroundWorkerID(x.ID)` (`collaboration.go:303`),
  `backgroundWorkerID` returns `"thread-" + id`
  (`internal/adapters/projectassistantstore/background_tasks.go:222`).

The worker is a separate hidden aggregate with its own conversation and native
binding (`background_tasks.go:224-251`): `Hidden=true, ManageTeam=false,
Lead=false` (`:230`), `BackgroundOwner` set to Bot B (`:243`), and the turn's
origin is `bot_request` (`:239`). Bot B's main chat receives only a structured
notice for the request (`collaboration.go:304-311`).

### 2.2 Where the reply goes

`FinishExchange` (`collaboration.go:392-541`):

- The reply text becomes `x.Reply` and is queued as a **`bot_reply` turn in Bot
  A's chat** (`:462-487`).
- In the recipient, completion is deliberately "a trace and structured notice,
  not an ordinary answer" (`:491-499`). For a worker the reply is written as an
  assistant message **inside the hidden worker conversation**
  (`:505-506`), and Bot B's main chat gets one more notice whose text is the
  *request*, not the reply (`:529`).

So the only place the question to the person ever appears is Bot A's chat, as
data inside Bot A's next turn (`internal/app/projectassistant/collaboration.go:251`).

### 2.3 What the worker turn may do to reach the person

The prompt for a peer request says "Send your answer as the final response to
the requesting Bot. Do not send chat messages, publish files/artifacts, schedule
follow-up work or contact another Bot" (`internal/app/projectassistant/collaboration.go:249`).
The host enforces it:

| Channel | Result in a `bot_request` turn | Where |
|---|---|---|
| `send_message` | refused `bot_request_tools` | `internal/app/projectassistant/runtime.go:1835` |
| `send_file` / `publish_artifact` | refused | `internal/adapters/projectassistanthost/provider.go:770` |
| routines / follow-up | refused except list | `provider.go:732` |
| `dispatch_work` | refused `peer_dispatch` | `provider.go:1023` |
| network, memory edit, react | refused | `provider.go:623`, `:927`, `:588` |
| `message_bot` (forward or ask back) | refused | `internal/domain/projectassistant/collaboration.go:116` |
| `manage_bots` | only in a direct user turn | `provider.go:525` |
| completion notification | suppressed for `bot_request` | `internal/domain/projectassistant/types.go:525` |
| native tool permission card | **allowed**; offer lands in the worker | `internal/adapters/projectassistantacp/client.go:920` |

There is no Bot tool for "ask the person a question with suggested replies"
(the tool list in `internal/adapters/projectassistantacp/mcp.go` has none), and
the Clawdline answer Note belongs to Sessions, not Bots. The only interactive
surface a worker can raise is a native permission card, and only when the
agent asks to run a tool; it lives in the worker conversation and is surfaced
through the background capsule's `waiting_permission` state
(`web/console/src/project-assistants/BackgroundTasks.tsx:74`). A reply that
merely *says* "please authorize" raises nothing.

This is by design: the owned-MCP decision keeps "credential entry and native
permission answers on the person's own surface"
(`docs/project-assistants-owned-mcp.md:76-77`). The defect is that no such
surface is created for a decision that originates in peer work.

### 2.4 When the person types in Bot B's main chat

A message the person types is a `user` turn (`runtime.go:516`), and
`DirectUserTurn` accepts it (`internal/domain/projectassistant/lead.go:36`), so
it can authorize. But it runs in Bot B's **main** native session, which never
saw the worker's conversation; and the notices carrying the exchange are
excluded from context carried between sessions
(`internal/app/projectassistant/session_handoff.go:163`, `m.Exchange == nil`).
The person must restate everything, and the original exchange is already
`completed`, so Bot A is not told of the outcome.

### 2.5 What the capsule opens

The capsule opens `ExchangePanel`
(`web/console/src/project-assistants/ExchangePanel.tsx:64-91`): a read-only
dialog with the request, the reply and the state. There is no composer and no
action. A background thread view also disables input (`Chat.tsx:556`, `:673`).

### 2.6 Summary

Three things combine: (1) peer work runs in a hidden worker whose answer goes
only to the requester; (2) the worker has no tool to ask the person, and its
prompt forbids chat; (3) neither the capsule nor the target's main session
carries the exchange in an actionable form.

## 3. Directions

Mockups follow the current Bots dark styling (`--pa-bg #080808`,
`--pa-card #141414`, `.pa-exchange-entry` capsule) at 1280×760 and 390×844.
The images are in the task's artifacts (`shots/<variant>-desktop.png`,
`shots/<variant>-mobile.png`; `now` is the current screen). They are static
HTML, not the running console.

Common mechanism for A, B and D: add one Bot tool, `ask_person`, available to
peer-request turns (and, for consistency, ordinary turns). Arguments: a question
and 1–3 suggested replies. The host stores a durable **decision offer** in the
same namespace as permission offers (`PermissionOffer` with a `decision` kind)
on the *owner* Bot's main conversation, ends the worker turn as
`completed` with outcome `needs_person`, and tells the requester so.

### A — The target Bot's main chat shows "Needs your decision"

Bot B's main chat shows a card under the capsule with the question, the
suggested replies and "Reply yourself…". The roster shows a badge. A choice is
committed as a `user` message in Bot B's main chat (a real direct user turn),
prefixed with the exchange's request and reply as quoted context, and the
outcome is forwarded to Bot A as a correlated notice.

- Pros: the decision is made where the authority lives; the resulting turn is
  an ordinary direct user turn, so no new authorization rule is needed. Works
  on phone without new navigation.
- Cons: the person may be in Bot A's chat and not notice Bot B's badge.

### B — The requesting Bot relays an actionable card

Bot A's chat shows "Bot B needs your decision" with the same options. Pressing
one sends the person's message into Bot B's main chat.

- Pros: appears where the person already is.
- Cons: the card sits in a chat whose Bot did not ask, so the person must read
  attribution carefully; Bot A's own model text appears around it and can frame
  the choice. Cross-chat writes from a card in another chat need a new
  authorization path (`Service.Answer` checks `c.Key == answer.Fence.Key`,
  `internal/app/projectassistant/service.go:666`).

### C — The capsule opens with a reply box

`ExchangePanel` gains quick replies and an input "Reply to Bot B as yourself".
Sending posts a `user` message in Bot B's main chat.

- Pros: smallest UI change; reuses an existing surface.
- Cons: nothing tells the person a decision is waiting — the capsule looks the
  same as any completed exchange. It does not fix the reported discovery
  problem by itself.

### D (recommended) — A, plus a pointer in the requester's chat

As A, and Bot A's chat shows a non-actionable row "Bot B is waiting for your
decision · Go to Bot B ›". Bot A pauses instead of asking the person to type an
authorization.

- Pros: the person sees it wherever they are; the only actionable control is in
  the chat of the Bot that asked, so attribution is unambiguous; the pointer
  carries no authority.
- Cons: two UI pieces; one extra tap from Bot A's chat.

## 4. Security: how a press is known to be the person's

1. **Only the console answers.** A decision is answered through the same route
   as permission answers, `Service.Answer` → `authorize(ctx, key, true)` and
   `authorizeEffect` (`service.go:657-675`), which requires the person's
   authenticated console source. No Bot MCP tool answers an offer; the new
   `ask_person` only creates one. A test must prove a Bot-side call to answer
   is refused.
2. **The authority is the press, not the text.** Suggested replies are
   Bot-authored data. The card labels them as suggestions, shows the full text
   that will be sent, and the result is the person's own `user` message — the
   same authority as if they had typed it. Nothing executes on Bot A's or Bot
   B's word.
3. **No cross-Bot authority.** The answer always commits to the Bot that asked
   (Bot B). The requester's pointer (D) is navigation only. B's cross-chat
   answer path is not needed.
4. **Single use and expiry.** The offer is answered once (existing
   pending→answered transition), expires with the exchange, and is withdrawn if
   Bot B is deleted, hidden or its binding is revoked.
5. **Hop and loop limits.** `ask_person` does not create a new exchange, so
   `MaxCollaborationHops` and the outstanding caps are unchanged. Offers per Bot
   are bounded by a new entry in `internal/domain/capacity`.
6. **Prompt framing.** The resulting turn's prompt marks the quoted exchange as
   data and the person's chosen reply as the instruction, as
   `collaborationPrompt` does today for replies.

## 5. Setting switch

Recommendation: **no switch.** This restores a missing path for a decision the
product already expects the person to make; without it the work is silently
stuck. The new card appears only when a Bot explicitly asks, and nothing runs
until the person presses. A switch that defaulted off would keep the defect for
everyone, and one that defaulted on would only add a way to hide decisions.
If a control is wanted later, it belongs to per-Bot notification settings
(whether the badge also notifies), not to whether the card exists.

## 6. Items for confirmation

1. Add the Bot tool `ask_person(question, suggested_replies[1..3])`, available
   in peer-request and ordinary turns; it creates one decision offer and does
   not send chat.
2. In a peer-request worker, `ask_person` places the offer on the **owner Bot's
   main chat**, not in the hidden worker.
3. The worker turn ends; the exchange records `needs_person` and Bot A is told
   "waiting for the person" instead of receiving an instruction to relay.
4. Bot B's main chat shows a "Needs your decision" card (permission-card
   styling) under the exchange capsule, with suggested replies and "Reply
   yourself…"; the roster row shows a badge.
5. Pressing a reply commits a `user` message in Bot B's main chat, with the
   exchange request and reply quoted as context; it is a direct user turn.
6. When that turn finishes, its reply is forwarded to Bot A as a correlated
   notice so Bot A can continue.
7. Bot A's chat shows a non-actionable "Bot B is waiting for your decision ·
   Go to Bot B ›" row that opens Bot B's chat at the card.
8. Answers go only through the console's authorized answer route; no Bot tool
   can answer an offer; tests prove a Bot-side answer is refused.
9. Offers are single-use, expire with the exchange, are withdrawn when Bot B is
   deleted, hidden or revoked, and are bounded in `internal/domain/capacity`.
10. No setting switch; English and Taiwan Traditional Chinese copy ship
    together; the change is tested on desktop and phone widths.

## 7. Deliberately left out

- C's reply box in `ExchangePanel` is not part of the recommendation; it could
  follow later as a convenience once D exists.
- Team-manager recipients already run in their main chat; they gain the same
  card through item 1 without the worker hand-off.
- Native tool permission cards raised inside a worker are unchanged here;
  whether they should also surface in the owner's main chat is a separate
  question.
