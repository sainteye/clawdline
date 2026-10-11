import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { OPENED_ROW_QUIET_MS, openedRowShow } from "./opened-row.ts"

test("an opened Session's row is on its way until the quiet stretch is over", () => {
  assert.equal(openedRowShow(0), "reading")
  assert.equal(openedRowShow(OPENED_ROW_QUIET_MS - 1), "reading")
})

test("a row that has not come by then is said, not waited for", () => {
  assert.equal(openedRowShow(OPENED_ROW_QUIET_MS), "absent")
  // A background tab's timers are throttled, so the stretch is read from the
  // clock and not from the timer that woke it.
  assert.equal(openedRowShow(60_000), "absent")
})

test("a stretch that cannot be read keeps the reading line rather than inventing a failure", () => {
  assert.equal(openedRowShow(Number.NaN), "reading")
})
