import { test } from "node:test"
import assert from "node:assert/strict"

// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { BUBBLE_MARGIN, BUBBLE_SIZE, CopilotDock, bubbleSide, firstPoint, isTap, restingPoint } from "./copilot-dock.ts"

const PHONE = { width: 375, height: 812 }
const NOTCH = { top: 47, right: 0, bottom: 34, left: 0 }

test("a live call shrinks to the bubble and a tap on it opens the sheet again", () => {
  const dock = new CopilotDock()
  const seen: string[] = []
  dock.subscribe((m: string) => seen.push(m))
  dock.open()
  dock.minimize(true)
  dock.open()
  assert.deepEqual(seen, ["closed", "sheet", "bubble", "sheet"])
})

test("without a call there is no bubble to shrink to", () => {
  const dock = new CopilotDock()
  dock.open()
  dock.minimize(false)
  assert.equal(dock.get(), "closed")
})

test("a call that ends while shrunk takes the bubble away, and leaves an open sheet alone", () => {
  const dock = new CopilotDock()
  dock.open(); dock.minimize(true); dock.callEnded()
  assert.equal(dock.get(), "closed")
  dock.open(); dock.callEnded()
  assert.equal(dock.get(), "sheet", "a sheet shows why the call ended")
})

test("a released bubble rests against the nearer side, clear of the phone's bars", () => {
  const left = restingPoint({ x: 40, y: 300 }, PHONE, NOTCH)
  assert.deepEqual(left, { x: BUBBLE_MARGIN, y: 300 })
  const right = restingPoint({ x: 250, y: 300 }, PHONE, NOTCH)
  assert.deepEqual(right, { x: PHONE.width - BUBBLE_MARGIN - BUBBLE_SIZE, y: 300 })
  assert.equal(restingPoint({ x: 0, y: -100 }, PHONE, NOTCH).y, NOTCH.top + BUBBLE_MARGIN, "not under the status bar")
  assert.equal(restingPoint({ x: 0, y: 2000 }, PHONE, NOTCH).y, PHONE.height - NOTCH.bottom - BUBBLE_MARGIN - BUBBLE_SIZE, "not under the home indicator")
})

test("a side inset keeps the bubble out of a landscape phone's notch", () => {
  const sides = { top: 0, right: 47, bottom: 21, left: 47 }
  assert.equal(restingPoint({ x: 0, y: 100 }, { width: 812, height: 375 }, sides).x, 47 + BUBBLE_MARGIN)
  assert.equal(restingPoint({ x: 800, y: 100 }, { width: 812, height: 375 }, sides).x, 812 - 47 - BUBBLE_MARGIN - BUBBLE_SIZE)
})

test("the bubble first appears on the right, a third of the way down", () => {
  assert.deepEqual(firstPoint(PHONE, NOTCH), { x: PHONE.width - BUBBLE_MARGIN - BUBBLE_SIZE, y: 271 })
})

test("a small slip of the finger is still a tap, a real move is a drag", () => {
  assert.equal(isTap({ x: 10, y: 10 }, { x: 14, y: 13 }), true)
  assert.equal(isTap({ x: 10, y: 10 }, { x: 30, y: 10 }), false)
})

test("a bubble resting against either edge opens its notice toward the screen", () => {
  assert.equal(bubbleSide(restingPoint({ x: 300, y: 400 }, PHONE, NOTCH), PHONE), "right")
  assert.equal(bubbleSide(restingPoint({ x: 20, y: 400 }, PHONE, NOTCH), PHONE), "left")
  assert.equal(bubbleSide(firstPoint(PHONE, NOTCH), PHONE), "right")
})
