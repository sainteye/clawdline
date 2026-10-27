import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node's source runner needs the TypeScript extension.
import { videoPosterScore } from "./video-poster-quality.ts"

const width = 64, height = 36
function image(color: number[], detail = false) {
  const pixels = new Uint8ClampedArray(width * height * 4)
  for (let y = 0; y < height; y++) for (let x = 0; x < width; x++) {
    const ink = detail && x > 15 && x < 48 && y > 8 && y < 28 && (x + y) % 6 < 3
    pixels.set(ink ? [240, 110, 40, 255] : [...color, 255], (y * width + x) * 4)
  }
  return videoPosterScore(pixels, width, height)
}
test("picture detail outranks blank, black and solid-colour intros", () => {
  for (const background of [[0, 0, 0], [255, 255, 255], [25, 38, 65], [54, 60, 65]]) {
    assert.equal(image(background), 0)
    assert.ok(image(background, true) > image(background))
  }
})
test("a near-uniform fade does not outrank a visible subject", () => {
  const fade = new Uint8ClampedArray(width * height * 4)
  for (let i = 0; i < fade.length; i += 4) fade.set([251 + (i % 3), 251, 251, 255], i)
  assert.equal(videoPosterScore(fade, width, height), 0)
  assert.ok(image([240, 235, 220], true) > videoPosterScore(fade, width, height))
})
