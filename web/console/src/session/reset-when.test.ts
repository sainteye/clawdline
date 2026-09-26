// The info card's reset moment: `node --test web/console/src/session/*.test.ts`.
import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node; see order.test.ts.
import { resetWhen } from "./reset-when.ts"

const minutes = (n: number) => `in ${n} min`
const local = (y: number, mo: number, d: number, h: number, mi: number) => new Date(y, mo - 1, d, h, mi).getTime() / 1000

test("a reset under an hour ahead counts minutes, not 'just now'", () => {
  const now = local(2026, 9, 26, 18, 50)
  assert.equal(resetWhen(now + 25 * 60, now, "en", minutes), "in 25 min")
  assert.equal(resetWhen(now + 10, now, "en", minutes), "in 1 min")
})

test("a reset later today is its clock time", () => {
  const now = local(2026, 9, 26, 18, 50)
  assert.equal(resetWhen(local(2026, 9, 26, 23, 30), now, "en", minutes), "23:30")
})

test("a reset on another day carries its day", () => {
  const now = local(2026, 9, 26, 18, 50)
  const said = resetWhen(local(2026, 10, 2, 4, 0), now, "en", minutes)
  assert.match(said, /Fri/)
  assert.match(said, /10\/2/)
  assert.match(said, /04:00$/)
  const zh = resetWhen(local(2026, 10, 2, 4, 0), now, "zh-TW", minutes)
  assert.match(zh, /10\/2/)
  assert.match(zh, /週五/)
  assert.match(zh, /04:00$/)
})

test("a reset tomorrow early morning is not drawn as today", () => {
  const now = local(2026, 9, 26, 23, 30)
  const said = resetWhen(local(2026, 9, 27, 0, 45), now, "en", minutes)
  assert.notEqual(said, "00:45")
  assert.match(said, /9\/27/)
})

test("a reset already past is never 'just now'", () => {
  const now = local(2026, 9, 26, 18, 50)
  const said = resetWhen(local(2026, 9, 26, 18, 0), now, "en", minutes)
  assert.equal(said, "18:00")
})
