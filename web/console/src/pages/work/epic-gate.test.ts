// The Epic plan gate: `node --test web/console/src/pages/work/epic-gate.test.ts`.
import assert from "node:assert/strict"
import test from "node:test"
import type { WorkV2Document } from "./api.js"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { EPIC_GATE_HINT, FEATURE_GATE_HINT, epicGate, epicGateDetailShown, epicGateShown, epicPlanDocuments, isEpic, planGateHint, planReviewRequired } from "./epic-gate.ts"

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
    assert.equal(epicGateShown({ kind: "epic", review_required: false, phase, closed_at: null, gate_snapshot_cycle: 1, planning_gate: true }), true, phase)
  }
  for (const phase of ["implementing", "verifying", "merging", "deploying", "done"] as const) {
    assert.equal(epicGateShown({ kind: "epic", review_required: false, phase, closed_at: null, gate_snapshot_cycle: 1, planning_gate: true }), false, phase)
  }
  assert.equal(epicGateShown({ kind: "epic", review_required: false, phase: "assigned", closed_at: null, gate_snapshot_cycle: 0, planning_gate: true }), false)
  assert.equal(epicGateShown({ kind: "epic", review_required: false, phase: "assigned", closed_at: null, gate_snapshot_cycle: 1, planning_gate: false }), false)
  assert.equal(epicGateShown({ kind: "epic", review_required: false, phase: "assigned", closed_at: 1, gate_snapshot_cycle: 1, planning_gate: true }), false)
  assert.equal(epicGateShown({ kind: "issue", review_required: false, phase: "assigned", closed_at: null, gate_snapshot_cycle: 1, planning_gate: true }), false)
  assert.equal(isEpic({ kind: "epic" }), true)
  assert.equal(isEpic({ kind: "refactor" }), false)
})

test("the gate detail appears only for an Epic with written acceptance", () => {
  assert.equal(epicGateDetailShown({ kind: "issue", acceptance_criteria: "- Done" }), false)
  assert.equal(epicGateDetailShown({ kind: "feature", acceptance_criteria: "- Done" }), false)
  assert.equal(epicGateDetailShown({ kind: "epic", acceptance_criteria: "" }), false)
  assert.equal(epicGateDetailShown({ kind: "epic", acceptance_criteria: "  " }), false)
  assert.equal(epicGateDetailShown({ kind: "epic", acceptance_criteria: "- Done" }), true)
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

test("an unchecked Feature needs no plan or review, so its checklist never shows them missing", () => {
  const feature = { kind: "feature" as const, review_required: false, closed_at: null, gate_snapshot_cycle: 1, planning_gate: true }
  assert.equal(planReviewRequired(feature), false)
  for (const phase of ["created", "assigning", "assigned"] as const) {
    assert.equal(epicGateShown({ ...feature, phase }), false, phase)
  }
})

test("a Feature checked as needing independent review shows its plan and review missing like an Epic", () => {
  const feature = { kind: "feature" as const, review_required: true, closed_at: null, gate_snapshot_cycle: 1, planning_gate: true }
  assert.equal(planReviewRequired(feature), true)
  for (const phase of ["created", "assigning", "assigned"] as const) {
    assert.equal(epicGateShown({ ...feature, phase }), true, phase)
  }
  for (const phase of ["implementing", "verifying", "merging", "deploying", "done"] as const) {
    assert.equal(epicGateShown({ ...feature, phase }), false, phase)
  }
  assert.equal(epicGateShown({ ...feature, phase: "assigned", planning_gate: false }), false)
  assert.equal(epicGateShown({ ...feature, phase: "assigned", gate_snapshot_cycle: 0 }), false)
  assert.equal(epicGateShown({ ...feature, phase: "assigned", closed_at: 1 }), false)
  assert.deepEqual(epicGate(undefined), { plan: false, review: false, ready: false })
  assert.equal(planGateHint(feature), FEATURE_GATE_HINT)
  assert.equal(planGateHint({ kind: "epic" }), EPIC_GATE_HINT)
})

test("only a Feature's own checkbox matters: an Epic always needs plan review, an Issue never does", () => {
  assert.equal(planReviewRequired({ kind: "epic", review_required: false }), true)
  assert.equal(planReviewRequired({ kind: "issue", review_required: true }), false)
  assert.equal(planReviewRequired({ kind: "refactor", review_required: true }), false)
  assert.equal(planReviewRequired({ kind: "plan", review_required: true }), false)
})
