import assert from "node:assert/strict"
import test from "node:test"
import type { WorkV2Item } from "./api.js"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { appendWorkPage } from "./work-pages.ts"

const item = (id: string, version = 1) => ({ id, version } as WorkV2Item)

test("a later Board page appends once and refreshes an overlapping row", () => {
  const rows = appendWorkPage([item("a"), item("b")], [item("b", 2), item("c")])
  assert.deepEqual(rows.map(({ id, version }) => [id, version]), [["a", 1], ["b", 2], ["c", 1]])
})
