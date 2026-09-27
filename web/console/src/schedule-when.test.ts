// A schedule with no time: `node --test web/console/src/schedule-when.test.ts`.
import assert from "node:assert/strict"
import test from "node:test"

// @ts-expect-error -- a `.ts` path for node's type-stripping test runner.
import { isTriggerOnly, scheduleNextLine, scheduleWhenFields } from "./schedule-when.ts"
// @ts-expect-error -- a `.ts` path for node's type-stripping test runner.
import { recordBody } from "./cloud/schedule-move.ts"

const words = { next: "Next", disabled: "Disabled", noNext: "No next run is scheduled.", triggerOnly: "Runs only by webhook or by hand" }

test("a trigger-only row says how it runs instead of a next run", () => {
  assert.equal(scheduleNextLine({ enabled: true, trigger_only: true }, "", words), "Runs only by webhook or by hand")
  assert.equal(scheduleNextLine({ enabled: false, trigger_only: true }, "", words), "Disabled · Runs only by webhook or by hand")
})

test("a timed row still says its next run, or that it has none", () => {
  assert.equal(scheduleNextLine({ enabled: true, next_fire: 100 }, "in 5 minutes", words), "Next in 5 minutes")
  assert.equal(scheduleNextLine({ enabled: false, next_fire: 100 }, "in 5 minutes", words), "Disabled · next in 5 minutes")
  assert.equal(scheduleNextLine({ enabled: true }, "", words), "No next run is scheduled.")
})

test("a trigger-only body sends trigger_only and none of at, days and on", () => {
  const fields = scheduleWhenFields(true, "09:00", ["mon"])
  assert.deepEqual(fields, { trigger_only: true })
  assert.equal("at" in fields || "days" in fields || "on" in fields, false)
  assert.deepEqual(scheduleWhenFields(false, "09:00", "daily"), { at: "09:00", days: "daily" })
})

test("a list row and a detail record both read as trigger-only", () => {
  assert.equal(isTriggerOnly({ trigger_only: true }), true)
  assert.equal(isTriggerOnly({ when: { trigger_only: true } }), true)
  assert.equal(isTriggerOnly({ when: {} }), false)
  assert.equal(isTriggerOnly(null), false)
})

test("a move's disable of a trigger-only schedule keeps it trigger-only", () => {
  const body = recordBody({ id: "s1", title: "Hook", when: { trigger_only: true }, task: { assistant: "claude", instructions: "go" } }, "p1", false)
  assert.equal(body.trigger_only, true)
  assert.equal("at" in body || "days" in body || "on" in body, false)
})
