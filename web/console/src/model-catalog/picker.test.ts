import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node executes source extensions.
import { catalogPicker, paSelectStatus } from "./picker.ts"
// @ts-expect-error -- Node executes source extensions.
import { isCurrentModel, currentModelRow } from "./match.ts"
// @ts-expect-error -- Node executes source extensions.
import { machineCatalogSource } from "./machine.ts"

const words = {
  defaultNamed: "Default ({name})", defaultPlain: "Default", loading: "Asking {agent}", failed: "No {agent}",
  empty: "{agent} lists nothing", older: "Too old", missing: "Gone", retry: "Retry",
}
const codex = (models: string[], extra: Record<string, unknown> = {}) => ({
  agent: "codex", models: models.map(value => ({ value, name: value.toUpperCase() })), default_name: "Sol",
  efforts: ["high", "xhigh"], source: "agent", observed_at: "", refreshing: false, ...extra,
})

test("a ready catalog offers the named default first, then each model, with the agent's efforts", () => {
  const p = catalogPicker({ status: "ready", catalog: codex(["model-sol", "model-astra"]), waiting: false }, undefined, "Codex", words)
  assert.deepEqual(p.options.map((o: { label: string }) => o.label), ["Default (Sol)", "MODEL-SOL", "MODEL-ASTRA"])
  assert.equal(p.status, undefined)
  assert.deepEqual(p.efforts, ["high", "xhigh"])
  assert.equal(p.loading, false)
})

test("loading keeps a saved model visible with no hint, and says who is being asked", () => {
  const p = catalogPicker({ status: "loading" }, "model-old", "Codex", words)
  assert.deepEqual(p.options, [{ value: "", label: "Default" }, { value: "model-old", label: "model-old", hint: undefined }])
  assert.deepEqual(p.status, { kind: "loading", text: "Asking Codex" })
  assert.equal(p.loading, true)
})

test("a refreshing answer with nothing listed still loads; with models it is ready", () => {
  assert.equal(catalogPicker({ status: "ready", catalog: codex([], { refreshing: true }), waiting: true }, undefined, "Codex", words).status?.kind, "loading")
  assert.equal(catalogPicker({ status: "ready", catalog: codex(["model-sol"], { refreshing: true }), waiting: true }, undefined, "Codex", words).status, undefined)
})

test("a saved model the agent no longer lists stays, marked as gone", () => {
  const p = catalogPicker({ status: "ready", catalog: codex(["model-sol"]), waiting: false }, "model-old", "Codex", words)
  assert.deepEqual(p.options.at(-1), { value: "model-old", label: "model-old", hint: "Gone" })
})

test("failed offers Retry; unsupported and empty are notes", () => {
  assert.deepEqual(catalogPicker({ status: "failed", code: "unavailable" }, undefined, "Codex", words).status, { kind: "failed", text: "No Codex", retryLabel: "Retry" })
  const older = catalogPicker({ status: "unsupported" }, undefined, "Codex", words)
  assert.deepEqual(older.options, [{ value: "", label: "Default" }])
  assert.deepEqual(older.status, { kind: "unsupported", text: "Too old" })
  assert.deepEqual(catalogPicker({ status: "ready", catalog: codex([]), waiting: false }, undefined, "Codex", words).status, { kind: "empty", text: "Codex lists nothing" })
})

test("the Bots drop-down draws failure as an error row that retries", () => {
  let retried = 0
  const s = paSelectStatus({ kind: "failed", text: "No", retryLabel: "Retry" }, () => { retried++ })
  assert.equal(s?.kind, "error")
  s?.onRetry?.()
  assert.equal(retried, 1)
  assert.equal(paSelectStatus({ kind: "unsupported", text: "Too old" }, () => {})?.kind, "empty")
})

test("the current model is the same word, a dated release, or a bracketed variant, never a shorter version", () => {
  assert.equal(isCurrentModel("claude-opus-4-1", "claude-opus-4-1"), true)
  assert.equal(isCurrentModel("claude-opus-4-1-20250805", "claude-opus-4-1"), true)
  assert.equal(isCurrentModel("opus[1m]", "opus"), true)
  assert.equal(isCurrentModel("claude-opus-4-1", "claude-opus-4"), false)
  assert.equal(isCurrentModel("claude-opus-4-1-beta", "claude-opus-4-1"), false)
  assert.equal(isCurrentModel("", "opus"), false)
  const rows = [{ id: "claude-opus-4" }, { id: "claude-opus-4-1" }]
  assert.equal(currentModelRow("claude-opus-4-1-20250805", rows, r => r.id)?.id, "claude-opus-4-1")
  assert.equal(currentModelRow("claude-sonnet-4", rows, r => r.id), undefined)
})

const answer = (status: number, body: unknown) => async () => new Response(JSON.stringify(body), { status })

test("the page machine's catalog: an answer, an older daemon, and a failure", async () => {
  const signal = new AbortController().signal
  const ok = machineCatalogSource(120, answer(200, { agents: [codex(["model-sol"])] }))
  assert.equal((await ok.modelCatalog!(undefined, signal)).agents[0].agent, "codex")
  await assert.rejects(machineCatalogSource(undefined, answer(404, { error: { code: "not_found" } })).modelCatalog!(undefined, signal), { code: "pa_unsupported" })
  await assert.rejects(machineCatalogSource(undefined, answer(501, { error: { code: "cloud_not_carried" } })).modelCatalog!(undefined, signal), { code: "pa_unsupported" })
  await assert.rejects(machineCatalogSource(120, answer(503, { error: { code: "busy" } })).modelCatalog!(undefined, signal), { code: "busy" })
  await assert.rejects(machineCatalogSource(120, async () => { throw new TypeError("offline") }).modelCatalog!(undefined, signal), { code: "unavailable" })
  assert.equal(machineCatalogSource(119).canModelCatalog!(), false)
  assert.equal(machineCatalogSource(undefined).canModelCatalog!(), true)
})
