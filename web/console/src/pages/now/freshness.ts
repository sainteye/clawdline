/*
 * What one block of the "now" page may print, given what its source answered.
 *
 * This is the whole rule of the page in one function, kept out of the
 * component so that it can be read and tested without a browser: **a source
 * that was not read has no count**, and the number `0` is only ever printed
 * for a reading that happened and found nothing.
 *
 * The four words come from the daemon (`SourceFreshness`, api/v1/
 * coordination.schema.json) and they are four different facts, not four
 * degrees of one:
 *
 *   current     read in full just now.
 *   stale       read, and known to be short — there is more than this.
 *   missing     could not be read at all.
 *   unverified  read in full, and a fact in it rests on something the daemon
 *               cannot confirm still holds.
 *
 * `unverified` is the one this page was built around. Until it existed the
 * daemon had three words and all three answered the same question — did the
 * reading happen — so a ledger that read its own records perfectly and could
 * not say whether the work had landed answered `current`, and every screen
 * drew that as settled. A count under `unverified` is printed and is never
 * printed alone: the sentence saying what is unconfirmed goes with it, every
 * time.
 *
 * **It answers in word keys, not in words**, so that nothing here imports the
 * catalog and `node --test` loads it as it is:
 *   node --test --experimental-strip-types "src/pages/now/freshness.test.ts"
 * `count: null` is the whole of the rule and cannot be turned into "0" by a
 * later edit, because there is no number in it to reach for.
 */

/** The daemon's four words. A block never invents a fifth. */
export type Freshness = "current" | "stale" | "missing" | "unverified"

/** `BearingsSource` on the wire, as every one of the three routes sends it. */
export interface Source {
  observed_at: number
  provenance: string
  freshness: Freshness
}

/** Which block is asking, which chooses the sentence an `unverified` carries. */
export type Block = "doing" | "owed" | "waiting"

/**
 * What a block knows: the answer it got, or the sentence for the refusal it
 * got instead. `rows` is meaningless while `read` is false and is never read
 * from there.
 */
export interface Reading {
  read: boolean
  /** The named refusal as words, when the read did not happen. */
  failure?: string
  rows?: number
  source?: Source
}

/** What the block draws. `count: null` is "there is no number to show". */
export interface Drawn {
  count: number | null
  /** The word key for the line under the count; null when `said` carries it. */
  line: "freshCurrent" | "freshStale" | "freshMissing" | "freshUnverified" | "unreadable" | null
  /** The word key for what an `unverified` line's hole is filled with. */
  why: "freshWhyTasks" | "freshWhyLandings" | "freshWhyWaiting" | null
  /** A sentence already in words — the refusal's — when there is one. */
  said: string | null
  tone: "plain" | "warn" | "bad"
  /** True only for a reading that happened and whose facts are confirmed. */
  settled: boolean
}

/** The sentence a source's `unverified` carries, per block. */
const WHY = {
  doing: "freshWhyTasks",
  owed: "freshWhyLandings",
  waiting: "freshWhyWaiting",
} as const

/**
 * The one rule, applied.
 *
 * A read that did not happen has no count at any freshness, and a read that
 * happened keeps its count at every freshness but `missing` — which a route
 * cannot answer without having answered, and which therefore appears here
 * only for a source named inside an answer that arrived.
 */
export function draw(block: Block, reading: Reading): Drawn {
  if (!reading.read) {
    return {
      count: null,
      line: reading.failure ? null : "unreadable",
      why: null,
      said: reading.failure ?? null,
      tone: "bad",
      settled: false,
    }
  }
  const rows = reading.rows ?? 0
  const freshness = reading.source?.freshness ?? "unverified"
  switch (freshness) {
    case "current":
      return { count: rows, line: "freshCurrent", why: null, said: null, tone: "plain", settled: true }
    case "stale":
      return { count: rows, line: "freshStale", why: null, said: null, tone: "warn", settled: false }
    case "missing":
      // The source named itself unreadable inside an answer that arrived. The
      // answer's rows are not that source's, so the count goes with it.
      return { count: null, line: "freshMissing", why: null, said: null, tone: "bad", settled: false }
    default:
      return { count: rows, line: "freshUnverified", why: WHY[block], said: null, tone: "warn", settled: false }
  }
}

/**
 * How long something has been going on, in the coarsest unit that is still
 * true. A page read on a phone has one line for this and no room for a second.
 */
export function ageWords(seconds: number | null | undefined, chinese = inChinese()): string {
  const s = Math.max(0, Math.floor(seconds ?? 0))
  const days = Math.floor(s / 86400)
  if (days >= 1) return chinese ? `${days} 天` : `${days}d`
  const hours = Math.floor(s / 3600)
  if (hours >= 1) return chinese ? `${hours} 小時` : `${hours}h`
  const minutes = Math.floor(s / 60)
  if (minutes >= 1) return chinese ? `${minutes} 分鐘` : `${minutes}m`
  return chinese ? `${s} 秒` : `${s}s`
}

/** When a reading was taken, as a clock reads it; "" when it says no time. */
export function readClock(source: Source | undefined, chinese = inChinese()): string {
  if (!source?.observed_at) return ""
  return new Date(source.observed_at * 1000).toLocaleTimeString(chinese ? "zh-TW" : "en", {
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  })
}

/** The page's language, the one thing here that asks the document anything. */
function inChinese(): boolean {
  const lang = (typeof document !== "undefined" ? document.documentElement.lang : "") ||
    (typeof navigator !== "undefined" ? navigator.language : "") || ""
  return lang.toLowerCase().startsWith("zh")
}
