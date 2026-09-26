// The 驗收 page's decisions, without a browser:
// `node --test --experimental-strip-types web/console/src/pages/verify/model.test.ts`.
import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
import test from "node:test"
import type { UsageCompactionComparison, UsageCompactionGroup, Verification } from "@clawdline/contract"
// @ts-expect-error -- `.ts` paths let Node's strip-types runner execute this test.
import { comparisonNotes, comparisonRows, dueWords, splitRows, tally } from "./model.ts"
// @ts-expect-error -- `.ts` paths let Node's strip-types runner execute this test.
import { verifyWordIn, type VerifyWord } from "./words.ts"

function row(id: string, fields: Partial<Verification> = {}): Verification {
  return {
    id, title: id, why: "", started_at: 0, due_at: 0, criteria: [], notes: [], schedule_id: "", seed: "",
    status: "open", close_reason: "", closed_at: 0, created_at: 0, updated_at: 0, ...fields,
  }
}

function group(fields: Partial<UsageCompactionGroup>): UsageCompactionGroup {
  return {
    group: "300000", window: 300000, sessions: 5, tasks: 5, read_tasks: 5, running: 0, ended: 5, success: 5,
    failure: 0, cancelled: 0, timeout: 0, stalled: 0, lost: 0, respawns: 0, success_rate: 1, failure_rate: 0,
    timeout_rate: 0, stalled_rate: 0, too_few: false, cost_total: 10, cost_median_per_task: 1.5, cost_known: true,
    calls_per_task: 12.345, compactions_per_task: 0.4, above_200k_share: 0.25, peak_context_max: 0,
    peak_context_median: 0, ...fields,
  }
}

test("the list shows the open records soonest first, and keeps the closed ones apart", () => {
  const { open, closed } = splitRows([
    row("late", { due_at: 900 }),
    row("done-early", { status: "accepted", closed_at: 100 }),
    row("soon", { due_at: 200 }),
    row("done-late", { status: "rejected", closed_at: 300 }),
  ])
  assert.deepEqual(open.map((r: Verification) => r.id), ["soon", "late"])
  assert.deepEqual(closed.map((r: Verification) => r.id), ["done-late", "done-early"])
})

test("the countdown says how long is left, or how long ago it was due, in both languages", () => {
  const now = 1_790_000_000
  assert.deepEqual(dueWords("en", now + 6 * 86_400 + 21 * 3_600 + 59, now), { text: "due in 6d 21h", overdue: false })
  assert.deepEqual(dueWords("zh-Hant", now + 6 * 86_400 + 21 * 3_600, now), { text: "還有 6 天 21 小時到期", overdue: false })
  assert.deepEqual(dueWords("en", now + 2 * 3_600 + 5 * 60, now), { text: "due in 2h 5m", overdue: false })
  assert.deepEqual(dueWords("en", now + 90, now), { text: "due in 1m", overdue: false })
  assert.deepEqual(dueWords("en", now - 2 * 3_600 - 5 * 60, now), { text: "overdue by 2h 5m", overdue: true })
  assert.deepEqual(dueWords("zh-Hant", now - 86_400, now), { text: "已逾期 1 天 0 小時", overdue: true })
})

test("criteria are counted by the state each is in", () => {
  assert.deepEqual(tally(row("r", { criteria: [
    { index: 0, text: "a", state: "passed", updated_at: 1 },
    { index: 1, text: "b", state: "unset", updated_at: 0 },
    { index: 2, text: "c", state: "failed", updated_at: 1 },
    { index: 3, text: "d", state: "passed", updated_at: 1 },
  ] })), { passed: 2, failed: 1, unset: 1 })
})

test("the comparison table says what the terminal says, and a withheld share is never a zero", () => {
  const c: UsageCompactionComparison = {
    since: 1, until: 2, min_tasks: 3, truncated: true, excluded: 2, excluded_truncated: false,
    excluded_tasks: [], not_recorded: [{ name: "peak context", why: "not kept before 2026-09-20" }],
    groups: [
      group({ group: "none", window: 0, tasks: 2, read_tasks: 1, sessions: 2, too_few: true, success_rate: null,
        above_200k_share: null, cost_known: false, cost_median_per_task: 2 }),
      group({ running: 1, read_tasks: 4 }),
      group({ group: "400000", read_tasks: 0 }),
    ],
  }
  const rows = comparisonRows("en", c)
  assert.deepEqual(rows[0], { group: "before setting", cells: ["2", "2", "$2.00+", "12.3", "0.40", "—", "—", "0", "0"] })
  assert.deepEqual(rows[1].cells, ["5", "5", "$1.50", "12.3", "0.40", "25%", "100%", "0", "0"])
  assert.equal(rows[2].cells[2], "—", "no task read, so no cost — not $0.00")
  assert.equal(comparisonRows("zh-Hant", c)[0].group, "before-setting")
  assert.deepEqual(comparisonNotes("en", c), [
    "before setting: 2 tasks, fewer than 3 — too few to compare, so no percentage is shown.",
    "before setting: 0 still running, 1 not read by the ledger yet.",
    "300000: 1 still running, 1 not read by the ledger yet.",
    "400000: 0 still running, 5 not read by the ledger yet.",
    "2 tasks in the range had no known window and are in no group.",
    "The range held more tasks than one answer reads; these are the newest.",
    "peak context — not kept before 2026-09-20",
  ])
})

test("every word the page says exists in both languages, and the drawer says 驗收", () => {
  const source = readFileSync(new URL("../verify.tsx", import.meta.url), "utf8") +
    readFileSync(new URL("./model.ts", import.meta.url), "utf8")
  const used = new Set([...source.matchAll(/verifyWord(?:In)?\((?:lang, |"(?:en|zh-Hant)", )?"(\w+)"/g)].map((m) => m[1]))
  assert.ok(used.size >= 30, "the scan found " + used.size + " words; it has stopped reading the page")
  for (const key of used as Set<VerifyWord>) {
    assert.notEqual(verifyWordIn("en", key), key, key + " has no English")
    assert.notEqual(verifyWordIn("zh-Hant", key), key, key + " has no 繁體中文")
  }
  assert.equal(verifyWordIn("zh-Hant", "nav"), "驗收")
})

test("the drawer has a 驗收 row that opens this page", () => {
  const app = readFileSync(new URL("../../App.tsx", import.meta.url), "utf8")
  assert.match(app, /id="nav-verify"[\s\S]{0,200}go\("verify"\)[\s\S]{0,120}verifyWord\("nav"\)/)
  assert.match(readFileSync(new URL("../verify.tsx", import.meta.url), "utf8"), /export const page: PageModule = \{ id: "verify"/)
})
