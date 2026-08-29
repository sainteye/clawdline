# Session states: a vocabulary for one person deciding what to do next

Every state below exists to be looked at by one person, so that after looking they know what
they should do next. That is the whole design test: **one state = one distinct action.** A value
that does not change what the reader does is a field riding on the row, not a state. This page
is the contract in both directions — what a person may conclude on seeing a state, and what a
session must mean when it declares one.

The wire carries this as two independent things on every Session row:

- `work_state` — one closed value: `ready`, `working`, `holding`, `waiting_you`,
  `waiting_session`, `unknown`, `milestone_complete`, `work_complete`.
- `owed` — an optional debt object that rides beside *any* state.

Two of the old names are gone. `waiting_human` is now `waiting_you` — the state is an
instruction to the reader, not a taxonomy of blockers. `needs_triage` is now `unknown`, because
what that case has always meant is *"the broker has no positive evidence"* — an absence. The old
name wrote the observer's ignorance down as the reader's to-do, and one evening it put five rows
of demands on a phone when not one of them needed anything.

## The vocabulary

For each state: what it means, what you do, and what a row looks like on a phone in Traditional
Chinese (the first-class copy; every other language translates it).

| state | icon | you should | zh-Hant row |
| --- | --- | --- | --- |
| `waiting_you` | 🙋 | **Answer now.** A turn is stopped on you; every second unnoticed costs something. The only state that pushes or lights the row. | `🙋 在等你回答` |
| `owed` (overlay) | 📥 | **You owe this line a decision.** Nothing is stopped; it ages, and the age is the risk. Never pushes. | `📥 欠一個決定 · 3d`（通常帶 session 自己的字句） |
| `waiting_session` | ⏳ | **Do nothing — but if it stays, another session is stuck.** A peer wait, an owed file release, or your own live child. It can wedge; you may have to go and unwedge it. | `⏳ Clawdfather · 檔案釋出` |
| `holding` | 🔜 | **Do nothing; it moves by itself.** The mover is an event, a clock, or something the session started — never a person or a session. It cannot wedge. | `🔜 自行推進中 · 自述`（帶宣告的下一步） |
| `working` | *(spinner)* | Nothing — it is executing now. The live line says what. | `▸ 正在編譯 Orchestrator.swift…` |
| `ready` | 📭 | **You can hand this one work.** An invitation, almost always the session's own declaration, marked as such. | `📭 可接新工作 · 自述` |
| `unknown` | *(none, on purpose)* | Nothing is being asked of you. The broker has no positive evidence — an absence, not a category. Giving an absence a symbol is how `needs_triage` came to read as a demand, so this one has no icon at all. | `狀態未知`（灰、斜體、安靜） |
| `milestone_complete` | ✓ (one CSS check) | **Review and accept it.** Delivered on authenticated evidence; review or landing may remain. | `✓ 已交付，等待驗收` |
| `work_complete` | ✓✓ (two CSS checks) | **That task scope has landed.** This alone does not prove a root tab is safe to close; until the separate closeability projection ships, use the current root audit and `lost_if_closed` close gate. | `✓✓ 已驗收完成` |

The icons are meaning, not severity: a raised hand asks, a tray holds what you owe, an hourglass
is somebody else's time, an "up next" sign moves on its own, an open empty mailbox can receive.
`working` needs no icon because it already has the one honest animation on the page, and the
completed pair keeps the repository's established one-check/two-check strokes
(docs/orchestrator.md) rather than inventing new marks.

## The two axes

One value used to compress two independent questions: *(a) can this session move forward by
itself right now* and *(b) does somebody owe it something*. Those combine freely, and the most
common real combination — "my main line waits on a person; my side work proceeds" — had no
spelling at all.

So axis (a) is `work_state`, and axis (b) is the `owed` overlay:

```json
"work_state": "working",
"owed": { "note": "schedules 的取捨還是你的決定", "since": 1787640000,
          "person_needed": true, "moved_by": "the user", "provenance": "self" }
```

`owed` is deliberately **not** a ninth enum case. As a case it would collide with every other
state a session can be in at the same time; as an overlay it rides beside `working`, `waiting_you`
or anything else, and the client appends it as its own 📥 badge. It **survives turns**: a debt is
not paid by its owner doing side work, only by an explicit clear (`owed: null`) or by the
process in the terminal changing. Re-declaring the same debt keeps its original `since` — the
clock must not reset every time the debt is mentioned, because "nobody remembers in three days"
is the failure this field exists to prevent.

## The fields beside the state

- `work_provenance` — `broker` (projected from evidence) or `self` (the session's declared
  claim). The row shows `自述` on self states, so a stated state can never dress as a proven one.
- `work_note` — one line, the session's own words: *"RootSession fix landed; no open child or
  wait; can take new work"*.
- `work_since` — when the leading evidence was recorded. Self claims and debts carry clocks;
  live terminal observations honestly do not.
- `work_moved_by` / `work_person_needed` — who or what will move this, and whether that mover is
  a person. "Your build; nobody" and "the user's decision; the user" are the same colour of idle
  and opposite calls to action.
- `disposition` — unchanged: the receipt behind a check state, evidence-typed, never accepted
  back as truth.

## Declaring: `POST /v1/orchestrator/sessions/:id/state`

The `self` half of provenance. Machine token, identity resolved from the live watched process
(exactly like `/complete`), and only while the declaring turn is observably working:

```json
{ "state": "ready",
  "note": "fix landed; can take new work",
  "owed": { "note": "schedules 的取捨還是你的決定", "moved_by": "the user" } }
```

- `state` may be **only** `ready` or `holding`. The check states are refused by name
  (`403 self_completion_refused`): a self-declaration may never produce ☑︎ or ✅ — that boundary
  is the existing design's whole point, and the checks stay evidence-only.
- A `ready`/`holding` claim lives one turn, like a delivery receipt: the first idle settles it,
  the next working/waiting transition consumes it. Declare again at the end of the next turn if
  it is still true.
- `owed` sets the debt; `"owed": null` clears it; omitting it leaves it alone. The debt persists
  across turns until cleared.
- All notes are one line of at most 200 characters.

### `holding` is deliberately hard to enter

`needs_triage` went wrong by being the projection's default exit — the last line of the
function, collecting everything no rule caught. `holding` must never inherit that role: **it is
no branch's default and has exactly one entrance** — a declaration carrying the next step
(`note`), a mover (`moved_by`), and `person_needed: false`, refused otherwise
(`422 holding_needs_evidence`). The web client enforces the same rule from its side: a frame
claiming `holding` without `self` provenance fails closed to `unknown`.

The boundary against ⏳ is the one that decides whether a person goes and pokes someone:
**`waiting_session` can wedge — somebody has to resolve it if it stays; `holding` cannot** — its
mover is an event, a clock, or something the session itself started. The moment the honest mover
is a session or a person, the truth is a wait, not a hold: waiting on your own child is
`waiting_session` (a child can wedge), waiting on a file release is `waiting_session`, waiting
for your build or a scheduled time is `holding`. On this Mac most "events" turn out to be other
sessions, so real holds are rare — if `holding` ever becomes common in live data, that is a spec
problem to report, not a threshold to tune.

## How the broker projects (precedence)

`waiting_you` (terminal question) → `waiting_session` (coordination wait, either side) →
`unknown` (unreadable screen) → `working` (live activity) → `waiting_session` (own live child) →
the task receipt (checks for finished success; `unknown` for finished non-success — the failure
is on the row as the task's own word, and the *next action* belongs to the root that dispatched
it, so it earns no demand here) → session delivery (milestone) → the self claim (`ready` /
`holding`) → `ready` for a plain assistant-free prompt → `unknown`.

Current activity still outranks old receipts, and an idle assistant prompt without evidence is
still not `ready` — but `ready` is no longer structurally unreachable for assistant sessions:
the session itself is the one witness who knows its assignment ended, and its authenticated,
self-marked declaration is how that knowledge enters the projection without weakening the
evidence-only checks.

## `lost_if_closed`: a gate, not a label

What closing a session takes with it — live descendant tasks, waiters stranded on files it owns
— is computed **at the moment somebody presses close**, and nowhere else. It is not a list
column and not a state: the night a root with four live children was closed anyway,
`hasOutstandingChild` was already in the projection; a label read earlier does not stop a close,
and never will. So `POST /v1/sessions/:id/end` computes the list at its end of the press and
refuses (`409 would_lose_work`, with the list) unless the request carries `accept_loss: true` —
which the web page sends only after showing that list in the close confirmation. A close with
nothing at stake stays one press.
