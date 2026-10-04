import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- `.ts` paths let Node's strip-types runner execute this test.
import { confirmDecisionAnswer, decisionsForWorkItem, proposalsForProject, withoutAnsweredDecision, withoutAnsweredWait } from "./board-attention.ts"

test("questions stay with the Board item that gives them context", () => {
  const rows = [
    { id: "one", work_id: "work-a" },
    { id: "two", work_id: "work-b" },
  ]
  assert.deepEqual(decisionsForWorkItem(rows, "work-a").map((row) => row.id), ["one"])
})

test("Agent proposals follow the Board's Project scope", () => {
  const rows = [{ id: "one", project_id: "a" }, { id: "two", project_id: "b" }]
  assert.deepEqual(proposalsForProject(rows, "a").map((row) => row.id), ["one"])
  assert.deepEqual(proposalsForProject(rows, "").map((row) => row.id), ["one", "two"])
})

test("a confirmed answer clears only its choice and wait even when the next Board read fails", async () => {
  let choices = [{ id: "answered" }, { id: "other" }]
  let item = { decision_id: "answered", condition: "waiting_user", user_action: "reply" }
  await confirmDecisionAnswer(async () => undefined, () => {
    choices = withoutAnsweredDecision(choices, "answered")
    item = withoutAnsweredWait(item, "answered")
  })
  await assert.rejects(async () => { throw new Error("Board GET failed") }, /Board GET failed/)
  assert.deepEqual(choices, [{ id: "other" }])
  assert.deepEqual(item, { decision_id: undefined, condition: null, user_action: "" })
})

test("an uncertain answer leaves the choice and wait available for retry", async () => {
  let confirmed = false
  await assert.rejects(confirmDecisionAnswer(async () => { throw new Error("network") }, () => { confirmed = true }), /network/)
  assert.equal(confirmed, false)
})
