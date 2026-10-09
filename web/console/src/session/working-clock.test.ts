import test from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node; see order.test.ts.
import { elapsedSpan, elapsedWords, liveWorkingLine } from "./working-clock.ts"

test("the clock is found where the daemon finds it", () => {
  const line = "Thinking… (1m 12s · ↑ 3k tokens)"
  const span = elapsedSpan(line)
  assert.deepEqual(span && { text: line.slice(span.start, span.end), seconds: span.seconds }, { text: "1m 12s", seconds: 72 })
  assert.equal(elapsedSpan("Working (11m 38s • esc to interrupt)")?.seconds, 698)
  assert.equal(elapsedSpan("Reading (3 stages)"), null)
  assert.equal(elapsedSpan("Thinking…"), null)
})

test("the clock is spelled as the providers spell it", () => {
  assert.equal(elapsedWords(7), "7s")
  assert.equal(elapsedWords(72), "1m 12s")
  assert.equal(elapsedWords(3723), "1h 2m 3s")
  assert.equal(elapsedWords(-4), "0s")
})

test("a row with working_since draws its own clock", () => {
  const line = "Hullaballooing… (40s · ↓ 3.1k tokens · thinking)"
  assert.equal(liveWorkingLine(line, 1_000, 1_082), "Hullaballooing… (1m 22s · ↓ 3.1k tokens · thinking)")
})

test("an older daemon's row is drawn as it was sent", () => {
  const line = "Working (7s • esc to interrupt)"
  assert.equal(liveWorkingLine(line, undefined, 1_082), line)
  assert.equal(liveWorkingLine(line, 0, 1_082), line)
  assert.equal(liveWorkingLine("Thinking…", 1_000, 1_082), "Thinking…")
})
