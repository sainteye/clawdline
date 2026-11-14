/** A row's top and bottom, and its message sequence. */
export interface RowBox { top: number; bottom: number; seq: number }

/**
 * The last message the person has seen: a message counts once its end is on
 * screen, so a reply taller than the window is read when scrolled to its end,
 * instead of never.
 */
export function seenThrough(rows: readonly RowBox[], viewTop: number, viewBottom: number): number {
  let seen = 0
  for (const r of rows) if (r.bottom <= viewBottom + 1 && r.bottom >= viewTop) seen = Math.max(seen, r.seq)
  return seen
}
