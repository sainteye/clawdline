// The one rule for "which listed model is the one this session runs", shared
// by the session info panel and the status line. They used two before: the
// panel matched exactly or by a date suffix, the status line by any prefix,
// so "claude-opus-4" lit up for a session on "claude-opus-4-1".

/**
 * Whether `current` (what the session reports it runs) is the listed `id`:
 * the same word, the same word with a release date (`-20250805`), or the same
 * word with a bracketed context variant (`[1m]`). A shorter version never
 * matches a longer one.
 */
export function isCurrentModel(current: string | undefined, id: string | undefined): boolean {
  const c = (current ?? "").trim(), m = (id ?? "").trim()
  if (!c || !m) return false
  if (c === m) return true
  const plain = c.replace(/\[[^\]]*\]$/, "")
  if (plain === m) return true
  return plain.startsWith(m + "-") && /^\d{8}$/.test(plain.slice(m.length + 1))
}

/** The listed row the session runs, preferring an exact word over a dated one. */
export function currentModelRow<T>(current: string | undefined, rows: readonly T[], idOf: (row: T) => string): T | undefined {
  return rows.find(r => (current ?? "").trim() === idOf(r)) ?? rows.find(r => isCurrentModel(current, idOf(r)))
}
