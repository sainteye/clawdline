import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- Node's type-stripping runner resolves the source file itself.
import { imagePoint, markWidth } from "./image-markup.ts"

test("a pointer on a fitted preview maps back to original image pixels", () => {
  const bounds = { left: 20, top: 100, width: 320, height: 180 }
  assert.deepEqual(imagePoint(180, 190, bounds, 1600, 900), { x: 800, y: 450 })
  assert.deepEqual(imagePoint(-10, 500, bounds, 1600, 900), { x: 0, y: 900 })
})

test("the red pen remains useful on tiny and large images", () => {
  assert.equal(markWidth(200, 100), 6)
  assert.ok(Math.abs(markWidth(1600, 900) - 10.8) < 0.0001)
  assert.equal(markWidth(5000, 4000), 24)
})
