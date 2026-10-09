import assert from "node:assert/strict"
import test from "node:test"
import type { SessionRow } from "@clawdline/contract"
// @ts-expect-error -- `.ts` paths let Node's strip-types runner execute this test.
import { conditionWords, deploymentWords, needsPerson, nextActionWords, ownerOnlineWords, phaseStayWords } from "./board-card-facts.ts"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { withCatalog } from "../../catalog-testing.ts"

withCatalog("zh-Hant")

const project = { available: true } as const

test("every condition asks for attention in readable words", () => {
  const conditions = ["blocked", "waiting_user", "owner_required", "owner_offline", "evidence_unknown", "assignment_failed", "assigned_unnotified"]
  for (const condition of conditions) {
    assert.equal(needsPerson({ condition, user_action: "" }, 0), true)
    assert.doesNotMatch(conditionWords({ condition, project }), /_|^[a-z]+$/)
  }
  assert.equal(needsPerson({ condition: null, user_action: "" }, 1), true)
  assert.equal(needsPerson({ condition: null, user_action: "" }, 0), false)
})

test("phase stay uses its transition time and exposes missing evidence", () => {
  assert.equal(phaseStayWords(1000, 1000 + 90 * 60), "停留 1 小時 30 分鐘")
  assert.equal(phaseStayWords(null, 1000), "停留時間不明")
})

test("online state needs a current Session reading", () => {
  const row = { sessionId: "owner", state: "idle", source: { freshness: "current" } } as SessionRow
  assert.equal(ownerOnlineWords("owner", [row]), "負責 Session 在線")
  assert.equal(ownerOnlineWords("owner", [{ ...row, source: { freshness: "unverified" } } as SessionRow]), "負責 Session 在線狀態不明")
  assert.equal(ownerOnlineWords("owner", []), "負責 Session 未在線")
})

test("closed cards show the first nonempty deployment line or its reason", () => {
  assert.equal(deploymentWords({ deployment_evidence: "\nCloud build 123\nmore", no_deployment_reason: "" }), "部署證據：Cloud build 123")
  assert.equal(deploymentWords({ deployment_evidence: "", no_deployment_reason: "No release needed\nmore" }), "未部署：No release needed")
  assert.equal(deploymentWords({}), "尚無部署說明")
})

test("a card waiting on a decision names its question, and an older free-text wait still reads", () => {
  const question = { id: "d1", question: "Is the release window confirmed?" }
  assert.equal(needsPerson({ condition: null, user_action: "", decision_id: "d1" }, 0), true)
  assert.equal(nextActionWords({ user_action: "", decision_id: "d1" }, [question]), "等你決定：Is the release window confirmed?")
  // A decision this read does not hold is a question that waits, not nothing.
  assert.equal(nextActionWords({ user_action: "", decision_id: "d2" }, [question]), "等你回答一個問題")
  assert.equal(nextActionWords({ user_action: "Confirm the window." }, []), "下一步：Confirm the window.")
  assert.equal(nextActionWords({ user_action: "" }, [question]), "")
})
