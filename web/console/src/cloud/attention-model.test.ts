import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node's strip-types runner needs the source extension.
import { buildAttentionOverview, filterAttention, targetKey, type MachineStatusSnapshot, type StatusSession } from "./attention-model.ts"
// @ts-expect-error -- Node's strip-types runner needs the source extension.
import { formatAttentionReport } from "./attention-report.ts"
// @ts-expect-error -- Node's strip-types runner needs the source extension.
import { fromListProjection } from "./attention-adapter.ts"

const NOW = Date.UTC(2026, 9, 9, 10)
const POLICY_MS = 60 * 60 * 1000 // Fixture supplied by the source, not a product policy.
function session(machine: string, id: string, state: StatusSession["state"]): StatusSession {
  return { target: { machine_id: machine, session_id: id, execution_generation: "gen-1" }, title: `${machine} ${id}`, state,
    waitingForReply: state === "waiting",
    closeBlocked: state === "blocked", failedAgentCount: state === "failed" ? 1 : 0,
    source: { provenance: "tmux", observedAt: NOW - 1000, inventoryComplete: true, snapshotGeneration: "snap-7" },
    lastMovementAt: state === "working" ? NOW : null }
}
function machine(id: string, sessions: StatusSession[] = []): MachineStatusSnapshot {
  return { machine_id: id, name: `Machine ${id}`, platform: "Linux", access: "readable", freshness: "current", completeness: "complete", observedAt: NOW - 1000, snapshotGeneration: "snap-7", noProgressAfterMs: POLICY_MS, sessions }
}

test("ten machines retain separate targets and count only known current exceptions", () => {
  const machines = Array.from({ length: 10 }, (_, i) => machine(`m${i}`, [session(`m${i}`, "same-id", i % 2 ? "waiting" : "working")]))
  const overview = buildAttentionOverview(machines, NOW)
  assert.equal(overview.complete, true)
  assert.equal(overview.unknownMachines, 0)
  assert.equal(overview.entries.length, 5)
  assert.equal(new Set(overview.entries.map((entry) => targetKey(entry.session!.target))).size, 5)
  assert.equal(filterAttention(overview.entries, { machine: "m1", platform: "Linux", kind: "reply" }).length, 1)
  assert.equal(filterAttention(overview.entries, { machine: "m0", kind: "reply" }).length, 0)
})

test("partial failure, stale, permission denial and revocation remain gaps, not zeros", () => {
  const machines = [
    machine("good", [session("good", "x", "failed")]),
    { ...machine("offline", [session("offline", "x", "waiting")]), freshness: "offline" as const },
    { ...machine("stale", [session("stale", "x", "blocked")]), freshness: "stale" as const },
    { ...machine("denied"), access: "denied" as const, sessions: null },
    { ...machine("revoked", [session("revoked", "secret", "failed")]), access: "revoked" as const },
    { ...machine("unknown"), freshness: "unknown" as const, sessions: null, observedAt: null },
  ]
  const overview = buildAttentionOverview(machines, NOW)
  assert.equal(overview.complete, false)
  assert.equal(overview.unknownMachines, 5)
  assert.deepEqual(overview.entries.map((entry) => [entry.machine.machine_id, entry.kind, entry.evidence]), [
    ["good", "failed", "current"], ["offline", "offline", "current"], ["offline", "reply", "retained"], ["stale", "blocked", "retained"],
  ])
  assert.deepEqual(filterAttention(overview.entries, { freshness: "stale" }).map((entry) => entry.machine.machine_id), ["stale"])
  assert.deepEqual(filterAttention(overview.entries, { freshness: "offline" }).map((entry) => entry.kind), ["offline", "reply"])
  const report = formatAttentionReport(overview, NOW, "en")
  assert.match(report, /Unknown machines are not counted as zero/)
  assert.match(report, /Machine offline/)
  assert.match(report, /Stale observation/)
  assert.match(report, /Machine access revoked/)
  assert.doesNotMatch(report, /revoked secret/)
  assert.match(report, /ss\/good/)
  assert.match(report, /snap-7/)
  assert.match(report, /2026-10-09T09:59:59.000Z/)
})

test("no-progress age is measured only against a current working observation", () => {
  const old = session("a", "working", "working")
  old.lastMovementAt = NOW - 1000 - POLICY_MS
  const before = { ...old, target: { ...old.target, session_id: "before" }, lastMovementAt: NOW - 1000 - POLICY_MS + 1 }
  const stale = { ...old, target: { ...old.target, session_id: "stale" }, freshness: "stale" as const }
  const offline = { ...machine("b", [{ ...old, target: { ...old.target, machine_id: "b", session_id: "offline" } }]), freshness: "offline" as const }
  const overview = buildAttentionOverview([machine("a", [old, before, stale]), offline], NOW)
  assert.deepEqual(overview.entries.filter((entry) => entry.kind === "no_progress").map((entry) => entry.session!.target.session_id), ["working"])
  assert.equal(overview.unknownMachines, 2)
})

test("report names each source, target and next step in Taiwan Traditional Chinese", () => {
  const one = { ...session("a", "same", "completed"), completedUnconfirmed: true }
  const two = { ...session("b", "same", "blocked"), target: { machine_id: "b", session_id: "same", execution_generation: "gen-2" } }
  const report = formatAttentionReport(buildAttentionOverview([machine("a", [one]), machine("b", [two])], NOW), NOW, "zh-Hant-TW")
  assert.match(report, /完成未確認/)
  assert.match(report, /受阻/)
  assert.match(report, /下一步/)
  assert.ok(report.includes('- 目標: \\["a","same","gen-1"\\]'))
  assert.ok(report.includes('- 目標: \\["b","same","gen-2"\\]'))
  assert.match(report, /ss\/a/)
  assert.match(report, /ss\/b/)
})

test("list adapter preserves gaps and does not classify generic attention as a specific issue", () => {
  const descriptor = { id: "m", name: "Machine", platform: "macOS" }
  const row = { destination: { machineID: "m", sessionID: "s", executionGeneration: "g" }, title: "Task", state: "working", freshness: "current" as const, needsAttention: true, observedAt: NOW }
  const ready = fromListProjection(descriptor, { phase: "settled", value: { kind: "ready", complete: true, observedAt: NOW, rows: [row] } })
  assert.equal(ready.snapshotGeneration, null)
  assert.match(ready.gap!, /snapshot_generation_unavailable/)
  assert.match(ready.gap!, /session_blocked_failed_unavailable/)
  assert.equal(buildAttentionOverview([ready], NOW).entries.length, 0)
  const denied = fromListProjection(descriptor, { phase: "settled", value: { kind: "unavailable", reason: "no_permission" } })
  assert.equal(denied.access, "denied")
  assert.equal(denied.sessions, null)
  const gap = fromListProjection(descriptor, { phase: "settled", value: { kind: "unavailable", reason: "event_gap", observedAt: NOW, rows: [row] } })
  assert.equal(gap.freshness, "stale")
  assert.equal(gap.sessions?.[0].freshness, "stale")
  assert.equal(gap.observedAt, NOW)
  assert.match(gap.gap!, /event_gap/)
})

test("a generic waiting state and missing no-activity policy cannot become live facts", () => {
  const waiting = { ...session("m", "waiting", "waiting"), waitingForReply: false }
  const working = { ...session("m", "working", "working"), lastMovementAt: NOW - POLICY_MS * 2 }
  const snapshot = { ...machine("m", [waiting, working]), noProgressAfterMs: null }
  const overview = buildAttentionOverview([snapshot], NOW)
  assert.equal(overview.entries.length, 0)
  assert.equal(overview.unknownMachines, 1)
  assert.match(formatAttentionReport(overview, NOW, "en"), /Unknown machines are not counted as zero/)
})

test("list adapter emits reply only for an explicit source-proven signal", () => {
  const descriptor = { id: "m", name: "Machine", platform: "macOS", freshness: "current" as const }
  const row = { destination: { machineID: "m", sessionID: "s", executionGeneration: "0123456789abcdef0123456789abcdef" },
    title: "Session", state: "waiting", freshness: "current" as const, needsAttention: true, observedAt: NOW,
    sourceProvenance: "tmux", inventoryComplete: true, snapshotGeneration: "pass-1", closeBlocked: false, failedAgentCount: 0 }
  const projection = (waitingForReply?: boolean) => fromListProjection(descriptor, { phase: "settled", value: {
    kind: "ready", complete: true, observedAt: NOW, snapshotGeneration: "pass-1", rows: [{ ...row, waitingForReply }],
  } })
  const proven = projection(true)
  assert.deepEqual(buildAttentionOverview([proven], NOW).entries.map((entry) => entry.kind), ["reply"])
  assert.doesNotMatch(proven.gap!, /reply_signal_unavailable/)
  const missing = projection()
  assert.equal(buildAttentionOverview([missing], NOW).entries.length, 0)
  assert.match(missing.gap!, /reply_signal_unavailable/)
  const negative = projection(false)
  assert.equal(buildAttentionOverview([negative], NOW).entries.length, 0)
  assert.match(negative.gap!, /reply_signal_unavailable/)
})

test("list adapter retains the marker and row provenance without promoting related failures", () => {
  const descriptor = { id: "m", name: "Machine", platform: "macOS" }
  const row = { destination: { machineID: "m", sessionID: "s", executionGeneration: "0123456789abcdef0123456789abcdef" },
    title: "Session", state: "working", freshness: "current" as const, needsAttention: false, observedAt: NOW - 1000,
    sourceProvenance: "tmux", inventoryComplete: true, snapshotGeneration: "pass-1", noProgressAfterMs: POLICY_MS,
    lastMovementAt: NOW - POLICY_MS - 1000, noMovement: true, completedUnconfirmed: false,
    closeBlocked: true, failedAgentCount: 2 }
  const adapted = fromListProjection(descriptor, { phase: "settled", value: { kind: "ready", complete: true,
    observedAt: NOW, snapshotGeneration: "pass-1", rows: [row] } })
  assert.equal(adapted.completeness, "complete")
  assert.equal(adapted.snapshotGeneration, "pass-1")
  const overview = buildAttentionOverview([adapted], NOW)
  assert.deepEqual(overview.entries.map((entry) => entry.kind), ["blocked", "failed", "no_progress"])
  const report = formatAttentionReport(overview, NOW, "en")
  assert.match(report, /Provenance: tmux/)
  assert.match(report, /Snapshot generation: pass-1/)
  assert.match(report, /Failed subagents: 2/)
  assert.match(report, /Whole-Session blocked and failed states are not projected/)
})

test("a row from another snapshot generation cannot contribute a report fact", () => {
  const bad = session("m", "s", "waiting")
  bad.source = { ...bad.source!, snapshotGeneration: "other-pass" }
  const overview = buildAttentionOverview([machine("m", [bad])], NOW)
  assert.equal(overview.entries.length, 0)
  assert.equal(overview.unknownMachines, 1)
})

test("a row with unknown freshness remains unknown in filters and report", () => {
  const row = { ...session("m", "s", "waiting"), freshness: "unknown" as const }
  const overview = buildAttentionOverview([machine("m", [row])], NOW)
  assert.equal(filterAttention(overview.entries, { freshness: "current" }).length, 0)
  assert.equal(filterAttention(overview.entries, { freshness: "unknown" }).length, 1)
  assert.match(formatAttentionReport(overview, NOW, "en"), /Freshness: Unknown/)
})

test("a retained machine snapshot cannot be promoted to current by a ready row", () => {
  const descriptor = { id: "m", name: "Machine", platform: "macOS", freshness: "stale" as const }
  const row = { destination: { machineID: "m", sessionID: "s", executionGeneration: "0123456789abcdef0123456789abcdef" },
    title: "Session", state: "working", freshness: "current" as const, needsAttention: false, observedAt: NOW - 1000,
    sourceProvenance: "tmux", inventoryComplete: true, snapshotGeneration: "pass-1", noProgressAfterMs: POLICY_MS,
    lastMovementAt: NOW - POLICY_MS - 1000, closeBlocked: true, failedAgentCount: 0 }
  const adapted = fromListProjection(descriptor, { phase: "settled", value: { kind: "ready", complete: true,
    observedAt: NOW, snapshotGeneration: "pass-1", rows: [row] } })
  assert.equal(adapted.freshness, "stale")
  const overview = buildAttentionOverview([adapted], NOW)
  assert.deepEqual(overview.entries.map((entry) => [entry.kind, entry.evidence]), [["blocked", "retained"]])
  assert.equal(filterAttention(overview.entries, { freshness: "current" }).length, 0)
  assert.match(formatAttentionReport(overview, NOW, "en"), /Stale observation/)
})
