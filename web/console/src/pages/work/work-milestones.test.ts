import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- `.ts` paths let Node's strip-types runner execute this test.
import { WORK_MILESTONES, workMilestoneStates } from "./work-milestones.ts"

test("work phases map to one shared five-stage progress reading", () => {
  assert.deepEqual(WORK_MILESTONES, ["實作", "驗證", "Commit / Merge", "部署", "完成"])
  assert.deepEqual(workMilestoneStates("assigned"), ["current", "pending", "pending", "pending", "pending"])
  assert.deepEqual(workMilestoneStates("verifying"), ["done", "current", "pending", "pending", "pending"])
  assert.deepEqual(workMilestoneStates("merging"), ["done", "done", "current", "pending", "pending"])
  assert.deepEqual(workMilestoneStates("deploying"), ["done", "done", "done", "current", "pending"])
  assert.deepEqual(workMilestoneStates("done"), ["done", "done", "done", "done", "done"])
})

test("cancelled work does not claim an unfinished milestone", () => {
  assert.deepEqual(workMilestoneStates("cancelled"), ["pending", "pending", "pending", "pending", "pending"])
})
