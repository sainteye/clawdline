import assert from "node:assert/strict"
import test from "node:test"

// @ts-expect-error -- Node's strip-types tests import this module directly.
import { HandoverController, forgetHandovers, heldMasterKeyFor, loadHandover, takeIdentityPairing, saveHandover } from "./handover.ts"
// @ts-expect-error -- Node's strip-types tests import this module directly.
import { sealedKeyFor } from "./source.ts"
// @ts-expect-error -- Node's strip-types tests import this module directly.
import { IdentityPairingError } from "./identity-pair.ts"

// An IndexedDB with one object store, kept in a Map outside every "page", so a second controller
// and a second open read what the first wrote: the reload seam. Values are kept by reference, as
// a structured clone of a CryptoKey keeps it non-extractable.
function memoryIDB(rows = new Map<string, unknown>()) {
  ;(globalThis as Record<string, unknown>).IDBKeyRange ??= { bound: (lower: string, upper: string) => ({ lower, upper }) }
  const later = (fn: () => void) => setTimeout(fn, 0)
  const factory = {
    rows,
    open() {
      const db = {
        objectStoreNames: { contains: () => true },
        createObjectStore() {},
        close() {},
        transaction() {
          const tx: Record<string, any> = {}
          let pending = 0
          let aborted = false
          const commit = () => { if (!aborted && pending === 0) tx.oncomplete?.() }
          tx.abort = () => { aborted = true; tx.error = new Error("transaction aborted"); later(() => tx.onabort?.()) }
          const store = {
            put(value: unknown, key: string) { rows.set(key, value) },
            get(key: string) {
              const request: Record<string, any> = {}
              pending++
              later(() => { request.result = rows.get(key); request.onsuccess?.(); pending--; later(commit) })
              return request
            },
            delete(range: { lower: string; upper: string }) {
              for (const key of [...rows.keys()]) if (key >= range.lower && key <= range.upper) rows.delete(key)
            },
          }
          tx.objectStore = () => store
          later(commit)
          return tx
        },
      }
      const request: Record<string, any> = { result: db }
      later(() => request.onsuccess?.())
      return request
    },
  }
  return factory as unknown as IDBFactory & { rows: Map<string, unknown> }
}

async function keys() {
  const raw = crypto.getRandomValues(new Uint8Array(32))
  return {
    hkdfKey: await crypto.subtle.importKey("raw", raw, "HKDF", false, ["deriveKey", "deriveBits"]),
    masterKey: await crypto.subtle.importKey("raw", raw, "AES-GCM", false, ["encrypt", "decrypt"]),
  }
}

function context(idb: IDBFactory, over: Record<string, unknown> = {}) {
  return {
    apiOrigin: "https://api.test", fetch: async () => new Response("{}"), accountID: "usr_a", deviceID: "wd_1",
    signingPublicKey: new Uint8Array(32), currentAccount: () => "usr_a", indexedDB: idb, ...over,
  }
}

/** A pairing run that asks for the code, then persists what a verified confirm would. */
function fakePair(epoch = 1, account = "usr_a") {
  return async (_fragment: string, deps: any) => {
    if (!await deps.confirmCode("123456", "MACH-FP")) throw new IdentityPairingError("not_confirmed")
    if (!deps.isCurrent()) throw new IdentityPairingError("stale")
    const result = { accountID: account, machineID: "mac_1", machineFingerprint: "MACH-FP", contentKeyEpoch: epoch, identityEpoch: 0, ...await keys() }
    await deps.persist(result)
    return result
  }
}

async function until(controller: HandoverController, phase: string) {
  for (let i = 0; i < 200 && controller.state().phase !== phase; i++) await new Promise((r) => setTimeout(r, 1))
  assert.equal(controller.state().phase, phase)
}

test("keys are kept only after the code is confirmed, and survive a reload", async () => {
  const idb = memoryIDB()
  const controller = new HandoverController()
  const done = controller.run("frag", context(idb, { pair: fakePair(3) }))
  await until(controller, "comparing")
  assert.equal(idb.rows.size, 0, "nothing is stored while the person compares")
  controller.confirm(true)
  const final = await done
  assert.equal(final.phase, "done")

  // A reload: a fresh controller and a fresh open of the same database.
  const reloaded = memoryIDB(idb.rows)
  const record = await loadHandover("usr_a", "mac_1", reloaded)
  assert.ok(record)
  assert.equal(record.contentKeyEpoch, 3)
  assert.equal(record.hkdfKey.extractable, false)
  const held = await heldMasterKeyFor("usr_a", "mac_1", reloaded)
  const source = { heldMasterKey: async () => held } as any
  const opening = await sealedKeyFor(source, "mac_1", 3)
  assert.equal(opening.contentKeyEpoch, 3)
  await assert.rejects(sealedKeyFor(source, "mac_1", 4), (e: any) => e.code === "wrong_epoch")
})

test("a denied code keeps nothing", async () => {
  const idb = memoryIDB()
  const controller = new HandoverController()
  const done = controller.run("frag", context(idb, { pair: fakePair() }))
  await until(controller, "comparing")
  controller.confirm(false)
  assert.deepEqual(await done, { phase: "failed", code: "not_confirmed" })
  assert.equal(idb.rows.size, 0)
})

test("a browser with only a legacy AES pairing must hand over again", async () => {
  const held = await heldMasterKeyFor("usr_a", "mac_1", memoryIDB())
  assert.deepEqual(held, { key: null, contentKeyEpoch: null })
  await assert.rejects(sealedKeyFor({ heldMasterKey: async () => held } as any, "mac_1", 1),
    (e: any) => e.code === "key_handover_required")
})

test("signing out while the person compares stops the run and keeps nothing", async () => {
  const idb = memoryIDB()
  const controller = new HandoverController()
  const done = controller.run("frag", context(idb, { pair: fakePair() }))
  await until(controller, "comparing")
  controller.stop()
  assert.equal((await done).phase, "idle")
  assert.equal(idb.rows.size, 0)
})

test("an account switch during the write deletes what was written", async () => {
  const idb = memoryIDB()
  let account = "usr_a"
  const controller = new HandoverController()
  const pair = fakePair()
  const done = controller.run("frag", context(idb, {
    currentAccount: () => account,
    pair: async (fragment: string, deps: any) => pair(fragment, { ...deps, persist: async (r: any) => { account = "usr_b"; await deps.persist(r) } }),
  }))
  await until(controller, "comparing")
  controller.confirm(true)
  assert.equal((await done).phase, "idle")
  assert.equal(idb.rows.size, 0)
})

test("revocation forgets one account's handovers and no other's", async () => {
  const idb = memoryIDB()
  for (const account of ["usr_a", "usr_ab"]) {
    const controller = new HandoverController()
    const done = controller.run("frag", context(idb, { accountID: account, currentAccount: () => account, pair: fakePair(1, account) }))
    await until(controller, "comparing")
    controller.confirm(true)
    await done
  }
  assert.equal(idb.rows.size, 2)
  await forgetHandovers("usr_a", idb)
  assert.equal(await loadHandover("usr_a", "mac_1", idb), null)
  assert.ok(await loadHandover("usr_ab", "mac_1", idb))
})

test("the #pairid= link leaves the address as soon as it is read", () => {
  const kept = new Map<string, string>()
  let replaced = ""
  const scope = {
    location: { hash: "#pairid=abc", pathname: "/", search: "?x=1" },
    history: { state: null, replaceState: (_s: unknown, _u: string, url: string) => { replaced = url } },
    sessionStorage: { getItem: (k: string) => kept.get(k) ?? null, setItem: (k: string, v: string) => { kept.set(k, v) }, removeItem: (k: string) => { kept.delete(k) } },
  }
  assert.equal(takeIdentityPairing(scope), "abc")
  assert.equal(replaced, "/?x=1")
  scope.location.hash = ""
  assert.equal(takeIdentityPairing(scope), "abc", "kept through a sign-in redirect")
  assert.equal(takeIdentityPairing({ ...scope, location: { hash: "#pair=legacy", pathname: "/", search: "" } }), "abc")
})

test("diagnostic handovers retain their epoch floor and trusted machine key on reload", async () => {
  const idb = memoryIDB()
  const held = { accountID: "usr_a", machineID: "mac_1", machineFingerprint: "MACH-FP",
    contentKeyEpoch: 3, identityEpoch: 4, contentKeyID: "mk-3-" + "ab".repeat(32),
    machineSigningKey: "trusted-machine", ...await keys() }
  await saveHandover(held, 100, idb)
  for (const change of [
    { contentKeyEpoch: 2 }, { identityEpoch: 3 },
    { contentKeyID: "mk-3-" + "cd".repeat(32) }, { machineSigningKey: "substituted" },
  ]) {
    await assert.rejects(saveHandover({ ...held, ...change }, 200, idb))
    const reloaded = await loadHandover("usr_a", "mac_1", memoryIDB(idb.rows))
    assert.equal(reloaded?.contentKeyEpoch, 3)
    assert.equal(reloaded?.contentKeyID, held.contentKeyID)
    assert.equal(reloaded?.machineSigningKey, held.machineSigningKey)
  }
})
