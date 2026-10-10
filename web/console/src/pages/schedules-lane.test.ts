import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { laneOnScreen, laneOwesRead } from "./schedules-lane.ts"

test("a section with a box and no hidden ancestor is on screen", () => {
  assert.equal(laneOnScreen({ laidOut: true, visibility: "visible" }), true)
})

test("the phone's hidden list pane takes the section off screen", () => {
  // responsive.css gives .pane-list `visibility: hidden` while the open
  // Session is the one showing, and visibility is inherited.
  assert.equal(laneOnScreen({ laidOut: true, visibility: "hidden" }), false)
})

test("a scroller taken out of the layout takes the section off screen", () => {
  assert.equal(laneOnScreen({ laidOut: false, visibility: "visible" }), false)
})

test("nothing drawn yet counts as on screen, so the first read is still made", () => {
  assert.equal(laneOnScreen(null), true)
})

const onScreen = { pageHidden: false, lastRefreshAt: null, now: 1_000_000, everyMs: 60_000, onScreen: () => true }

test("a lane that has never read owes a read", () => {
  assert.equal(laneOwesRead(onScreen), true)
})

test("a read a moment ago is left alone", () => {
  assert.equal(laneOwesRead({ ...onScreen, lastRefreshAt: onScreen.now - 59_999 }), false)
  assert.equal(laneOwesRead({ ...onScreen, lastRefreshAt: onScreen.now - 60_000 }), true)
})

test("nothing is read for a page nobody can see", () => {
  assert.equal(laneOwesRead({ ...onScreen, pageHidden: true }), false)
})

test("nothing is read while the section is off screen", () => {
  assert.equal(laneOwesRead({ ...onScreen, onScreen: () => false }), false)
})

test("the layout is only asked about once the reading is stale", () => {
  let asked = 0
  const box = () => {
    asked += 1
    return true
  }
  laneOwesRead({ ...onScreen, lastRefreshAt: onScreen.now - 1_000, onScreen: box })
  assert.equal(asked, 0, "a fresh reading answers without a style or layout read")
  laneOwesRead({ ...onScreen, lastRefreshAt: onScreen.now - 61_000, onScreen: box })
  assert.equal(asked, 1)
})
