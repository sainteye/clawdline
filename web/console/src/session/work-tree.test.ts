import { test } from "node:test"
import assert from "node:assert/strict"
import type { SessionRow } from "@clawdline/contract"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { agentDisplayName, runningAgentCount, workTree } from "./work-tree.ts"

const row = {
  id: "%fixture",
  sessionId: "conversation-fixture",
  agents_reading: { state: "complete" },
  agents: [
    { id: "provider-running", at: 0, depth: 1, type: "Explore", what: "inspect fixture", state: "running" },
    { id: "provider-done", at: 0, depth: 1, type: "Explore", what: "verify fixture", state: "done" },
  ],
} as SessionRow

test("the work tree is provider-native work, not a second broker task list", () => {
  const nodes = workTree(row)
  assert.deepEqual(nodes.map((node) => [node.source, node.state, node.agent.what]), [
    ["provider", "running", "inspect fixture"],
    ["provider", "done", "verify fixture"],
  ])
})

test("the folded summary counts running provider work", () => {
  assert.equal(runningAgentCount(row), 1)
})

test("Codex uses its nickname, then an honest numbered fallback instead of repeating thread_spawn", () => {
  const named = { ...row.agents![0], type: "thread_spawn", what: "Fixture Scout" }
  const unnamed = { ...named, what: "thread_spawn" }
  assert.equal(agentDisplayName(named, "codex", 1, "not recorded", "agent"), "Fixture Scout")
  assert.equal(agentDisplayName(unnamed, "codex", 2, "not recorded", "agent"), "not recorded · 2")
})
