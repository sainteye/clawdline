import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- Node runs this test against the source file directly.
import { BOARD_DETAIL_KEY, readBoardDetailMode, writeBoardDetailMode } from "./board-detail-mode.ts"

function memory() {
  const values = new Map<string, string>()
  return { getItem: (key: string) => values.get(key) ?? null, setItem: (key: string, value: string) => { values.set(key, value) }, values }
}

test("a device that never chose shows brief cards", () => {
  assert.equal(readBoardDetailMode(memory()), false)
  assert.equal(readBoardDetailMode(null), false)
})

test("each device remembers its own detail choice", () => {
  const phone = memory()
  const desktop = memory()
  writeBoardDetailMode(true, phone)
  assert.equal(readBoardDetailMode(phone), true)
  assert.equal(readBoardDetailMode(desktop), false)
  writeBoardDetailMode(false, phone)
  assert.equal(readBoardDetailMode(phone), false)
  assert.equal(phone.values.get(BOARD_DETAIL_KEY), "brief")
})

test("blocked storage falls back to brief cards without throwing", () => {
  const blocked = { getItem: () => { throw new Error("blocked") }, setItem: () => { throw new Error("blocked") } }
  assert.equal(readBoardDetailMode(blocked), false)
  assert.doesNotThrow(() => writeBoardDetailMode(true, blocked))
})
