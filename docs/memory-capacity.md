# Project memory capacity

A Project's shared memory (`docs/project-memory.md`) holds at most 1,152 live entries: 1,024
active and 128 pending (`internal/adapters/memory.LiveEntryLimit`). An add or restore past that
answers `409 memory_full`, and `/v1/health` reports `capacity_exhausted`. The daemon never removes
an entry on its own. Dreaming archives only exact copies and records that name their successor
with `Superseded-by:`, and it never touches `user` or `feedback` entries.

On 2026-11-23 one Project reached 1,152 of 1,152 with nothing archived, after about 30 additions a
day. Nobody was told before writes were refused. The `memory.entries` row reported to diagnostics,
to the refused sender and to health, but not as a notice. Health also reported every Evidence row
only once it was exhausted.

## Early notice

The `memory.entries` capacity row now warns at 85% (`NoticeEntryLimit`, the first live count at
85% of `LiveEntryLimit`: 980). It announces entry into warn as a `capacity.notify` event and push
(`NoticeOnWarn`). The notice uses the same one-a-day budget as every other row's notice. If a Project
reaches warn and then critical on the same day, the person still gets one push.

At 980, the Project still has room for 172 entries. At the rate observed on 2026-11-23, that is
nearly six days. A person can review the suggestions in that time. At 95%, the next ordinary
notice threshold, it would be about two days.

Once the fullest Project reaches the notice bound, the capacity reading carries the number of entries
its maintenance plan already offers: proposed archives plus review candidates
(`Store.FullestProject`, `Reading.Reclaimable`). The notice says that number and names
`clawdline memory dream` as the place to read the suggestions. Below the bound, the plan is not
computed on each capacity pass.

The notice adds no permission. It tells the person or the Bot reading the notice that there is
something to decide.

## Suggestions, never automatic moves

Dreaming's review list has always been advice for a person: `same_description` and
`successor_chain` (`docs/project-memory.md`) and `related_delivery` (same delivery name with an
earlier stage suffix). It now also has `near_duplicate`. Two live entries of the same type
(`project` or `reference` only) qualify when their bodies share at least 80% of their distinct
lines. Lines shorter than 16 bytes and `Superseded-by:` lines do not count. Neither does a line that
more than eight entries share, because that is boilerplate. When one body names the other entry,
the named entry is offered as the one to keep. Otherwise, the longer body is offered.

A `near_duplicate` row is never archived by maintenance. To act on one, the person or Bot compares
both originals and uses the existing review route (`POST /v1/projects/{place}/memory/{name}/review`
with `archive`, `merge` or `keep`). Every archive remains restorable with `memory restore`. On a
read-only copy of a 1,152-entry Project, the whole plan took about 9 ms and found one
near-duplicate pair.

Dreaming still does not decide that an entry is obsolete by age, and it does not call a model.
Recognizing an obsolete delivery record needs judgment. A person or Bot makes that judgment and
records it with `Superseded-by:` or a review.

## Comparison with Codync

Codync, read at
[`9dae3e2b`](https://github.com/leepokai/Codync/tree/9dae3e2bec9b91a4a42f85a943eb3983f95d0503),
keeps a different kind of memory: a person's facts per Bot, not project knowledge. Its README says
that project knowledge stays in the harness's own files
([README](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/host/crates/memory/README.md)).

| Question | Codync | Clawdline Project memory |
|---|---|---|
| Bound | Token caps: 400 for the shared layer, 1,200 for each Bot, and 150 for each line ([tokens.rs:7–11](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/host/crates/memory/src/tokens.rs#L7-L11)). Notes have 2,000 tokens each and 20 notes per layer ([files/mod.rs:26–29](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/host/crates/memory/src/files/mod.rs#L26-L29)). | Entry counts: 1,024 active, 128 pending, and 512 archived. |
| Near full | At 90%, a run tidies `MEMORY.md` ([dream/mod.rs:113–117](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/host/crates/memory/src/dream/mod.rs#L113-L117)). A model moves detail lines word for word into topic notes and leaves one summary line. | Pressure starts at 820 live entries and can run Dreaming early. At 85%, the person receives one notice with the suggestion count. |
| At full | A write past the cap is refused. The Bot is told to write a note instead and "don't tell the user memory is full" ([check.rs:42–45](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/host/crates/memory/src/check.rs#L42-L45), [files/mod.rs:201–204](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/host/crates/memory/src/files/mod.rs#L201-L204)). | The add is refused with `409 memory_full`, and health reports `capacity_exhausted`. |
| Dream trigger | After five quiet minutes or eight messages, once a day for date review, and while a layer is nearly full ([host/src/chat/memory/dream.rs:15–18, 80–84](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/host/src/chat/memory/dream.rs#L15-L84)). | Dreaming checks every five minutes. Its weekly and pressure passes wait for a quiet store and skip busy Projects. |
| Duplicate and obsolete rules | A model proposes up to ten changes ([dream/mod.rs:26](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/host/crates/memory/src/dream/mod.rs#L26)). Each change must cite the user's own words. "Don't add what memory already says." "Never remove lines written by the user." A passed future date is rewritten in the past tense ([dream/mod.rs:153–161](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/host/crates/memory/src/dream/mod.rs#L153-L161)). Deterministic checks then audit the result. | Deterministic rules with no model: exact copy, `Superseded-by:`, and suggestions for same description, successor chain, related delivery and near duplicate. |
| Automatic or confirmed | Automatic. Accepted edits are written without a person's confirmation and logged as a dream. | Only exact copies and named successors are automatic. Every other rule is a suggestion that waits for a person or Bot to confirm it. |
| What the person sees | A read-only Memory view: "About you", "This bot" and "Dreams", with added lines in green and removed lines in red. "To change a memory, tell the bot." ([MemoryView.tsx:22, 79–89](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/apps/desktop/src/renderer/views/bots/MemoryView.tsx#L22-L89), [MemoryInfoSheet.tsx:30–35](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/apps/desktop/src/renderer/views/bots/MemoryInfoSheet.tsx#L30-L35)). | `memory list`, `show`, and `dream` (the preview), plus `restore` and `forget`. The capacity row appears in Settings → Capacity. |
| Undo | Codync keeps every file version with its author and cause. The dream log has an `undone` column ([schema.rs:32](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/host/crates/memory/src/schema.rs#L32)), but this tree contains no writer for it and no undo control. | `memory restore` brings back the archived original byte for byte. |
| Telling the person memory is nearly full | None. Codync tells the Bot not to say it, and nearly full memory triggers tidying. | A notice at 85%. |

Searched in Codync for a nearly-full notification: `host/crates/memory` (all of `src/`),
`host/src/chat/memory/`, and `apps/desktop/src/renderer/views/bots/` (Memory view, info sheet and
files). The searches covered `full`, `cap`, `warn`, `notify`, `undo` and `restore`. Codync tidies a
nearly full layer without notifying the person or offering a review queue.

Clawdline differs deliberately in three places:

- **A notice instead of silent tidying.** Codync can make room because its model rewrites one
  person's facts into notes. Clawdline's Project store is shared by every Session in a Project, and
  its entries are lessons and records that Sessions cite by name. A model that merges or moves them
  could change what another Session reads without anyone deciding it. Room has to come from a
  person or a Bot confirming suggestions, and that takes time, so the person is told early.
- **Suggestions instead of model edits.** Codync limits risk with a model proposal plus deterministic
  audit, with every change citing the user's words. Project memory has no single user's words to
  cite. Clawdline keeps automatic action to the two provable cases and leaves every other rule as a
  suggestion.
- **Counts instead of tokens.** Codync bounds what a launch reads in full. Clawdline already bounds
  the launch index separately (`IndexByteLimit`) and lists grouped entries by group. The entry count
  bounds the store itself.
