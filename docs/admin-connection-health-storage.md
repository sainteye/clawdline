# Connection health: durable local storage

Status: 2026-10-20. `internal/adapters/healthjournal` is implemented and tested, and the daemon
opens it: `cmd/clawdline/health.go` `startConnectionHealth` opens it under
`<state>/connection-health/journal` on its own goroutine after the runtime starts (never on the
startup path), and `Watchdog.Attach` makes the watchdog's one journal writer hand each record it
appended, as the journal stamped it, to `Offer` — no `Journal.Read` per record; the ring is read
once, at attach, under the writer's lock, so nothing is offered twice or skipped. A failed open is
reported by its code on the status route and the boot stays memory only. The in-memory ring in
`internal/domain/connectionhealth` still says `memory_only_lost_on_restart`; this store does not
relabel it. `GET /v1/connection-health/history?cursor&limit` and `clawdline health history` page
it (this machine's own token only, never through Cloud). Product scope is
`docs/admin-connection-health.md`; the record contract is `docs/admin-connection-health-contract.md`.

## 1. What it is for

The foundation journal loses everything when the daemon restarts. This store keeps copies of the
records that journal already stamped, so a later boot can page through what an earlier boot saw,
with each boot and build named, and with every loss, prune and damaged byte counted instead of
silently missing.

## 2. API

```go
s, err := healthjournal.Open(healthjournal.Options{Dir, BootID, Build, Clock})
rc, err := s.Offer(rec)            // rec: a stamped connectionhealth.Record of this boot
state := s.Commitment(rc.Ticket)   // queued | written_unsynced | synced | lost | unknown
synced, err := s.Flush(ctx)        // write + fsync everything admitted; the durability receipt
page, err := s.Read(healthjournal.Query{Cursor, Limit})
st := s.Status()
err = s.Close(ctx)                 // <-s.Done() closes when the worker exited and the lock is free
```

- `Options.Dir` must be absolute and clean. It is created `0700`. A symlink, a non-directory, a
  directory owned by another user or writable by group/other (Unix) is `unsafe_directory`.
  `BootID` and `Build` follow the foundation's opaque-id rule.
- `Open` takes an exclusive non-blocking lock on `Dir/LOCK` (`flock` on Unix, `LockFileEx` on
  Windows; released by the kernel when the process dies). A second opener gets `locked`.
- `Open` scans every segment synchronously and enforces retention before it returns. Measured on a
  full 96 MiB store: 0.57 s (Apple M4 Pro, SSD). Call it off the daemon's startup critical path.
- `Offer` takes a record the foundation stamped; the daemon hands it the record the journal
  writer just appended (`Watchdog.Attach`), never re-reading the ring per append (see §8).

## 3. Contract by failure

| Situation | Caller sees | Counted in |
| --- | --- | --- |
| Record of another boot, unstamped, or with any field the foundation would refuse | `invalid_record` naming the field, never its value | `refused_invalid` |
| Encoded record > 1,024 B (foundation `MaxRecordBytes`) | `record_too_large` | `refused_too_large` |
| Queue at 1,024 records or 2 MiB | `queue_full` at once; never waits | `refused_queue_full`; a `queue_full` loss marker is written before the next record that reaches disk |
| Offer after Close | `closed` | — |
| Disk full on write | first sheds the oldest sealed segment and retries once; then the batch is lost | `disk_full` state, `shed_on_disk_full`, `lost_records`; tickets answer `lost`; a `write_failed` loss marker precedes the next written records |
| Read-only, permission denied, other I/O error | batch lost, next attempt after 1 s doubling to 60 s | state `read_only` / `permission_denied` / `io_error`, `lost_records` |
| A write or fsync running > 5 s | Offer still answers; the queue fills and refuses | Status state `stalled` (read from an atomic start time, no lock) |
| A failed write left half a frame | the segment is truncated back to its last whole frame and abandoned; the next write opens a new segment | — |
| Failed fsync | the segment is abandoned; `synced_through` does not advance | state `io_error` |
| Close's context ends first | `shutdown_canceled`; the worker drops what is queued as soon as its current system call returns, then exits and frees the lock | `dropped_at_close`, `lost_records`, tickets `lost` |
| Five concurrent readers | the fifth gets `readers_busy` at once | — |
| Bad or forged cursor; cursor past the newest segment or offset | `invalid_cursor` | — |
| Limit > 200 | `query_bound_exceeded` | — |

Every error is `*healthjournal.Error{Code, Field}`; no path, file name or operating-system message
appears in an error, a page or a status.

## 4. Durability

- `Offer`'s receipt is always `admitted_memory_lost_on_crash`. It never says durable.
- `Commitment(ticket)` says `written_unsynced` once the bytes are in the file (survive a process
  crash), `synced` after fsync, `lost` when a failure or a canceled close dropped it, and `unknown`
  for a ticket never issued or one in a lost range that has been forgotten (the store remembers
  the newest 64 lost ranges).
- There is no fsync per event. The worker fsyncs at most every 5 s while there are unsynced
  writes, on every `Flush`, when sealing a segment and at `Close`.
- **Crash-loss window** (also `Status.crash_loss_window`): a process crash loses what is still
  queued — at most 1,024 records / 2 MiB. An operating-system crash or power loss additionally
  loses what was written in the last 5 s before an fsync. Refusals not yet followed by a written
  record are only in memory counters and are lost with the process.

## 5. On disk

```
Dir/LOCK                       advisory lock; never deleted
Dir/seg-0000000000000001.chj   segments, index ascending across all boots
```

Frame: `u32le length | u32le CRC-32C(type‖payload) | u8 type | payload(JSON)`, at most 4,096 B.
A segment is one `segment` frame (`clawdline.connection-health.segment/1`, its index, boot,
build, creation time), then `record` and `loss` frames, then at most one `seal` frame. Each boot
starts a new segment on its first write; a segment is sealed before a frame would take it past
8 MiB (room for the seal is reserved).

**Recovery.** Every file named like a segment is opened only if it is a regular file (checked
with `Lstat`, then `SameFile` on the open handle). Its header must parse strictly and name the
same index, or the file is *foreign*: preserved, never deleted, its bytes not counted, its name
never reused. Frames are read until the first torn or corrupt frame; the rest of that file is
`torn_bytes` and is never read. Every record payload is decoded strictly (unknown fields refused)
and validated with the same rule `Offer` uses — schema, boot, `event_id` = `boot-seq`, increasing
sequence, enum allowlists, location and id patterns, build only on `boot` — so a well-framed but
forged record is skipped and counted (`invalid_frames`, a page's `skipped_invalid`) and never
reaches an export. Reads re-validate every frame and the header, since the file can change after
`Open`. Other file names are counted as `unrecognized_files` and never touched. Only the first
4,096 directory entries are looked at; past that recovery says `listing_truncated`.

## 6. Retention

Time or bytes, whichever comes first, and a cap on file count:

| Bound | Value | At the bound |
| --- | --- | --- |
| Age (`cloud.health_store_seconds`) | 7 days | delete sealed segments whose newest record (or creation, if empty) is older |
| Bytes (`cloud.health_store_bytes`) | 100 MiB | delete the oldest sealed segment |
| Files (`cloud.health_store_segments`) | 256 | delete the oldest sealed segment |

Only this store's own sealed segments are deleted — never the active one, never a foreign or
unrecognized file — and only after `Lstat` + `SameFile` confirm the name still holds the file the
store scanned or created. Retention runs in `Open`, after each segment seal, every minute, and
once (shedding one segment) on a full disk. Age uses wall time from the record stamps: a clock set
backwards delays age pruning, never byte pruning.

Every page and Status carry the **actual** retention floor (`retention_floor`: wall time of the
oldest retained record; absent when nothing is retained — unknown, not "now") next to the target,
and `pruned_before_target_total` counts segments removed by bytes, count or shedding rather than
age, so a reader never takes 100 MiB of a busy day for seven days.

## 7. Reading

`Read(Query{Cursor, Limit})` returns `clawdline.connection-health.durable-page/1`: entries oldest
first (`record` entries carry the validated foundation record; `loss` entries say how many records
never reached disk and why), the boots and builds they belong to, and coverage: quality
(`partial` on any gap, skipped frame or loss in the page), the durability statement
`disk_unsynced_tail_lost_on_os_crash`, retention floor and targets, `truncated`, `next_cursor`, and
the store's pruned / lost / refused totals.

A cursor is `v1.<segment>.<offset>`. Both numbers never change once written, so a cursor from an
earlier boot keeps working in a later one. A cursor into a deleted segment answers from the next
retained one with `gap.reason = pruned` (or `missing_segment` when a middle file vanished); a
segment's unreadable tail answers `unreadable_tail`, a frame that fails mid-read `corrupt_frames`.
When a gap follows entries, the page stops there (`truncated`) so the next page starts with it.
A page is at most 200 entries and 1 MiB of payload (the foundation's query bounds).

Readers copy the segment list under a read lock held for the copy only, open their own handles
and read only up to the length the worker had published, so a reader never sees a half-written
frame and never blocks a producer, the worker or Status.

Authorization is not here. The route or CLI that exposes `Read` must check scope (owner on this
machine) before calling it; the page is content-free but still describes this machine's activity.

## 8. Measured cost

`go test -bench . ./internal/adapters/healthjournal` and `HEALTHJOURNAL_MEASURE=<scratch dir>
go test -run TestMeasureFullStoreRecovery -v`, Apple M4 Pro, macOS, APFS SSD, 2026-10-20:

| Measure | Result |
| --- | --- |
| `Offer` (validate, encode, enqueue) | 2.0 µs, 8 allocations |
| Foundation `Journal.Append` alone (store off) | 1.2 µs |
| Append + `Journal.Read` of the one new record + `Offer` | 22 µs — the ring re-read dominates; integration should pass the stamped record directly |
| Write + fsync of 1,000 records (`Flush`) | 6.4 ms, 6.4 µs/record, 371 B/record on disk |
| Fill 96 MiB (260,049 records, 12 segments) | 0.54 s |
| `Open` of that full store (recovery scan) | 0.57 s |
| One 200-entry page | 0.48 ms, 521 KB allocated |
| Idle store over two worker ticks | 0 writes, 0 fsyncs (`TestAnIdleStoreDoesNoIO`) |

These are one machine's numbers, not service-level targets.

## 9. Bounds

All registered in `internal/domain/capacity/register_healthjournal.go` and measured from the
open store's `Status` (`internal/transport/http/capacity_measures_connection_health.go`; a known
zero while it is opening or monitoring is off, unmeasured with its code when it failed to open): `cloud.health_store_bytes` 100 MiB, `_seconds` 604,800, `_segment_bytes` 8 MiB,
`_segments` 256, `_queue_rows` 1,024, `_queue_bytes` 2 MiB, `_frame_bytes` 4,096, `_readers` 4,
`_dir_entries` 4,096, `_lost_ranges` 64. Segment occupancy is the largest retained
segment; lost ranges are counted live. Directory occupancy is the number of
entries examined during the opening scan, not a fresh directory enumeration;
truncated recovery remains a failure. Cadences, not held state: fsync every 5 s, retention every
minute, stall after 5 s, worker tick 1 s, write retry backoff 1 s → 60 s.

## 10. Not done here

- `Close` at shutdown is done for SIGTERM and interrupt (`stopConnectionHealthOnSignal`): the
  watchdog and its writer stop first, then `Flush` and `Close` within 3 s, then the signal's own
  action ends the process. A kill without a signal handler (SIGKILL, a crash) is still the
  crash-loss window of §4.
- Upload is `internal/adapters/healthupload` (contract §9a); it reads pages with `Read` and keeps
  its own cursor, so the store is unchanged by it.
- Measured budgets: the bounds above are the plan's proposals.
- Three store rows (`_segment_bytes`, `_dir_entries`, `_lost_ranges`) read as unmeasured: `Status`
  does not report their occupancy.
- Windows and Linux were compiled and vetted, not run.
