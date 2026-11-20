import test from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node's strip-types runner uses source extensions.
import { sheetShouldClose, SheetCloseDistance } from "./sheet-swipe.ts"

test("a sheet closes when dragged far enough or flicked, and never when pushed up", () => {
  assert.equal(sheetShouldClose(SheetCloseDistance, 2000), true, "a slow long drag closes")
  assert.equal(sheetShouldClose(SheetCloseDistance - 1, 2000), false, "a slow short drag springs back")
  assert.equal(sheetShouldClose(50, 60), true, "a quick flick closes sooner")
  assert.equal(sheetShouldClose(20, 10), false, "a tiny twitch does not close")
  assert.equal(sheetShouldClose(0, 100), false)
  assert.equal(sheetShouldClose(-80, 50), false, "dragging up never closes")
  assert.equal(sheetShouldClose(Number.NaN, 50), false)
})
