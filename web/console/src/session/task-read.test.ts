// What a failed read of the dispatched-work list leaves behind:
// `node --test web/console/src/session/*.test.ts`.
import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { TaskReadTrail, taskReadReason } from "./task-read.ts"

/** The two failures `ClawdlineClient` rejects with (`core/src/refusal.ts`). */
const refusal = Object.assign(new Error("cloud_not_carried: not carried"), { name: "RefusalError", code: "cloud_not_carried" })
const unanswered = Object.assign(new Error("GET /v1/orchestrator/tasks did not complete"), { name: "TransportError" })

test("the reason is the daemon's own refusal code, or what kind of failure it was", () => {
  assert.equal(taskReadReason(refusal), "cloud_not_carried")
  assert.equal(taskReadReason(unanswered), "unanswered")
  assert.equal(taskReadReason(new TypeError("offline")), "TypeError")
  assert.equal(taskReadReason(null), "unknown")
  assert.equal(taskReadReason("a string nobody threw on purpose"), "unknown")
})

test("a reason is said once however often the read runs, and coming back is said too", () => {
  const said: string[] = []
  const trail = new TaskReadTrail((line) => said.push(line))

  // The lane is ten seconds and this one was failing on every poll for as long
  // as the console was hosted. One line, not six a minute.
  for (let i = 0; i < 12; i++) trail.failed(refusal)
  assert.equal(said.length, 1)
  assert.match(said[0], /cloud_not_carried/)
  assert.equal(trail.reason, "cloud_not_carried")

  // A different reason is a different fact.
  trail.failed(unanswered)
  assert.equal(said.length, 2)
  assert.match(said[1], /unanswered/)

  // And a list that arrives ends it — once, and only if something was said.
  trail.arrived()
  trail.arrived()
  assert.equal(said.length, 3)
  assert.match(said[2], /again \(was unanswered\)/)
  assert.equal(trail.reason, "")

  // After which the same reason is news again.
  trail.failed(unanswered)
  assert.equal(said.length, 4)
})

test("a trail that has said nothing says nothing when the list arrives", () => {
  const said: string[] = []
  const trail = new TaskReadTrail((line) => said.push(line))
  trail.arrived()
  assert.deepEqual(said, [])
})
