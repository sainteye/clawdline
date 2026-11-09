import assert from "node:assert/strict"
import { test } from "node:test"
import { createRequire } from "node:module"
import { fileURLToPath } from "node:url"
import { build } from "esbuild"

const { outputFiles } = await build({
  entryPoints: [fileURLToPath(new URL("../../core/src/fleet.ts", import.meta.url))],
  bundle: true, platform: "node", format: "cjs", packages: "external", write: false,
})
const compiled = { exports: {} }
new Function("require", "module", "exports", outputFiles[0].text)(createRequire(import.meta.url), compiled, compiled.exports)
const { FleetStore } = compiled.exports
const row = (label, extra = {}) => ({ id: "terminal", sessionId: "conversation", execution_generation: "execution", label, closeability: { state: "safe", reasons: [] }, ...extra })
function fixture() {
  let generation = 0
  let rows = [row("Before"), row("Unrelated", { id: "other" })]
  let epoch = 1
  let complete = true
  const store = new FleetStore({ sessions: async () => ({ at: 100, scan: { epoch, generation: ++generation, complete }, sessions: rows }) })
  return { store, rows: next => { rows = next }, restart: () => { epoch++ }, partial: () => { complete = false } }
}
const title = store => store.get().snapshot.sessions.find(row => row.id === "terminal")?.label

test("an acknowledged title is immediate and survives held list reads until the producer confirms it", async () => {
  const f = fixture()
  await f.store.start()
  let observed
  f.store.subscribe(() => { observed = title(f.store) })
  f.store.titleSaved("terminal", "After")
  assert.equal(observed, "After")
  assert.equal(f.store.get().snapshot.sessions.find(row => row.id === "other").label, "Unrelated")
  await f.store.refresh()
  assert.equal(title(f.store), "After")
  f.rows([row("After")])
  await f.store.refresh()
  f.rows([row("Renamed elsewhere")])
  await f.store.refresh()
  assert.equal(title(f.store), "Renamed elsewhere", "confirmation releases the local presentation")
})

for (const [name, replacement] of [
  ["conversation", { sessionId: "another-conversation" }],
  ["execution", { execution_generation: "another-execution" }],
]) test(`an acknowledged title cannot transfer to another ${name} at the same terminal`, async () => {
  const f = fixture()
  await f.store.start()
  f.store.titleSaved("terminal", "After")
  f.rows([row("Replacement", replacement)])
  await f.store.refresh()
  assert.equal(title(f.store), "Replacement")
})

test("clearing a title uses the server's display fallback and later renames still work", async () => {
  const f = fixture()
  await f.store.start()
  f.store.titleSaved("terminal", "Project fallback")
  await f.store.refresh()
  assert.equal(title(f.store), "Project fallback")
  f.store.titleSaved("terminal", "Second name")
  await f.store.refresh()
  assert.equal(title(f.store), "Second name")
  f.rows([])
  await f.store.refresh()
  f.rows([row("A new appearance")])
  await f.store.refresh()
  assert.equal(title(f.store), "A new appearance")
})

test("a retained row in a partial reading is not confirmation from the producer", async () => {
  const f = fixture()
  await f.store.start()
  f.store.titleSaved("terminal", "After")
  f.partial()
  f.rows([])
  await f.store.refresh()
  assert.equal(title(f.store), "After")
  assert.equal(f.store.get().error, null)
  f.rows([row("Before")])
  await f.store.refresh()
  assert.equal(title(f.store), "After")
})
