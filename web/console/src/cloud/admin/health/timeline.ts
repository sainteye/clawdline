/* Places stored connection-health evidence on time and on stages without
 * inventing any of it.
 *
 * Everything here is a pure mapping from what the Cloud's reads returned:
 * - A record is a mark at its server receive time. Between marks nothing was
 *   observed, so a lane never draws a healthy interval, only marks over a
 *   "not observed" track.
 * - The window stops at the query's own bounds. A record outside it is counted
 *   at the edge, never drawn inside; time before the retention floor, after the
 *   read's `as_of`, or earlier than the oldest row of a list that has more pages
 *   is an explicit band, not empty track.
 * - A source's boots are separate lanes. Order inside a lane is the producer's
 *   own sequence; a record received after one with a higher sequence is late,
 *   and a producer clock that goes backwards against that sequence is said so.
 *   Producer clocks are compared only within one boot and never with the
 *   server's receive time.
 * - Service stages show the service's own elapsed time per stage. Nothing is
 *   added up into an end-to-end duration and receive times are never latency.
 * Nothing account-scoped is copied: no account id reaches these shapes.
 * Nothing is imported at run time except stored.ts, so node's strip-types tests load it. */
import type { Coverage, GroupOutcome, RecordOutcome, StoredEvent, StoredGroup, StoredQuality, StoredSource, StageRole } from "./stored.ts"
// @ts-expect-error -- a `.ts` path, so node's strip-types tests can import this module.
import { isServiceSource, QUALITY, STAGE_ORDER, stageRole } from "./stored.ts"

/* ---- the axis ---------------------------------------------------------------- */

export type Axis = { from: number; to: number; asOf: number; floor: number | null }

/** The query window as the reads used it. Null when there is no usable window. */
export function axisOf(window: { from: string; to: string } | null, asOf: string | null, retentionFloor: string | null): Axis | null {
  if (window === null) return null
  const from = Date.parse(window.from)
  const to = Date.parse(window.to)
  if (!Number.isFinite(from) || !Number.isFinite(to) || to <= from) return null
  const a = asOf === null ? to : Date.parse(asOf)
  const f = retentionFloor === null ? null : Date.parse(retentionFloor)
  return { from, to, asOf: Number.isFinite(a) ? a : to, floor: f !== null && Number.isFinite(f) ? f : null }
}

/** Where an instant falls: a fraction of the window, or which edge it lies past. */
export function place(axis: Axis, iso: string): { at: number } | { edge: "before" | "after" } {
  const t = Date.parse(iso)
  if (t < axis.from) return { edge: "before" }
  if (t > axis.to) return { edge: "after" }
  return { at: (t - axis.from) / (axis.to - axis.from) }
}

const frac = (axis: Axis, t: number) => Math.min(1, Math.max(0, (t - axis.from) / (axis.to - axis.from)))

export type BandKind = "before_retention" | "after_as_of" | "not_loaded" | "gap"
export type Band = { kind: BandKind; from: number; to: number; count: number | null }

/** Bands every lane of one list shares: retention, `as_of`, and rows not loaded yet. */
export function windowBands(axis: Axis, oldestHeld: string | null, more: boolean): Band[] {
  const out: Band[] = []
  if (axis.floor !== null && axis.floor > axis.from) out.push({ kind: "before_retention", from: 0, to: frac(axis, axis.floor), count: null })
  if (more) {
    // The list has more pages: anything older than the oldest row held was not loaded, not absent.
    const edge = oldestHeld === null ? axis.to : Date.parse(oldestHeld)
    const start = axis.floor !== null && axis.floor > axis.from ? frac(axis, axis.floor) : 0
    const end = frac(axis, edge)
    if (end > start) out.push({ kind: "not_loaded", from: start, to: end, count: null })
  }
  if (axis.asOf < axis.to) out.push({ kind: "after_as_of", from: frac(axis, axis.asOf), to: 1, count: null })
  return out
}

/* ---- marks and lanes ----------------------------------------------------------- */

export type MarkKind = "stall" | "loss" | "slow" | "recovered" | "boot" | "error" | "other"

/** What a record's mark looks like, from its own event name and (record/2) its own result. */
export function markKind(event: string, outcome: RecordOutcome | null): MarkKind {
  if (event === "stall_detected" || outcome === "pending") return "stall"
  if (event === "loss" || event === "loss_detected") return "loss"
  if (event === "slow_detected") return "slow"
  if (event === "recovered" || event === "released_after_stall") return "recovered"
  if (event === "boot") return "boot"
  if (outcome === "error" || outcome === "timeout" || outcome === "threw" || outcome === "refused") return "error"
  return "other"
}

export function groupMarkKind(g: GroupOutcome): MarkKind | "unsupported" {
  switch (g) {
    case "stall_detected": return "stall"
    case "released_after_stall": case "recovered": return "recovered"
    case "slow": return "slow"
    case "loss": return "loss"
    case "unsupported": return "unsupported"
  }
}

export type Mark = {
  id: string
  at: number
  received_at: string
  kind: MarkKind | "unsupported"
  event: string
  seq: number
  stage: string | null
  correlation: string | null
  /** Received after a record of the same boot with a higher producer sequence. */
  late: boolean
}

export type Lane = {
  key: string
  machine_id: string
  source_id: string
  producer_instance: string
  source: string | null
  boot_id: string
  /** The Cloud's generation for this boot, when the sources read listed it; null is unknown. */
  generation: number | null
  marks: Mark[]
  before: number
  after: number
  gaps: Band[]
  /** Listed gaps of this boot that no two held records bracket, so they cannot be placed in time. */
  gapsUnplaced: number
  late: number
  /** Pairs, in producer order, where the producer's monotonic clock went backwards. */
  monoBackwards: number
  /** Pairs, in producer order, where the producer's wall clock went backwards (display only). */
  wallBackwards: number
  firstSeq: number
  lastSeq: number
  newest: string
}

const laneKey = (source_id: string, boot_id: string) => `${source_id}|${boot_id}`

function generationOf(sources: StoredSource[] | null, source_id: string, boot_id: string): number | null {
  const s = sources?.find((x) => x.source_id === source_id)
  const b = s?.boots.find((x) => x.boot_id === boot_id)
  return b ? b.server_generation : null
}

/** One lane per source and boot, with marks, edge counts, gaps and clock flags. */
export function eventLanes(events: StoredEvent[], axis: Axis, coverage: Coverage | null, sources: StoredSource[] | null): Lane[] {
  const byLane = new Map<string, StoredEvent[]>()
  for (const e of events) {
    const k = laneKey(e.source_id, e.boot_id)
    const list = byLane.get(k)
    if (list) list.push(e)
    else byLane.set(k, [e])
  }
  const lanes: Lane[] = []
  for (const [key, list] of byLane) {
    const head = list[0]
    const bySeq = [...list].sort((a, b) => a.producer_seq - b.producer_seq)
    const byReceipt = [...list].sort((a, b) => Date.parse(a.received_at) - Date.parse(b.received_at) || a.producer_seq - b.producer_seq)
    const late = new Set<string>()
    let maxSeq = -1
    for (const e of byReceipt) {
      if (e.producer_seq < maxSeq) late.add(e.id)
      else maxSeq = e.producer_seq
    }
    let monoBackwards = 0
    let wallBackwards = 0
    for (let i = 1; i < bySeq.length; i++) {
      if (bySeq[i].mono_ns < bySeq[i - 1].mono_ns) monoBackwards++
      if (Date.parse(bySeq[i].wall) < Date.parse(bySeq[i - 1].wall)) wallBackwards++
    }
    let before = 0
    let after = 0
    const marks: Mark[] = []
    for (const e of bySeq) {
      const p = place(axis, e.received_at)
      if ("edge" in p) {
        if (p.edge === "before") before++
        else after++
        continue
      }
      marks.push({ id: e.id, at: p.at, received_at: e.received_at, kind: markKind(e.event, e.outcome), event: e.event, seq: e.producer_seq, stage: e.stage, correlation: e.correlation, late: late.has(e.id) })
    }
    const gaps: Band[] = []
    let gapsUnplaced = 0
    for (const g of coverage?.gaps ?? []) {
      if (g.source_id !== head.source_id || g.boot_id !== head.boot_id) continue
      // The missing records were produced between the nearest held neighbours; place the band between their receipts.
      const lower = [...bySeq].reverse().find((e) => e.producer_seq < g.from)
      const upper = bySeq.find((e) => e.producer_seq > g.to)
      if (!lower || !upper) { gapsUnplaced++; continue }
      const a = Date.parse(lower.received_at)
      const b = Date.parse(upper.received_at)
      if (b < axis.from || a > axis.to || b < a) { gapsUnplaced++; continue }
      gaps.push({ kind: "gap", from: frac(axis, a), to: frac(axis, b), count: g.count })
    }
    lanes.push({
      key, machine_id: head.machine_id, source_id: head.source_id, producer_instance: head.producer_instance, source: head.source, boot_id: head.boot_id,
      generation: generationOf(sources, head.source_id, head.boot_id), marks, before, after, gaps, gapsUnplaced, late: late.size, monoBackwards, wallBackwards,
      firstSeq: bySeq[0].producer_seq, lastSeq: bySeq[bySeq.length - 1].producer_seq, newest: byReceipt[byReceipt.length - 1].received_at,
    })
  }
  return sortLanes(lanes)
}

function sortLanes(lanes: Lane[]): Lane[] {
  // Machine, then source, then boot generation (unknown last), then the newest receipt.
  return lanes.sort((a, b) => a.machine_id.localeCompare(b.machine_id) || a.source_id.localeCompare(b.source_id)
    || (a.generation ?? Number.MAX_SAFE_INTEGER) - (b.generation ?? Number.MAX_SAFE_INTEGER) || a.newest.localeCompare(b.newest))
}

/** Stall groups as marks at their last receipt: the last observed transition, not an interval. */
export function groupLanes(groups: StoredGroup[], axis: Axis, sources: StoredSource[] | null): Lane[] {
  const byLane = new Map<string, Lane>()
  groups.forEach((g, i) => {
    const key = laneKey(g.source_id, g.boot_id)
    let lane = byLane.get(key)
    if (!lane) {
      lane = { key, machine_id: g.machine_id, source_id: g.source_id, producer_instance: "", source: null, boot_id: g.boot_id, generation: generationOf(sources, g.source_id, g.boot_id),
        marks: [], before: 0, after: 0, gaps: [], gapsUnplaced: 0, late: 0, monoBackwards: 0, wallBackwards: 0, firstSeq: g.first_seq, lastSeq: g.last_seq, newest: g.last_received_at }
      byLane.set(key, lane)
    }
    lane.firstSeq = Math.min(lane.firstSeq, g.first_seq)
    lane.lastSeq = Math.max(lane.lastSeq, g.last_seq)
    if (g.last_received_at > lane.newest) lane.newest = g.last_received_at
    const p = place(axis, g.last_received_at)
    if ("edge" in p) {
      if (p.edge === "before") lane.before++
      else lane.after++
      return
    }
    lane.marks.push({ id: `${key}|${g.phase_id ?? ""}|${g.correlation ?? ""}|${i}`, at: p.at, received_at: g.last_received_at, kind: groupMarkKind(g.group_outcome), event: g.last_event, seq: g.last_seq, stage: g.last_stage, correlation: g.correlation, late: false })
  })
  return sortLanes([...byLane.values()])
}

/** Coverage gaps the timeline could not place (no lane holds their neighbours), summed per boot. */
export function gapTotals(coverage: Coverage | null): { listed: number; missing: number | null } | null {
  if (coverage === null || coverage.gaps === null) return null
  let missing: number | null = 0
  for (const g of coverage.gaps) missing = g.count === null || missing === null ? null : missing + g.count
  return { listed: coverage.gaps.length, missing }
}

/** Sequence coverage per source and boot, in producer-sequence space (not time): received ranges and listed gaps. */
export type SeqLane = { key: string; machine_id: string; source_id: string; boot_id: string; from: number; to: number; ranges: { from: number; to: number }[]; gaps: { from: number; to: number; count: number | null }[] }

export function seqLanes(coverage: Coverage | null): SeqLane[] | null {
  if (coverage === null || coverage.seq_ranges === null) return null
  const lanes = new Map<string, SeqLane>()
  const lane = (r: { source_id: string; machine_id: string; boot_id: string; from: number; to: number }) => {
    const key = laneKey(r.source_id, r.boot_id)
    let l = lanes.get(key)
    if (!l) { l = { key, machine_id: r.machine_id, source_id: r.source_id, boot_id: r.boot_id, from: r.from, to: r.to, ranges: [], gaps: [] }; lanes.set(key, l) }
    l.from = Math.min(l.from, r.from)
    l.to = Math.max(l.to, r.to)
    return l
  }
  for (const r of coverage.seq_ranges) lane(r).ranges.push({ from: r.from, to: r.to })
  for (const g of coverage.gaps ?? []) lane(g).gaps.push({ from: g.from, to: g.to, count: g.count })
  return [...lanes.values()].sort((a, b) => a.key.localeCompare(b.key))
}

/* ---- sources on the axis --------------------------------------------------------- */

export type SourceMark = {
  source_id: string
  machine_id: string
  producer_instance: string
  source: string | null
  freshness: StoredSource["freshness"]
  /** The newest receipt, placed; or the edge it lies past. */
  last: { at: number } | { edge: "before" | "after" }
  ageMs: number
  expectedEveryMs: number | null
  bootOrder: StoredSource["boot_order"]
  boots: { boot_id: string; generation: number; first: { at: number } | { edge: "before" | "after" }; last: { at: number } | { edge: "before" | "after" } }[]
  bootsEvicted: number
  retainUntil: string
}

/** Each source's newest receipt and its boots' first and last receipts. Nothing in between is drawn as observed. */
export function sourceMarks(sources: StoredSource[], axis: Axis): SourceMark[] {
  return sources.map((s) => ({
    source_id: s.source_id, machine_id: s.machine_id, producer_instance: s.producer_instance, source: s.source, freshness: s.freshness,
    last: place(axis, s.last_received_at), ageMs: s.last_receipt_age_ms, expectedEveryMs: s.expected_every_ms, bootOrder: s.boot_order,
    boots: [...s.boots].sort((a, b) => a.server_generation - b.server_generation).map((b) => ({ boot_id: b.boot_id, generation: b.server_generation, first: place(axis, b.first_received_at), last: place(axis, b.last_received_at) })),
    bootsEvicted: s.boots_evicted, retainUntil: s.retain_until,
  })).sort((a, b) => (a.freshness === b.freshness ? 0 : a.freshness === "stale" ? -1 : 1) || b.ageMs - a.ageMs)
}

/* ---- service stages ------------------------------------------------------------------ */

export type StageCell = {
  stage: string
  role: StageRole | null
  records: number
  /** Summed `count` of aggregate rows; null when any row's count is unknown. */
  count: number | null
  outcomes: Partial<Record<RecordOutcome | "unknown", number>>
  /** The largest elapsed time the service reported for this stage; null when none reported one. */
  maxElapsedMs: number | null
  newest: string
  newestId: string
}

export type StageRow = { key: string; source: string; producer_instance: string; cells: StageCell[] }

/** Service records on the loaded page as source × stage, in the contract's stage order. */
export function stageMatrix(events: StoredEvent[]): StageRow[] {
  const rows = new Map<string, Map<string, StageCell>>()
  const heads = new Map<string, StoredEvent>()
  for (const e of events) {
    if (!isServiceSource(e.source) || e.stage === null) continue
    const key = `${e.source}|${e.producer_instance}`
    if (!heads.has(key)) heads.set(key, e)
    let cells = rows.get(key)
    if (!cells) { cells = new Map(); rows.set(key, cells) }
    let c = cells.get(e.stage)
    if (!c) {
      c = { stage: e.stage, role: stageRole(e.stage), records: 0, count: 0, outcomes: {}, maxElapsedMs: null, newest: e.received_at, newestId: e.id }
      cells.set(e.stage, c)
    }
    c.records++
    c.count = c.count === null || e.count === null ? null : c.count + e.count
    const o = e.outcome ?? "unknown"
    c.outcomes[o] = (c.outcomes[o] ?? 0) + 1
    if (e.elapsed_ms !== null) c.maxElapsedMs = Math.max(c.maxElapsedMs ?? 0, e.elapsed_ms)
    if (e.received_at > c.newest) { c.newest = e.received_at; c.newestId = e.id }
  }
  const order = (s: string) => { const i = (STAGE_ORDER as readonly string[]).indexOf(s); return i < 0 ? Number.MAX_SAFE_INTEGER : i }
  return [...rows.entries()].map(([key, cells]) => ({
    key, source: heads.get(key)!.source!, producer_instance: heads.get(key)!.producer_instance,
    cells: [...cells.values()].sort((a, b) => order(a.stage) - order(b.stage) || a.stage.localeCompare(b.stage)),
  })).sort((a, b) => a.key.localeCompare(b.key))
}

export type Bottleneck = { why: "stall" | "failure" | "slowest"; row: StageRow; cell: StageCell; ageMs: number }

const FAILURES: (RecordOutcome | "unknown")[] = ["error", "timeout", "threw", "refused"]

/** The one stage to look at first: a still-pending stage, else a failed one, else the largest own elapsed time. */
export function bottleneck(rows: StageRow[], asOf: string): Bottleneck | null {
  const now = Date.parse(asOf)
  let best: Bottleneck | null = null
  const rank = { stall: 0, failure: 1, slowest: 2 } as const
  for (const row of rows) for (const cell of row.cells) {
    const why: Bottleneck["why"] | null = (cell.outcomes.pending ?? 0) > 0 ? "stall" : FAILURES.some((f) => (cell.outcomes[f] ?? 0) > 0) ? "failure" : cell.maxElapsedMs !== null && cell.maxElapsedMs > 0 ? "slowest" : null
    if (why === null) continue
    const cand: Bottleneck = { why, row, cell, ageMs: Math.max(0, now - Date.parse(cell.newest)) }
    if (best === null || rank[why] < rank[best.why] || (rank[why] === rank[best.why] && (cell.maxElapsedMs ?? 0) > (best.cell.maxElapsedMs ?? 0))) best = cand
  }
  return best
}

/* ---- one operation's stages ------------------------------------------------------------ */

export type TraceStep =
  | { kind: "record"; id: string; seq: number; event: string; stage: string | null; role: StageRole | null; outcome: RecordOutcome | null; elapsedMs: number | null; received_at: string; late: boolean }
  | { kind: "gap"; from: number; to: number }

export type TraceLane = { key: string; machine_id: string; source: string | null; producer_instance: string; boot_id: string; steps: TraceStep[] }

/** One operation's records per source and boot, in producer order. Source sequences also include other operations; a filtered trace cannot prove a loss.
 *  Lanes are never merged into one order: their clocks are unrelated. */
export function traceLanes(events: StoredEvent[]): TraceLane[] {
  const byLane = new Map<string, StoredEvent[]>()
  for (const e of events) {
    const k = laneKey(e.source_id, e.boot_id)
    const list = byLane.get(k)
    if (list) list.push(e)
    else byLane.set(k, [e])
  }
  const out: TraceLane[] = []
  for (const [key, list] of byLane) {
    const bySeq = [...list].sort((a, b) => a.producer_seq - b.producer_seq)
    const byReceipt = [...list].sort((a, b) => Date.parse(a.received_at) - Date.parse(b.received_at) || a.producer_seq - b.producer_seq)
    const late = new Set<string>()
    let maxSeq = -1
    for (const e of byReceipt) { if (e.producer_seq < maxSeq) late.add(e.id); else maxSeq = e.producer_seq }
    const steps: TraceStep[] = []
    bySeq.forEach((e, i) => {
      steps.push({ kind: "record", id: e.id, seq: e.producer_seq, event: e.event, stage: e.stage, role: stageRole(e.stage), outcome: e.outcome, elapsedMs: e.elapsed_ms, received_at: e.received_at, late: late.has(e.id) })
    })
    const h = list[0]
    out.push({ key, machine_id: h.machine_id, source: h.source, producer_instance: h.producer_instance, boot_id: h.boot_id, steps })
  }
  return out.sort((a, b) => a.key.localeCompare(b.key))
}

/* ---- how much is known ------------------------------------------------------------------- */

export type PanelQuality = { part: string; quality: StoredQuality | "failed"; stale: boolean; reason: string | null }

export type QualitySummary = {
  /** The weakest quality among the reads this screen relies on; a failed or stale read makes it at most partial. */
  overall: StoredQuality
  panels: PanelQuality[]
  failed: number
  stale: number
  /** Groups the Cloud itself marked partial. */
  partialGroups: number
}

const qRank = (q: StoredQuality) => (QUALITY as readonly string[]).indexOf(q)

/** Combines per-read qualities without ever promising more than the weakest one. */
export function qualitySummary(panels: PanelQuality[], groups: StoredGroup[] | null): QualitySummary {
  let overall: StoredQuality = "complete"
  let failed = 0
  let stale = 0
  const worse = (q: StoredQuality) => { if (qRank(q) > qRank(overall)) overall = q }
  for (const p of panels) {
    if (p.quality === "failed") { failed++; worse("partial"); continue }
    worse(p.quality)
    if (p.stale) { stale++; worse("partial") }
  }
  const partialGroups = groups?.filter((g) => g.group_quality === "partial").length ?? 0
  if (partialGroups > 0) worse("partial")
  if (panels.length === 0) overall = "not_collected"
  return { overall, panels, failed, stale, partialGroups }
}

/* ---- what to look at first ----------------------------------------------------------------- */

export type ProblemKind = "loss" | "stall_detected" | "slow" | "released_after_stall" | "stale_sources" | "gaps" | "failed_reads"
export type Problem = { kind: ProblemKind; count: number; worst: { machine_id: string; stage: string | null; correlation: string | null; ageMs: number } | null }

const SEVERITY: ProblemKind[] = ["loss", "stall_detected", "slow", "released_after_stall", "failed_reads", "gaps", "stale_sources"]

/** Problems in the loaded evidence, worst first. An empty list is not a healthy verdict. */
export function problems(groups: StoredGroup[] | null, sources: StoredSource[] | null, coverage: Coverage | null, failedReads: number, staleCount: number | null = null): Problem[] {
  const out: Problem[] = []
  for (const kind of ["loss", "stall_detected", "slow", "released_after_stall"] as const) {
    const hit = (groups ?? []).filter((g) => g.group_outcome === kind).sort((a, b) => a.evidence_age_ms - b.evidence_age_ms)
    if (hit.length) out.push({ kind, count: hit.length, worst: { machine_id: hit[0].machine_id, stage: hit[0].last_stage, correlation: hit[0].correlation, ageMs: hit[0].evidence_age_ms } })
  }
  const stale = (sources ?? []).filter((s) => s.freshness === "stale").sort((a, b) => b.last_receipt_age_ms - a.last_receipt_age_ms)
  if (stale.length) out.push({ kind: "stale_sources", count: stale.length, worst: { machine_id: stale[0].machine_id, stage: null, correlation: null, ageMs: stale[0].last_receipt_age_ms } })
  else if (sources === null && staleCount !== null && staleCount > 0) out.push({ kind: "stale_sources", count: staleCount, worst: null })
  const gaps = gapTotals(coverage)
  if (gaps && gaps.listed > 0) out.push({ kind: "gaps", count: gaps.missing ?? gaps.listed, worst: null })
  if (failedReads > 0) out.push({ kind: "failed_reads", count: failedReads, worst: null })
  return out.sort((a, b) => SEVERITY.indexOf(a.kind) - SEVERITY.indexOf(b.kind))
}

/** The page's local narrowing of a loaded list: never a Cloud-wide filter. */
export function filterLoaded<T extends { source: string | null }>(rows: T[], source: string | null): { rows: T[]; of: number } {
  return { rows: source === null ? rows : rows.filter((r) => r.source === source), of: rows.length }
}
