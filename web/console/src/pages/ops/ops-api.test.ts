import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node strips source TypeScript in the browser-free test.
import { OpsError, createOps, deleteOps, listOps, readOpsSetup, requestOpsResolve, updateOpsEntry } from "./ops-api.ts"

async function answering<T>(status: number, body: unknown, run: () => Promise<T>) {
  const previous = globalThis.fetch
  let url = ""
  let sent: RequestInit | undefined
  globalThis.fetch = async (target: any, init?: RequestInit) => {
    url = String(target); sent = init
    return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } })
  }
  try { return { result: await run(), url, sent } } catch (error) { return { error, url, sent } } finally { globalThis.fetch = previous }
}

test("the list is read from the place's ops route", async () => {
  const { result, url } = await answering(200, { project_key: "/repo", collections: [], entries: [] }, () => listOps("place one"))
  assert.equal(url, "/v1/projects/place%20one/ops")
  assert.equal((result as any).project_key, "/repo")
})

test("the GitHub probe is asked only when the caller asks for it", async () => {
  assert.equal((await answering(200, {}, () => readOpsSetup("p"))).url, "/v1/projects/p/ops/setup")
  assert.equal((await answering(200, {}, () => readOpsSetup("p", true))).url, "/v1/projects/p/ops/setup?probe=github")
})

test("a create carries its body and the action's key", async () => {
  const body = { kind: "markdown", name: "Runbook", content: "# Runbook", git: "exclude" } as const
  const { sent } = await answering(201, { entry: {} }, () => createOps("p", body, "key-1"))
  assert.equal(sent?.method, "POST")
  assert.equal((sent?.headers as Record<string, string>)["Idempotency-Key"], "key-1")
  assert.equal(sent?.body, JSON.stringify(body))
})

test("a refused write keeps the machine's code and is certain; a server failure is uncertain", async () => {
  const taken = await answering(409, { error: "ops_name_taken", detail: "taken" }, () => createOps("p", { kind: "markdown", name: "a", content: "" }, "k"))
  assert.ok(taken.error instanceof OpsError)
  assert.equal(taken.error.code, "ops_name_taken")
  assert.equal(taken.error.uncertain, false)
  const lost = await answering(504, { error: "cloud_unanswered", outcome: "unknown" }, () => updateOpsEntry("p", "ope_1", { title: "x" }, "k"))
  assert.equal((lost.error as any).uncertain, true)
  const changed = await answering(409, { error: "file_changed" }, () => updateOpsEntry("p", "ope_1", { content: "x", expected_version: "v1" }, "k"))
  assert.equal((changed.error as any).code, "file_changed")
})

test("delete and resolve-request use their own routes", async () => {
  const removed = await answering(200, { ok: true, deleted: "ope_1" }, () => deleteOps("p", "ope_1", "k"))
  assert.equal(removed.url, "/v1/projects/p/ops/ope_1")
  assert.equal(removed.sent?.method, "DELETE")
  const asked = await answering(200, { entry: {}, todo: { id: "t" } }, () => requestOpsResolve("p", "ope_1", "%3", "k"))
  assert.equal(asked.url, "/v1/projects/p/ops/ope_1/resolve-request")
  assert.equal(asked.sent?.body, JSON.stringify({ terminal: "%3" }))
})
