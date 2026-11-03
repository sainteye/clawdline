import { test } from "node:test"
import assert from "node:assert/strict"
import { createHash } from "node:crypto"
// @ts-expect-error -- Node's source runner uses the TypeScript extension.
import { readPersonaDisplay, clearPersonaDisplayCache, PERSONA_DISPLAY_CACHE_KEY, canonicalPersonaCatalog } from "./persona-cache.ts"

class Storage {
  value: string | null = null
  getItem() { return this.value }
  setItem(_key: string, value: string) { this.value = value }
  removeItem() { this.value = null }
}
const catalog = { personas: [{ id: "reviewer", name: { en: "Reviewer" }, icon: { cells: [] } }] }
const text = canonicalPersonaCatalog(catalog)
const version = createHash("sha256").update(text).digest("hex")
const fresh = () => Response.json({ view: "display-v1", version, catalog })
const unchanged = () => Response.json({ view: "display-v1", version, unchanged: true })

test("a reload validates the exact cached catalog with the authorized machine before showing it", async () => {
  const store = new Storage()
  const calls: unknown[] = []
  assert.deepEqual(await (await readPersonaDisplay(async (fields) => { calls.push(fields); return fresh() }, "account/device/machine", undefined, store)).json(), catalog)
  let answered = false
  const pending = readPersonaDisplay(async (fields) => {
    calls.push(fields)
    await new Promise((resolve) => setImmediate(resolve))
    answered = true
    return unchanged()
  }, "account/device/machine", undefined, store)
  assert.equal(answered, false)
  assert.deepEqual(await (await pending).json(), catalog)
  assert.deepEqual(calls, [{ view: "display-v1" }, { view: "display-v1", version }])
})

test("account, device and machine switches discard the one slot; tampering and missing storage cold-read", async () => {
  const store = new Storage()
  for (const scope of ["account-a/device-a/mac-a", "account-b/device-a/mac-a", "account-b/device-b/mac-a", "account-b/device-b/mac-b"]) {
    await readPersonaDisplay(async (fields) => { assert.deepEqual(fields, { view: "display-v1" }); return fresh() }, scope, undefined, store)
  }
  store.value = JSON.stringify({ scope: "account-b/device-b/mac-b", version, text: "tampered" })
  await readPersonaDisplay(async (fields) => { assert.deepEqual(fields, { view: "display-v1" }); return fresh() }, "account-b/device-b/mac-b", undefined, store)
  await readPersonaDisplay(async (fields) => { assert.deepEqual(fields, { view: "display-v1" }); return fresh() }, null, undefined, null)
  assert.ok(PERSONA_DISPLAY_CACHE_KEY)
})

test("a changed or removed private role replaces the catalog; authority failures never display a held catalog", async () => {
  const store = new Storage()
  await readPersonaDisplay(async () => fresh(), "scope", undefined, store)
  const nextText = canonicalPersonaCatalog({ personas: [] })
  const nextVersion = createHash("sha256").update(nextText).digest("hex")
  const changed = await readPersonaDisplay(async (fields) => {
    assert.equal(fields.version, version)
    return Response.json({ view: "display-v1", version: nextVersion, catalog: { personas: [] } })
  }, "scope", undefined, store)
  assert.deepEqual(await changed.json(), { personas: [] })
  const refused = await readPersonaDisplay(async () => Response.json({ error: "permission_denied" }, { status: 403 }), "scope", undefined, store)
  assert.equal(refused.status, 403)
  assert.equal(store.value, null)
})

test("corrupt and mismatched receipts are rejected once; cancellation and revocation clear the path", async () => {
  const store = new Storage()
  await readPersonaDisplay(async () => fresh(), "scope", undefined, store)
  let count = 0
  const invalid = await readPersonaDisplay(async () => { count++; return Response.json({ view: "display-v1", version: "0".repeat(64), unchanged: true }) }, "scope", undefined, store)
  assert.equal(invalid.status, 502)
  assert.equal(count, 1)
  assert.equal(store.value, null)
  const controller = new AbortController()
  await assert.rejects(readPersonaDisplay(async () => { controller.abort(); return fresh() }, "scope", controller.signal, store))
  assert.equal(store.value, null)
  clearPersonaDisplayCache(store)
  assert.equal(store.value, null)
})

test("a disabled or full browser cache still gives a fresh catalog and an old daemon keeps its own body", async () => {
  const broken = { getItem() { throw Error("disabled") }, setItem() { throw Error("full") }, removeItem() {} }
  assert.deepEqual(await (await readPersonaDisplay(async () => fresh(), "scope", undefined, broken)).json(), catalog)
  assert.deepEqual(await (await readPersonaDisplay(async () => Response.json(catalog), "scope", undefined, broken)).json(), catalog)
})


test("catalog hashes preserve HTML characters, Unicode separators and escaped text across readers", () => {
 const fixture = {license:"", personas:[{icon:{accent:"",cells:null},id:"example",name:{en:'A<&>\u2028\u2029 "quoted"\n',"zh-Hant":"角色"},source:"",suggested_kinds:null,summary:{en:"","zh-Hant":""},teams:null}]}
 assert.equal(createHash("sha256").update(canonicalPersonaCatalog(fixture)).digest("hex"), "9b9953cf5eeb72ef57532b2acd4bd71d080352606c9234f9def774c48b86bf88")
})

test("revocation while a read is finishing cannot repopulate persistent storage", async () => {
 const store = new Storage()
 const result = await readPersonaDisplay(async () => { clearPersonaDisplayCache(store); return fresh() }, "scope", undefined, store)
 assert.equal(result.status, 200)
 assert.equal(store.value, null)
})
