import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node executes source extensions.
import { choiceLabel, choiceName, catalogRuntime, hasEffortOption, joinChoice, resolvedDefault, runtimeModelPicker, settingSummary, splitChoice } from "./agent-model-summary.ts"
// @ts-expect-error -- Node executes source extensions.
import { paSelectStatus } from "../model-catalog/picker.ts"

// The shapes Claude's ACP adapter 0.106.0 sends without the AIR recommended-value capability.
const model = { id: "model", name: "Model", category: "model", current: "default", choices: [
  { value: "default", name: "Default (recommended)", description: "Opus 4.5" },
  { value: "sonnet", name: "Sonnet", description: "Sonnet 4.5" },
] }
const effort = { id: "effort", name: "Effort", category: "thought_level", current: "default", choices: [
  { value: "default", name: "Default" }, { value: "low", name: "Low" }, { value: "high", name: "High" },
] }
const fast = { id: "fast", name: "Fast mode", category: "model_config", current: "off", choices: [
  { value: "on", name: "On" }, { value: "off", name: "Off" },
] }

test("an unset model names the model its Default entry resolves to", () => {
  assert.equal(resolvedDefault(model), "Opus 4.5")
  // An Agent without a note but with a concrete current value names that value.
  assert.equal(resolvedDefault({ ...model, current: "gpt-5", choices: [{ value: "gpt-5", name: "GPT-5" }] }), "GPT-5")
  // A Default entry with no note says nothing rather than repeating "Default".
  assert.equal(resolvedDefault(effort), "")
})

test("the summary row lists chosen settings and enabled switches only", () => {
  assert.equal(settingSummary(effort, undefined), "")
  assert.equal(settingSummary(effort, "high"), "High")
  assert.equal(settingSummary(fast, undefined), "")
  assert.equal(settingSummary(fast, "on"), "Fast mode")
  assert.equal(settingSummary({ ...fast, current: "true", choices: [{ value: "false", name: "false" }, { value: "true", name: "true" }] }, undefined), "Fast mode")
})

test("native switch names follow the interface language", () => {
  const w = { nativeOn: "開啟", nativeOff: "關閉" }
  assert.deepEqual(["On", "true", "Off", "false", "High"].map(n => choiceName(n, w)), ["開啟", "開啟", "關閉", "關閉", "High"])
})

const words = { defaultNamed: "Agent default ({name})", defaultPlain: "Agent default", loading: "Asking {agent}…", failed: "Could not read {agent}.",
  empty: "{agent} reported no other models.", older: "Too old to list models.", missing: "Not in the current list", retry: "Try again", effortDefault: "Default strength" }
const codex = { agent: "codex", models: [{ value: "gpt-5.5", name: "GPT-5.5" }, { value: "gpt-5.4-mini", name: "GPT-5.4 mini" }],
  default_name: "GPT-5.5", efforts: ["high", "xhigh"], source: "agent", observed_at: "2026-11-25T06:00:00Z", refreshing: false }
const claude = { agent: "claude", models: [{ value: "sonnet", name: "Sonnet 5" }, { value: "haiku", name: "Haiku 4.5" }],
  default_name: "Opus 5.5", efforts: [], source: "native", observed_at: "2026-11-25T06:00:00Z", refreshing: false }
const ready = (catalog: unknown) => ({ status: "ready" as const, catalog: catalog as never, waiting: false })
const values = (options: { value: string }[]) => options.map(o => o.value)

test("a saved ACP value reads as its catalog entry", () => {
  assert.deepEqual(splitChoice("gpt-5.5/high"), { model: "gpt-5.5", effort: "high" })
  assert.deepEqual(splitChoice("default"), { model: "", effort: "" })
  assert.deepEqual(splitChoice("sonnet"), { model: "sonnet", effort: "" })
  // A model id that only contains "/" is one model.
  assert.deepEqual(splitChoice("models/gemini-pro"), { model: "models/gemini-pro", effort: "" })
  assert.equal(joinChoice("gpt-5.5", "high"), "gpt-5.5/high")
  assert.equal(joinChoice("", "high"), "")
  assert.equal(choiceLabel("gpt-5.5/high", "Agent default"), "gpt-5.5 · high")
  assert.equal(choiceLabel("default", "Agent default"), "Agent default")
  assert.equal(choiceLabel(undefined, "Agent default"), "Agent default")
})

test("a Bot's runtime models are the catalog's, the Agent default first", () => {
  const p = runtimeModelPicker(ready(codex), "gpt-5.5/high", "Codex", words)
  assert.deepEqual(values(p.options), ["", "gpt-5.5", "gpt-5.4-mini"])
  assert.equal(p.options[0].label, "Agent default (GPT-5.5)")
  assert.equal(p.model, "gpt-5.5")
  assert.equal(p.effort, "high")
  assert.equal(p.modelLabel, "GPT-5.5")
  assert.deepEqual(values(p.efforts), ["", "high", "xhigh"])
  assert.equal(p.status, undefined)
  // Claude's saved Default entry is the Agent default, and Claude has no catalog effort.
  const c = runtimeModelPicker(ready(claude), "default", "Claude Code", words)
  assert.equal(c.model, "")
  assert.equal(c.modelLabel, "Agent default (Opus 5.5)")
  assert.deepEqual(c.efforts, [])
})

test("an unmapped saved value stays as it is, marked as not listed", () => {
  const p = runtimeModelPicker(ready(claude), "opus[1m]", "Claude Code", words)
  assert.deepEqual(p.options.at(-1), { value: "opus[1m]", label: "opus[1m]", hint: "Not in the current list" })
  assert.equal(p.modelLabel, "opus[1m]")
  const e = runtimeModelPicker(ready(codex), "gpt-5.5/low", "Codex", words)
  assert.deepEqual(e.efforts.at(-1), { value: "low", label: "low", hint: "Not in the current list" })
})

test("the effort drop-down is left to the Agent's own effort option when it has one", () => {
  assert.equal(hasEffortOption([effort]), true)
  assert.equal(hasEffortOption([fast]), false)
  assert.deepEqual(runtimeModelPicker(ready(codex), "gpt-5.5", "Codex", words, true).efforts, [])
  // A saved effort is still shown so it can be changed back.
  assert.deepEqual(values(runtimeModelPicker(ready(codex), "gpt-5.5/high", "Codex", words, true).efforts), ["", "high", "xhigh"])
})

test("loading, failure with Retry, and an older machine read as the catalog pickers do", () => {
  const loading = runtimeModelPicker({ status: "loading" }, "sonnet", "Claude Code", words)
  assert.equal(loading.loading, true)
  assert.deepEqual(loading.status, { kind: "loading", text: "Asking Claude Code…" })
  // The saved model stays choosable while the list loads, with no "not listed" hint yet.
  assert.deepEqual(loading.options, [{ value: "", label: "Agent default" }, { value: "sonnet", label: "sonnet", hint: undefined }])

  let retried = 0
  const failed = runtimeModelPicker({ status: "failed", code: "unavailable" }, "", "Codex", words)
  const status = paSelectStatus(failed.status, () => retried++)!
  assert.equal(status.kind, "error")
  assert.equal(status.retryLabel, "Try again")
  status.onRetry!()
  assert.equal(retried, 1)
  assert.deepEqual(values(failed.options), [""])

  const older = runtimeModelPicker({ status: "unsupported" }, "", "Codex", words)
  assert.deepEqual(older.options, [{ value: "", label: "Agent default" }])
  assert.deepEqual(older.status, { kind: "unsupported", text: "Too old to list models." })
  assert.deepEqual(paSelectStatus(older.status, () => {}), { kind: "empty", text: "Too old to list models." })
  assert.deepEqual(older.efforts, [])
})

test("Gemini keeps its own ACP model list", () => {
  assert.equal(catalogRuntime("antigravity"), false)
  for (const agent of ["claude", "codex", "grok"]) assert.equal(catalogRuntime(agent), true)
})
