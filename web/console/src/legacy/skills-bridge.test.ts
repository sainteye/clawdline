import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node source tests use the source extension.
import { filterSkills, skillQuery, loadSkills, heldSkills } from "./skills-bridge.ts"
// @ts-expect-error -- Node source tests use the source extension.
import { commandCatalog, commandInvocation } from "./command-catalog.ts"

test("all matches remain reachable and aliases and Chinese descriptions are searchable", () => {
 const commands = commandCatalog("claude", [])
 assert.equal(filterSkills(commands, "").length, commands.length)
 assert.equal(filterSkills(commands, "continue")[0].name, "resume")
 assert.ok(filterSkills(commands, "推理").some(c => c.name === "effort"))
 assert.equal(skillQuery("/model argument", "codex"), null)
 assert.equal(skillQuery("/", "codex"), "")
})

test("dollar completion excludes slash commands without hiding Codex skills", () => {
 const commands = commandCatalog("codex", [{name:"model",source:"project",description:"skill"}])
 assert.deepEqual(filterSkills(commands, "model", "$").map(c=>commandInvocation(c,"codex")), ["$model"])
 assert.deepEqual(filterSkills(commands, "model", "/").filter(c=>c.name==="model").map(c=>commandInvocation(c,"codex")).sort(), ["$model","/model"])
})

test("a reused terminal id cannot mix Claude and Codex catalogs", async () => {
 const claude = await loadSkills("reused-provider-fixture", async()=>[], "claude")
 const codex = await loadSkills("reused-provider-fixture", async()=>[], "codex")
 assert.ok(claude.some(command=>command.name === "effort"))
 assert.ok(!codex.some(command=>command.name === "effort"))
 assert.ok(codex.some(command=>command.name === "memories"))
 assert.equal(heldSkills("reused-provider-fixture", "claude"),claude)
 assert.equal(heldSkills("reused-provider-fixture", "codex"),codex)
})
