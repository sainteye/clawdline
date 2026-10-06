import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node strips source TypeScript in the browser-free test.
import { ProjectFileError, applyUnify, readUnifyPlan, saveProjectFile } from "./project-files-api.ts"

test("an unanswered Cloud save stays uncertain so the editor rereads before confirming it", async () => {
  const previous = globalThis.fetch
  globalThis.fetch = async () => new Response(JSON.stringify({ error: "cloud_unanswered", outcome: "unknown" }), {
    status: 504, headers: { "Content-Type": "application/json" },
  })
  try {
    await assert.rejects(saveProjectFile("project", "file", "old", "new text", "save-1"), (error: unknown) =>
      error instanceof ProjectFileError && error.uncertain)
  } finally { globalThis.fetch = previous }
})

async function answering<T>(status: number, body: unknown, run: () => Promise<T>): Promise<{ result?: T; error?: unknown; sent?: RequestInit }> {
  const previous = globalThis.fetch
  let sent: RequestInit | undefined
  globalThis.fetch = async (_url: any, init?: RequestInit) => {
    sent = init
    return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } })
  }
  try { return { result: await run(), sent } } catch (error) { return { error, sent } } finally { globalThis.fetch = previous }
}

test("a unify apply sends the plan version with its key, and a stopped run is an answer, not a throw", async () => {
  const stopped = { outcome: "stopped", ran: [], failed: { kind: "skill_link" }, error: "name_taken", detail: "Unify stopped: taken", plan: { status: "drifting" } }
  const { result, sent } = await answering(500, stopped, () => applyUnify("project", "plan-v1", "unify-1"))
  assert.equal((result as any).outcome, "stopped")
  assert.equal((result as any).plan.status, "drifting")
  assert.equal(sent?.method, "POST")
  assert.equal((sent?.headers as Record<string, string>)["Idempotency-Key"], "unify-1")
  assert.equal(sent?.body, JSON.stringify({ version: "plan-v1" }))
})

test("a changed plan is refused by name and is certain: nothing was written", async () => {
  const { error } = await answering(409, { error: "plan_changed", detail: "The files changed." }, () => applyUnify("project", "old", "unify-2"))
  assert.ok(error instanceof ProjectFileError)
  assert.equal(error.code, "plan_changed")
  assert.equal(error.uncertain, false)
})

test("an unanswered Cloud apply stays uncertain so the screen rereads the plan before saying it applied", async () => {
  const { error } = await answering(504, { error: "cloud_unanswered", outcome: "unknown" }, () => applyUnify("project", "v", "unify-3"))
  assert.ok(error instanceof ProjectFileError && error.uncertain)
  const previous = globalThis.fetch
  globalThis.fetch = async () => { throw new TypeError("network") }
  try {
    await assert.rejects(applyUnify("project", "v", "unify-4"), (e: unknown) => e instanceof ProjectFileError && e.uncertain)
    // A lost read is never uncertain: it wrote nothing.
    await assert.rejects(readUnifyPlan("project"), (e: unknown) => e instanceof ProjectFileError && !e.uncertain && e.code === "network")
  } finally { globalThis.fetch = previous }
})

test("a plan read that is refused carries the machine's code, never an empty plan", async () => {
  const { error } = await answering(404, { error: "project_not_found", detail: "gone" }, () => readUnifyPlan("project"))
  assert.ok(error instanceof ProjectFileError)
  assert.equal(error.code, "project_not_found")
})
