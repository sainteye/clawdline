import type { DecisionPage, ProposalPage } from "./api.js"
import type { Reading, Source } from "./freshness.js"

/*
 * The third block of the "now" page — proposals to confirm, questions to
 * answer — joined from its two routes. Kept apart from the component, and
 * importing types only, so that `node --test` loads it as it is:
 *   node --test --experimental-strip-types "src/pages/now/waiting.test.ts"
 */

export type Block<T> = { reading: Reading; rows: T[] }

/** One thing that is waiting for the person, from either of the two routes. */
export interface WaitingRow {
  kind: "proposal" | "decision"
  id: string
  title: string
  age: number
}

export type Answered<T> = { ok: true; page: T } | { ok: false; why: string }

/**
 * The two halves of "waiting for you", joined.
 *
 * Neither half standing in for the other: with one refused the block keeps
 * the rows it has and says, in the failed half's own words, that it is short.
 * That is `stale` — read, and known to be less than what there is — and it is
 * the reason this does not simply drop the refused half and show a smaller
 * number as if it were the answer.
 */
export function joinWaiting(
  proposals: Answered<ProposalPage>,
  decisions: Answered<DecisionPage>,
): Block<WaitingRow> {
  const now = Math.floor(Date.now() / 1000)
  const rows: WaitingRow[] = []
  if (proposals.ok) {
    for (const p of proposals.page.rows) {
      rows.push({ kind: "proposal", id: p.id, title: p.title, age: now - p.created_at })
    }
  }
  if (decisions.ok) {
    for (const d of decisions.page.rows) {
      rows.push({ kind: "decision", id: d.id, title: d.question, age: now - d.created_at })
    }
  }
  rows.sort((a, b) => b.age - a.age)
  if (!proposals.ok && !decisions.ok) {
    return { reading: { read: false, failure: proposals.why }, rows: [] }
  }
  if (!proposals.ok || !decisions.ok) {
    return {
      reading: { read: true, rows: rows.length, source: { observed_at: now, provenance: "work", freshness: "stale" } },
      rows,
    }
  }
  // Both answered. The block is only as good as its weaker source. The
  // proposals half has no source of its own: whole, it adds no doubt; cut at a
  // page, it is short, and says so.
  const a: Source | undefined = proposals.page.truncated
    ? { observed_at: now, provenance: "work", freshness: "stale" }
    : undefined
  return { reading: { read: true, rows: rows.length, source: worse(a, decisions.page.source) }, rows }
}

/** The weaker of two readings, in the order a reader should be warned. */
export function worse(a: Source | undefined, b: Source | undefined): Source | undefined {
  if (!a) return b
  if (!b) return a
  const rank = { missing: 3, stale: 2, unverified: 1, current: 0 }
  return rank[b.freshness] > rank[a.freshness] ? b : a
}
