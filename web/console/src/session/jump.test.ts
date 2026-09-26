import test from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node; see order.test.ts.
import { JUMP_AT_END_PX, atNewest, fromNewest, jumpOffered } from "./jump.ts"

const box = (scrollTop: number, scrollHeight = 5000, clientHeight = 800) => ({ scrollTop, scrollHeight, clientHeight })

test("at the bottom, or within the allowance, nothing is offered", () => {
  assert.equal(jumpOffered(box(4200), false, false), false)
  assert.equal(jumpOffered(box(4200 - JUMP_AT_END_PX), false, true), false)
  assert.equal(atNewest(box(4200 - JUMP_AT_END_PX), false), true)
})

test("less than a screen up is reading, not lost", () => {
  assert.equal(fromNewest(box(3500), false), 700)
  assert.equal(jumpOffered(box(3500), false, false), false)
})

test("a screen or more up is offered the way back", () => {
  assert.equal(jumpOffered(box(3400), false, false), true)
  assert.equal(jumpOffered(box(0), false, false), true)
})

test("something new at the end is offered as soon as the reader is past it", () => {
  assert.equal(jumpOffered(box(4100), false, true), true)
})

test("newest first measures from the top", () => {
  assert.equal(fromNewest(box(0), true), 0)
  assert.equal(jumpOffered(box(0), true, true), false)
  assert.equal(jumpOffered(box(900), true, false), true)
  assert.equal(jumpOffered(box(4200), false, false), false)
})

test("a transcript shorter than its box is always at the end", () => {
  assert.equal(fromNewest(box(0, 300, 800), false), 0)
  assert.equal(jumpOffered(box(0, 300, 800), false, true), false)
})
