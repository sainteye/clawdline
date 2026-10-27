import assert from "node:assert/strict"
import test from "node:test"
import type { WorkV2Item, WorkV2Phase } from "./api.js"
// @ts-expect-error -- Node's strip-types runner needs the source extension.
import { boardReadStatus, desktopBoardItems } from "./desktop-board.ts"

function item(id: string, values: Partial<WorkV2Item> = {}): WorkV2Item {
  return { id, kind: "feature", phase: "implementing", area: "implementing", owner_session: "owner",
    condition: null, user_action: "", closed_at: null, ...values } as WorkV2Item
}

test("execution phases update a card without moving it between desktop columns", () => {
  const phases: WorkV2Phase[] = ["assigned", "implementing", "verifying", "merging", "deploying"]
  for (const phase of phases) {
    const rows = [item("first"), item("changing", { phase, area: phase }), item("last")]
    const grouped = desktopBoardItems(rows, [])
    assert.deepEqual(grouped.active.map((row) => row.id), ["first", "changing", "last"])
    assert.equal(grouped.attention.length, 0)
    assert.equal(grouped.unassigned.length, 0)
    assert.equal(rows[1].phase, phase)
    assert.equal(grouped.active[1], rows[1])
  }
})

test("conditions, pending questions and free-text actions keep their phase in Needs attention", () => {
  const conditions = ["blocked", "waiting_user", "owner_required", "owner_offline", "evidence_unknown", "assignment_failed", "assigned_unnotified"]
  const rows = conditions.map((condition) => item(condition, { condition, phase: "verifying", area: "verifying" }))
  rows.push(item("question"), item("linked", { decision_id: "not-loaded" }), item("action", { user_action: "Confirm the result" }), item("ordinary"))
  const grouped = desktopBoardItems(rows, [{ work_id: "question" }])
  assert.deepEqual(grouped.attention.map((row) => row.id), [...conditions, "question", "linked", "action"])
  assert.deepEqual(grouped.active.map((row) => row.id), ["ordinary"])
  assert.equal(grouped.attention[0].phase, "verifying")
})

test("a resolved condition returns the same item to execution without assigning or advancing it", () => {
  const row = item("blocked", { condition: "blocked", phase: "deploying", area: "deploying" })
  assert.equal(desktopBoardItems([row], []).attention[0], row)
  const resumed = { ...row, condition: null }
  assert.equal(desktopBoardItems([resumed], []).active[0], resumed)
  assert.equal(resumed.phase, "deploying")
  assert.equal(resumed.owner_session, "owner")
})

test("Plans remain separate from executable Epics and Refactors awaiting assignment", () => {
  const rows = [item("plan", { kind: "plan", phase: "created", area: "planning", owner_session: null }),
    item("epic", { kind: "epic", phase: "created", area: "unassigned", owner_session: null }),
    item("refactor", { kind: "refactor", phase: "created", area: "unassigned", owner_session: null })]
  const grouped = desktopBoardItems(rows, [])
  assert.deepEqual(grouped.planning.map((row) => row.id), ["plan"])
  assert.deepEqual(grouped.unassigned.map((row) => row.id), ["epic", "refactor"])
})

test("opening a new Session is in progress, while a failed assignment needs attention", () => {
  const grouped = desktopBoardItems([
    item("opening", { phase: "assigning", area: "assigning", owner_session: null }),
    item("failed", { phase: "created", area: "unassigned", owner_session: null, condition: "assignment_failed" }),
  ], [])
  assert.deepEqual(grouped.active.map((row) => row.id), ["opening"])
  assert.deepEqual(grouped.attention.map((row) => row.id), ["failed"])
  assert.deepEqual(grouped.unassigned, [])
})

test("completed items leave active columns; cancellation is not successful completion", () => {
  const grouped = desktopBoardItems([
    item("done", { phase: "done", closed_at: 100, owner_session: null, condition: "waiting_user" }),
    item("cancelled", { phase: "cancelled", closed_at: 100, owner_session: null }),
    item("closed-plan", { kind: "plan", area: "planning", phase: "done", closed_at: 100 }),
  ], [{ work_id: "done" }])
  assert.deepEqual(grouped.done.map((row) => row.id), ["done", "closed-plan"])
  assert.deepEqual(grouped.active, [])
  assert.deepEqual(grouped.attention, [])
  assert.deepEqual(grouped.planning, [])
})

test("desktop completion reads are independent of every phone status selection", () => {
  for (const phoneStatus of ["open", "done", "all"] as const) {
    assert.equal(boardReadStatus(true, false, phoneStatus), "open")
    assert.equal(boardReadStatus(true, true, phoneStatus), "all")
    assert.equal(boardReadStatus(false, false, phoneStatus), phoneStatus)
    assert.equal(boardReadStatus(false, true, phoneStatus), phoneStatus)
  }
})
