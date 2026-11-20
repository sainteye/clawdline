import type { ScheduleBody, ScheduleRecord } from "../legacy/schedules-bridge.js"

export type ScheduleClockRule = {
  time_zone: string
  catch_up_hours: number
  at: string
  days?: string | string[]
  on?: string
  every_days?: number
  start?: string
}

export type ScheduleClock = {
  schedule_id: string
  hook_id: string
  revision: number
  rule: ScheduleClockRule
  enabled: boolean
  next_at: string | null
  last_due_at: string | null
  last_delivery_id: string | null
  last_state: "queued" | "expired" | "blocked" | null
  delivery_state?: string
  outcome?: string
}

export function clockRule(payload: ScheduleBody): ScheduleClockRule {
  const { at, days, on, every_days, start, time_zone, catch_up_hours } = payload
  if (!at || !time_zone) throw new Error("invalid_schedule_clock")
  return { at, time_zone, catch_up_hours: catch_up_hours ?? 0, days, on, every_days, start }
}

/** Only the task is sent to the machine. This record can never fire on its clock. */
export function clockExecutor(payload: ScheduleBody): ScheduleBody {
  const executor = { ...payload, trigger_only: true as const }
  delete executor.at
  delete executor.days
  delete executor.on
  delete executor.every_days
  delete executor.start
  return executor
}

export function clockScheduleRecord(record: ScheduleRecord, clock: ScheduleClock | null): ScheduleRecord {
  if (!clock) return record
  return {
    ...record, when: clock.rule, time_zone: clock.rule.time_zone, catch_up_hours: clock.rule.catch_up_hours,
    trigger_only: false, once: !!clock.rule.on, next_fire: clock.next_at ? Date.parse(clock.next_at) / 1000 : undefined,
  }
}
