import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node executes source extensions.
import { choiceName, resolvedDefault, settingSummary } from "./agent-model-summary.ts"

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
