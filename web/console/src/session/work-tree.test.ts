import { test } from "node:test"
import assert from "node:assert/strict"
import type { SessionRow } from "@clawdline/contract"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { agentDisplayName, agentModelName, runningAgentCount, workTree } from "./work-tree.ts"

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

test("subagent model labels retain the recorded family and version without guessing unknown ids", () => {
  for (const [recorded, displayed] of [
    ["claude-opus-5-5", "Opus 5.5"], ["claude-sonnet-4-6", "Sonnet 4.6"],
    ["claude-haiku-4-5-20251001", "Haiku 4.5"], ["claude-3-5-sonnet-20241022", "Sonnet 3.5"],
    ["claude-sonnet-4-20250514", "Sonnet 4"], ["gpt-fixture-1", "gpt-fixture-1"], ["", ""],
  ]) assert.equal(agentModelName(recorded), displayed)
  assert.equal(agentModelName(undefined), "")
})
