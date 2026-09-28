// The Epic plan gate: `node --test web/console/src/pages/work/epic-gate.test.ts`.
import assert from "node:assert/strict"
import test from "node:test"
import type { WorkV2Document } from "./api.js"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { epicGate, epicGateShown, epicPlanDocuments, isEpic } from "./epic-gate.ts"

function document(id: string, role: WorkV2Document["role"], created_at: number): WorkV2Document {
  return { id, role, created_at, title: id, body: id, reference: "", position: 0, version: 1 }
}

test("an Epic with nothing written has neither the plan nor its review", () => {
  assert.deepEqual(epicGate(undefined), { plan: false, review: false, ready: false })
  assert.deepEqual(epicGate([document("spec", "spec", 100)]), { plan: false, review: false, ready: false })
})

test("a review with no plan does not count", () => {
  assert.deepEqual(epicGate([document("review", "plan_review", 100)]), { plan: false, review: false, ready: false })
})

test("a plan alone waits for its review", () => {
  assert.deepEqual(epicGate([document("plan", "plan", 100)]), { plan: true, review: false, ready: false })
})

test("a review written at or after the latest plan opens the gate", () => {
  assert.deepEqual(epicGate([document("plan", "plan", 100), document("review", "plan_review", 100)]),
    { plan: true, review: true, ready: true })
  assert.deepEqual(epicGate([document("plan", "plan", 100), document("review", "plan_review", 200)]),
    { plan: true, review: true, ready: true })
})

test("a plan rewritten after its review needs a new review", () => {
  const documents = [document("plan-1", "plan", 100), document("review-1", "plan_review", 150), document("plan-2", "plan", 200)]
  assert.deepEqual(epicGate(documents), { plan: true, review: false, ready: false })
  assert.deepEqual(epicGate([...documents, document("review-2", "plan_review", 250)]), { plan: true, review: true, ready: true })
})

test("the checklist shows only when this Epic captured planning on", () => {
  for (const phase of ["created", "assigning", "assigned"] as const) {
    assert.equal(epicGateShown({ kind: "epic", phase, closed_at: null, gate_snapshot_cycle: 1, planning_gate: true }), true, phase)
  }
  for (const phase of ["implementing", "verifying", "merging", "deploying", "done"] as const) {
    assert.equal(epicGateShown({ kind: "epic", phase, closed_at: null, gate_snapshot_cycle: 1, planning_gate: true }), false, phase)
  }
  assert.equal(epicGateShown({ kind: "epic", phase: "assigned", closed_at: null, gate_snapshot_cycle: 0, planning_gate: true }), false)
  assert.equal(epicGateShown({ kind: "epic", phase: "assigned", closed_at: null, gate_snapshot_cycle: 1, planning_gate: false }), false)
  assert.equal(epicGateShown({ kind: "epic", phase: "assigned", closed_at: 1, gate_snapshot_cycle: 1, planning_gate: true }), false)
  assert.equal(epicGateShown({ kind: "feature", phase: "assigned", closed_at: null, gate_snapshot_cycle: 1, planning_gate: true }), false)
  assert.equal(isEpic({ kind: "epic" }), true)
  assert.equal(isEpic({ kind: "refactor" }), false)
})

test("each plan is followed by the reviews written for it, newest plan first", () => {
  const documents = [
    document("stray-review", "plan_review", 50),
    document("plan-1", "plan", 100),
    document("review-1", "plan_review", 150),
    document("report", "completion_report", 160),
    document("plan-2", "plan", 200),
    document("review-2", "plan_review", 250),
  ]
  assert.deepEqual(epicPlanDocuments(documents).map((d) => d.id), ["plan-2", "review-2", "plan-1", "review-1", "stray-review"])
  assert.deepEqual(epicPlanDocuments(undefined), [])
})
