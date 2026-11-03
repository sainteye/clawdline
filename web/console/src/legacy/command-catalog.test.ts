import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node source tests use the source extension.
import { commandCatalog, commandDescription, commandInvocation, nativeDefinitions } from "./command-catalog.ts"

test("both complete snapshots produce distinct invocations and translated descriptions", () => {
 for (const assistant of ["claude", "codex"]) {
  const commands = commandCatalog(assistant, [])
  assert.ok(commands.length > 9)
  assert.equal(new Set(commands.map(command => commandInvocation(command, assistant))).size, commands.length)
  for (const command of commands) {
   assert.ok(commandInvocation(command, assistant).startsWith("/"))
   assert.ok(commandDescription(command, "en"))
   assert.ok(commandDescription(command, "zh-Hant"))
   assert.notEqual(commandDescription(command, "zh-Hant"), commandDescription(command, "en"))
  }
 }
 assert.equal(nativeDefinitions("antigravity").length, 0)
})

test("Codex /model, $model and /prompts:model remain separate", () => {
 const commands = commandCatalog("codex", [
  {name:"model", description:"skill", source:"project"},
  {name:"prompts:model", description:"prompt", source:"personal", invocation:"/prompts:model"},
 ])
 assert.deepEqual(commands.filter(c => c.name === "model" || c.name === "prompts:model").map(c => commandInvocation(c,"codex")).sort(), ["$model","/model","/prompts:model"])
 const custom = {name:"model",description:"custom",source:"project" as const}
 assert.equal(commandCatalog("claude", [custom]).filter(c=>c.name==="model").length,1)
 assert.equal(commandCatalog("claude", [custom]).find(c=>c.name==="model"),custom)
})
