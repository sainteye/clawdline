/*
 * A schedule's `when` as the page reads and writes it: a time with its days, or
 * no time at all — a trigger-only schedule the clock never fires, run only by
 * Run now or by its webhook (`ScheduleRequest.trigger_only`).
 *
 * Nothing here touches the DOM, so `node --test` loads it as it is:
 * `node --test web/console/src/schedule-when.test.ts`.
 */

/** What a list row, a record or its `when` says about being trigger-only. */
export interface TriggerOnlyShape {
  trigger_only?: boolean
  when?: { trigger_only?: boolean }
}

/** Whether a list row or a detail record is a schedule with no time. */
export function isTriggerOnly(schedule: TriggerOnlyShape | null | undefined): boolean {
  return !!schedule && (schedule.trigger_only === true || schedule.when?.trigger_only === true)
}

/**
 * The `when` fields of a create or save body. A trigger-only schedule sends
 * `trigger_only: true` and none of `at`, `days` and `on`; a timed one sends its
 * time and days, which is also how a save turns a trigger-only schedule back.
 */
export function scheduleWhenFields(
  triggerOnly: boolean,
  at: string,
  days: string | string[],
): { trigger_only: true } | { at: string; days: string | string[] } {
  return triggerOnly ? { trigger_only: true } : { at, days }
}

/** The list row's words for when it runs next. */
export interface NextLineWords {
  /** "Next" — followed by the relative time. */
  next: string
  disabled: string
  noNext: string
  /** What a trigger-only schedule says in place of a next run. */
  triggerOnly: string
}

/**
 * The list row's next-run line: a trigger-only schedule says how it runs
 * instead of a time it never has; otherwise the next fire (`relative`, already
 * worded), or that none is scheduled.
 */
export function scheduleNextLine(
  row: { enabled?: boolean; next_fire?: number; trigger_only?: boolean },
  relative: string,
  words: NextLineWords,
): string {
  if (isTriggerOnly(row)) return row.enabled ? words.triggerOnly : words.disabled + " · " + words.triggerOnly
  if (!row.next_fire) return words.noNext
  return (row.enabled ? words.next + " " : words.disabled + " · next ") + relative
}
