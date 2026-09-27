// An Epic's children and a child's Epic: `node --test web/console/src/pages/work/epic-family.test.ts`.
import assert from "node:assert/strict"
import test from "node:test"
import type { WorkV2Item } from "./api.js"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { epicChildren, epicParent, epicProgress, epicProgressWords, needsFamilyList, shortWorkID } from "./epic-family.ts"

type Row = Pick<WorkV2Item, "id" | "kind" | "title" | "phase" | "parent_id">

function row(id: string, phase: WorkV2Item["phase"], parent_id?: string, kind: WorkV2Item["kind"] = "feature"): Row {
  return { id, kind, title: `title ${id}`, phase, parent_id }
}

test("an Epic's children are the items that name it as parent, in list order", () => {
  const items = [row("epic", "implementing", undefined, "epic"), row("a", "done", "epic"), row("b", "created", "other"),
    row("c", "implementing", "epic"), row("d", "verifying", "")]
  assert.deepEqual(epicChildren(items, "epic").map((item: Row) => item.id), ["a", "c"])
  assert.deepEqual(epicChildren(items, ""), [], "an empty id is no Epic, so it must not match items with an empty parent")
})

test("progress counts done children over those not cancelled; cancelled is counted apart", () => {
  assert.deepEqual(epicProgress([]), { done: 0, total: 0, cancelled: 0 })
  const children = [row("a", "done"), row("b", "done"), row("c", "implementing"), row("d", "cancelled"), row("e", "created")]
  assert.deepEqual(epicProgress(children), { done: 2, total: 4, cancelled: 1 })
  assert.equal(epicProgressWords(epicProgress(children)), "2 / 4 已完成 · 1 已取消")
  assert.equal(epicProgressWords(epicProgress([row("a", "done"), row("b", "verifying")])), "1 / 2 已完成")
})

test("an Epic whose remaining children are all done reads as complete even with one cancelled", () => {
  const progress = epicProgress([row("a", "done"), row("b", "cancelled")])
  assert.deepEqual(progress, { done: 1, total: 1, cancelled: 1 })
})

test("a child names its Epic by title when the Epic is listed, by id when it is not", () => {
  const items = [row("epic-0001-abcdef", "implementing", undefined, "epic")]
  assert.deepEqual(epicParent({ parent_id: "epic-0001-abcdef" }, items), { id: "epic-0001-abcdef", title: "title epic-0001-abcdef" })
  assert.deepEqual(epicParent({ parent_id: "missing-9999-xyz" }, items), { id: "missing-9999-xyz" })
  assert.equal(shortWorkID("missing-9999-xyz"), "missing-")
  assert.equal(epicParent({ parent_id: "" }, items), null)
  assert.equal(epicParent({}, items), null)
})

test("the family list is read only when an Epic or a child is on the Board", () => {
  assert.equal(needsFamilyList([row("a", "created"), row("b", "done", "")]), false)
  assert.equal(needsFamilyList([row("a", "created", undefined, "epic")]), true)
  assert.equal(needsFamilyList([row("a", "created", "epic")]), true)
})
