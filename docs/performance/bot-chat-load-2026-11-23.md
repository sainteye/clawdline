# Why a Bot chat loads much slower than a Session

Measurement and root-cause review for the report that Bot messages load far more
slowly than a Session conversation. It retains no product change. It follows the
selection rule in `AGENTS.md` ("Choose performance work by user impact") and
[the local API review](local-api-review-2026-11-08.md).

## What was and was not measured

- Source: `347831863` on Apple M4 Pro, Go 1.27.1, darwin/arm64, shared and
  uncontrolled background load (other Sessions were building at the same time).
- **Measured:** the daemon's Bot read-view store (`ViewStore.Read`) on synthetic
  conversations of 100, 300, 1000 and 3000 messages (about 1.5 KB of text each,
  100 dispatch cards), seven repetitions per mode, plus the repository's existing
  9001-message fixture. This is the in-process store cost, without HTTP, JSON
  encoding, Cloud relay or rendering.
- **Not measured:** browser end-to-end timings for actions A–D (open a Bot chat,
  open a Session, live-message latency, switching), phone 390×844 and desktop
  1280×800 viewports, the Cloud phone path, and the real size of the person's Bots.
  Reading the running daemon on port 7727, even with read-only `GET`s, was refused
  by this child's permission policy, and the time budget did not allow a seeded
  daemon plus browser harness. Those numbers are still owed (see *Next
  measurements*). Everything below about the browser and the Session path comes
  from source reading, not timing.

The harness is a temporary test in `internal/adapters/projectassistantstore`
(kept in the task artifacts, not committed). Rerun it with
`ZZ_MEASURE=1 go test -count=1 -run TestZZMeasureBotReadScaling -v ./internal/adapters/projectassistantstore/`.

## Baseline: daemon read-view cost grows faster than the conversation

Seven reads per cell; median and worst. "Export" is the JSON size of the
restored Bot export (messages, events, turns, work links), a proxy for the
aggregate the queries walk.

| Messages | Export | Snapshot (open) | Permissions | Traces | Idle events poll |
| ---: | ---: | ---: | ---: | ---: | ---: |
| 100 | 443 KB | 27.9 / 36.3 ms | 17.8 / 23.6 ms | 17.8 / 18.8 ms | 6.5 / 6.9 ms |
| 300 | 1.25 MB | 58.2 / 124.6 ms | 51.8 / 82.3 ms | 50.3 / 87.1 ms | 16.2 / 17.4 ms |
| 1000 | 4.1 MB | 952 / 1379 ms | 381 / 687 ms | 384 / 544 ms | 56.8 / 145.6 ms |
| 3000 | 12.2 MB | 3436 / 4079 ms | 1701 / 2108 ms | 1623 / 3288 ms | 545 / 973 ms |

The existing long-history fixture (9001 short messages, 5.2 MB of message JSON,
600 events) logged a single run each: snapshot 2189 ms, history page 1896 ms,
events page 563 ms, and an **idle** events poll with nothing new 702 ms.

Each mode returns at most one page (100 rows, or 256 events), so the output is
constant while the cost goes from 28 ms to 3.4 s. The work is proportional to
the whole stored Bot, and above roughly 1000 messages it grows faster than linear.

## Where the time goes when a Bot chat opens

Critical path from source (`web/console/src/project-assistants/Chat.tsx`):

| Step | Requests | Order | Visible effect |
| --- | --- | --- | --- |
| 1. `GET …/{bot}/view?mode=snapshot&page_size=100` | 1 | serial | Messages, queued turns and the input become usable when this returns (`recoverSnapshot`, `Chat.tsx:363`). |
| 2. `mode=permissions` and `mode=traces` | 2 | parallel, after 1 (`Chat.tsx:367`) | Permission cards and activity rows fill in. |
| 3. `mode=events&page_size=256` | 1 every 3000 ms | polling (`EventRefreshMs`, `Chat.tsx:66`, `Chat.tsx:375`, `Chat.tsx:381`) | New messages appear. |

So with the measured store cost, the messages of a 1000-message Bot are readable
about 1 s after opening (worst 1.4 s) before network, encoding and rendering, and
permission/trace rows another 0.4–0.7 s later. At 3000 messages that is 3.4 s,
then about 1.7 s more. Step 2 does not delay readable messages but does keep the
chat busy and competes for the same database.

Live messages (action C) wait for the next poll: on average half of 3000 ms,
worst 3000 ms, **plus** the idle-poll cost (57 ms at 1000, 545 ms at 3000
messages). Each open Bot chat also spends that idle cost every three seconds,
which competes with the open of the next chat (action D).

## Root causes

1. **Every query walks the Bot's entire JSON aggregate.** A Bot is one row,
   `project_assistant_bots.data`, and each read-view query begins with
   `viewAggregateSQL` (`internal/adapters/projectassistantstore/read_view_query.go:21`)
   and expands it with `json_each` over `$.Messages`, `$.Turns` and the other
   arrays. There are 28 `json_each` uses across the read-view, query and
   background-task files.
2. **One read repeats that walk many times.** `ViewStore.Read`
   (`internal/adapters/projectassistantstore/read_view.go:46`) runs `viewHead`
   (`:69`), `viewValidateFacts` (`:77`, all modes except events),
   `viewOwnChatHead` (`:81`), `backgroundTaskSummaries` (`:85`), `viewCounts`
   (`:90`, four `json_each` expansions on its own), then the mode query. Snapshot
   alone adds two more aggregate passes in `viewSnapshot` (pending turns and the
   last terminal turn) before the timeline query. An idle events poll still pays
   head, own-chat head, background tasks and counts: that is why "nothing new"
   costs 0.5–0.7 s on a large Bot.
3. **Opening is two serial round trips that each redo item 2.** Permissions and
   traces are separate reads (`Chat.tsx:367`) and recompute the same head and
   counts the snapshot just produced.
4. **Per-message reaction lookups (N+1).** `messageReactions`
   (`internal/adapters/projectassistantstore/reactions.go:18`) runs two indexed
   queries per returned message (`read_view.go:129`, `read_view.go:136`), 200
   small queries for one snapshot page. They are indexed and cheap compared with
   item 1, but they are avoidable.
5. **Authorization is evaluated twice per read.** `Service.View`
   (`internal/app/projectassistant/service_read_view.go:65` and `:104`) calls
   `readViewAuthority` before and after the store read. This is a correctness
   check against revocation during the read and is not the bottleneck; it is listed
   so nobody optimizes it first.
6. **No push for an open chat.** The Bot chat polls every 3000 ms; the roster has
   an SSE stream (`/v1/project-assistants/roster-events`) but the chat does not use
   one.

## Why a Session is fast

A Session conversation reads `GET /v1/transcript` (`internal/transport/http/transcript.go:83`),
which returns the newest `limit` entries (default 200, at most 1000) from the
session's own record with revision deltas and caches, so its cost tracks the page,
not the history length. New output is pushed over `/v1/events` SSE instead of
being polled. The Bot read view, by contrast, re-derives the page from the full
aggregate every time.

## Improvement options, ranked

Saving estimates are store-side milliseconds from the table above; the real
end-to-end saving still has to be confirmed (see *Next measurements*).

| # | Change | Expected saving | Frequency | Correctness risk | Main files |
| --- | --- | --- | --- | --- | --- |
| 1 | Normalize the read path: keep messages, turns, work links and events in indexed per-row tables (or a maintained side index keyed by bot and seq) so a page is read with `ORDER BY seq DESC LIMIT n`, and head counts come from maintained columns instead of `json_each`. | Open: ~0.9 s at 1000 msgs, ~3.4 s at 3000, to a near-constant few tens of ms. Idle poll: 57–545 ms to a few ms. | Every Bot open, every 3 s while open | High: the write path (`Store.Commit`, `Restore`, import/export) must keep both forms consistent; cursors, `event_frontier`, receipts and unread counts must stay identical. Needs migration and the existing read-view test suite. | `internal/adapters/projectassistantstore/store*.go`, `read_view*.go`, migrations |
| 2 | Compute the shared head once per read: one materialized CTE that yields head, own-chat head, background tasks and all counts in one aggregate pass, reused by the mode query. | Removes most of the repeated passes; expected roughly 40–70% of each read until #1 lands (to be measured). | Every read | Medium: same results, fewer queries; covered by existing read-view tests. | `read_view.go`, `read_view_query.go`, `background_tasks.go` |
| 3 | Return the first permissions and traces pages inside the snapshot response (optional fields) so opening is one round trip. | One extra round trip plus one full read: 0.4–1.7 s of "busy" after messages appear. Does not move first readable message. | Every open | Low–medium: contract field addition; collection cursors must keep their own epochs. | `read_view.go`, contract, `Chat.tsx` |
| 4 | Push instead of poll for the open chat: notify on the existing roster/SSE channel when a Bot's `event_frontier` advances, and read events then (keep the 3 s poll as fallback). | Live message: mean ~1.5 s, worst 3 s, to near-immediate. Removes idle-poll load. | Every live message | Medium: SSE resume and `caughtUp` semantics; the events cursor remains the source of truth, so a missed push only falls back to the poll. | `Chat.tsx`, roster events handler, `internal/transport/http/project_assistants_view.go` |
| 5 | Batch reactions for a page in one query (`message IN (…)`). | Small (hundreds of indexed queries → one); likely single-digit ms. | Every read | Low | `reactions.go`, `read_view.go` |

Not recommended now: removing the second authorization check (#5 above), or
front-end rendering work, before the browser trace shows render time is a
material share. The Bot chat list does not appear to be virtualized, but the
snapshot page is capped at 100 rows, so rendering is unlikely to explain multi-second
waits; confirm with the browser measurement before acting.

## Recommendation

Do **#2 first**, then **#4**. #2 is contained to the store, preserves every
contract and is checkable with the existing read-view tests plus this harness; it
should cut both open time and idle polling without a migration. #4 removes the
up-to-3-second live-message delay, which dominates action C regardless of store
speed. Plan #1 as the structural fix once the gains from #2 are measured, because
it is the only change that makes Bot reads independent of history length, like a
Session.

To prove a change helped:

1. Rerun the harness at 100/300/1000/3000 messages, same machine, seven
   repetitions, before and after; report median and worst for each mode.
2. Run a browser trace against an isolated daemon seeded with a Bot of realistic
   size, desktop 1280×800 and phone 390×844, at least five repetitions each:
   time from click to last message readable, scrolled to bottom and input
   enabled; and for a live message, time from the daemon commit to the row
   appearing. Compare with a Session of comparable length under the same load.
3. Keep or revert by absolute end-to-end saving, not by the store
   microbenchmark alone.

## Next measurements still owed

- The real message and dispatch-card counts of the person's largest Bots (a
  read-only count from the running daemon), to place them on the scaling table.
- Browser end-to-end for actions A–D on both viewports, and the Cloud phone path.
- Session side timings for the same actions, measured rather than inferred.
