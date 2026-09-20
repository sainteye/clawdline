// The status line's context cell: `node --test web/console/src/session/*.test.ts`.
import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node; see order.test.ts.
import { contextCell } from "./context.ts"

test("an exact window names both sides in the tooltip", () => {
  const cell = contextCell({ usedPercent: 16.2277, usedTokens: 162_277, windowTokens: 1_000_000 }, "tokens")
  assert.equal(cell?.percent, 16)
  assert.equal(cell?.level, "ok")
  assert.match(cell?.title ?? "", /^ctx 16% \(162,277 \/ 1,000,000 tokens\)$/)
})

test("a window the daemon withheld leaves the tooltip a percentage", () => {
  // No `windowTokens` on the wire is the daemon saying it guessed the window.
  // Drawing `162,277 / ?` or inventing the denominator is the failure this
  // gate exists to stop, so the tooltip says only what was measured.
  const cell = contextCell({ usedPercent: 16.2277, usedTokens: 162_277 }, "tokens")
  assert.equal(cell?.title, "ctx 16%")
})

test("the two thresholds are the ones the plan windows use", () => {
  // They are read off the rounded percentage, as the original reads them, so
  // the colour and the number the reader sees can never disagree.
  assert.equal(contextCell({ usedPercent: 59.4 }, "tokens")?.level, "ok")
  assert.equal(contextCell({ usedPercent: 59.6 }, "tokens")?.level, "warn")
  assert.equal(contextCell({ usedPercent: 60 }, "tokens")?.level, "warn")
  assert.equal(contextCell({ usedPercent: 84.4 }, "tokens")?.level, "warn")
  assert.equal(contextCell({ usedPercent: 85 }, "tokens")?.level, "bad")
})

test("no reading draws no cell, which is not a zero", () => {
  assert.equal(contextCell(undefined, "tokens"), null)
  assert.equal(contextCell(null, "tokens"), null)
  assert.equal(contextCell({ usedPercent: Number.NaN }, "tokens"), null)
  // A conversation that really is empty still says so.
  assert.equal(contextCell({ usedPercent: 0 }, "tokens")?.percent, 0)
})

test("a percentage past the window is full, not more than full", () => {
  assert.equal(contextCell({ usedPercent: 104 }, "tokens")?.percent, 100)
  assert.equal(contextCell({ usedPercent: -3 }, "tokens")?.percent, 0)
})
