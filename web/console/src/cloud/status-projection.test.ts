import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- node's type-stripping runner resolves the source .ts file.
import { statusProjection } from "./status-projection.ts"

const INVENTORY = "__clawdline_inventory_v1__"
const execution = "0123456789abcdef0123456789abcdef"
const passOne = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const passTwo = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
function fleet(machine: string, sessions: string[], generation = passOne, missing = "") {
  const statusSnapshots = new Map<string, unknown>()
  const hold = (session: string, payload: unknown, sequence: number) => statusSnapshots.set(JSON.stringify([machine, session]), {
    identity: { machine, session }, payload, observedAt: 1000, sequence,
  })
  sessions.filter((id) => id !== missing).forEach((id, index) => hold(id, {
    machine_id: machine, session_id: id, execution_generation: execution, assistant: "codex", backend: "tmux",
    state: "waiting", source: { provenance: "session_watch", observed_at: 1, freshness: "current" },
    inventory_complete: true, projected_at: 2, snapshot_generation: generation,
    attention_required: true, waiting_for_reply: true, completed_unconfirmed: false, last_movement_at: 1,
    no_progress_after_ms: 1800000, no_movement: true,
    close_blocked: true, failed_agent_count: 2,
    parent_session_id: "root", machine_scope: true,
  }, index + 1))
  hold(INVENTORY, { inventory: { version: 1, sessions }, at: 2, complete: true, snapshot_generation: generation }, sessions.length + 1)
  return { statusSnapshots }
}

test("a complete ss pass with the same Session id on two machines stays scoped", () => {
  const a = statusProjection(fleet("a", ["same"]), "a", 2000)
  const b = statusProjection(fleet("b", ["same"]), "b", 2000)
  assert.equal(a.kind, "ready")
  assert.equal(b.kind, "ready")
  if (a.kind !== "ready" || b.kind !== "ready") return
  assert.equal(a.rows[0].destination.machineID, "a")
  assert.equal(b.rows[0].destination.machineID, "b")
  assert.equal(a.rows[0].needsAttention, true)
  assert.equal(a.rows[0].assistant, "codex")
  assert.equal(a.rows[0].backend, "tmux")
  assert.equal(a.rows[0].waitingForReply, true)
  assert.equal(a.rows[0].lastMovementAt, 1000)
  assert.equal(a.rows[0].noProgressAfterMs, 1800000)
  assert.equal(a.rows[0].noMovement, true)
  assert.equal(a.rows[0].closeBlocked, true)
  assert.equal(a.rows[0].failedAgentCount, 2)
  assert.equal(a.rows[0].parentSessionID, "root")
  assert.equal(a.rows[0].machineScope, true)
  assert.equal(a.rows[0].sourceProvenance, "session_watch")
  assert.equal(a.rows[0].inventoryComplete, true)
  assert.equal(a.snapshotGeneration, passOne)
})

test("old rows cannot complete a new inventory pass", () => {
  const source = fleet("a", ["same"])
  const marker = source.statusSnapshots.get(JSON.stringify(["a", INVENTORY])) as { payload: { snapshot_generation: string } }
  marker.payload.snapshot_generation = passTwo
  const reading = statusProjection(source, "a", 2000)
  assert.deepEqual(reading.kind === "unavailable" && reading.reason, "event_gap")
})

test("a missing row is an event gap, not an empty list", () => {
  const reading = statusProjection(fleet("a", ["same"], passOne, "same"), "a", 2000)
  assert.equal(reading.kind, "unavailable")
  assert.equal(reading.kind === "unavailable" && reading.reason, "event_gap")
  const empty = statusProjection(fleet("a", []), "a", 2000)
  assert.equal(empty.kind, "ready")
  assert.equal(empty.kind === "ready" && empty.rows.length, 0)
  assert.equal(empty.kind === "ready" && empty.snapshotGeneration, passOne)
})

test("an unverified execution generation cannot be a navigable row", () => {
  const source = fleet("a", ["same"])
  const entry = source.statusSnapshots.get(JSON.stringify(["a", "same"])) as { payload: { execution_generation?: string } }
  delete entry.payload.execution_generation
  const reading = statusProjection(source, "a", 2000)
  assert.equal(reading.kind, "ready")
  if (reading.kind !== "ready") return
  assert.equal(reading.rows.length, 0)
  assert.equal(reading.unknownTargets, 1)
})

test("a current row preserves explicit negative attention facts", () => {
  const source = fleet("a", ["same"])
  const entry = source.statusSnapshots.get(JSON.stringify(["a", "same"])) as { payload: {
    close_blocked: boolean; failed_agent_count: number
  } }
  entry.payload.close_blocked = false
  entry.payload.failed_agent_count = 0
  const reading = statusProjection(source, "a", 2000)
  assert.equal(reading.kind, "ready")
  if (reading.kind !== "ready") return
  assert.equal(reading.rows[0].closeBlocked, false)
  assert.equal(reading.rows[0].failedAgentCount, 0)
})

test("waiting state alone does not imply a reply is needed", () => {
  const source = fleet("a", ["same"])
  const entry = source.statusSnapshots.get(JSON.stringify(["a", "same"])) as { payload: { waiting_for_reply?: boolean } }
  delete entry.payload.waiting_for_reply
  const reading = statusProjection(source, "a", 2000)
  assert.equal(reading.kind, "ready")
  if (reading.kind !== "ready") return
  assert.equal(reading.rows[0].state, "waiting")
  assert.equal(reading.rows[0].waitingForReply, undefined)
})

test("an old client and an absent marker never become authoritative emptiness", () => {
  assert.equal(statusProjection(null, "a").kind, "unavailable")
  const absent = statusProjection({ statusSnapshots: new Map() }, "a", 2000)
  assert.equal(absent.kind, "unavailable")
  assert.equal(absent.kind === "unavailable" && absent.reason, "unknown")
})

test("an old marker or projected row is stale even when the source says current", () => {
  const expired = statusProjection(fleet("a", ["same"]), "a", 302001)
  assert.equal(expired.kind === "unavailable" && expired.reason, "stale")
  assert.equal(expired.rows?.[0].destination.sessionID, "same")
  assert.equal(expired.rows?.[0].freshness, "stale")
  assert.equal(expired.observedAt, 2000)
  assert.equal(expired.snapshotGeneration, passOne)
  assert.equal(expired.complete, true)
  const source = fleet("a", ["same"])
  const entry = source.statusSnapshots.get(JSON.stringify(["a", "same"])) as { payload: { projected_at: number } }
  entry.payload.projected_at = -400
  const reading = statusProjection(source, "a", 2000)
  assert.equal(reading.kind === "unavailable" && reading.reason, "stale")
  assert.equal(reading.rows?.[0].freshness, "stale")
})

test("an expired pass with a missing or mismatched row still reports a gap", () => {
  const missing = statusProjection(fleet("a", ["same"], passOne, "same"), "a", 302001)
  assert.equal(missing.kind === "unavailable" && missing.reason, "event_gap")
  assert.equal(missing.rows, undefined)
  const mismatched = fleet("a", ["same"])
  const marker = mismatched.statusSnapshots.get(JSON.stringify(["a", INVENTORY])) as { payload: { snapshot_generation: string } }
  marker.payload.snapshot_generation = passTwo
  const gap = statusProjection(mismatched, "a", 302001)
  assert.equal(gap.kind === "unavailable" && gap.reason, "event_gap")
  assert.equal(gap.rows, undefined)
})
