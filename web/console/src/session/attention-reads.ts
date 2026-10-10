/*
 * When an open Session's attention notes are read, and what the closed head
 * draws in between.
 *
 * The row carries `attention_count`: how many notes for this conversation are
 * unresolved, published with the rest of the row on `/v1/events`. A Session
 * with none of them — the ordinary Session — has nothing for this panel to
 * show, so nothing is read for it at all; the head's dot and number come from
 * the count. Until 2026-10-10 the panel read the notes when the page opened
 * and every minute after that (`TODO_SAFETY_MS`): on a phone that was 3 of an
 * open Session's 32 requests in three minutes, each of them an empty page
 * where the row had already said zero.
 *
 * A Session that does have notes still reads them as the page opens, so that
 * tapping the head shows the words rather than a wait, and while the panel is
 * open it keeps the safety read. What it no longer has is a clock of its own
 * while the panel is closed: the count moves on the stream, and a move is what
 * asks for the page again (`countMoveNeedsRead`).
 *
 * A row with no count — a daemon from before the field, or a conversation
 * whose identity could not be established — has nothing for the head to draw
 * from, so it keeps what it had: a read when the page opens and another every
 * `TODO_REFRESH_MS` while the page is visible. The field is the feature test;
 * no version is asked for.
 *
 * Nothing here is imported at run time, so `node --test` loads this file as it
 * is: the lane is named rather than measured, and `Interventions.tsx` spends
 * it in `todo-refresh.ts`'s own milliseconds.
 */

/**
 * Which clock, if any, the panel reads on while the page is visible.
 * `safety` is `TODO_SAFETY_MS`, `refresh` is `TODO_REFRESH_MS`
 * (`todo-refresh.ts`), and `none` is no clock at all.
 */
export type AttentionLane = "none" | "safety" | "refresh"

export interface AttentionReads {
  /** The notes themselves are needed: read them when this panel has no page. */
  page: boolean
  lane: AttentionLane
}

/**
 * What an open Session's attention panel reads.
 *
 * `counted` is whether the row carries `attention_count` and `count` is that
 * number; `expanded` is whether the panel is open, where the notes' own words
 * are on screen and the count cannot stand in for them.
 */
export function attentionReads(o: { counted: boolean; expanded: boolean; count: number | null }): AttentionReads {
  if (!o.counted) return { page: true, lane: "refresh" }
  if (o.expanded) return { page: true, lane: "safety" }
  return { page: (o.count ?? 0) > 0, lane: "none" }
}

/**
 * Whether a moved `attention_count` has to be read.
 *
 * The move is the head's own news: the new number came with the row and is
 * drawn from it, so a closed panel that has read nothing has nothing to ask
 * for. A panel that is open is showing the notes themselves, and one that read
 * a page earlier is still drawing that page's number, so both would otherwise
 * keep an account the stream has already moved past.
 */
export function countMoveNeedsRead(o: { expanded: boolean; hasPage: boolean }): boolean {
  return o.expanded || o.hasPage
}

/**
 * How many unresolved notes the head draws, or null for "nobody has said yet",
 * which draws neither a dot nor a count.
 *
 * A page this panel read wins over the row's count: it is what the body is
 * showing, and after the person resolves a note it is right a scan earlier than
 * the row is.
 */
export function unresolvedNoteCount(read: number | null, counted: number | null): number | null {
  return read !== null ? read : counted
}
