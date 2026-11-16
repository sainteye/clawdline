import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node executes source extensions.
import { agentName, fallbackChoices, fallbackEdit, fallbackModeSupported, MAX_FALLBACK_AGENTS, moveFallback, previousAgent } from "./fallback-settings.ts"

test("fallback edits preserve off, configured and unfamiliar values until deliberately changed", () => {
  assert.deepEqual(fallbackEdit(undefined, [], true), {})
  assert.deepEqual(fallbackEdit(["codex"], ["codex"], true), {})
  assert.deepEqual(fallbackEdit(["codex"], [], true), { fallback_agents: [] })
  assert.deepEqual(fallbackEdit([], ["codex"], true), { fallback_agents: ["codex"] })
  assert.deepEqual(fallbackEdit(["codex"], [], false), {})
  // Order is part of the configuration: a reorder is a deliberate change.
  assert.deepEqual(fallbackEdit(["codex", "claude"] as never, ["claude", "codex"] as never, true), { fallback_agents: ["claude", "codex"] })
})

test("the add choices never offer the primary, a chosen Agent, or more than the source accepts", () => {
  assert.deepEqual(fallbackChoices("claude", []), ["codex"])
  assert.deepEqual(fallbackChoices("codex", []), ["claude"])
  assert.deepEqual(fallbackChoices("claude", ["codex"]), [])
  assert.deepEqual(fallbackChoices("claude", Array(MAX_FALLBACK_AGENTS).fill("x") as never), [])
  assert.equal(MAX_FALLBACK_AGENTS, 3)
})

test("moving keeps every entry and swaps only with an existing neighbour", () => {
  assert.deepEqual(moveFallback(["a", "b", "c"], 1, -1), ["b", "a", "c"])
  assert.deepEqual(moveFallback(["a", "b", "c"], 1, 1), ["a", "c", "b"])
  assert.deepEqual(moveFallback(["a", "b"], 0, -1), ["a", "b"])
  assert.deepEqual(moveFallback(["a", "b"], 1, 1), ["a", "b"])
  assert.deepEqual(previousAgent("claude", ["codex"], 0), "claude")
  assert.deepEqual(previousAgent("claude", ["codex", "x"] as never, 1), "codex")
})

test("only Auto grows the chain; Agent names are the product names", () => {
  assert.equal(fallbackModeSupported(undefined), true)
  assert.equal(fallbackModeSupported("auto"), true)
  for (const mode of ["plan", "default", "acceptEdits", "unknown"]) assert.equal(fallbackModeSupported(mode), false)
  assert.equal(agentName("claude"), "Claude Code")
  assert.equal(agentName("codex"), "Codex")
  assert.equal(agentName("future-agent"), "future-agent")
})
