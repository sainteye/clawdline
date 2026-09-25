/**
 * The order the Board draws its items in.
 *
 * The daemon answers newest-updated first, so an item somebody just edited
 * jumps to the top of its region on the next answer — out from under the eye
 * of the person who edited it. While the same view stays open, an item already
 * on screen keeps the place it had; only items that were not on screen yet
 * take the daemon's order, above the ones that were. A fresh arrangement (the
 * view reopened, its filter changed, or Refresh pressed) takes the daemon's
 * order whole.
 */
export function arrangeWorkItems<T extends { id: string }>(rows: readonly T[], kept: ReadonlyMap<string, number> | null): T[] {
  if (!kept) return [...rows]
  const fresh = rows.filter((row) => !kept.has(row.id))
  const known = rows.filter((row) => kept.has(row.id)).sort((a, b) => kept.get(a.id)! - kept.get(b.id)!)
  return [...fresh, ...known]
}

/** The places an arrangement gives, for the next answer to keep. */
export function workItemPlaces(rows: readonly { id: string }[]): Map<string, number> {
  return new Map(rows.map((row, index) => [row.id, index]))
}
