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
