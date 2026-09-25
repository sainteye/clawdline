/**
 * The `claude_auto_compact_window` field: the context size, in tokens, at which
 * the Claude sessions Clawdline opens compact their history (the broker's
 * compact.go). An empty field is none — the default, and what 0 means in the
 * file — so a person clears the box to stop the experiment.
 *
 * The bounds are the daemon's; it refuses anything outside them by name. They
 * are checked here too so a typo is answered before a write rather than as a
 * failed one.
 */
export const COMPACT_MIN = 50_000
export const COMPACT_MAX = 1_000_000

/** What the field shows for a value the file holds: nothing for none. */
export function compactWindowText(value: number | null | undefined): string {
  return value ? String(value) : ""
}

/**
 * What a field's text asks the file to hold. Separators a person may type —
 * `200,000`, `200 000`, `200_000` — are read through, and `200k` is 200000.
 */
export function compactWindowValue(text: string): { value: number } | { invalid: true } {
  let t = text.trim().toLowerCase().replace(/[\s,_]/g, "")
  if (t === "" || t === "0" || t === "off") return { value: 0 }
  let scale = 1
  if (t.endsWith("k")) {
    scale = 1000
    t = t.slice(0, -1)
  }
  if (!/^\d+(\.\d+)?$/.test(t)) return { invalid: true }
  const value = Number(t) * scale
  if (!Number.isInteger(value) || value < COMPACT_MIN || value > COMPACT_MAX) return { invalid: true }
  return { value }
}
