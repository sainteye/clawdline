import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- `.ts` paths let Node's strip-types runner execute this test.
import { todoProgress, todoProgressLabel } from "./todo-progress.ts"

const me = "c0ffee00-0000-4000-8000-000000000000"

test("a Board item is started once it moves past assigned", () => {
  const p = todoProgress({
    assigned_items: [{ phase: "assigning" }, { phase: "assigned" }, { phase: "implementing" }, { phase: "verifying" }, { phase: "merging" }, { phase: "deploying" }],
    recent_items: [],
    direct_todos: [],
  }, me)
  assert.deepEqual(p, { done: 0, active: 4, waiting: 2 })
})

test("a direct to-do is started once the Session has read it or wrote it", () => {
  const p = todoProgress({
    assigned_items: [],
    recent_items: [],
    direct_todos: [
      { read_at: null, completed_at: null },
      { read_at: 1790000000, completed_at: null },
      { read_at: null, completed_at: null, created_by: me },
      { read_at: null, completed_at: null, created_by: "device:someone" },
    ],
  }, me)
  assert.deepEqual(p, { done: 0, active: 2, waiting: 2 })
})

test("finished counts recent Board items and completed direct to-dos", () => {
  const p = todoProgress({
    assigned_items: [{ phase: "assigned" }],
    recent_items: [{}, {}],
    direct_todos: [{ read_at: 1, completed_at: 2 }, { read_at: null, completed_at: 3 }],
  }, me)
  assert.deepEqual(p, { done: 4, active: 0, waiting: 1 })
})

test("without a conversation id nothing counts as written by the Session", () => {
  const p = todoProgress({ assigned_items: [], recent_items: [], direct_todos: [{ read_at: null, completed_at: null, created_by: me }] }, undefined)
  assert.deepEqual(p, { done: 0, active: 0, waiting: 1 })
})

test("the spoken label names all three counts and the total", () => {
  assert.equal(todoProgressLabel({ done: 1, active: 2, waiting: 3 }), "6 個待辦：1 個完成、2 個進行中、3 個未開始")
})
