import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- `.ts` paths let Node's strip-types runner execute this test.
import { WORK_MILESTONES, workMilestonesShown, workMilestoneStates } from "./work-milestones.ts"

test("work phases map to one shared five-stage progress reading", () => {
  assert.deepEqual(WORK_MILESTONES, ["實作", "驗證", "合併", "部署", "完成"])
  assert.deepEqual(workMilestoneStates("assigned"), ["pending", "pending", "pending", "pending", "pending"])
  assert.deepEqual(workMilestoneStates("verifying"), ["done", "current", "pending", "pending", "pending"])
  assert.deepEqual(workMilestoneStates("merging"), ["done", "done", "current", "pending", "pending"])
  assert.deepEqual(workMilestoneStates("deploying"), ["done", "done", "done", "current", "pending"])
  assert.deepEqual(workMilestoneStates("done"), ["done", "done", "done", "done", "done"])
})

test("progress stays out of sight until implementation actually starts", () => {
  for (const phase of ["created", "assigning", "assigned", "cancelled"] as const) {
    assert.equal(workMilestonesShown(phase), false, phase)
  }
  for (const phase of ["implementing", "verifying", "merging", "deploying", "done"] as const) {
    assert.equal(workMilestonesShown(phase), true, phase)
  }
})

test("cancelled work does not claim an unfinished milestone", () => {
  assert.deepEqual(workMilestoneStates("cancelled"), ["pending", "pending", "pending", "pending", "pending"])
})
