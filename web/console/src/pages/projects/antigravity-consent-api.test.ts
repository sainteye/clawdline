import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node's strip-types runner uses source extensions.
import { readAntigravityConsent, changeAntigravityConsent, consentStatus, consentRoute } from "./antigravity-consent-api.ts"
const status = { phase: "disabled", revision: 3, scope: "project-and-linked-worktrees", hooks: "unknown" } as const
const input = { action: "disable", revision: 2, scope: status.scope } as const

test("unreadable and invalid status stay unknown rather than becoming consent", async () => {
  for (const value of [null, {}, { ...status, hooks: "ready" }, { ...status, revision: -1 }, { ...status, revision: Number.MAX_SAFE_INTEGER + 1 }, { ...status, scope: "all" }, { ...status, preview: { before_digest: "a" } }]) assert.throws(() => consentStatus(value))
  assert.deepEqual(consentStatus(status), status)
  assert.equal(consentRoute("machine/project"), "/v1/projects/machine%2Fproject/antigravity-consent")
})
test("a lost mutation is uncertain, never retried, and an explicit retry preserves approval and key", async () => {
  const original = globalThis.fetch
  const calls: RequestInit[] = []
  globalThis.fetch = async (_url, init) => { calls.push(init!); throw new Error("lost answer after commit") }
  try {
    await assert.rejects(changeAntigravityConsent("project", input, "same-operation"), { code: "network", uncertain: true })
    assert.equal(calls.length, 1)
    globalThis.fetch = async (_url, init) => { calls.push(init!); return Response.json(status) }
    assert.deepEqual(await readAntigravityConsent("project"), status)
    assert.equal(calls[1].method, undefined)
    assert.deepEqual(await changeAntigravityConsent("project", input, "same-operation"), status)
    assert.equal(calls[0].body, calls[2].body)
    assert.deepEqual(calls[0].headers, calls[2].headers)
    globalThis.fetch = async () => Response.json({ error: "antigravity_project_hooks_unverified" }, { status: 409 })
    await assert.rejects(changeAntigravityConsent("project", input, "same-operation"), { code: "antigravity_project_hooks_unverified", uncertain: true })
    await assert.rejects(readAntigravityConsent("project"), { uncertain: false })
  } finally { globalThis.fetch = original }
})

test("a hanging response body expires without repeating a mutation", async t => {
  t.mock.timers.enable({ apis: ["setTimeout"] })
  const original = globalThis.fetch
  let signal: AbortSignal | null | undefined
  let count = 0
  globalThis.fetch = async (_url, init) => { ++count; signal = init?.signal; return { ok: true, json: () => new Promise(() => {}) } as Response }
  let result: unknown = null
  const pending = changeAntigravityConsent("project", input, "one-key").catch(error => { result = error })
  try {
    await Promise.resolve(); await Promise.resolve()
    t.mock.timers.tick(15_000)
    await Promise.resolve(); await Promise.resolve()
    assert.equal(signal?.aborted, true, "whole-answer deadline must abort even after response headers")
    await pending
    assert.equal((result as { code: string }).code, "timeout")
    assert.equal((result as { uncertain: boolean }).uncertain, true)
    assert.equal(count, 1)
  } finally { globalThis.fetch = original; t.mock.timers.reset() }
})
