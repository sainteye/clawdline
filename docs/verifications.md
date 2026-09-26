# Things waiting to be verified

A change is often made on a guess: lower the compaction window and tasks should get cheaper without
getting worse. Whether the guess held can only be read later, after enough work has run, and "later"
is where it gets lost — a reminder in a session's head does not outlive the session. A verification
record keeps it: **why** the change was made, **when** to look again, **what would count as it
holding**, and **the data it is judged by**, read at the moment someone looks.

It is reached three ways: the sidebar's **驗收** page, `clawdline verify`, and
`/v1/verifications`. A scheduled task that reads the data for it writes its readout onto the record
with its own task secret.

## The record

| Field | What it is |
| --- | --- |
| `id` | A uuid-shaped id this machine made (letters, digits, `-`, at most 64). |
| `title`, `why` | What is being verified and why it was changed. |
| `started_at`, `due_at` | When the change took effect (now, if not said) and when to look again. `due_at` is required and not before `started_at`. |
| `criteria` | Up to 12 sentences, each `unset`, `passed` or `failed`, in the order they were written. Marked one at a time, by index. |
| `source` | Optional: where the data comes from. `{kind, since}`. The only kind is `compaction_compare`, read with `since` like `clawdline usage --compare-compaction --since`. Any other kind is refused as `unknown_source`, naming it. |
| `schedule_id` | Optional: the schedule whose task reads the data. It is what lets that task write a note. |
| `notes` | Appended, never edited. Each has its time, its text, and its author: `person` (a paired device), or `session` with the conversation or `task:<id>` that wrote it. |
| `status` | `open`, then `accepted` or `rejected`, with the reason. A verdict is not changed afterwards: closing it the same way again is answered as done, closing it the other way is `verification_closed`. A closed record keeps its criteria as they were and still takes notes. |

The data is **not stored**. Opening a record (`GET /v1/verifications/{id}`) reads its source then, and
the answer carries it beside the record as `data`: the comparison, or `data.error` saying why it
could not be read. The list and every write answer the record without it.

Bounds are in [limits.md](limits.md) N45: 200 records, 12 criteria and 200 notes a record, and a
length for every text field. Nothing is evicted — every record is something a person asked to be
reminded of — so the 201st is refused, and deleting a closed record makes room.

## Routes

`api/v1/verifications.schema.json` is the contract; both generated contract files carry its types.

| Route | Does |
| --- | --- |
| `GET /v1/verifications` | Every record, open first by due time. `at` is the daemon's clock, which the page counts down from. |
| `GET /v1/verifications/{id}` | One record, with its data source read now. |
| `POST /v1/verifications` | A new record. `201` when made; an `Idempotency-Key` seen before answers the first record with `200`. |
| `POST /v1/verifications/{id}/notes` | A note. `{text, session?}`. |
| `POST /v1/verifications/{id}/criteria/{n}` | `{state}` for criterion `n` (0-based, written as a plain integer). Refused once the record is closed. |
| `POST /v1/verifications/{id}/close` | `{status: accepted\|rejected, reason}`. The reason is required. |
| `DELETE /v1/verifications/{id}` | A closed record. An open one only with `?force=1`, otherwise `verification_open`. There is no route that deletes more than one. |

Who may: a paired device and this machine's orchestrator token read them; a change needs a device
that may send, or that token — the same door as `/v1/work`. A note says who wrote it by the door it
came in by. A device is the person and may not name a session; the orchestrator token is a session,
named by `session` (the CLI sends its own conversation).

Refusals are the flat `{error, message}`: `bad_request` naming the field, `unknown_source`,
`not_found`, `verification_limit_reached`, `notes_limit_reached`, `verification_closed`,
`verification_open`, and for the task route `schedule_mismatch` and `not_scheduled`.

### Over Clawdline Cloud

Every route crosses, so the page works on a phone: two machine reads (`verification.list`,
`verification.get`) and five commands (`verification.create`, `.note`, `.criterion`, `.close`,
`.delete`) — [cloud-wire.md](cloud-wire.md) has the wire shapes. `web/console/src/cloud/carry.test.ts`
scans every `/v1/verifications` path the console spells and every route the contract names, and
fails on one that does not parse to a carried word.

## A scheduled task's readout

`POST /v1/orchestrator/tasks/{task}/verification-note` with `X-Clawdline-Task-Secret` and
`{verification, text}`. It is the one write a task secret opens here, and a narrow one: the note lands
only on a record whose `schedule_id` is the schedule that started this task (`schedule_mismatch`
otherwise), is signed `task:<task id>`, and a task that no schedule started is `not_scheduled`. The
task never needs the orchestrator token for it. The skill guide's "Schedule future work" section has
the call.

## The CLI

```
clawdline verify list [--json]
clawdline verify show <id> [--json]
clawdline verify add --title … --due <2026-10-03 09:00 | 7d | RFC 3339> --criterion … [--criterion …]
                     [--why …] [--started …] [--compaction-since 14d] [--schedule <id>]
clawdline verify note <id> "…"
clawdline verify done <id> --accepted|--rejected "reason"
clawdline verify delete <id> [--force]
```

A misuse — no id, two ids, an unknown flag, a verdict with no reason — exits 2 before anything is
asked. `show` prints the comparison exactly as `clawdline usage --compare-compaction` does.

## The page

驗收 in the sidebar lists the open records with how long is left (red once overdue) and how many
criteria are marked; closed ones fold under a button. A record shows why, each criterion with its
state and the presses to change it, the data (the comparison as a table, with the same cells and
footnotes the terminal prints), the notes and a box to add one, accept/reject with a required reason,
and delete behind a second press that says whether the record is still open. The words are in
`pages/verify/words.ts`, in English and 繁體中文.

It is measured at 390px by `web/console/src/pages/verify/verify.e2e.ts`, which loads the built console
in headless Chrome at 390×844 against a stand-in daemon: nothing on the list or a record reaches past
the screen, the ten-column table scrolls inside its own box, and each press sends the one request it
names. Removing the table's own scroll box makes it fail.

## The first record

`StartVerifications` plants one record when the daemon starts, **only on the machine that holds the
schedule reading the compaction experiment**. That schedule is named here only by the SHA-256 of its
id (`app.CompactionScheduleDigest`): a real schedule's id belongs to one machine, and this repository
is public. The daemon hashes the ids it holds (`Store.ScheduleIDs`, which, unlike `ScheduleFiles`,
stamps nothing) and plants the record only on a match. On any other machine it does nothing.

- 壓縮門檻實驗（300000）, started 2026-09-26, due 2026-10-03 09:00 (the machine's zone)
- data: `compaction_compare` since `14d`; linked to that schedule
- criteria: 300000 組每個 task 成本明顯低於 before-setting · 成功率沒有下降、stalled 與重派沒有增加 ·
  抽查 3–5 個壓縮過的 task：沒有遺失指示或重做

The seed's key is written in `verification_seeds` in the same transaction as the record, and it is
never removed: planting again writes nothing, and **deleting the record does not bring it back**. A
record the person already made for the same source and schedule stands in for it, and so does a
store already at its limit.
