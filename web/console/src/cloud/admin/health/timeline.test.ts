// Placing stored evidence on time and stages: what the charts may and may not draw.
// `node --experimental-strip-types --test web/console/src/cloud/admin/health/timeline.test.ts`.
// Records are built here; none is Cloud's.
import { test } from "node:test"
import assert from "node:assert/strict"
import type { Coverage, StoredEvent, StoredGroup, StoredSource } from "./stored.ts"
import type { PanelQuality } from "./timeline.ts"
// @ts-expect-error -- a `.ts` path, for node.
import { axisOf, bottleneck, eventLanes, filterLoaded, groupLanes, problems, qualitySummary, seqLanes, sourceMarks, stageMatrix, traceLanes, windowBands } from "./timeline.ts"

const FROM = Date.parse("2026-10-20T11:00:00.000Z")
const TO = Date.parse("2026-10-20T12:00:00.000Z")
const at = (minutes: number) => new Date(FROM + minutes * 60_000).toISOString()
const WINDOW = { from: at(0), to: at(60) }

function ev(seq: number, minute: number, over: Partial<StoredEvent> = {}): StoredEvent {
  return {
    id: `he_${over.source_id ?? "s1"}_${over.boot_id ?? "b1"}_${seq}`, account: "acct_private_1", machine_id: "m1", source_id: "s1", producer_instance: "daemon",
    schema: "clawdline.connection-health.record/1", producer_seq: seq, event_id: `b1-${seq}`, boot_id: "b1", batch_id: null, build_commit: null,
    event: "scheduling_gap", wall: at(minute), mono_ns: seq * 1_000_000_000, source: "daemon", stage: null, class: null, kind: null, phase_id: null,
    correlation: null, count: 1, rule: null, outcome: null, elapsed_ms: null, state: null, received_at: at(minute), retain_until: at(2000), ...over,
  }
}

const coverage = (gaps: Coverage["gaps"]): Coverage => ({ retention_floor: null, truncated: false, sequence: "exact", sequence_reason: null, seq_ranges: [], gaps, gap_edges: null, dropped: null })
const axis = axisOf(WINDOW, WINDOW.to, null)!

test("sparse records are marks at their receipts, never a healthy interval between them", () => {
  const lanes = eventLanes([ev(1, 5), ev(2, 50, { event: "stall_detected" })], axis, null, null)
  assert.equal(lanes.length, 1)
  assert.deepEqual(lanes[0].marks.map((m: { at: number; kind: string }) => [Math.round(m.at * 60), m.kind]), [[5, "other"], [50, "stall"]])
  // Nothing describes the 45 minutes between: no band, and no band kind that could mean "healthy".
  assert.deepEqual(lanes[0].gaps, [])
  assert.deepEqual(windowBands(axis, at(5), false), [])
})

test("a record received after a higher sequence is late, and order stays the producer's", () => {
  const lanes = eventLanes([ev(8, 30), ev(7, 31), ev(9, 32)], axis, null, null)
  const l = lanes[0]
  assert.equal(l.late, 1)
  assert.deepEqual(l.marks.map((m: { seq: number; late: boolean }) => [m.seq, m.late]), [[7, true], [8, false], [9, false]])
})

test("producer clocks that step back are counted within the boot and never move a receipt", () => {
  const lanes = eventLanes([ev(1, 10, { mono_ns: 5e9 }), ev(2, 20, { mono_ns: 4e9, wall: at(-30) }), ev(3, 30, { mono_ns: 6e9 })], axis, null, null)
  assert.equal(lanes[0].monoBackwards, 1)
  assert.equal(lanes[0].wallBackwards, 1)
  // Placement follows the server receive time; the backwards wall clock does not drag seq 2 out of the window.
  assert.deepEqual(lanes[0].marks.map((m: { at: number }) => Math.round(m.at * 60)), [10, 20, 30])
  assert.equal(lanes[0].before, 0)
})

test("a listed gap sits between its held neighbours' receipts, or is counted as unplaced", () => {
  const gaps = [
    { source_id: "s1", machine_id: "m1", boot_id: "b1", from: 3, to: 4, count: 2, cause: "unexplained" as const },
    { source_id: "s1", machine_id: "m1", boot_id: "b1", from: 9, to: 9, count: null, cause: "unverified" as const },
    { source_id: "other", machine_id: "m2", boot_id: "b9", from: 1, to: 1, count: 1, cause: "unexplained" as const },
  ]
  const lanes = eventLanes([ev(2, 12), ev(5, 24)], axis, coverage(gaps), null)
  assert.equal(lanes[0].gaps.length, 1)
  const g = lanes[0].gaps[0]
  assert.equal(g.kind, "gap")
  assert.equal(g.count, 2)
  assert.deepEqual([Math.round(g.from * 60), Math.round(g.to * 60)], [12, 24])
  // seq 9 has no held neighbour above it; another source's gap never lands on this lane.
  assert.equal(lanes[0].gapsUnplaced, 1)
})

test("records outside the window are counted at its edges, not drawn inside", () => {
  const lanes = eventLanes([ev(1, -5), ev(2, 15), ev(3, 61), ev(4, 70)], axis, null, null)
  assert.equal(lanes[0].before, 1)
  assert.equal(lanes[0].after, 2)
  assert.deepEqual(lanes[0].marks.map((m: { seq: number }) => m.seq), [2])
})

test("retention, rows not loaded and time after the read are explicit bands", () => {
  const a = axisOf(WINDOW, at(50), at(10))!
  const bands = windowBands(a, at(30), true)
  assert.deepEqual(bands.map((b: { kind: string; from: number; to: number }) => [b.kind, Math.round(b.from * 60), Math.round(b.to * 60)]), [
    ["before_retention", 0, 10], ["not_loaded", 10, 30], ["after_as_of", 50, 60],
  ])
  // No more pages: nothing older is claimed missing.
  assert.equal(windowBands(a, at(30), false).some((b: { kind: string }) => b.kind === "not_loaded"), false)
  assert.equal(axisOf(null, null, null), null)
  assert.equal(axisOf({ from: at(10), to: at(5) }, null, null), null)
})

test("each boot is its own lane, ordered by the Cloud's generation, unknown generation last", () => {
  const sources = [{ source_id: "s1", boots: [{ boot_id: "b-new", server_generation: 2 }, { boot_id: "b-old", server_generation: 1 }] }] as unknown as StoredSource[]
  const lanes = eventLanes([ev(1, 40, { boot_id: "b-new" }), ev(1, 5, { boot_id: "b-old" }), ev(1, 20, { boot_id: "b-gone" })], axis, null, sources)
  assert.deepEqual(lanes.map((l: { boot_id: string; generation: number | null }) => [l.boot_id, l.generation]), [["b-old", 1], ["b-new", 2], ["b-gone", null]])
  // Each boot's sequence starts again: the same seq 1 in three boots is not a reorder.
  assert.equal(lanes.reduce((n: number, l: { late: number }) => n + l.late, 0), 0)
})

test("no account id reaches any chart shape", () => {
  const svc = [ev(1, 10, { source: "relay", producer_instance: "relay-worker", source_id: "r", stage: "worker_admitted", outcome: "ok", elapsed_ms: 3, account: "svc:relay" })]
  const group = { account: "acct_private_1", machine_id: "m1", source_id: "s1", boot_id: "b1", phase_id: 1, correlation: "c1", group_outcome: "stall_detected", group_quality: "complete", group_reason: null, last_event: "stall_detected", last_stage: "x", class: null, first_seq: 1, last_seq: 2, events: 2, dropped: null, last_received_at: at(20), evidence_age_ms: 1000, last_wall: at(20), rule_version: "cloud-derive/1" } as StoredGroup
  const all = JSON.stringify([eventLanes([ev(1, 5), ...svc], axis, null, null), groupLanes([group], axis, null), stageMatrix(svc), traceLanes(svc), problems([group], null, null, 0)])
  assert.equal(all.includes("acct_private_1"), false)
  assert.equal(all.includes("svc:relay"), false)
})

test("empty reads draw nothing and claim nothing", () => {
  assert.deepEqual(eventLanes([], axis, null, null), [])
  assert.deepEqual(stageMatrix([]), [])
  assert.equal(bottleneck([], WINDOW.to), null)
  assert.deepEqual(problems(null, null, null, 0), [])
  assert.equal(seqLanes(null), null)
  assert.equal(qualitySummary([], null).overall, "not_collected")
})

test("the overall quality is the weakest read's, and a failed or stale read caps it at partial", () => {
  const q = (parts: { quality: PanelQuality["quality"]; stale?: boolean }[]) => qualitySummary(parts.map((p, i) => ({ part: `p${i}`, quality: p.quality, stale: p.stale ?? false, reason: null })), null).overall
  assert.equal(q([{ quality: "complete" }, { quality: "complete" }]), "complete")
  assert.equal(q([{ quality: "complete" }, { quality: "partial" }]), "partial")
  assert.equal(q([{ quality: "complete" }, { quality: "failed" }]), "partial")
  assert.equal(q([{ quality: "complete", stale: true }]), "partial")
  assert.equal(q([{ quality: "partial" }, { quality: "not_collected" }]), "not_collected")
  const groups = [{ group_quality: "partial" }] as StoredGroup[]
  assert.equal(qualitySummary([{ part: "outcomes", quality: "complete", stale: false, reason: null }], groups).overall, "partial")
})

test("service stages keep each stage's own time, follow contract order, and a pending stage outranks a slower one", () => {
  const svc = (seq: number, stage: string, outcome: StoredEvent["outcome"], elapsed: number | null, minute: number) =>
    ev(seq, minute, { source: "relay", producer_instance: "relay-worker", source_id: "r", stage, outcome, elapsed_ms: elapsed })
  const rows = stageMatrix([svc(3, "outer_settled", "ok", 900, 30), svc(1, "worker_admitted", "ok", 5, 10), svc(2, "account_dispatch_started", "pending", 40, 20), svc(4, "outer_settled", "ok", 300, 31), ev(9, 5)])
  assert.equal(rows.length, 1)
  assert.deepEqual(rows[0].cells.map((c: { stage: string }) => c.stage), ["worker_admitted", "account_dispatch_started", "outer_settled"])
  const settled = rows[0].cells[2]
  // Two rows of one stage: the largest, never the sum.
  assert.equal(settled.maxElapsedMs, 900)
  assert.equal(settled.records, 2)
  const b = bottleneck(rows, at(25))!
  assert.equal(b.why, "stall")
  assert.equal(b.cell.stage, "account_dispatch_started")
  assert.equal(b.ageMs, 5 * 60_000)
  assert.equal(b.cell.role, "downstream")
})

test("interleaved source sequences do not invent missing records in an operation trace", () => {
  const lanes = traceLanes([ev(5, 30, { correlation: "c" }), ev(2, 10, { correlation: "c" }), ev(3, 12, { correlation: "c" }), ev(1, 50, { source_id: "r", source: "relay", correlation: "c" })])
  assert.equal(lanes.length, 2)
  const daemon = lanes.find((l: { key: string }) => l.key.startsWith("s1"))!
  assert.deepEqual(daemon.steps.map((s: { kind: string; seq?: number; from?: number; to?: number }) => s.kind === "gap" ? `gap ${s.from}-${s.to}` : s.seq), [2, 3, 5])
})

test("stale sources come first, and a local filter says how much of the page it kept", () => {
  const s = (id: string, freshness: "fresh" | "stale", age: number) => ({ source_id: id, machine_id: id, producer_instance: "daemon", source: "daemon", freshness, last_received_at: at(30), last_receipt_age_ms: age, expected_every_ms: null, boot_order: "single_boot", boots: [], boots_evicted: 0, retain_until: at(100) }) as unknown as StoredSource
  assert.deepEqual(sourceMarks([s("a", "fresh", 1), s("b", "stale", 10), s("c", "stale", 99)], axis).map((m: { source_id: string }) => m.source_id), ["c", "b", "a"])
  const f = filterLoaded([{ source: "relay" }, { source: "cloud_api" }, { source: "relay" }], "relay")
  assert.equal(f.rows.length, 2)
  assert.equal(f.of, 3)
})
