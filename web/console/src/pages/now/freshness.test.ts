// The rule the "now" page exists for, held to:
// `node --test --experimental-strip-types "src/pages/now/freshness.test.ts"`
//
// Every case here is a way the machine can answer, and the one thing none of
// them may produce is a number for something nobody read. That is the mistake
// the work-system review found the coordinator making — `sources.sessions:
// stale` sitting beside nineteen rows reported in the present tense — and a
// page drawing it in 26px type would be the same mistake, larger.
import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node; see `session/order.test.ts`.
import { ageWords, draw, readClock, type Source } from "./freshness.ts"

const at = (freshness: Source["freshness"]): Source => ({
  observed_at: 1_700_000_000,
  provenance: "broker",
  freshness,
})

test("a block that could not be read has no count, whatever else it was handed", () => {
  for (const rows of [undefined, 0, 7]) {
    const drawn = draw("owed", { read: false, failure: "the broker store could not be opened", rows })
    assert.equal(drawn.count, null)
    assert.equal(drawn.tone, "bad")
    assert.equal(drawn.settled, false)
    // The refusal's own sentence survives; it is not replaced by a general one.
    assert.equal(drawn.said, "the broker store could not be opened")
  }
})

test("a read that failed with no sentence still says something", () => {
  const drawn = draw("doing", { read: false })
  assert.equal(drawn.count, null)
  assert.equal(drawn.line, "unreadable")
})

test("a source that names itself missing loses its count too", () => {
  const drawn = draw("doing", { read: true, rows: 3, source: at("missing") })
  assert.equal(drawn.count, null)
  assert.equal(drawn.line, "freshMissing")
  assert.equal(drawn.settled, false)
})

test("only a reading that happened and is confirmed prints a plain zero", () => {
  const drawn = draw("waiting", { read: true, rows: 0, source: at("current") })
  assert.equal(drawn.count, 0)
  assert.equal(drawn.tone, "plain")
  assert.equal(drawn.settled, true)
})

test("unverified keeps its count and is never settled", () => {
  const drawn = draw("owed", { read: true, rows: 19, source: at("unverified") })
  assert.equal(drawn.count, 19)
  assert.equal(drawn.settled, false)
  assert.equal(drawn.tone, "warn")
  // The count is never on screen without the sentence saying what is
  // unconfirmed, and the sentence is the one for this block's own source.
  assert.equal(drawn.line, "freshUnverified")
  assert.equal(drawn.why, "freshWhyLandings")
})

test("each block explains its own unverified, not a shared one", () => {
  const whys = new Set(
    (["doing", "owed", "waiting"] as const).map(
      (block) => draw(block, { read: true, rows: 1, source: at("unverified") }).why,
    ),
  )
  assert.equal(whys.size, 3)
})

test("a short reading keeps its rows and says it is short", () => {
  const drawn = draw("waiting", { read: true, rows: 2, source: at("stale") })
  assert.equal(drawn.count, 2)
  assert.equal(drawn.settled, false)
  assert.equal(drawn.line, "freshStale")
})

test("an answer with no freshness at all is unconfirmed, never fine", () => {
  const drawn = draw("doing", { read: true, rows: 4 })
  assert.equal(drawn.settled, false)
  assert.equal(drawn.line, "freshUnverified")
  assert.equal(drawn.count, 4)
})

test("every answer carries a line: a number is never shown bare", () => {
  const readings = [
    { read: false as const },
    { read: false as const, failure: "refused" },
    { read: true as const, rows: 0, source: at("current") },
    { read: true as const, rows: 1, source: at("stale") },
    { read: true as const, rows: 1, source: at("missing") },
    { read: true as const, rows: 1, source: at("unverified") },
  ]
  for (const reading of readings) {
    const drawn = draw("owed", reading)
    assert.ok(drawn.line || drawn.said, `nothing to say for ${JSON.stringify(reading)}`)
  }
})

test("an age is said in the coarsest unit that is still true", () => {
  assert.equal(ageWords(0, false), "0s")
  assert.equal(ageWords(59, false), "59s")
  assert.equal(ageWords(60, false), "1m")
  assert.equal(ageWords(3600, false), "1h")
  assert.equal(ageWords(90_000, false), "1d")
  assert.equal(ageWords(90_000, true), "1 天")
  // A negative or absent age is not a large one.
  assert.equal(ageWords(-5, false), "0s")
  assert.equal(ageWords(null, false), "0s")
})

test("a source with no time on it has no clock, rather than the epoch", () => {
  assert.equal(readClock(undefined, false), "")
  assert.equal(readClock({ observed_at: 0, provenance: "broker", freshness: "current" }, false), "")
  assert.notEqual(readClock(at("current"), false), "")
})
