/*
 * When a schedule's history sheet opens, and what it may show.
 *
 * The sheet carries a Run now button, so what is on it must be the schedule
 * whose row was pressed and nothing else. Nothing here touches the DOM, so
 * `node --test` loads it as it is:
 * `node --test web/console/src/schedule-history-open.test.ts`.
 */

/** The sheet as `ScheduleHistory` holds it. */
export interface HistorySheet {
  shown: boolean
  scheduleId: string | null
  /** A resume or a Run now is in flight; the sheet cannot be closed either. */
  busy: boolean
}

/**
 * Whether pressing the row for `id` (re)opens the sheet. A sheet already
 * showing another schedule is replaced rather than kept: it used to be kept,
 * and its Run now then ran that other schedule.
 */
export function shouldOpenHistory(sheet: HistorySheet, id: string | undefined): boolean {
  if (!id) return false
  if (!sheet.shown) return true
  return !sheet.busy && sheet.scheduleId !== id
}

/** The record an answer carries for `id`, or null when it carries none or another schedule's. */
export function historyRecordFor<R extends { id?: string }>(
  answer: { schedule?: R } | null | undefined,
  id: string,
): R | null {
  const record = answer && answer.schedule
  if (!record || record.id !== id) return null
  return record
}
