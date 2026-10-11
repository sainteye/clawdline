/*
 * When the opened Session's row has not arrived, and the pane says so.
 *
 * Opening a Session on Cloud needs two things: the conversation, which the
 * machine answers in about 130 ms, and the Session's own rich row, which the
 * machine publishes on its status cadence and the page may not hold yet. Until
 * the row is there the pane has no name, no state and no composer, so it says
 * "Reading this Session's conversation…" — and said it for as long as the row
 * took. Measured on 2026-10-11: one row re-read from the relay's retained
 * channel took 19.6 seconds, and nothing on screen changed in that time.
 *
 * The reader now asks the machine for the row instead of waiting for it
 * (`relay-reader.ts`, `ROW_RESTATE_MS`), which answered in one second. This is
 * the other half: a row that still has not come is news, not a longer wait, and
 * the pane says which machine has not sent it and offers the press that asks
 * again.
 */

/**
 * How long the opened Session's row may be missing before the pane says so.
 *
 * Longer than the ask and its answer (about one second, twice over), so a row
 * that is on its way is never called absent; short enough that a person who is
 * looking at a blank pane is told inside one breath, as the transcript's own
 * quiet stretch tells them (`session/transcript-trouble.ts`).
 */
export const OPENED_ROW_QUIET_MS = 8_000

/**
 * - `reading`: the row may still be on its way; the pane keeps its reading line.
 * - `absent`: say the machine has not sent it, with the press that asks again.
 */
export type OpenedRowShow = "reading" | "absent"

export function openedRowShow(waitedMs: number): OpenedRowShow {
  // A stretch that cannot be read (`NaN`) is not a stretch that is over.
  return waitedMs >= OPENED_ROW_QUIET_MS ? "absent" : "reading"
}
