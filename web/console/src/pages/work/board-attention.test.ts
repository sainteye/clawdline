import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- `.ts` paths let Node's strip-types runner execute this test.
import { decisionsForWorkItem, proposalsForProject } from "./board-attention.ts"

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
