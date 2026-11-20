import test from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node's strip-types runner uses source extensions.
import { sheetRelease, sheetShouldClose, SheetCloseDistance } from "./sheet-swipe.ts"

test("a sheet closes when dragged far enough or flicked, and never when pushed up", () => {
  assert.equal(sheetShouldClose(SheetCloseDistance, 2000), true, "a slow long drag closes")
  assert.equal(sheetShouldClose(SheetCloseDistance - 1, 2000), false, "a slow short drag springs back")
  assert.equal(sheetShouldClose(50, 60), true, "a quick flick closes sooner")
  assert.equal(sheetShouldClose(20, 10), false, "a tiny twitch does not close")
  assert.equal(sheetShouldClose(0, 100), false)
  assert.equal(sheetShouldClose(-80, 50), false, "dragging up never closes")
  assert.equal(sheetShouldClose(Number.NaN, 50), false)
})

test("a half sheet swiped up fills the screen; a full one swiped down returns to half", () => {
  assert.equal(sheetRelease(-SheetCloseDistance, 2000, false), "expand")
  assert.equal(sheetRelease(-50, 60, false), "expand", "a quick flick up expands")
  assert.equal(sheetRelease(-20, 2000, false), "stay")
  assert.equal(sheetRelease(-300, 100, true), "stay", "a full sheet cannot grow further")
  assert.equal(sheetRelease(SheetCloseDistance, 2000, true), "collapse", "down from full returns to half, not closed")
  assert.equal(sheetRelease(SheetCloseDistance, 2000, false), "close")
  assert.equal(sheetRelease(30, 2000, true), "stay")
})
