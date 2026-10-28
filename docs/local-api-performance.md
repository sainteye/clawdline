# Owner-local API performance history

API level 31 adds `GET /v1/api-performance`. It is a separate owner-local
record from operator Cloud performance monitoring and from the existing
cumulative `/v1/diagnostics.routes` table. It never enters connection-health
upload, operator ingest, a notification, or a Cloud response.

The outer HTTP handler observes every request, including authentication
refusals, redirects, unmatched routes and static resources. It reuses
`routeKey`: an approved route shape is recorded, never a body, query value,
header, token, IP address, terminal, message, account/session identifier,
command line or environment. Shape overflow keeps a single aggregate row.
An observation records completion wall time, not time to first header.

## Permission and request contract

Use a direct connection to the selected daemon, authenticated with its local
token or an ordinary locally paired device's `read` credential. Read-only
devices do not need `send` or admin permission. Anonymous callers, unrelated
credentials, orchestrator-only credentials and task secrets cannot read it.
The existing diagnostics permission is unchanged.

All `ViaCloud` requests are explicitly refused, including an owner request
carried in process with the daemon's local token. Operator admin authority
does not grant this permission. A Cloud UI must currently show the direct
connection requirement instead of silently forwarding this request or reading
raw diagnostics as a fallback. Encrypted remote owner routing is not enabled
by this endpoint.

```
GET /v1/api-performance?hours=1|6|24|168&until=<Unix milliseconds>
```

`hours` defaults to 1; `until` defaults to now. The end is exclusive and
rounded down to the interval resolution. The returned `from_ms`, `until_ms`
and `resolution_seconds` are authoritative. Unknown or repeated parameters,
an invalid interval, a future end, an end older than the configured retention,
or a query string longer than 256 bytes are refused with 400
`api_performance_bad_query`. The string cap is checked before parsing.

| hours | Point resolution | Points | Range |
| ---: | ---: | ---: | --- |
| 1 | 60 seconds | 60 | Last complete hour of minute buckets |
| 6 | 3600 seconds | 6 | Six complete aligned hours |
| 24 | 3600 seconds | 24 | Twenty-four complete aligned hours |
| 168 | 3600 seconds | 168 | Seven complete aligned days, downsampled to hours |

The current partial interval is deliberately separate: `recent_minutes`
always has the five preceding complete minutes and the current minute,
oldest first, relative to **now**, even when `until` requests an older window.
`recent_slow_routes` merges those same six minutes. `slow_routes` merges only
the selected complete query window. Both lists sort by descending finite
latency sum, with route spelling as the tie-breaker. They contain every
retained route, at most 128 shapes plus overflow.

GET and HEAD return `Cache-Control: no-store`. HEAD has the same query and
permission checks and no body. Other methods are refused. The gate also sets
no-store on unauthenticated refusals for this path. Reader admission, worker
availability, query cancellation/deadline and response size failures return
503 `api_performance_unavailable`; the caller may retry later. A stopped
monitor is unavailable, not an empty healthy response. The outer gate uses
its existing 401/403 authentication envelope. Owner-local Cloud denial is
403 `api_performance_owner_local`.

## JSON contract and interpretation

The public structs are `apiperformance.Reading`, `Point`, `Totals`,
`Histogram`, `RouteSummary` and `Provenance`. All times ending in `_ms` are
Unix milliseconds, except age fields, which are elapsed milliseconds.

`totals` in each point/route and at the top level has these always-present
fields:

| Field | Meaning |
| --- | --- |
| `completed` | HTTP requests completed once, including streams when they end |
| `status_2xx`, `status_3xx`, `status_4xx`, `status_5xx` | Completed status classes, excluding cancellations |
| `cancelled` | Cancelled request context, failed response write, or explicit 499; excluded from 5xx and 4xx |
| `stream_opened` | Successful SSE/101 opens in the interval; not added to `completed` |
| `stream_completed` | Streams ending in the interval |
| `finite_latency_ms` | Fixed histogram of completed finite request durations, including cancelled finite requests |
| `stream_lifetime_ms` | Separate fixed histogram of completed stream lifetimes |

An SSE response is a successful `text/event-stream` response. HTTP 101 is
also treated as streaming. A still-open stream contributes to live open
counts; its eventual lifetime never enters finite latency. Panic completion
is a server failure in the new history and the panic still propagates to the
HTTP server. Informational 1xx headers do not replace the final status.

Every histogram has `bins` (16 integer counts), `count`, `sum_ms`, and
`max_ms`. The first 15 bins have inclusive upper bounds
`[1,2,5,10,20,50,100,200,500,1000,2000,5000,10000,30000,60000]` ms; the final
bin has no finite upper bound. To merge, add each corresponding bin, count
and sum, and take the larger maximum. Never average percentiles. For a
nearest-rank percentile, find the first cumulative bin reaching
`ceil(count * percentile / 100)` and show that bin's upper bound. An empty
histogram has no percentile; an overflow percentile is `>60000 ms`, not zero.
`max_ms` is an observed maximum, not a quantile approximation. A mean, if
needed, is `sum_ms / count` only when count is positive.

Each point has `start_ms`, `end_ms`, `covered_seconds`, `state`, `totals` and
`source`. The states distinguish `missing` (no coverage or observations),
`partial` (some observations or incomplete coverage), `no_traffic` (covered
interval with no completions/new stream opens) and `ok`. No traffic does not
claim that an already-open stream sent no bytes: payloads are never measured.
Coverage is a union of observed second slots, not additive elapsed time.
Restart/tick repetition cannot duplicate it. At most two elapsed seconds
are covered by a delayed tick; earlier paused time stays missing.

`dropped` is the observed cumulative lost-event count, restored from the
last successful checkpoint. `loss_hours_ms` locates loss conservatively by
hour: otherwise covered points in those hours become `partial`. Older loss
ages out with retention, without making future clean hours partial. If bounded
atomic admission cannot locate a loss, `loss_scope_known` is false and all
otherwise covered points become partial instead of inventing a location.
`queued_events` reports asynchronous backlog; otherwise covered points are
also partial while observations await processing. Queue loss and crash loss
are different: an abrupt crash can lose
uncheckpointed observations without a recoverable exact lost count.

`source` on the reading is `owner_local`. Top-level `boot_id`, `build`,
`started_ms`, `at_ms` and `last_tick_ms` identify the current producer. Each
point's `source` identifies its original boot/build. A bucket spanning a
restart or build has `source: {"mixed":true}` instead of attributing old
observations to the new build. `stale` means its worker tick is older than
five seconds or the clock moved behind that tick/history/checkpoint. Missing buckets remain
missing after a restart; no history is synthesized for daemon downtime.

`active`, `open_streams` and `oldest_active_age_ms` are current, best-effort
atomic observations, independent of the query window. Active includes
streams. Start times use 512 slots; when any active start is untracked,
`active_coverage_complete` is false and oldest age is null. Counts continue
even when start-time slots are exhausted. Zero active work has age zero.

`storage_state` is `ok`, `unavailable` or `snapshot_too_large` after opening.
`store_failures` counts failures this boot. `persisted_at_ms` advances only
after SQLite commits successfully; zero means no durable checkpoint.
`checkpoint_interval_seconds` is the intended 15-second cadence, **not a
guarantee that only 15 seconds can be lost**. `unpersisted_seconds` measures
time since the last successful checkpoint (or this boot's start when none
exists), and grows during a failed or delayed writer. Retention floors state
the effective policy, not proof of continuous observations within it.

This synthetic example abbreviates the point, recent and route arrays for
readability; real responses use the point counts above and six recent minutes. It is not a
measurement from a person's machine.

```json
{
  "schema": 1,
  "source": "owner_local",
  "boot_id": "0123456789abcdef0123456789abcdef",
  "build": "synthetic-build",
  "at_ms": 1767355201000,
  "started_ms": 1767351600000,
  "last_tick_ms": 1767355200000,
  "stale": false,
  "from_ms": 1767351600000,
  "until_ms": 1767355200000,
  "resolution_seconds": 60,
  "retention_floor_ms": 1766750400000,
  "minute_floor_ms": 1767351600000,
  "persisted_at_ms": 1767355200000,
  "checkpoint_interval_seconds": 15,
  "unpersisted_seconds": 1,
  "storage_state": "ok",
  "store_failures": 0,
  "dropped": 0,
  "loss_hours_ms": [],
  "loss_scope_known": true,
  "queued_events": 0,
  "route_overflow": 0,
  "active": 0,
  "open_streams": 0,
  "oldest_active_age_ms": 0,
  "active_coverage_complete": true,
  "histogram_upper_bounds_ms": [1,2,5,10,20,50,100,200,500,1000,2000,5000,10000,30000,60000],
  "points": [{
    "start_ms": 1767355140000,
    "end_ms": 1767355200000,
    "state": "ok",
    "covered_seconds": 60,
    "source": {"boot_id":"0123456789abcdef0123456789abcdef","build":"synthetic-build","mixed":false},
    "totals": {
      "completed": 1, "status_2xx": 1, "status_3xx": 0, "status_4xx": 0, "status_5xx": 0,
      "cancelled": 0, "stream_opened": 0, "stream_completed": 0,
      "finite_latency_ms": {"bins":[0,0,0,1,0,0,0,0,0,0,0,0,0,0,0,0],"count":1,"sum_ms":8,"max_ms":8},
      "stream_lifetime_ms": {"bins":[0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0],"count":0,"sum_ms":0,"max_ms":0}
    }
  }],
  "recent_minutes": [],
  "totals": {
    "completed": 1, "status_2xx": 1, "status_3xx": 0, "status_4xx": 0, "status_5xx": 0,
    "cancelled": 0, "stream_opened": 0, "stream_completed": 0,
    "finite_latency_ms": {"bins":[0,0,0,1,0,0,0,0,0,0,0,0,0,0,0,0],"count":1,"sum_ms":8,"max_ms":8},
    "stream_lifetime_ms": {"bins":[0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0],"count":0,"sum_ms":0,"max_ms":0}
  },
  "slow_routes": [],
  "recent_slow_routes": []
}
```

## Local lifecycle and bounds

`StartAPIPerformance` runs once in daemon startup. It returns immediately;
opening the store runs off startup and request paths. Each request completion
tries a short in-memory admission lock and a bounded queue, never waits on a
writer, and sheds/counts when admission fails. The single aggregation worker
owns history. Queries use that worker, with a separate bounded reader door.
A pending query cannot hold a business lock. Its deadline/cancellation is
checked during the bounded scan; the HTTP response gets a write deadline.

The store lives at `<daemon cfg.Dir>/api-performance/history.sqlite`.
The directory is mode 0700 and the database is mode 0600 on Unix; Windows
inherits the state directory's owner access policy. It uses the existing
pure-Go SQLite dependency, one dedicated connection, DELETE journaling,
FULL synchronization, busy timeout zero and no automatic retries. It shares
no connection, transaction or lock with business storage. The writer retains
one in-flight and one coalesced pending cumulative JSON snapshot. SQLite
revision compare-and-swap fences another boot from overwriting a newer
checkpoint. Replacement is atomic; replay does not re-add completions.

Malformed/unknown schema, oversized reads, locking, full disk and write
failures disable persistence until restart. Existing unreadable bytes are
preserved. Memory observation and owner reads continue with explicit storage
status; a hung store cannot hold request completion. Shutdown drains admitted
events and attempts a checkpoint, with a bounded wait from the daemon. A
filesystem operation that ignores cancellation can outlive that wait; no
second writer/retry is started. Abrupt termination loses the actual
unpersisted window; the next boot exposes its uncovered time.

All of these limits are `diagnostics.api_*` capacity rows, with runtime
readings and English/Taiwan Traditional Chinese explanations. The environment
capacity override can lower each default; it cannot raise one.

| Suffix | Default | Behavior at limit |
| --- | ---: | --- |
| `queue` | 1024 observations | Nonblocking drop, increment `dropped` |
| `routes` | 128 shapes | Aggregate new shapes in overflow |
| `route_bytes` | 256 bytes | Oversized shape becomes overflow |
| `minutes` | 61 buckets | Remove oldest minutes |
| `hours` | 169 buckets | Remove oldest hours |
| `retention` | 608400 seconds | At most seven days plus one partial aligned hour |
| `active` | 512 start-time slots | Keep counts; oldest age becomes unknown |
| `readers` | 2 concurrent readers | Refuse excess readers |
| `query_bytes` | 2 MiB | Refuse an oversized JSON response |
| `query_seconds` | 2 seconds | Stop cancelled/deadline reads; response write deadline |
| `store` | 64 MiB database | SQLite page cap; refuse background growth |
| `snapshot` | 32 MiB encoded checkpoint | Refuse oversized write |
| `catchup` | 2 seconds per delayed tick | Earlier elapsed seconds remain missing |

DELETE rollback journaling needs an additional database-sized journal plus
per-page/header overhead: peak store-owned disk is below 129 MiB for the
64 MiB database cap, rather than a claimed 64 MiB total. There are no segment
scans or file-deletion retries. Each pruning pass examines at most 61 minute
and 169 hour buckets; checkpoint replacement reuses database pages. Query
work is at most 168 hourly points, 128 retained route shapes plus overflow,
and six current minute points. Histogram storage is fixed, never a sample
ring used as a window percentile.

## Verification and integration

Unit/transport tests cover inclusive histogram bounds, exclusive time-window
ends, merged quantiles, no traffic/missing/partial/stale coverage, mixed boot
provenance, cancellation versus 5xx, finite versus stream timing, route
privacy/overflow, bounded admission, disk-full rollback, unsupported schema,
restart/deduplication, concurrent writer fencing, checkpoint coalescing and
the real paired-device/local-token/Cloud gate.

The capacity declaration guard was run before registration and failed on the
new constants/channels; after registration it passed. Snapshot generators are:

```
go test ./internal/domain/capacity -run TestTheRegisterMatchesItsSnapshot -update
go test ./internal/transport/http -run TestCapacityMeasuresMatchTheirSnapshot -update
go run ./tools/contract-gen
```

The route inventory also needs `api/v1/agent-routes.json`; contract-gen updates
both route guides and both generated capacity guides. This is discoverability,
not broader Agent authority. UI, encrypted remote owner routing, operator Cloud
collectors, deployment, installation and release acceptance belong to the
integrating feature, not this producer.

## Integrating root measurements (2026-10-28)

Apple M4 Pro, Go 1.27.1, task-isolated checkout, `go test ./internal/domain/apiperformance -run '^$' -bench . -benchtime=100ms -benchmem`: completion admission 378.7 ns/op, 64 B/op, one allocation; bounded aggregation 30.719 µs/op with zero allocation; worst-case seven-day read 4.767 ms/op and 443,229 B/op; bounded SQLite checkpoint 6.514 ms/op. These are local microbenchmarks, not production overhead or service-level guarantees.

The integrating root also removed only the `MaxQueryBytes` registration source in this disposable checkout: `TestEveryBoundIsRegistered` failed specifically on `internal/domain/apiperformance.MaxQueryBytes`. The source was restored before the green gate. The original uncommitted child was recovered with a hashed 27-file archive after timeout; no running daemon was restarted during recovery.
