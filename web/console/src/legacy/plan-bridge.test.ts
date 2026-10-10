import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path for Node's test runner.
import { bindPlan, filterPlanDetails, PLAN_ELEMENT_IDS } from "./plan-bridge.ts"
import { T } from "./js/core/i18n.js"

function page() {
  const nodes = new Map<string, {
    textContent: string; hidden: boolean; disabled: boolean; dataset: Record<string, string>
    setAttribute(): void; addEventListener(): void; querySelectorAll(): unknown[]
  }>(PLAN_ELEMENT_IDS.map((id) => [id, {
    textContent: "", hidden: false, disabled: false, dataset: {} as Record<string, string>,
    setAttribute() {}, addEventListener() {}, querySelectorAll() { return [] },
  }]))
  return { doc: { getElementById: (id: string) => nodes.get(id) ?? null } as unknown as Document, nodes }
}

test("the hosted Plan page reads the signed-in account while the local page makes no billing call", async () => {
  const original = globalThis.fetch
  const calls: { url: string; credentials: RequestCredentials | undefined }[] = []
  globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input)
    calls.push({ url, credentials: init?.credentials })
    if (url.endsWith("/v1/entitlements")) return new Response(JSON.stringify({
      entitlements: { tier: "free", max_machines: 1 }, purchasable_tiers: ["pro"],
    }), { status: 200 })
    return new Response("{}", { status: 404 })
  }) as typeof fetch
  try {
    const hosted = page()
    const hostedPlan = bindPlan(hosted.doc, "https://api.example.test")
    await hostedPlan.enter()
    assert.equal(hostedPlan.state(), "free")
    assert.equal(hosted.nodes.get("plan-upgrade")?.hidden, false)
    assert.ok(calls.some((call) => call.url === "https://api.example.test/v1/entitlements" && call.credentials === "include"))
    await hostedPlan.enter({ returning: true })
    assert.equal(hostedPlan.state(), "waiting")
    hostedPlan.leave()

    const local = page()
    const before = calls.length
    const localPlan = bindPlan(local.doc)
    await localPlan.enter()
    assert.equal(localPlan.state(), "not_here")
    assert.equal(calls.length, before)
  } finally {
    globalThis.fetch = original
  }
})

test("the Plan page shows only limits that apply to each tier", () => {
  function rows() {
    const terms = [T.webPlanRowMacs, T.webPlanRowSessions, T.webPlanRowViewers,
      T.webPlanRowDispatches, T.webPlanRowHistory, T.webPlanRowFleet, T.webPlanRowTeam]
    const entries = terms.map((text) => {
      const value = { remove() {} }
      return { textContent: text, nextElementSibling: value, removed: false,
        remove() { this.removed = true } }
    })
    return { entries, list: { querySelectorAll: () => entries } as unknown as HTMLElement }
  }
  const free = rows()
  filterPlanDetails(free.list, { tier: "free", entitlements: {
    max_concurrent_sessions: 3, dispatches_per_month: 50 } })
  assert.deepEqual(free.entries.filter((row) => !row.removed).map((row) => row.textContent), [
    T.webPlanRowMacs, T.webPlanRowSessions, T.webPlanRowViewers,
    T.webPlanRowDispatches, T.webPlanRowHistory,
  ])
  const pro = rows()
  filterPlanDetails(pro.list, { tier: "pro", entitlements: {
    max_concurrent_sessions: null, dispatches_per_month: 1000 } })
  assert.deepEqual(pro.entries.filter((row) => !row.removed).map((row) => row.textContent), [
    T.webPlanRowMacs, T.webPlanRowViewers, T.webPlanRowHistory,
  ])
})
