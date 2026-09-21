/**
 * A newly answered, non-empty proposal count gets one visible arrival.
 * Keeping the previous count outside the DOM lets a person close the fold
 * without the periodic board refresh immediately opening it again.
 */
export function proposalFoldShouldOpen(previous: number | null, total: number): boolean {
  return total > 0 && previous !== total
}
