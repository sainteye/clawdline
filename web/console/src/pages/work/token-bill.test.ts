// A Board card, an item detail and a session detail say what the token ledger counted:
// `node --test --experimental-strip-types web/console/src/pages/work/token-bill.test.ts`.
import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
import test from "node:test"
import type { UsageBill, UsageGap, UsageReason } from "@clawdline/contract"
// @ts-expect-error -- `.ts` paths let Node's strip-types runner execute this test.
import { UsageCache, cardLine, formatCost, formatShare, formatTokens, gapWords, itemAbove, moneyOf, reasonWords, sessionReasonWords } from "./token-bill.ts"
// @ts-expect-error -- `.ts` paths let Node's strip-types runner execute this test.
import { workWordIn } from "./words.ts"

const card = readFileSync(new URL("./WorkV2.tsx", import.meta.url), "utf8")
const bill = readFileSync(new URL("./TokenBill.tsx", import.meta.url), "utf8")
const todos = readFileSync(new URL("../../session/Todos.tsx", import.meta.url), "utf8")
const words = readFileSync(new URL("./words.ts", import.meta.url), "utf8")

const NAMES = ["board", "protocol", "rules", "impl", "delegate", "harness", "talk", "compaction", "other"]

function tokens(total: number, cost: number, known = true, unpriced = 0) {
  return { cache_read: 0, cache_write_1h: 0, cache_write_5m: 0, input: total, output: 0, total, cost, cost_known: known, unpriced }
}

function billOf(parts: Record<string, [number, number]>, shareOf: "cost" | "tokens" = "cost"): UsageBill {
  const total = Object.values(parts).reduce((a, [t, c]) => [a[0] + t, a[1] + c], [0, 0])
  const known = shareOf === "cost"
  return {
    share_of: shareOf,
    total: tokens(total[0], total[1], known, known ? 0 : 1000),
    categories: NAMES.map((name) => {
      const [t, c] = parts[name] ?? [0, 0]
      return { name, tokens: tokens(t, c), upper_bound: name === "rules", share: known ? (total[1] ? c / total[1] : 0) : (total[0] ? t / total[0] : 0) }
    }),
  } as UsageBill
}

test("the card line is the cost and the two largest categories, as the brief's example reads", () => {
  const b = billOf({ impl: [700_000, 4.809], harness: [180_000, 1.2366], rules: [50_000, 0.5], talk: [70_000, 0.3244] })
  assert.equal(cardLine(b), "$6.87 · impl 70% · harness 18%")
})

test("rules leads with ≤ wherever its share is shown, because it is an upper bound", () => {
  const b = billOf({ rules: [700_000, 5], impl: [300_000, 2] })
  assert.equal(cardLine(b), "$7.00 · rules ≤71% · impl 29%")
})

test("a bill whose cost is not whole is named by its tokens, never by the priced part alone", () => {
  const b = billOf({ impl: [900_000, 1], talk: [300_000, 1] }, "tokens")
  assert.equal(cardLine(b), "1.2M tokens · impl 75% · talk 25%")
  assert.equal(moneyOf(b.total, "en"), "$2.00 + 1k unpriced tokens")
  assert.equal(moneyOf(tokens(5000, 0, false, 5000), "zh-Hant"), "5k tokens")
})

test("a bill that counted nothing draws no line, so a card never says $0", () => {
  assert.equal(cardLine(billOf({})), null)
})

test("numbers are as short as still says them", () => {
  assert.equal(formatCost(0.001), "<$0.01")
  assert.equal(formatCost(0), "$0.00")
  assert.equal(formatCost(123.4), "$123")
  assert.equal(formatTokens(812.4), "812")
  assert.equal(formatTokens(34_512), "34.5k")
  assert.equal(formatTokens(200_000), "200k")
  assert.equal(formatTokens(1_000_000), "1M")
  assert.equal(formatShare(0.001), "<1%")
  assert.equal(formatShare(0), "0%")
})

test("each reason a reading is not current has its own words in both languages", () => {
  const reasons: UsageReason[] = ["not_yet_read", "transcript_missing", "transcript_unreadable"]
  for (const lang of ["en", "zh-Hant"] as const) {
    const said = reasons.map((r) => reasonWords(r, lang))
    assert.equal(new Set(said).size, 3, lang)
    const whole = reasons.map((r) => sessionReasonWords({ reason: r }, lang))
    assert.equal(new Set(whole).size, 3, lang)
    for (const s of [...said, ...whole]) assert.doesNotMatch(String(s), /\b0\b|\$0/, lang)
  }
  assert.equal(sessionReasonWords({}, "en"), null)
})

test("a gap names what it is, why, and whether an earlier reading is counted", () => {
  const gap: UsageGap = { kind: "session", id: "3f1c2a9e-0000-4000-8000-000000000001", reason: "transcript_missing", counted: true }
  assert.equal(gapWords(gap, "en"), "Session 3f1c2a9e: its transcript is gone; its last reading is in the totals")
  assert.equal(gapWords({ ...gap, kind: "task", reason: "not_yet_read", counted: false }, "zh-Hant"),
    "任務 3f1c2a9e：還沒讀到；總數裡完全沒有它")
  assert.match(gapWords({ ...gap, kind: "something_new" }, "en"), /^something_new 3f1c2a9e:/)
})

test("an item's calls above 200k add up its owners' and its tasks' sessions", () => {
  // Only what the sum reads; the rest of a session is not its business.
  const session = (calls: number, cost: number, known = true) => ({ calls_above: calls, above: tokens(calls * 1000, cost, known) }) as never
  const got = itemAbove({ sessions: [session(2, 1.5)], tasks: [{ sessions: [session(1, 0.5), session(0, 0)] }] } as never)
  assert.equal(got.calls, 3)
  assert.equal(got.above.cost, 2)
  assert.equal(got.above.cost_known, true)
  assert.equal(itemAbove({ sessions: [session(1, 1, false)], tasks: [] } as never).above.cost_known, false)
})

test("one reading per item is shared until it is a minute old, and a failure is not kept", async () => {
  let now = 0
  const asked: string[] = []
  let fail = false
  const cache = new UsageCache((id: string) => {
    asked.push(id)
    return fail ? Promise.reject(new Error("no")) : Promise.resolve(id)
  }, () => now, 60_000, 2)
  await cache.get("a"); await cache.get("a")
  assert.deepEqual(asked, ["a"])
  now = 60_000
  await cache.get("a")
  assert.deepEqual(asked, ["a", "a"])
  await cache.get("a", true)
  assert.equal(asked.length, 3)
  await cache.get("b"); await cache.get("c")
  assert.equal(cache.size, 2)
  fail = true
  await assert.rejects(cache.get("d"))
  assert.equal(cache.size, 1, "the failed read is not kept")
  fail = false
  assert.equal(await cache.get("d"), "d")
})

test("every word the bills say is in both catalogs", () => {
  const keys = (lang: string) => {
    const start = words.indexOf(lang === "en" ? "  en: {" : '  "zh-Hant": {')
    const body = words.slice(start, words.indexOf("\n  },", start))
    return [...body.matchAll(/^\s+(usage\w+):/gm)].map((m) => m[1]).sort()
  }
  assert.ok(keys("en").length > 30)
  assert.deepEqual(keys("zh-Hant"), keys("en"))
  for (const k of keys("en")) assert.ok(workWordIn("zh-Hant", k as never).length > 0, k)
})

test("the card, the item detail and the session detail each draw the bill", () => {
  assert.match(card, /<ItemUsageCard itemId=\{item\.id\} version=\{item\.version\} \/>/)
  assert.match(todos, /<ItemUsageDetail itemId=\{item\.id\} version=\{item\.version\} \/>/)
  assert.match(todos, /\{row\.sessionId && <SessionUsage conversation=\{row\.sessionId\} \/>\}/)
  // The card asks once it is on screen, through the shared cache, and a session asks when its fold opens.
  assert.match(bill, /const \[ref, seen\] = useSeen<HTMLDivElement>\(\)\n\s+const \{ reading \} = useUsage\(items, itemId, seen, version\)/)
  assert.match(bill, /useUsage\(sessions, conversation, open\)/)
  assert.match(bill, /usageRulesWhy/)
  assert.match(bill, /<Composition composition=\{usage\.composition\} \/>/)
  assert.match(bill, /<Gaps gaps=\{usage\.gaps\} \/>/)
})
