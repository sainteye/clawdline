import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- `.ts` paths let Node's strip-types runner execute this test.
import { WORK_MILESTONES, workMilestones, workMilestonesShown, workMilestoneStates } from "./work-milestones.ts"

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

test("an ungated item shows no empty verify and merge slots", () => {
  const row = (phase: Parameters<typeof workMilestones>[0], gate: boolean) =>
    workMilestones(phase, gate).map((m: { label: string; state: string }) => `${m.label}:${m.state}`)
  assert.deepEqual(row("implementing", false), ["實作:current", "部署:pending", "完成:pending"])
  assert.deepEqual(row("deploying", false), ["實作:done", "部署:current", "完成:pending"])
  assert.deepEqual(row("done", false), ["實作:done", "部署:done", "完成:done"])
  // A gated item keeps the whole line.
  assert.equal(row("implementing", true).length, 5)
  // An item already standing in verifying or merging keeps it in sight.
  assert.deepEqual(row("merging", false), ["實作:done", "驗證:done", "合併:current", "部署:pending", "完成:pending"])
  assert.equal(row("verifying", false).length, 5)
})
