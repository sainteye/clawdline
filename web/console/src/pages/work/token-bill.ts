import type { UsageBill, UsageCategory, UsageGap, UsageItem, UsageReason, UsageSession, UsageTokens } from "@clawdline/contract"
// @ts-expect-error -- a `.ts` path, so Node's strip-types runner can load this file in token-bill.test.ts.
import { workWordIn, type WorkWord } from "./words.ts"

/*
 * What the token ledger answers, as this console says it (docs/token-ledger.md
 * "What a person and a session see"). Nothing here computes a bill: the daemon
 * divided it, and this file only chooses the words. Unknown is never zero — a
 * bill with nothing counted draws no number, and every reason a reading is not
 * current has its own sentence.
 */

type Lang = "en" | "zh-Hant"

/** US dollars at list price, as short as still says the amount. */
export function formatCost(cost: number): string {
  if (cost > 0 && cost < 0.005) return "<$0.01"
  if (cost >= 100) return `$${Math.round(cost)}`
  return `$${cost.toFixed(2)}`
}

/** A token count as 812, 34.5k, 1.2M. */
export function formatTokens(n: number): string {
  const v = Math.round(n)
  if (v < 1000) return String(v)
  if (v < 1_000_000) return `${trim(v / 1000)}k`
  return `${trim(v / 1_000_000)}M`
}

function trim(v: number): string {
  return v >= 100 ? String(Math.round(v)) : v.toFixed(1).replace(/\.0$/, "")
}

/** A share (0…1) as a whole percent, and "<1%" for a part too small to round up. */
export function formatShare(share: number): string {
  if (share > 0 && share < 0.005) return "<1%"
  return `${Math.round(share * 100)}%`
}

/** The money of some tokens: the cost when it is whole, the priced part and the rest otherwise. */
export function moneyOf(t: UsageTokens, lang: Lang): string {
  if (t.cost_known) return formatCost(t.cost)
  if (t.cost <= 0) return workWordIn(lang, "usageTokens", { n: formatTokens(t.unpriced || t.total) })
  return workWordIn(lang, "usageCostPartial", { cost: formatCost(t.cost), n: formatTokens(t.unpriced) })
}

/** Whether a bill counted anything. A bill that counted nothing is not a bill of $0. */
export function billCounted(bill: UsageBill): boolean {
  return bill.total.total > 0
}

/** The categories that spent anything, largest share first. */
export function largestCategories(bill: UsageBill, n: number): UsageCategory[] {
  return bill.categories.filter((c) => c.share > 0).sort((a, b) => b.share - a.share).slice(0, n)
}

/** One category's share, "≤" in front when the category is an upper bound. */
export function shareWords(c: UsageCategory): string {
  return `${c.upper_bound ? "≤" : ""}${formatShare(c.share)}`
}

/**
 * The card's one line: the total cost (its tokens when the cost is not whole),
 * then the largest two categories, "$6.87 · impl 70% · harness 18%". Null when
 * the bill counted nothing: a card whose bill is not readable says nothing.
 */
export function cardLine(bill: UsageBill): string | null {
  if (!billCounted(bill)) return null
  const head = bill.share_of === "cost" && bill.total.cost_known
    ? formatCost(bill.total.cost)
    : `${formatTokens(bill.total.total)} tokens`
  return [head, ...largestCategories(bill, 2).map((c) => `${c.name} ${shareWords(c)}`)].join(" · ")
}

const REASON: Record<UsageReason, WorkWord> = {
  not_yet_read: "usageReasonNotYetRead",
  transcript_missing: "usageReasonTranscriptMissing",
  transcript_unreadable: "usageReasonTranscriptUnreadable",
}

const SESSION_REASON: Record<UsageReason, WorkWord> = {
  not_yet_read: "usageSessionNotYetRead",
  transcript_missing: "usageSessionTranscriptMissing",
  transcript_unreadable: "usageSessionTranscriptUnreadable",
}

const KIND: Record<string, WorkWord> = {
  session: "usageKindSession",
  subagent: "usageKindSubagent",
  task: "usageKindTask",
  root_assignment: "usageKindRootAssignment",
}

/** Why a reading is not current, in words; a reason this build does not know is named as it came. */
export function reasonWords(reason: UsageReason, lang: Lang): string {
  const key = REASON[reason]
  return key ? workWordIn(lang, key) : reason
}

/** What a whole session's answer says when it has no current reading, or null when it has one. */
export function sessionReasonWords(session: Pick<UsageSession, "reason">, lang: Lang): string | null {
  if (!session.reason) return null
  const key = SESSION_REASON[session.reason]
  return key ? workWordIn(lang, key) : session.reason
}

/** A long id as its first eight characters, which is how the rest of the console names one. */
export function shortId(id: string): string {
  return id.length > 12 ? id.slice(0, 8) : id
}

/** One gap as a sentence: what it is, why it is not current, and whether an earlier reading is counted. */
export function gapWords(gap: UsageGap, lang: Lang): string {
  const kindKey = KIND[gap.kind]
  const kind = kindKey ? workWordIn(lang, kindKey) : workWordIn(lang, "usageKindOther", { kind: gap.kind })
  const why = reasonWords(gap.reason, lang)
  const counted = workWordIn(lang, gap.counted ? "usageGapCounted" : "usageGapNotCounted")
  return workWordIn(lang, "usageGapLine", { kind, id: shortId(gap.id), reason: why, counted })
}

/**
 * The calls above 200k context of every session an item's bill counted — its
 * owners' and its tasks'. The item's answer carries only the sessions', so it
 * is added up here, from the same rows the daemon counted.
 */
export function itemAbove(item: UsageItem): { calls: number; above: UsageTokens } {
  const sessions = [...item.sessions, ...item.tasks.flatMap((t) => t.sessions)]
  const above: UsageTokens = {
    cache_read: 0, cache_write_1h: 0, cache_write_5m: 0, cost: 0, cost_known: true,
    input: 0, output: 0, total: 0, unpriced: 0,
  }
  let calls = 0
  for (const s of sessions) {
    calls += s.calls_above
    for (const k of ["cache_read", "cache_write_1h", "cache_write_5m", "cost", "input", "output", "total", "unpriced"] as const) {
      above[k] += s.above[k]
    }
    above.cost_known &&= s.above.cost_known
  }
  return { calls, above }
}

/** How long one reading of a bill is kept before the next card that needs it asks again. */
export const USAGE_FRESH_MS = 60_000
/** How many items' readings are kept at most; the oldest goes first. */
export const USAGE_KEPT = 200

/**
 * One reading per item, shared by every card and detail that shows it: a Board
 * that re-renders every thirty seconds asks the daemon at most once a minute
 * per item, and two views of one item never ask twice. A failed read is not
 * kept, so the next view asks again.
 */
export class UsageCache<T> {
  private kept = new Map<string, { at: number; answer: Promise<T> }>()
  private read: (id: string) => Promise<T>
  private now: () => number
  private freshMs: number
  private max: number

  constructor(read: (id: string) => Promise<T>, now: () => number = Date.now, freshMs = USAGE_FRESH_MS, max = USAGE_KEPT) {
    this.read = read
    this.now = now
    this.freshMs = freshMs
    this.max = max
  }

  get(id: string, fresh = false): Promise<T> {
    const had = this.kept.get(id)
    if (had && !fresh && this.now() - had.at < this.freshMs) return had.answer
    const answer = this.read(id)
    this.kept.delete(id)
    this.kept.set(id, { at: this.now(), answer })
    while (this.kept.size > this.max) this.kept.delete(this.kept.keys().next().value as string)
    answer.catch(() => { if (this.kept.get(id)?.answer === answer) this.kept.delete(id) })
    return answer
  }

  get size(): number {
    return this.kept.size
  }
}
