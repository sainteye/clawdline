import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- a `.ts` path, for node; see board-order.test.ts.
import { visibleWorkItems } from "./plan-visibility.ts"

const rows = [
  { id: "feature", kind: "feature" },
  { id: "plan", kind: "plan" },
  { id: "issue", kind: "issue" },
] as const

test("Plan items stay out of the Board until the person turns them on", () => {
  assert.deepEqual(visibleWorkItems(rows, false).map((item) => item.id), ["feature", "issue"])
})

test("the Plan switch restores Plan items without changing their order", () => {
  assert.deepEqual(visibleWorkItems(rows, true).map((item) => item.id), ["feature", "plan", "issue"])
  assert.deepEqual(rows.map((item) => item.id), ["feature", "plan", "issue"])
})
