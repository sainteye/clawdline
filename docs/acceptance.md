> **Retired Swift-generation record (through 2026-09-19):** The product reasoning is preserved, but Swift/AppKit/iTerm implementation details, source paths, route inventory, port 7717, `/tmp/.clawdline`, and claims about the running Mac app do not describe the Go daemon. References to unavailable retired files are rendered as code instead of live links.

# What this refactor must prove

The owner set one bar for the whole read/write-path refactor, on 2026-09-16:

> Using app.clawdline.com, for every type of operation — response speed, request size and system
> load are well designed, tested and verified. Board read and write, session read, opening and
> closing a session, showing session info, worktree read.

This page turns that sentence into numbers a run can pass or fail, and `tools/measure-operations.sh`
is the run. Nothing here is aspirational: every budget below is set against a measured baseline
that is also recorded here, so a later reader can see what was true before and decide whether the
budget was honest.

## Two different failures, which need two different budgets

The baseline separates cleanly, and the separation is the point:

| operation | bytes | seconds | what is wrong with it |
|---|---:|---:|---|
| `GET /v1/orchestrator/tasks` | 4,300,745 | 0.057 | **size** |
| `GET /v1/board?project=<id>` | 999,370 | 0.053 | **size** |
| `GET /v1/projects` | 122,955 | 0.032 | size |
| `GET /v1/strings` | 44,250 | 0.003 | — |
| `GET /v1/sessions/<id>/skills` | 10,053 | 0.013 | — |
| `GET /v1/sessions` | 12,351 | 0.021 | — |
| `GET /v1/sessions/<id>/info` | 1,732 | **0.719** | **blocking** |
| `GET /v1/sessions/<id>/links` | 691 | **0.383** | **blocking** |
| `GET /v1/screens` | 90 | **0.349** | **blocking** |
| `GET /v1/sessions/<id>/git` | 1,135 | 0.042 | — |
| `GET /v1/timeline?project=<id>` | 1,561 | 0.017 | — |
| `GET /v1/places` | 6,815 | 0.001 | — |
| `GET /v1/health` | 1,414 | 0.001 | — |

*Taken on the broker Mac, 2026-09-16, loopback, load average 4.32, 706 Board items, 716 task
records. `tools/measure-operations.sh` re-takes every row.*

**A 90-byte response that takes 349 ms is not a bandwidth problem.** It is waiting for something,
and what it is waiting for is the subject of a different fix than the megabyte reads. Reading the
two together as one "performance" number is how a refactor ends up shrinking payloads that were
never the complaint. So:

- **Size budgets** bound what crosses the wire.
- **Latency budgets** bound what a caller waits for, and a small slow response fails them while
  passing every size budget.

## The budgets

### Size

| class | budget | why |
|---|---:|---|
| first paint of any view | **≤ 150 KB** | one screen of rows plus the shell it needs — the Board asks for 30 of its 94 and offers the rest behind a control |
| any incremental or polled read | **≤ 16 KB**, or a `304` | a poll that finds nothing changed should cost a comparison |
| any single record's detail | **≤ 64 KB** | one record, and links to its collections |
| one page of a collection | **≤ 64 KB** | fifty rows at the observed card size, with room |
| event stream at connect | **one snapshot** | a page that reconnects is level without replaying; this is not a rate |
| event stream, steady, per viewer | **≤ 16 KB/min** | what changed, not the list it is in |

No response may embed a sibling collection. That rule is not a budget, it is what makes the
budgets reachable: the measured item read spent 35,551 of its 58,270 bytes re-sending a catalog the
caller already held, and no amount of field trimming fixes a response that is answering somebody
else's question.

### Latency

Measured at the loopback door, so that the Cloud tunnel's own floor is not attributed to the Mac.

**Two readings, and the warm one is judged.** A route that serves a maintained projection has two
costs, and a harness that takes one reading judges whichever it happened to get.
`/v1/sessions/<id>/links` is 326 ms on the first call after a restart and 1–3 ms on every call
after it; one reading called that a 326 ms route, and would have sent somebody to optimise a cache
that already worked. A long-running app serves warm, so the warm reading carries the budget and the
cold one is printed beside it and flagged over a second.

| class | budget | why |
|---|---:|---|
| anything on a first paint path | **≤ 250 ms** | the viewer is waiting and has nothing to read |
| anything polled | **≤ 100 ms** | it runs every fifteen seconds, per viewer |
| a write acknowledgement | **≤ 500 ms** | the person pressed a button |
| opening or closing a session | **≤ 2 s to acknowledge** | the work itself is longer; the *answer* is not |
| anything else on the shared queue | **≤ 100 ms** | it is holding every other request while it runs |

### System load

A measurement taken on a saturated machine is not a measurement. The harness reads the load
average and the swap usage before it starts and reports one of **three** states, never two:

- **pass** — within budget, on a machine quiet enough for the number to mean something.
- **fail** — outside budget.
- **cannot judge** — the machine was too loaded for a latency reading to carry information.
  Byte counts are still reported, because bytes do not vary with load.

The third state exists because of a real event: on 2026-09-16 this Mac served the same 3.2 MB
transcript parse in 187 ms and in 30,102 ms, four minutes apart, with a runaway daemon at 531% CPU
between the two readings. A harness without a "cannot judge" state would have recorded both as
facts about the code.

## What "through app.clawdline.com" adds, and what it does not

The Cloud path adds a tunnel, a relay and a browser. It does not change how many bytes the Mac
produced or how long the Mac took, and those two are what this refactor can fix. So the harness
measures the Mac at loopback, and the Cloud path separately, and **subtracts**: a Cloud reading is
only evidence about this code to the extent that it exceeds the measured Cloud floor for
`/v1/health`, which was 0.58–0.88 s on 2026-09-07 (`docs/runtime-performance.md`).

Measuring the Cloud path by opening a browser tab has a documented hazard: an automated Chrome
session on `app.clawdline.com` shares a device with the owner's own tab, and the Mac then rejects
one of them as a replay. The harness therefore reads the Mac's own log for the `read received`
lines, which carry the sender, rather than driving a second tab.

## The operations this must cover

Every one of these has a row in the harness, and a refactor is not done while any row is missing
rather than merely failing:

- Board: catalog, project, item list (paged), item detail, item history, **write**
- Session: list, one session, info, git, links, skills, screen, transcript
- Session lifecycle: **open**, **close** — acknowledgement latency, not completion
- Worktree: list and read
- Project: list, timeline
- The event stream, idle and under change

`open` and `close` are deliberately in the list and deliberately measured as *acknowledgement*.
They are the two operations where a slow answer is most often defended as "the work takes that
long" — and the work taking long is exactly why the answer must not.

## Status, measured 2026-09-16 at 15:01 on a quiet machine

**25 pass, 1 fail, 3 not measured.**

The count went 25/1 → 23/3 → 25/1 over the afternoon, and the middle number is the important one:
it fell because [the harness was corrected](#the-harness-was-sampling-the-lightest-session), not
because anything regressed, and it came back because the two failures it exposed were then fixed. Every number below came from
`tools/measure-operations.sh`; the "before" column is the same run against the build of that
morning.

| | before | after | |
|---|---:|---:|---|
| `GET /v1/orchestrator/tasks` | 4,300,745 B | **115,689 B** | 37× |
| Board first paint | 1,051,337 B | **106,510 B** | 9.9× |
| one item's detail | 58,270 B | **11,756 B** | 5.0× |
| the catalog alone | 36,646 B | 35,783 B | — |
| a poll that finds nothing changed | ~1,000,000 B | **0 B** (`304`) | — |
| event stream, steady, per viewer | ~3,300,000 B/min | **450 B/min** | ~7,000× |
| `GET /v1/sessions/<id>/links` | 320–390 ms, every call | **1 ms warm**, 290 ms cold | — |
| `GET /v1/sessions/<id>/transcript?limit=200` | 1,205,561 B in 614 ms | **153,123 B in 52 ms** | 7.9× |
| `GET /v1/sessions/<id>/skills` | 310 B in 243–434 ms | **1 ms warm** | — |
| durable document per mutation | 5,032,820 B | **4,832,058 B**, falling as items are touched | — |

### The harness was sampling the lightest session

Until 14:53 this harness took `.sessions[0]` as its subject. Session reads scale with the provider
record behind them, and the spread is not small:

| `transcript?limit=200` | bytes |
|---|---:|
| `%12` | **1,205,561** |
| `%11` | 285,071 |
| `%10` | 50,647 |

The same request, 24× apart, and `.sessions[0]` was one of the smallest. So every session row
passed while a megabyte response sat two rows down, and the failure was invisible for as long as
the scan kept ordering that session first. `/info` ran 0.055 s to 1.70 s across the same list for
the same reason.

It now picks the heaviest session by transcript size before it measures anything. `AGENTS.md` has
had the sentence for this the whole time — *a sample taken along one path measures that path* — and
this page was not following it.

It also now asks for `limit=200`, which is what both transports send
(`net/live.js`, and `TRANSCRIPT_LIMIT` in `net/cloud-client.js`). It had been asking for 50.

### The one that does not pass

**`GET /v1/board?project=<id>` without an audience, 1,050,931 bytes.** The legacy wire, deliberately
unchanged so that anything still holding it keeps working — an older hosted console, a script, a
Cloud `board` read from a page that has not reloaded. The Board page no longer takes this path, and
it stays a failure rather than being reclassified: a row moved out of the count because the current
design does not use it any more is a row that stops being looked at.



### Board write: 1.23 s to 0.20 s, measured 2026-09-17

It was **1.23 seconds** against a 500 ms budget, and nobody had taken the row because taking it
mutates the store. `tools/measure-operations.sh --write` takes it now: it sets one item's title to
the title it already has, which changes no content but adds a history entry and advances the
revision, because that is the only honest way to time a write.

Where it went, from `board: materialize` and `board: seed` in the app's log:

| | before |
|---|---:|
| `readSeed(rebuild:)` — re-materialize every item | 518 ms → index 49, summaries 126, **details 112** |
| build all 33 project models | **303 ms** |
| encode and `fsync` the durable document | 42 ms |

**One item changed, and 711 items and 33 projects were re-materialized to reflect it.**

The cut that mattered was not the one that looked biggest. Every project envelope carried its own
copy of the project catalog: 33 projects × a 35 KB catalog is **1.17 MB of dictionary copying**
inside the reply to every command, and the same rule this repository applies to every new route —
*a response carries one resource and links to the others* — had never been applied here. The
catalog is now held once in `ProjectBoardReadCache.Model` and spliced in on the way out. The wire
is unchanged: the old route's clients still read `board.projects` exactly where they always did.

**0.193–0.208 s** on a quiet machine, repeatably, and neither `board: seed` nor
`board: materialize` logs any more because both fell under their 200 ms thresholds.

Note what the durable write is *not*: 42 ms of the 1,230, and 93% of that 42 is the JSON encode
rather than the I/O — the `fsync` is 0.7 ms. Moving `receipts` (685 KB) and `evidence` (942 KB)
out of the aggregate would save about 16 ms of it, which is why
[the architecture page](architecture.md) records that measurement as a reason **not** to do the
segment-file change yet.

The app now logs this line whenever a materialization passes 200 ms, and says nothing when it does
not. The reason it was invisible this long is that nothing ever said it.

### Two rows that are still not measured

Session open and session close each start or cancel real work — a terminal, or somebody else's
descendants — so the sweep reports them rather than taking them, and there is no `--open` flag
because there is no harmless version of either. A row that is not measured is not a row that
passed.

### What has not been measured at all

**No reading has been taken through `app.clawdline.com`.** Everything above is loopback, which is
the half of the number this refactor controls — but the bar names the hosted console, and the
console serves a bundle deployed separately from the Mac app. `boardItems` does not appear in the
bundle it is serving today, so the Board there is still taking the old path regardless of what the
Mac now offers.
