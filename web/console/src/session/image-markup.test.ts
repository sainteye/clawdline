import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- Node's type-stripping runner resolves the source file itself.
import { fitImage, imagePoint, markWidth, panView, zoomView, type MarkupLayout } from "./image-markup.ts"

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

test("fit shows every edge of a long image before any zoom", () => {
  assert.deepEqual(fitImage(1174, 1600, 780, 700), { width: 513.625, height: 700, maxScale: 4 })
  const wide = fitImage(1600, 1174, 780, 700)
  assert.equal(wide.width, 780)
  assert.ok(Math.abs(wide.height - 572.325) < 0.0001)
  assert.equal(wide.maxScale, 4)
})

test("zoom stays under its anchor and pan stops at the image edges", () => {
  const layout: MarkupLayout = {
    left: 10,
    top: 20,
    width: 800,
    height: 600,
    fitWidth: 800,
    fitHeight: 400,
    maxScale: 4,
  }
  assert.deepEqual(zoomView({ scale: 1, x: 0, y: 0 }, 2, 410, 320, layout), { scale: 2, x: 0, y: 0 })
  assert.deepEqual(zoomView({ scale: 1, x: 0, y: 0 }, 2, 210, 220, layout), { scale: 2, x: 200, y: 100 })
  assert.deepEqual(panView({ scale: 2, x: 0, y: 0 }, 999, -999, layout), { scale: 2, x: 400, y: -100 })
})
