# Recovering context in a full Bot chat: Codync compared with Clawdline (2026-11-23)

Research only; no product code changed. The question: when a Bot's native context is nearly or entirely
full, what does Codync do to make room again, automatically or at the person's request, and what does
Clawdline do today?

Situation that prompted it: a Codex Bot in a Clawdline chat showing roughly 345K of a 475K window in use
(about 27% left, the amber meter level) during a turn that had been running for 28 minutes.

## Source and method

- Codync: `https://github.com/leepokai/Codync`, HEAD **`9dae3e2bec9b91a4a42f85a943eb3983f95d0503`**
  (2026-11-23, "Merge pull request #568 from leepokai/release/2.53.0"). All Codync links below are pinned to
  that commit.
- Searched with `git grep -i` for `compact`, `compaction`, `rollover`, `handoff`, `newSession`, `/new`,
  `/clear`, `usage_update`, `context window`, `ctxUsed`/`contextUsage` across `host/`, `apps/desktop/src`,
  `apps/ios`, `apps/android` and `docs/`.
- Clawdline: this repository at `884dc6354`.
- Nothing was run against a real Claude Code or Codex agent in this task. Statements about Codync's
  behaviour come from its source, tests and design document, not from observation.

## Codync: the design in one paragraph

"The harness writes the summary, because it knows the work; the host decides **when**"
([bot-memory.md#L407-L411](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/docs/design/bot-memory.md#L407-L411)). Codync never writes its own summaries. It either lets the agent compact itself, or sends the
agent's own `/compact` at a moment the host picks, and when summaries stop helping it rolls the chat over
into a fresh native session that starts from an editable handoff. The rules are pure functions in
`host/crates/memory/src/compaction.rs`; the Bot actor in `host/src/agent/bot/compaction.rs` records usage
and acts on the decisions.

## Codync: automatic mechanisms

### A1. The agent's own auto-compaction (default, "Auto")

- **Trigger:** the harness's internal limit when a turn fills the window. Codync's setting `Threshold::Off`
  is the default and is labelled "Auto (default)" in the apps
  ([threshold.rs#L17-L32](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/host/crates/memory/src/compaction/threshold.rs#L17-L32),
  [compaction-options.ts#L4-L8](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/apps/desktop/src/renderer/lib/compaction-options.ts#L4-L8)).
- **What happens:** nothing from the host. The design doc calls it "the safety net"
  ([bot-memory.md#L431](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/docs/design/bot-memory.md#L431)); it is the only mechanism that can act in the
  middle of a long turn, for every setting.
- **What Codync does afterwards (all settings):** it detects the compaction from ACP `compaction_update`
  completed ([events.rs#L78-L82](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/host/crates/agent/src/events.rs#L78-L82),
  [updates.rs#L140-L148](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/host/src/agent/bot/updates.rs#L140-L148)) or, for an agent that never sends one,
  from `usage_update.used` falling below half of a previous reading of at least 20K
  ([compaction.rs#L379-L394](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/host/src/agent/bot/compaction.rs#L379-L394)). Then `compacted()`
  ([compaction.rs#L163-L200](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/host/src/agent/bot/compaction.rs#L163-L200)) bumps the context epoch so the
  next turn gets freshly rendered instructions and memory, re-sends the `<bot-profile>` to harnesses that
  only got it in their first message, flushes memory "dreaming", stores the summary for a later handoff,
  and posts a chat notice.
- **What the person sees:** an info line "Earlier context was summarized to make room." with the harness's
  summary attached when there is one ([compaction.rs#L191-L199](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/host/src/agent/bot/compaction.rs#L191-L199)).
- **Claude Code vs Codex:** Claude's adapter carries the summary on a completed update, so the line can unfold
  to it. Codex's `compaction_update` has no summary, so its line has none and its handoff is built from the
  chat ([bot-memory.md#L38-L39](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/docs/design/bot-memory.md#L38-L39)). The design doc records that Codex keeps
  only about the newest 20K tokens of user messages verbatim through its own compaction
  ([bot-memory.md#L70-L71](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/docs/design/bot-memory.md#L70-L71)), which is why the profile is re-sent.

### A2. Host-timed compaction at the end of a turn ("Summarize long chats")

- **Trigger:** per-Bot setting `compaction`, only for Claude Code and Codex Bots (and a custom command that
  turned out to run one of their adapters), never group chats
  ([compaction.rs#L16-L24](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/host/src/agent/bot/compaction.rs#L16-L24)). After a turn in the Bot's own main
  chat (not rooms, routines or bot-to-bot requests, [#L98-L107](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/host/src/agent/bot/compaction.rs#L98-L107)),
  `at_turn_end` compacts when `used >= trigger`
  ([memory compaction.rs#L139-L151](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/host/crates/memory/src/compaction.rs#L139-L151)). Triggers
  ([threshold.rs#L96-L107](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/host/crates/memory/src/compaction/threshold.rs#L96-L107)):
  - Early: `min(0.75 × window, 200K)`.
  - Near the limit: `window − 40K` rounded down to 10K, never earlier than Early.
  - A size: a multiple of 10K, at least 50K, held to the window's maximum.
  - For a 475K window these are 200K, 430K, or the chosen size. Under Early the situation above would have
    compacted at a turn end well before 345K.
- **What happens:** the actor queues a hidden compaction turn
  ([compaction.rs#L257-L263](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/host/src/agent/bot/compaction.rs#L257-L263)). Before it runs it re-checks
  that it is still due, flushes memory, renders a fresh opening, then sends the agent's own command as a raw
  prompt ([#L325-L347](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/host/src/agent/bot/compaction.rs#L325-L347)). The compaction turn does not count as
  a conversation turn ([#L244-L248](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/host/src/agent/bot/compaction.rs#L244-L248)).
- **Keep-list:** a fixed instruction to keep the latest request and every "don't/不要" verbatim, exact
  paths/URLs/ids, decisions with reasons and open tasks
  ([memory compaction.rs#L176-L178](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/host/crates/memory/src/compaction.rs#L176-L178)).
- **Claude Code vs Codex:** Claude gets `/compact <keep-list>`; Codex gets a bare `/compact` because it
  ignores trailing text, and the keep-list goes in as `compact_prompt` in the Codex config at launch
  ([compaction.rs#L114-L121](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/host/src/agent/bot/compaction.rs#L114-L121),
  [session.rs#L196-L197](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/host/src/agent/bot/session.rs#L196-L197)).
- **What the person sees:** the same "summarized to make room" line, marked `byHost`.
- **Loop guard:** at most 3 host compactions per day; if the last one left at least 60% of the trigger in use,
  or two came within 5 turns, the decision becomes a rollover (A4) instead
  ([memory compaction.rs#L25-L30](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/host/crates/memory/src/compaction.rs#L25-L30),
  [#L130-L136](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/host/crates/memory/src/compaction.rs#L130-L136)).

### A3. Idle compaction before the prompt cache expires

- **Trigger:** only with a one-hour cache (Claude on a subscription; learned from cached-read ratios or
  guessed from whether an API key is set), context at least 100K (or the chosen trigger), between 55 and
  60 minutes after the last turn, once per idle period
  ([memory compaction.rs#L153-L164](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/host/crates/memory/src/compaction.rs#L153-L164),
  [compaction.rs#L83-L96](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/host/src/agent/bot/compaction.rs#L83-L96),
  [#L266-L283](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/host/src/agent/bot/compaction.rs#L266-L283)).
- **What happens / is seen:** same hidden `/compact` turn and notice as A2.
- **Claude Code vs Codex:** effectively Claude only; a five-minute cache never qualifies, which is what Codex
  and API-key Claude are assumed to have.

### A4. Rollover into a fresh session with an editable handoff card

- **Trigger:** not under Auto. Otherwise any of
  ([memory compaction.rs#L166-L174](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/host/crates/memory/src/compaction.rs#L166-L174),
  [handoff.rs#L32-L58](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/host/src/chat/handoff.rs#L32-L58)):
  - Cold: the person returns after the cache expired with a large context.
  - Away: an absence of 8 hours or more after 3 or more compactions.
  - Full: the loop guard fired.
- **What happens:** a draft card is posted to the chat as soon as the rollover is due
  ([handoff.rs#L69-L84](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/host/src/chat/handoff.rs#L69-L84)). The draft carries:
  - the harness's last summary;
  - the last 4 user messages verbatim, each cut at 2,000 characters;
  - the agent's last final reply;
  - the open plan;
  - a bookmark for `search_history`.

  ([handoff.rs#L86-L112](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/host/src/chat/handoff.rs#L86-L112),
  [memory compaction.rs#L180-L248](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/host/crates/memory/src/compaction.rs#L180-L248)). The person's next
  message drops the native session and starts a new one with the handoff placed before the message
  ([compaction.rs#L349-L368](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/host/src/agent/bot/compaction.rs#L349-L368)).
- **What the person sees:** the reason sentence, a "What it carries" disclosure, **Keep this conversation**,
  and an edit sheet (up to 20,000 characters) on desktop, iOS and Android
  ([HandoffNotice.tsx#L37-L80](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/apps/desktop/src/renderer/views/chat/HandoffNotice.tsx#L37-L80),
  [HandoffRows.swift#L27](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/apps/ios/Kit/Sources/CodyncUI/Chat/HandoffRows.swift#L27),
  [HandoffRows.kt#L62](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/apps/android/app/src/main/kotlin/com/codync/android/HandoffRows.kt#L62)). Keeping
  suppresses rollover until the next turn; changing the setting withdraws a card it no longer calls for
  ([compaction.rs#L285-L305](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/host/src/agent/bot/compaction.rs#L285-L305)).
- **Claude Code vs Codex:** identical, except a Codex handoff has no harness summary to include.

## Codync: manual mechanisms

### M1. The Summarize long chats setting itself

A Bot setting row with Auto (default), Early, Near the limit, and "Set a size" once the agent has reported its
window ([CompactionRow.tsx#L22](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/apps/desktop/src/renderer/views/bots/CompactionRow.tsx#L22),
[compaction-options.ts#L33-L42](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/apps/desktop/src/renderer/lib/compaction-options.ts#L33-L42),
[CompactionPicker.kt#L32-L46](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/apps/android/app/src/main/kotlin/com/codync/android/CompactionPicker.kt#L32-L46)).
Only Claude Code and Codex Bots show it; other agents get `compaction: null`
([sync.rs#L19-L21](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/host/src/hub/sync.rs#L19-L21)).

### M2. Typing the agent's own `/compact`

There is no Codync-owned "Compact now" button. A message that starts with a slash command the running agent
advertised (`available_commands_update`) goes to the agent exactly as typed, without Codync's additions
([turn.rs#L255-L261](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/host/src/agent/bot/turn.rs#L255-L261),
[commands.rs#L86-L90](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/host/src/agent/bot/commands.rs#L86-L90)); the apps list those commands in the composer's
slash menu ([slash-commands.test.ts#L31-L34](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/apps/desktop/src/renderer/views/chat/slash-commands.test.ts#L31-L34)).
The resulting `compaction_update` goes through A1's handling, so the notice and profile re-send follow.
Claude Code accepts `/compact <what to keep>`; Codex ignores trailing text.

### M3. New session

Bot menu item **New session** with confirmation "The conversation stays here, but the agent starts with a fresh
context." on desktop, iOS, Android and the TUI; not offered for group chats
([ChatWindow.tsx#L343](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/apps/desktop/src/renderer/views/ChatWindow.tsx#L343),
[BotListView.swift#L250-L256](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/apps/ios/Views/BotListView.swift#L250-L256),
[BotListView.kt#L329](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/apps/android/app/src/main/kotlin/com/codync/android/BotListView.kt#L329)). The host stops only
the running turn, drops the native session, withdraws any pending handoff card, and flushes memory; queued
messages run in the new session ([queue.rs#L87-L97](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/host/src/agent/bot/queue.rs#L87-L97)). Nothing of the old
conversation is carried into it ([compaction.rs#L307-L314](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/host/src/agent/bot/compaction.rs#L307-L314)) beyond
the Bot's memory and profile.

### M4. Editing or declining a pending handoff

Described under A4: the person can rewrite what a rollover carries or keep the current conversation.

## Codync: what it does not have

- **No mid-turn host action.** Host compaction runs only at a turn end or while idle; during one long turn only
  the harness's own auto-compaction (A1) can free room. Searched `host/src/agent/bot/{compaction,turn,queue}.rs`.
- **No context meter in the chat.** No numeric "used / window" display was found in the desktop renderer, iOS
  or Android chat views (searched `contextUsage`, `ctxUsed`, `usedTokens`, `context left`, `contextWindow`).
  The window size only feeds the setting's size menu (`compactionTokens`).
- **No host-written summaries.** Explicitly deferred "until harness summaries prove insufficient"
  ([bot-memory.md#L173](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/docs/design/bot-memory.md#L173)).
- **No `/clear` or `/new` handled by Codync.** Tests confirm `/new` is not a built-in and "reaches the agent as
  typed" ([slash.rs#L328](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/host/src/tui/app/slash.rs#L328)); `/clear` appears nowhere in host or apps code.
- **No compaction for other agents or group chats**, by design.

## Clawdline Bot today

What exists:

- **Context meter.** A Bot chat shows used / window tokens and a green/amber/red meter (red at 15% or less
  remaining, amber at 35% or less) when the provider reports a valid reading
  (`web/console/src/project-assistants/ContextUsage.tsx:12-14`, `docs/bots.md:169-172`). It is display only.
- **New session.** Bot roster menu **New session** ("Its conversation and queued messages stay. The active
  turn will stop.", `web/console/src/project-assistants/words.ts:70-71`, `Bots.tsx:320`). The engine records a
  session-reset point and stops the active turn (`internal/app/projectassistant/new_session.go:9-55`). The next
  fresh native session gets a source-owned opening built from filtered memory, open work and a bookmark
  (`docs/project-assistants-runtime.md:278-301`).
- **Reviewed handoff card.** The information panel offers **Prepare a fresh conversation** while idle; the
  person can review, edit or keep, and the next message adopts it (API 93,
  `docs/project-assistants-runtime.md:303-329`). Its summary comes from Clawdline's filtered history, not the
  harness's compaction summary.
- **A guarded native `/compact` path, not in production.** The ACP client can send a bare `/compact` only when
  the agent advertised it, outside any prompt, once per client lifetime
  (`internal/adapters/projectassistantacp/client_compaction.go:29-65`). The host composes it only for isolated
  qualification; no production binding supplies a provider or journal
  (`internal/adapters/projectassistanthost/native_maintenance.go:22-27`; "The production writer stays
  disabled", `docs/project-assistants-runtime.md:409`).

What happens when the context fills:

- Only the harness's own auto-compaction (Claude Code or Codex inside its ACP adapter) frees room.
- Clawdline ignores `compaction_update` unless its own maintenance operation is in flight
  (`client_compaction.go:67-77`), so it does not record the compaction or post a notice. The meter simply drops
  on the next `usage_update`.
- Whether Clawdline's opening instructions and memory survive a harness compaction depends on the harness:
  - Clawdline puts them in the system prompt where the agent supports that; otherwise they go in the first
    prompt (`internal/adapters/projectassistantacp/client.go:584-590`, `:628-630`).
  - Nothing re-sends them after a compaction.
  - This task did not check which agents keep them.
- If the harness cannot compact, the outcome is the harness's error; this task did not observe that case.

What the person can do now: watch the meter, start a **New session**, or **Prepare a fresh conversation** and
review the handoff. Typing `/compact` is not a designed feature. A later prompt that carries no handoff,
memory update or quote starts with the person's text, so the adapter may treat it as its command. This was
not tested, and the first prompt of a session and memory-changed prompts prepend other text.

Gaps compared with Codync:

1. No record or notice of a harness compaction, and no re-delivery of instructions/memory after one.
2. No "compact now" action: room can only be recovered by abandoning the native session.
3. No host-timed compaction or automatic rollover; nothing reacts to the meter.
4. The fresh-conversation card does not reuse the harness's last summary (Claude) or the last user messages
   verbatim; it uses Clawdline's filtered history only.
5. Slash commands advertised by the agent are tracked (`CommandsKnown`, `CompactAdvertised`) but are not offered
   in the composer or passed through deliberately.

## Recommendations for Clawdline

Following the person's rule, every new automatic behaviour is a setting whose default keeps today's behaviour.

1. **Observe harness compactions** (no setting needed for the bookkeeping):
   - Accept `compaction_update` completed, and a halved `usage_update`, outside maintenance as an observed
     fact.
   - Post an info line such as "Earlier context was summarized to make room".
   - Mark the next prompt to carry the current memory and instructions, the way `MemoryChanged` already does.
   - Keep Claude's summary for the handoff card.

   This fixes gap 1, changes no native write and is the base for 2 and 3. If showing the new line counts as a
   visible behaviour change, put the line itself behind a default-off setting; the re-delivery is a
   correctness fix.
2. **Recommended: a manual "Compact now" action**:
   - Release the existing guarded native `/compact` path for Claude Code and Codex Bots when the agent
     advertised it and no turn is running.
   - Pass Codync's keep-list (Claude: after `/compact`; Codex: `compact_prompt` at launch).
   - Because it runs only when pressed, it needs no setting.
   - It must first pass the production qualification the runtime doc already requires.
   - Phone: no new permanent control in the composer. Tapping the context meter opens a bottom sheet with
     **Compact now**, **Prepare a fresh conversation** and **New session**, each with one sentence on what it
     keeps. On a wide screen the same three actions sit in the Bot information panel.
3. **"Summarize long chats", default Auto (off)**:
   - Port Codync's rules: Early, Near the limit and a size.
   - Compact at the end of a turn in the main chat only.
   - Add the loop guard; when it fires, roll over into the existing API 93 handoff card instead of compacting
     again.
   - Skip idle compaction at first: it pays only on a one-hour Claude cache.
   - Phone: one row in Bot settings with a picker sheet.
   - This automates what 2 does by hand. It does not help during a single long turn, which stays the harness's
     job in both products.
4. **Optional: low-context prompt**, default off. When the meter turns red at the end of a turn, show one
   dismissible line offering the sheet from 2. This is cheaper than 3 and keeps the person in control. On a
   phone it is a single line above the composer, never a modal.

Suggested order: 1, then 2, then 3. Leave 4 out unless 3 is declined.
