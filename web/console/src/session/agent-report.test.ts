import { test } from "node:test"
import assert from "node:assert/strict"
import type { SessionAgent } from "@clawdline/contract"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { agentReportIdentity } from "./agent-report.ts"

const agent = {
  id: "agent-fixture",
  at: 0,
  depth: 1,
  state: "done",
  type: "Explore",
  what: "檢查資料流",
} as SessionAgent

test("a hand-back uses the same identifiable name as the background-agent row", () => {
  assert.deepEqual(agentReportIdentity("agent-fixture", [agent], "codex", "zh-Hant"), {
    id: "agent-fixture",
    known: true,
    label: "檢查資料流",
    speaker: "背景 agent",
    detail: "agent-fixture",
  })
})

test("a Codex hand-back and its work-tree row share the same fallback name", () => {
  const unnamed = { ...agent, what: "Explore" }
  assert.equal(
    agentReportIdentity("agent-fixture", [unnamed], "codex", "zh-Hant").label,
    "Codex 沒有記下這個 thread 在做什麼 · 1",
  )
})

test("an unknown hand-back names its source instead of becoming the user", () => {
  const got = agentReportIdentity("missing-fixture", [agent], "codex", "zh-Hant")
  assert.equal(got.known, false)
  assert.equal(got.speaker, "背景 agent")
  assert.equal(got.detail, "來源是 missing-fixture，這台機器不認得它")
})
