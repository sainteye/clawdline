import assert from "node:assert/strict"
import { test } from "node:test"
// @ts-expect-error -- Node tests load the TypeScript source.
import { compactQuotaRows, quotaCountdown, quotaStale, QUOTA_VISIBLE_KEY, readQuotaVisible, saveQuotaVisible, visibleQuotaRows } from "./model.ts"
import type { ProviderQuotaRow } from "@clawdline/contract"
const row = (id: ProviderQuotaRow["id"], percent: number): ProviderQuotaRow => ({ id, label: id, windows: [{ name: "7d", used_percent: percent }], stale: false, source: "provider_api" })
test("providers with no number stay hidden while a real zero stays visible", () => {
  const claude = row("claude", 0), codex = row("codex", 60)
  assert.deepEqual(visibleQuotaRows([claude, codex, { ...row("gemini", 0), windows: [] }, { ...row("grok", 0), windows: [] }]), [claude, codex])
  assert.deepEqual(visibleQuotaRows([row("grok", NaN), row("gemini", Infinity)]), [])
})
test("a narrow footer keeps the tightest provider limits and details retain all rows", () => {
  const rows = [row("claude", 10), row("codex", 91), row("gemini", 20), row("grok", 88)]
  assert.deepEqual(compactQuotaRows(rows, 2).map((r) => r.id), ["codex", "grok"])
  assert.equal(rows.length, 4)
})
test("visibility defaults on and persists only in the chosen browser store", () => {
  const values = new Map<string, string>(); const browser = { getItem: (k: string) => values.get(k) ?? null, setItem: (k: string, v: string) => { values.set(k, v) } }
  assert.equal(readQuotaVisible(browser), true)
  assert.equal(saveQuotaVisible(false, browser), true)
  assert.equal(readQuotaVisible(browser), false)
  assert.equal(readQuotaVisible({ getItem: () => null }), true)
  assert.equal(saveQuotaVisible(true, browser), true)
  assert.equal(readQuotaVisible(browser), true)
  assert.equal(readQuotaVisible({ getItem() { throw new Error("blocked") } }), true)
  assert.equal(saveQuotaVisible(false, null), false)
})
test("expired reset data stays visibly stale rather than turning into a fresh zero", () => {
  const reading = row("codex", 80); reading.windows[0].resets_at = 100
  assert.equal(quotaStale(reading, 101), true)
  assert.equal(quotaCountdown(100, 101), "")
  assert.equal(quotaCountdown(undefined, 101), null)
  assert.equal(quotaCountdown(101 + 3660, 101), "1h 1m")
  assert.equal(visibleQuotaRows([reading])[0].windows[0].used_percent, 80)
})

test("desktop and mobile choices stay independent across reloads and legacy opt-out", () => {
  const values = new Map<string, string>([[QUOTA_VISIBLE_KEY, "false"]])
  const browser = { getItem: (key: string) => values.get(key) ?? null, setItem: (key: string, value: string) => { values.set(key, value) } }
  assert.equal(readQuotaVisible(browser, "desktop"), false)
  assert.equal(readQuotaVisible(browser, "mobile"), false)
  assert.equal(saveQuotaVisible(true, browser, "desktop"), true)
  assert.equal(readQuotaVisible(browser, "desktop"), true)
  assert.equal(readQuotaVisible(browser, "mobile"), false)
  assert.equal(saveQuotaVisible(true, browser, "mobile"), true)
  assert.equal(saveQuotaVisible(false, browser, "desktop"), true)
  assert.equal(readQuotaVisible(browser, "desktop"), false)
  assert.equal(readQuotaVisible(browser, "mobile"), true)
  assert.equal(readQuotaVisible({ getItem: () => null }, "mobile"), true)
})
