// The history sheet never shows one schedule under another's row:
// `node --test web/console/src/schedule-history-open.test.ts`.
import assert from "node:assert/strict"
import test from "node:test"

// @ts-expect-error -- a `.ts` path for node's type-stripping test runner.
import { historyRecordFor, shouldOpenHistory } from "./schedule-history-open.ts"

// Measured on 2026-09-27 against app.clawdline.com: with one schedule's sheet
// on screen, a click on another row left the first schedule's title, history
// and Run now button in place, because `open` returned whenever the sheet was
// showing. Run now there runs the schedule on screen, not the row pressed.
test("a row pressed while another schedule's sheet is showing opens that row", () => {
  assert.equal(shouldOpenHistory({ shown: true, scheduleId: "timed", busy: false }, "trigger-only"), true)
})

test("the sheet already showing that schedule, or busy running one, is left alone", () => {
  assert.equal(shouldOpenHistory({ shown: true, scheduleId: "timed", busy: false }, "timed"), false)
  assert.equal(shouldOpenHistory({ shown: true, scheduleId: "timed", busy: true }, "trigger-only"), false)
  assert.equal(shouldOpenHistory({ shown: false, scheduleId: null, busy: false }, undefined), false)
  assert.equal(shouldOpenHistory({ shown: false, scheduleId: null, busy: false }, "timed"), true)
})

test("an answer is the sheet's record only when it is the schedule asked for", () => {
  const trigger = { id: "trigger-only", when: { trigger_only: true } }
  assert.equal(historyRecordFor({ schedule: trigger }, "trigger-only"), trigger)
  assert.equal(historyRecordFor({ schedule: { id: "timed" } }, "trigger-only"), null)
  assert.equal(historyRecordFor({}, "trigger-only"), null)
  assert.equal(historyRecordFor(null, "trigger-only"), null)
})
