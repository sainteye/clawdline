// The human reading of an incident revision: what it may say, and what it must never claim.
// `node --experimental-strip-types --test web/console/src/cloud/admin/health/incident-summary.test.ts`.
// Every id here is synthetic.
import { test } from "node:test"
import assert from "node:assert/strict"
import type { BundleFact } from "./stored.ts"
import type { IncidentInput, IncidentReading } from "./incident-summary.ts"
// @ts-expect-error -- a `.ts` path, for node.
import { eventWords, readIncident, stageWords } from "./incident-summary.ts"
// @ts-expect-error -- a `.ts` path, for node.
import { healthWords } from "./words.ts"

const en = healthWords("en")
const zh = healthWords("zh-TW")

function fact(seq: number, event: string, stage: string | null, over: Partial<BundleFact> = {}): BundleFact {
  return {
    id: `he_${String(seq).padStart(32, "0")}`, schema: "clawdline.connection-health.record/2", event_id: `ev-${seq}`, producer_seq: seq,
    boot_id: "boot-test", batch_id: null, event, source: "cloud_api", stage, class: null, kind: null, phase_id: null,
    correlation: "op-test-0001", count: null, rule: null, outcome: null, elapsed_ms: null, state: null,
    build_commit: "0000000000000000000000000000000000000001", mono_ns: seq, wall: "2026-01-02T03:04:00.000Z",
    received_at: "2026-01-02T03:04:05.589Z", digest: `sha256:${"0".repeat(64)}`, ...over,
  }
}

const none = { observed: false, event_key: null, event: null, received_at: null, unverified_reason: null }

function input(over: { state?: IncidentInput["summary"]["state"]; trigger?: IncidentInput["summary"]["trigger"]; facts?: BundleFact[]; recovery?: IncidentInput["summary"]["recovery"]; gaps?: { from: number; to: number; count: number | null; cause: string }[]; exactness?: string; lost?: number | null; refused?: number; source?: string; correlation?: string | null; active?: string | null; asOf?: string | null; closure?: IncidentInput["summary"]["closure"] } = {}): IncidentInput {
  const correlation = over.correlation === undefined ? "op-test-0001" : over.correlation
  return {
    summary: {
      state: over.state ?? "active", trigger: over.trigger ?? "stall", opened_at: "2026-01-02T03:04:05.657Z", recovery: over.recovery ?? none,
      acknowledgement: null, closure: over.closure ?? null,
      stages: { last_proven: null, active: over.active ? { stage: over.active, event_key: "k", basis: "producer_watchdog_record" } : null, next: "unknown", root_cause: "not_derived", hypotheses: [] },
    },
    manifest: {
      scope: { machine_id: "m-test", source_id: "hs_test", producer_instance: "api", source: over.source ?? "cloud_api", boot_id: "boot-test", group: { rule: "incident-group/1", kind: "operation", correlation, phase_id: null } },
      sequence: { exactness: over.exactness ?? "exact", reason: null, present: [], gaps: over.gaps ?? [], gap_edges: "not_inferred" },
      dropped: { transitions_lost: over.lost === undefined ? 0 : over.lost, overflow: 0, facts_refused_by_bound: over.refused ?? 0 },
    },
    facts: over.facts ?? [],
    asOf: over.asOf === undefined ? "2026-01-02T05:04:05.589Z" : over.asOf,
  }
}

const all = (r: IncidentReading) => [r.headline, ...r.observed, ...r.now, ...r.missing, ...r.next.map((n) => n.text)].join("\n")

// Words that would claim the present, a cause, or a number nobody measured.
const FORBIDDEN = [/\bis (?:still )?stuck\b(?! now is unknown)/i, /\bcurrently\b/i, /\broot cause\b/i, /\bcaused by\b/i, /\bdatabase\b/i, /%/, /\bfalse alarm\b/i]

function noOverclaim(r: IncidentReading) {
  const text = all(r)
  for (const p of FORBIDDEN) assert.doesNotMatch(text, p)
  assert.doesNotMatch(text, /\b(?:api_|stall_detected|slow_detected|_completed|_started)\w*/, "no raw event or stage codes in the human summary")
}

test("an active stall at a completed dependency stage: observed, no ending, current status unknown", () => {
  const facts = [
    fact(64, "slow_detected", "api_dependency_completed", { elapsed_ms: 2924 }),
    fact(66, "stall_detected", "api_dependency_completed", { elapsed_ms: 11_020 }),
  ]
  const r = readIncident(input({ facts }), en)
  noOverclaim(r)
  assert.match(r.headline, /^The Cloud API reported a stall/)
  assert.ok(r.observed.some((l) => l === "Cloud received the records at 2026-01-02 03:04:05 UTC."), r.observed.join("|"))
  assert.ok(r.observed.some((l) => l.includes("stall reports 1") && l.includes("slow reports 1")))
  assert.ok(r.observed.some((l) => l.includes("the downstream call had returned to the API")), "the completed stage is not described as waiting")
  assert.ok(!all(r).includes("published before waiting"))
  assert.ok(r.observed.some((l) => l.includes("11 s") && l.includes("does not measure time spent at this stage")), "the producer's own measurement, quoted")
  assert.ok(r.now[0].includes("Whether the operation is still stuck now is unknown"))
  assert.ok(r.now.some((l) => l.includes("2 h older than this read")), "the age is the read's time minus the newest receive time")
  assert.ok(r.missing.includes(en("inc_miss_noEnd")))
  assert.deepEqual(r.next.map((n) => n.to), ["trace", "none", "export"])
})

test("no number appears that is not in the stored fields", () => {
  const r = readIncident(input({ facts: [fact(3, "stall_detected", "api_dependency_started")], asOf: null }), en)
  noOverclaim(r)
  assert.ok(!r.observed.some((l) => l.includes("measured")), "no elapsed time is invented when none was recorded")
  assert.ok(!r.now.some((l) => l.includes("older than this read")), "no age without the read's own time")
  const digits = all(r).replace(/2026-01-02 03:04:05 UTC/g, "").replace(/reports 1/g, "").match(/\d/g)
  assert.equal(digits, null, all(r))
})

test("an observed recovery awaiting review points at the review, and claims no ending was missing", () => {
  const facts = [fact(230, "slow_detected", "api_handler_started"), fact(231, "stall_detected", "api_handler_started"), fact(238, "recovered", "api_handler_started", { received_at: "2026-01-02T03:05:00.000Z" })]
  const r = readIncident(input({ state: "recovered_awaiting_review", facts, recovery: { observed: true, event_key: "k", event: "recovered", received_at: "2026-01-02T03:05:00.000Z", unverified_reason: null } }), en)
  noOverclaim(r)
  assert.equal(r.now[0], "A recovery record was received at 2026-01-02 03:05:00 UTC. A person has not reviewed it yet.")
  assert.ok(!r.missing.includes(en("inc_miss_noEnd")))
  assert.equal(r.next[0].to, "review")
  assert.ok(!r.next.some((n) => n.to === "none"), "no log search once the operation is known to have moved on")
})

test("a recovering incident keeps Cloud's reason that recovery is not proven", () => {
  const facts = [fact(6, "slow_detected", "fanout_sent", { source: "relay" }), fact(10, "released_after_stall", "fanout_sent", { source: "relay" })]
  const r = readIncident(input({ state: "recovering", trigger: "slow", source: "relay", facts, recovery: { observed: false, event_key: "k", event: "released_after_stall", received_at: null, unverified_reason: "released_outcome_unknown" }, gaps: [{ from: 9, to: 9, count: 1, cause: "unexplained" }] }), en)
  noOverclaim(r)
  assert.deepEqual(r.now.slice(0, 2), [en("inc_now_recovering"), en("inc_why_released_outcome_unknown")])
  assert.ok(r.missing.some((l) => l.includes("#9")))
  assert.ok(r.next.some((n) => n.to === "monitoring"))
})

test("loss and gaps: lost records are missing evidence, not an ending", () => {
  const facts = [fact(13, "transitions_lost", null, { correlation: null }), fact(14, "state_after_loss", null, { correlation: null, state: "unverified_after_gap" })]
  const r = readIncident(input({ trigger: "loss", source: "daemon", facts, correlation: null, lost: 1, gaps: [{ from: 11, to: 12, count: 2, cause: "unexplained" }], exactness: "unverified", refused: 3 }), en)
  noOverclaim(r)
  assert.match(r.headline, /^The machine's daemon reported that it lost monitoring records/)
  assert.ok(!r.missing.includes(en("inc_miss_noEnd")), "a loss marker has no operation to end")
  assert.ok(r.missing.some((l) => l.includes("#11–#12")))
  assert.ok(r.missing.includes(en("inc_miss_unverified")))
  assert.ok(r.missing.some((l) => l.includes("1 monitoring transitions")))
  assert.ok(r.missing.some((l) => l.startsWith("3 records were not frozen")))
  assert.deepEqual(r.next.map((n) => n.to), ["monitoring", "export"])
})

test("no stage recorded: the summary says so instead of guessing one", () => {
  const r = readIncident(input({ facts: [fact(2, "stall_detected", null)], source: "daemon" }), en)
  noOverclaim(r)
  assert.ok(r.observed.includes(en("inc_sum_noStage")))
  assert.ok(!r.next.some((n) => n.to === "none"), "no service-log step for the machine's daemon")
})

test("a completed final stage is an ending, not missing", () => {
  const facts = [fact(2, "slow_detected", "api_dependency_started"), fact(3, "stage_completed", "api_response_completed", { outcome: "ok" })]
  const r = readIncident(input({ state: "recovering", trigger: "slow", facts }), en)
  noOverclaim(r)
  assert.ok(!r.missing.includes(en("inc_miss_noEnd")))
  assert.deepEqual(r.missing, [en("inc_miss_none")])
})

test("the active stage Cloud proved is used when no signal names one", () => {
  const r = readIncident(input({ facts: [fact(4, "stall_detected", null)], active: "storage_started" }), en)
  assert.ok(r.observed.some((l) => l.includes(en("inc_stg_storage_started"))))
})

test("Taiwan Chinese reads the same facts without raw codes", () => {
  const r = readIncident(input({ facts: [fact(64, "stall_detected", "api_dependency_completed", { elapsed_ms: 2924 })] }), zh)
  noOverclaim(r)
  assert.ok(r.headline.startsWith("Cloud API 回報卡住"))
  assert.ok(r.now[0].includes("現在是否仍然卡住無法得知"))
})

test("unknown stages and events get generic words, never a guess", () => {
  assert.equal(stageWords(en, "some_new_stage"), en("inc_stg_other"))
  assert.equal(eventWords(en, "some_new_event"), en("inc_evt_other"))
  assert.match(eventWords(en, "stall_detected"), /not say the operation never finished/)
})

test("a warning naming a final stage does not prove completion", () => {
  for (const [event, outcome] of [["stall_detected", "pending"], ["stage_completed", null], ["stage_completed", "pending"]] as const) {
    const r = readIncident(input({ facts: [fact(3, event, "api_response_completed", { outcome })] }), en)
    assert.ok(r.missing.includes(en("inc_miss_noEnd")))
  }
})
