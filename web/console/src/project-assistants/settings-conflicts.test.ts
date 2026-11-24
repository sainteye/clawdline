import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node executes source extensions.
import { settingsBase, settingsConflicts } from "./settings-conflicts.ts"

const bot = {
  name: "Xavia", appearance: { shape: "blob", color: "#e39567" }, instructions: { text: "Keep it short", revision: 3 },
  agent: "grok", permission_policy: "ask", version: 42, fallback_agents: ["codex"], background_threads: 4,
} as any

test("a save's base is the editable settings the editor opened, not the Bot version", () => {
  const base = settingsBase(bot)
  assert.deepEqual(base, { name: "Xavia", appearance: bot.appearance, instructions: "Keep it short", agent: "grok", permission_policy: "ask", fallback_agents: ["codex"], background_threads: 4 })
  assert.equal("version" in base, false)
  // A later Bot version with the same settings yields the same base: activity is not a settings change.
  assert.deepEqual(settingsBase({ ...bot, version: 99 }), base)
})

test("only a refusal that names settings fields is a field conflict", () => {
  assert.deepEqual(settingsConflicts({ code: "pa_stale_version", field: "settings.instructions,name" }), ["instructions", "name"])
  assert.deepEqual(settingsConflicts({ code: "pa_stale_version", field: "bot" }), [])
  assert.deepEqual(settingsConflicts({ code: "pa_invalid", field: "settings.name" }), [])
  assert.deepEqual(settingsConflicts(undefined), [])
})
