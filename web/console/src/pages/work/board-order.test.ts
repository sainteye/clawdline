// The Board keeps what is on screen in place: `node --test web/console/src/pages/work/board-order.test.ts`.
import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { arrangeWorkItems, workItemPlaces } from "./board-order.ts"

const ids = (rows: { id: string }[]) => rows.map((row) => row.id)

test("a fresh arrangement takes the daemon's order whole", () => {
  assert.deepEqual(ids(arrangeWorkItems([{ id: "b" }, { id: "a" }], null)), ["b", "a"])
})

test("an item edited on screen keeps its place instead of jumping to the top", () => {
  const before = workItemPlaces([{ id: "a" }, { id: "b" }, { id: "c" }])
  // The daemon now answers `c` first because it was just updated.
  assert.deepEqual(ids(arrangeWorkItems([{ id: "c" }, { id: "a" }, { id: "b" }], before)), ["a", "b", "c"])
})

test("an item that was not on screen yet arrives above the ones that were", () => {
  const before = workItemPlaces([{ id: "a" }, { id: "b" }])
  assert.deepEqual(ids(arrangeWorkItems([{ id: "n" }, { id: "b" }, { id: "a" }], before)), ["n", "a", "b"])
})

test("an item that left is dropped and the rest keep their order", () => {
  const before = workItemPlaces([{ id: "a" }, { id: "b" }, { id: "c" }])
  assert.deepEqual(ids(arrangeWorkItems([{ id: "c" }, { id: "a" }], before)), ["a", "c"])
})
