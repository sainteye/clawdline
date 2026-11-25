import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node executes source extensions.
import { dispatchModels } from "./dispatch-models.ts"

const catalog = (agent: string, current: string, choices: { value: string; name: string; description?: string }[]) =>
  ({ agent, model: "", options: [{ id: "model", name: "Model", category: "model", current, choices }] }) as never

test("Claude's default entry becomes the named default and is not offered twice", () => {
  const out = dispatchModels(catalog("claude", "default", [
    { value: "default", name: "Default (recommended)", description: "Opus 4.5" },
    { value: "sonnet", name: "Sonnet", description: "Sonnet 4.5" },
    { value: "haiku", name: "Haiku" },
  ]))
  assert.equal(out.defaultName, "Opus 4.5")
  assert.deepEqual(out.choices, [{ value: "sonnet", label: "Sonnet", hint: "Sonnet 4.5" }, { value: "haiku", label: "Haiku", hint: undefined }])
})

test("Codex's per-effort entries collapse to one model each", () => {
  const out = dispatchModels(catalog("codex", "gpt-5.5/high", [
    { value: "gpt-5.5/low", name: "gpt-5.5 (low)" },
    { value: "gpt-5.5/high", name: "gpt-5.5 (high)" },
    { value: "gpt-5.4-mini/medium", name: "gpt-5.4-mini (medium)" },
  ]))
  assert.equal(out.defaultName, "gpt-5.5", "a current value without a default entry names the model that runs")
  assert.deepEqual(out.choices.map(c => c.value), ["gpt-5.5", "gpt-5.4-mini"])
})

test("names the launcher would refuse are never offered", () => {
  const out = dispatchModels(catalog("claude", "default", [
    { value: "default", name: "Default" },
    { value: "opus[1m]", name: "Opus 1M" },
    { value: "Sonnet", name: "Sonnet" },
    { value: "-x", name: "dash" },
    { value: "opus", name: "Opus" },
  ]))
  assert.deepEqual(out.choices.map(c => c.value), ["opus"])
})

test("no catalog or no model option offers only the agent default", () => {
  assert.deepEqual(dispatchModels(undefined), { choices: [], defaultName: "" })
  assert.deepEqual(dispatchModels({ agent: "grok", model: "", options: [] } as never), { choices: [], defaultName: "" })
})

test("a concrete current model is named by its name, not its description", () => {
  // The shape Codex's adapter sent on 2026-11-25: no Default entry, notes that describe each model.
  const out = dispatchModels(catalog("codex", "model-sol", [
    { value: "model-sol", name: "6.2 Sol", description: "Fast, go-to model for coding and everyday work." },
    { value: "model-astra", name: "6.2 Astra", description: "Our most capable model for complex coding and professional work." },
  ]))
  assert.equal(out.defaultName, "6.2 Sol")
  assert.deepEqual(out.choices.map(c => c.label), ["6.2 Sol", "6.2 Astra"])
})

test("a Default entry that copies a model's note is named by that model", () => {
  // The shape Codex's adapter sent on this machine on 2026-11-25.
  const out = dispatchModels(catalog("codex", "default", [
    { value: "default", name: "Default", description: "Fast, go-to model for coding and everyday work." },
    { value: "model-sol", name: "6.2 Sol", description: "Fast, go-to model for coding and everyday work." },
    { value: "model-astra", name: "6.2 Astra", description: "Our most capable model for complex coding and professional work." },
  ]))
  assert.equal(out.defaultName, "6.2 Sol")
  assert.deepEqual(out.choices.map(c => c.value), ["model-sol", "model-astra"])
})
