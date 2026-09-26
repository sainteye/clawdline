import type { UsageCompactionComparison, Verification } from "@clawdline/contract"
// @ts-expect-error -- a `.ts` path, so Node's strip-types runner can load this file in model.test.ts.
import { verifyWordIn, type Language } from "./words.ts"

/**
 * What the 驗收 page decides without drawing anything, so it can be tested
 * without a browser: which rows are listed, how long is left, and what each
 * cell of the compaction comparison says.
 */

/** Open records soonest first, then the closed ones latest first. */
export function splitRows(rows: readonly Verification[]): { open: Verification[]; closed: Verification[] } {
  const open = rows.filter((r) => r.status === "open").sort((a, b) => a.due_at - b.due_at)
  const closed = rows.filter((r) => r.status !== "open").sort((a, b) => b.closed_at - a.closed_at)
  return { open, closed }
}

/**
 * How long until a record is due, in words: "due in 6d 21h" or "overdue by
 * 2h 5m". `now` is the daemon's clock (the list's `at`, advanced by the time
 * the page has held it), so a phone whose clock is off counts the same.
 */
export function dueWords(lang: Language, due: number, now: number): { text: string; overdue: boolean } {
  const left = due - now
  const span = spanWords(lang, Math.abs(left))
  return left >= 0
    ? { text: verifyWordIn(lang, "dueIn", { left: span }), overdue: false }
    : { text: verifyWordIn(lang, "overdue", { left: span }), overdue: true }
}

function spanWords(lang: Language, seconds: number): string {
  const d = Math.floor(seconds / 86_400)
  const h = Math.floor((seconds % 86_400) / 3_600)
  const m = Math.floor((seconds % 3_600) / 60)
  if (d > 0) return verifyWordIn(lang, "days", { d, h })
  if (h > 0) return verifyWordIn(lang, "hours", { h, m })
  return verifyWordIn(lang, "minutes", { m })
}

/** How many criteria are in each state. */
export function tally(row: Pick<Verification, "criteria">): { passed: number; failed: number; unset: number } {
  const out = { passed: 0, failed: 0, unset: 0 }
  for (const c of row.criteria) out[c.state]++
  return out
}

/** One row of the comparison table, every cell already a string. */
export interface ComparisonRow {
  group: string
  cells: string[]
}

/**
 * The compaction comparison as the table `clawdline usage --compare-compaction`
 * prints (cmd/clawdline/usage.go `writeCompactionComparison`), cell for cell,
 * so the page and the terminal can be read against each other. A share the
 * daemon withheld because the group is too small is "—", never 0%.
 */
export function comparisonRows(lang: Language, c: UsageCompactionComparison): ComparisonRow[] {
  return c.groups.map((g) => {
    let cost = "—"
    if (g.read_tasks > 0) cost = "$" + g.cost_median_per_task.toFixed(2) + (g.cost_known ? "" : "+")
    return {
      group: g.group === "none" ? verifyWordIn(lang, "groupNone") : g.group,
      cells: [
        String(g.sessions),
        String(g.tasks),
        cost,
        g.calls_per_task.toFixed(1),
        g.compactions_per_task.toFixed(2),
        percent(g.above_200k_share, g.too_few),
        percent(g.success_rate, g.too_few),
        String(g.stalled),
        String(g.respawns),
      ],
    }
  })
}

function percent(v: number | null, tooFew: boolean): string {
  if (tooFew || v === null) return "—"
  return (v * 100).toFixed(0) + "%"
}

/** The sentences under the table, in the order the terminal prints them. */
export function comparisonNotes(lang: Language, c: UsageCompactionComparison): string[] {
  const out: string[] = []
  for (const g of c.groups) {
    const group = g.group === "none" ? verifyWordIn(lang, "groupNone") : g.group
    if (g.too_few) out.push(verifyWordIn(lang, "tooFew", { group, n: g.tasks, min: c.min_tasks }))
    if (g.running > 0 || g.read_tasks < g.tasks) {
      out.push(verifyWordIn(lang, "stillRunning", { group, running: g.running, unread: g.tasks - g.read_tasks }))
    }
  }
  if (c.excluded > 0) out.push(verifyWordIn(lang, "excluded", { n: c.excluded }))
  if (c.truncated) out.push(verifyWordIn(lang, "truncated"))
  for (const m of c.not_recorded) out.push(m.name + " — " + m.why)
  return out
}
