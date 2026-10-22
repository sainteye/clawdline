import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path for Node's test runner.
import { reasoningEffortHTML } from "./reasoning-effort.ts"

const esc = (s: string) => s.replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll('"', "&quot;")
const word = (key: string) => key.replace("infoEffort", "")

test("the meter locates each Codex level, including Max and Ultra", () => {
  for (const [index, level] of ["low", "medium", "high", "xhigh", "max", "ultra"].entries()) {
    const html = reasoningEffortHTML("codex", level, word, esc)
    assert.ok(html.includes('aria-valuenow="' + (index + 1) + '"'))
    assert.ok(html.includes('aria-valuemax="6"'))
    assert.equal((html.match(/data-current="1"/g) || []).length, 1)
    assert.ok(!html.includes("<input"))
  }
})

test("Claude uses its own ordinal scale and a translated accessible value", () => {
  const html = reasoningEffortHTML("claude", "high", (key) => key === "infoEffortHigh" ? "高" : word(key), esc)
  assert.ok(html.includes('aria-valuemax="5"'))
  assert.ok(html.includes('aria-valuenow="3"'))
  assert.ok(html.includes('aria-valuetext="高"'))
  assert.ok(!html.includes("Ultra"))
})

test("unknown or newly named levels are not drawn at a guessed position", () => {
  for (const effort of [undefined, "auto", '<unknown>']) {
    const html = reasoningEffortHTML("claude", effort, word, esc)
    assert.ok(!html.includes('role="meter"'))
    assert.ok(!html.includes("<unknown>"))
  }
  assert.ok(reasoningEffortHTML("claude", undefined, word, esc).includes("Unknown"))
})

test("older Codex none and minimal settings retain separate positions", () => {
  assert.ok(reasoningEffortHTML("codex", "none", word, esc).includes('aria-valuenow="1"'))
  assert.ok(reasoningEffortHTML("codex", "minimal", word, esc).includes('aria-valuenow="2"'))
})

test("Antigravity has five measured effort levels and no guessed position for other values", () => {
  for (const [index, level] of ["low", "medium", "high", "xhigh", "max"].entries()) {
    const html = reasoningEffortHTML("antigravity", level, word, esc)
    assert.ok(html.includes(`aria-valuenow="${index + 1}"`))
    assert.ok(html.includes('aria-valuemax="5"'))
    assert.equal((html.match(/data-current="1"/g) || []).length, 1)
    assert.ok(!html.includes("<input"))
  }
  for (const level of [undefined, "none", "minimal", "ultra"]) {
    assert.ok(!reasoningEffortHTML("antigravity", level, word, esc).includes('role="meter"'))
  }
})
