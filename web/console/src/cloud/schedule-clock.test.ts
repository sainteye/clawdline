import { deepEqual, equal } from "node:assert/strict"
import { test } from "node:test"
// @ts-expect-error -- Node runs the source TypeScript test directly.
import { clockExecutor, clockRule, clockScheduleRecord } from "./schedule-clock.ts"

const form = {
  title: "Example", assistant: "codex", model: "", place_id: "project", instructions: "Local task",
  enabled: true, close_tab: "on_success", notify_on_failure: true, timeout_minutes: 30,
  at: "10:00", days: ["tue", "thu"], time_zone: "Asia/Taipei", catch_up_hours: 6,
}

test("Cloud keeps the time and only a trigger-only task reaches the machine", () => {
  deepEqual(clockRule(form), {
    at: "10:00", days: ["tue", "thu"], time_zone: "Asia/Taipei", catch_up_hours: 6,
    every_days: undefined, start: undefined, on: undefined,
  })
  const sent = clockExecutor(form)
  equal(sent.trigger_only, true)
  equal(sent.at, undefined)
  equal(sent.days, undefined)
  equal(sent.instructions, "Local task")
  equal(form.at, "10:00")
})

test("A Cloud-held rule reopens the timed editor with the next occurrence", () => {
  const record = { id: "schedule", when: { trigger_only: true }, trigger_only: true }
  const clock = {
    schedule_id: "schedule", hook_id: "hook", revision: 2, enabled: true,
    rule: clockRule(form), next_at: "2026-11-24T02:00:00.000Z", last_due_at: null,
    last_delivery_id: null, last_state: null,
  }
  const display = clockScheduleRecord(record, clock)
  equal(display.trigger_only, false)
  equal(display.when?.at, "10:00")
  equal(display.next_fire, Date.parse(clock.next_at) / 1000)
  equal(record.trigger_only, true)
})
