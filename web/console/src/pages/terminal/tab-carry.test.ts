import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- Node's strip-types runner loads this module directly.
import { TAB_CARRY_KEY, carriedClientID, type TabCarryScope } from "./tab-carry.ts"

function tab(stored: Map<string, string> = new Map()) {
  const listeners = new Map<string, Array<(event: { persisted?: boolean }) => void>>()
  const scope: TabCarryScope = {
    sessionStorage: { getItem: (key) => stored.get(key) ?? null, setItem: (key, value) => { stored.set(key, value) },
      removeItem: (key) => { stored.delete(key) } },
    addEventListener: (type, listener) => listeners.set(type, [...(listeners.get(type) ?? []), listener]),
  }
  const fire = (type: string, event: { persisted?: boolean } = {}) => { for (const listener of listeners.get(type) ?? []) listener(event) }
  return { scope, stored, fire }
}
let n = 0
const fresh = () => "tab-" + String(++n).padStart(24, "0")

test("a reloaded page in the same tab keeps its lease id", () => {
  const first = tab()
  const id = carriedClientID(first.scope, fresh)
  first.fire("pagehide")
  const reloaded = tab(first.stored)
  assert.equal(carriedClientID(reloaded.scope, fresh), id)
})

test("a tab duplicated while the page is open gets an id of its own", () => {
  const first = tab()
  const id = carriedClientID(first.scope, fresh)
  // Duplicating copies session storage as it is now: nothing is in it while the page lives.
  const copy = tab(new Map(first.stored))
  assert.notEqual(carriedClientID(copy.scope, fresh), id)
})

test("a page back from the back-forward cache takes its id out again", () => {
  const first = tab()
  carriedClientID(first.scope, fresh)
  first.fire("pagehide", { persisted: true })
  first.fire("pageshow", { persisted: true })
  assert.equal(first.stored.has(TAB_CARRY_KEY), false)
})

test("a malformed or unreadable carried id is replaced", () => {
  assert.match(carriedClientID(tab(new Map([[TAB_CARRY_KEY, "x"]])).scope, fresh), /^tab-[0-9]{24}$/)
  const broken = { sessionStorage: { getItem() { throw new Error("denied") }, setItem() {}, removeItem() {} }, addEventListener() {} } as TabCarryScope
  assert.match(carriedClientID(broken, fresh), /^tab-/)
  assert.match(carriedClientID(undefined, fresh), /^tab-/)
})
