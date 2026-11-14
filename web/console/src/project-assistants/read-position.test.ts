import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error Node executes source extensions.
import { seenThrough } from "./read-position.ts"
test("a reply taller than the window counts as seen once its end is on screen",()=>{
  // The window shows 0–600; a 1,200 px reply starts above it and ends at 580.
  assert.equal(seenThrough([{ top: -620, bottom: 580, seq: 7 }], 0, 600), 7)
  // Its end is still below the window: not seen yet.
  assert.equal(seenThrough([{ top: 100, bottom: 1300, seq: 8 }], 0, 600), 0)
  assert.equal(seenThrough([{ top: 10, bottom: 90, seq: 3 }, { top: 100, bottom: 500, seq: 4 }], 0, 600), 4)
})
