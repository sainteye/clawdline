# Bots roster and conversation reads stall behind the writer (2026-11-23)

## Affected action

Opening the Bots list or one Bot conversation on a phone through Cloud. The
person reported the list often shows "暫時無法載入 Bot 清單" and conversation
content loads slowly. This is a current user-facing failure, the top priority
class in AGENTS.md.

## Baseline (production log, dev v3.2.0-dev.e60dd8c)

Source: `daemon.log` `read slot released` lines for 2026-11-23, real phone
requests through Cloud, rail `ctl`. Handler time is measured inside the daemon
after the read slot is acquired; queue time is the wait for one of the 8 slots.

| operation | n | median handler ms | max handler ms | mean queue ms |
|---|---|---|---|---|
| project-assistants.attachment | 158 | 22 | 12757 | 35 |
| project-assistants.view | 64 | 150 | 18362 | 76 |
| project-assistants.groups | 20 | 5122 | 18052 | 2158 |
| project-assistants.handoff-card | 32 | 20 | 18670 | 397 |
| project-assistants.dev-servers | 31 | 14 | 13863 | 439 |
| project-assistants.list | 4 | 19 | 4826 | 1121 |
| project-assistants.configuration-profile | 6 | 7231 | 20394 | 14 |

`read workers occupied ... active=8` appeared 214 times that day.

The decisive observation is not the slow operations' medians but that
operations whose handler normally takes 1–20 ms (attachment, handoff card, dev
servers) took 10–18 s **at the same moments**, and their slot releases cluster
on the same second (for example four handlers released at 15:45:45 after
10050–10113 ms, six at 15:46:11 after 1297–1377 ms). Independent slow work
does not finish together; a shared resource being released does.

So the 8-slot head-of-line blocking Atlas observed is a symptom. The slots were
full because every Bot read was waiting for the same thing.

## Root cause

Every Bot read over HTTP calls `projectAssistantHTTP.status`
(`internal/transport/http/project_assistants_http.go:53`), which reaches
`Host.Status` (`internal/adapters/projectassistanthost/host.go`) through
`projectAssistantSource` → `projectAssistantFeature` and again through
`projectAssistantAuthority`, and once more before publication. `Host.Status`
ran three SELECTs (`sqlite_schema`, `project_assistant_schema`, an `EXISTS`
over bots/groups) on `Options.Database`, which is the store's **writer**
handle, opened with `SetMaxOpenConns(1)`
(`internal/adapters/store/sqlite.go:177`). That one connection is shared by
every writer in the daemon: Bot turns, the broker, workflow launch
reconciliation (296 log lines that day), Session indexes.

While any write transaction held that connection, every Bot read — including
the 1 ms attachment read — queued for it. Eight such reads filled the Cloud
read slots, the roster read waited behind them, and the phone's deadline
expired. The query-only pool (`ReadConnectionsLimit` = 4 on the same WAL file)
already existed and was already handed to the Host as `Options.Reader`, but
only the conversation view used it.

`GroupStore.GroupPreviews`, the roster's group read, was a pure
single-statement read on the same writer, which is why `groups` had a 5 s
median, not just a slow tail.

## Why earlier fixes did not end it

`ed72b9df1`, `e8de3ed46`, `87be0d442` and `7b4e686bc` changed what the console
records, when the roster reappears, pull-to-reload and ordering. They made the
failure visible and recoverable on the client; none changed which connection
the daemon's Bot reads wait on, so the stall recurred under write load.

## Change

- `Host.Status`/`inspect` read through `statusReader()` — `Options.Reader` when
  present, the writer only as a fallback for fixtures without one. Startup
  (`New`) still inspects on the writer before installing schemas.
- `GroupStore.WithReader` routes `GroupPreviews` to the query-only pool. Writes
  and read-decide-write paths keep the writer.

No new bound was introduced: the reader pool and its limit already exist and
are registered.

## After (failure injection, same conditions before and after)

`internal/adapters/projectassistanthost/status_reader_test.go` holds a write
transaction on the writer and calls the read with a 2 s deadline.

| test | before | after |
|---|---|---|
| `TestStatusAnswersWhileTheWriterIsHeld` | fails: `context deadline exceeded after 2.002s` | passes, 0.07 s including fixture |
| `TestGroupPreviewsAnswerWhileTheWriterIsHeld` | fails: `context deadline exceeded after 2.002s` | passes, 0.07 s including fixture |

Before the change the read blocks for exactly as long as the writer is held;
after it, the read does not depend on the writer at all.

## Not measured yet

The end-to-end phone timings after the change (list open → list shown, Bot
open → content shown, five runs each, Cloud and local) need this commit
running as the dev install. The child that made the change may not install dev
or restart the person's daemon. Compare the same log fields after installation;
the expectation is that `attachment`, `handoff-card` and `dev-servers` maxima
fall to their medians and `groups` falls well below 5 s.

Other Bot reads that still use the writer and may hold a slot under write load:
the handoff-card and dev-server handlers' own store reads and
`configuration-profile` (7 s median, which looks like a separate external
probe). They were not changed here.

Later changes moved more of these reads to the reader. `Store.WithReader` now
lends the query-only pool to independent Bot projections, including the
handoff-card read, the Bot list and the send receipt
(`TestBotHandoffReadAnswersWhileTheWriterIsHeld`,
`TestBotListAndSendReceiptAnswerWhileTheWriterIsHeld`), and
`GroupStore.WithReader` also routes the group list and room `History`. The
dev-server catalog still opens on the writer.
