import test from "node:test"
import assert from "node:assert/strict"
// The frame reader is what decides whether the session page may stop reading
// the task list itself: a frame that is not a list must leave it reading.
// @ts-expect-error -- a `.ts` path, for node; nothing in it is imported at run time.
import { taskListFrame } from "../../core/src/task-frame.ts"

test("an orchestrator frame with a task list is taken whole", () => {
  const list = taskListFrame(JSON.stringify({ at: 1, page: {}, tasks: [{ id: "t1" }] }))
  assert.deepEqual(list?.tasks, [{ id: "t1" }])
})

test("a frame that is not a task list is not news that every task ended", () => {
  assert.equal(taskListFrame("{"), null)
  assert.equal(taskListFrame("null"), null)
  assert.equal(taskListFrame(JSON.stringify({ at: 1 })), null)
  assert.equal(taskListFrame(JSON.stringify({ tasks: "no" })), null)
})
