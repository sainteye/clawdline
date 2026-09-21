import { test } from "node:test"
import assert from "node:assert/strict"
import type { SessionRow, TaskRow } from "@clawdline/contract"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { taskWorkState, workTree } from "./work-tree.ts"

const row = {
  id: "%fixture",
  sessionId: "conversation-fixture",
  agents_reading: { state: "complete" },
  agents: [{ id: "provider-fixture", at: 0, depth: 1, type: "Explore", what: "inspect", state: "running" }],
} as SessionRow

const broker = {
  id: "broker-fixture", task_id: "broker-fixture", title: "verify", state: "briefed",
  root: { terminalId: "%fixture", sessionId: "conversation-fixture" },
  child: { terminalId: "%child-fixture" },
} as TaskRow

test("broker children and provider agents share one tree without losing source or exact state", () => {
  const nodes = workTree(row, [broker], 100)
  assert.deepEqual(nodes.map((node) => [node.source, node.state, node.exactState]), [
    ["broker", "running", "briefed"],
    ["provider", "running", "running"],
  ])
})

test("the shared display state does not pretend an unreadable broker record succeeded", () => {
  assert.equal(taskWorkState("success"), "done")
  assert.equal(taskWorkState("failure"), "failed")
  assert.equal(taskWorkState("unreadable"), "unknown")
})
