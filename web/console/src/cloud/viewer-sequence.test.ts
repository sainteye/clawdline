// The outer envelope sequence a browser device signs with (`durableSequence`, copied
// `cloud-boot.js`), shared by every tab of that device: blocks are reserved in one IndexedDB
// transaction, and local storage only tells a tab that another one has moved far ahead:
// `node --test web/console/src/cloud/viewer-sequence.test.ts`.
//
// On 2026-10-06 (21:01) a reloaded Cloud terminal page typed a dozen keys, then the machine
// dropped every later envelope as `reason=replay` from seq 26112 (a 64-block boundary): another
// app.clawdline.com tab of the same device had reserved that block, and each tab, on running out
// of its own block, reserved the next one counted from its own last number instead of from the
// ceiling the other tab had written.
import { test } from "node:test"
import assert from "node:assert/strict"
import { durableSequence } from "../legacy/js/net/cloud-boot.js"

/** One device's local storage, every tab seeing every write at once. */
function storage() {
  const values = new Map<string, string>()
  return { getItem: (key: string) => values.get(key) ?? null, setItem: (key: string, value: string) => { values.set(key, value) } }
}

/**
 * One device's local storage as browsers actually share it between tabs: each tab reads its own
 * copy, and another tab's write reaches it only when `deliver()` runs (Chromium and WebKit hand a
 * write to other processes asynchronously; there is no storage mutex).
 */
function staleStorage() {
  const copies: Map<string, string>[] = []
  const queue: [Map<string, string>, string, string][] = []
  return {
    tab() {
      const copy = new Map(copies[0] ?? [])
      copies.push(copy)
      return { getItem: (key: string) => copy.get(key) ?? null,
        setItem: (key: string, value: string) => { copy.set(key, value); for (const other of copies) if (other !== copy) queue.push([other, key, value]) } }
    },
    deliver() { for (const [copy, key, value] of queue.splice(0)) copy.set(key, value) },
  }
}

/**
 * The part of IndexedDB the sequence uses, with the one property it relies on: readwrite
 * transactions over the same store run one after another, and each sees what the last committed.
 */
function indexedDB() {
  const stores = new Map<string, Map<string, Map<IDBValidKey, unknown>>>()
  let queue: Promise<void> = Promise.resolve()
  const later = (run: () => void) => { setTimeout(run, 0) }
  const factory = {
    open(name: string) {
      const request: Record<string, any> = {}
      later(() => {
        const fresh = !stores.has(name)
        if (fresh) stores.set(name, new Map())
        const tables = stores.get(name)!
        const db = {
          objectStoreNames: { contains: (store: string) => tables.has(store) },
          createObjectStore: (store: string) => { tables.set(store, new Map()) },
          close() {},
          transaction(store: string) {
            const tx: Record<string, any> = {}
            const table = tables.get(store)!
            const steps: (() => void)[] = []
            tx.objectStore = () => ({
              get(key: IDBValidKey) { const req: Record<string, any> = {}; steps.push(() => { req.result = table.get(key); req.onsuccess?.() }); return req },
              put(value: unknown, key: IDBValidKey) { const req: Record<string, any> = {}; steps.push(() => { table.set(key, value); req.onsuccess?.() }); return req },
            })
            queue = queue.then(() => new Promise<void>((done) => {
              const drain = () => later(() => {
                if (steps.length) { steps.shift()!(); drain() } else { tx.oncomplete?.(); done() }
              })
              drain()
            }))
            return tx
          },
        }
        request.result = db
        if (fresh) request.onupgradeneeded?.()
        request.onsuccess?.()
      })
      return request
    },
  }
  return factory as unknown as IDBFactory
}

/** The machine's per-sender window (`internal/domain/cloud/replay.go`): a number is taken once, and none 1024 below the highest. */
function machine() {
  let highest = -1
  const seen = new Set<number>()
  return (seq: number): boolean => {
    if (seen.has(seq) || (highest >= 0 && seq < highest - 1024)) return false
    seen.add(seq); highest = Math.max(highest, seq)
    return true
  }
}

type Tab = () => number | Promise<number>

test("two tabs of one device, one of them reloaded, never sign the same sequence", async () => {
  const shared = storage(), db = indexedDB()
  const claim = machine()
  const other = durableSequence(shared, "seq", db)
  const send = async (tab: Tab, what: string) => { const seq = await tab(); assert.ok(claim(seq), `${what} seq ${seq} was dropped as a replay`) }
  for (let i = 0; i < 10; i++) await send(other, "the other tab")
  let page = durableSequence(shared, "seq", db)
  for (let i = 0; i < 30; i++) await send(page, "the terminal page")
  page = durableSequence(shared, "seq", db) // reloaded
  for (let round = 0; round < 300; round++) {
    await send(page, "the reloaded page")
    if (round % 3 === 0) await send(other, "the other tab")
  }
})

test("a tab that sat idle while another sent past the machine's window is not left below it", async () => {
  const shared = storage(), db = indexedDB()
  const claim = machine()
  const idle = durableSequence(shared, "seq", db)
  assert.ok(claim(await idle()))
  const busy = durableSequence(shared, "seq", db)
  for (let i = 0; i < 2000; i++) assert.ok(claim(await busy()))
  const seq = await idle()
  assert.ok(claim(seq), `the idle tab's seq ${seq} fell off the machine's window`)
})

test("a reload continues above every number the page before it handed out", async () => {
  const shared = storage(), db = indexedDB()
  const claim = machine()
  let page = durableSequence(shared, "seq", db)
  for (let load = 0; load < 5; load++) {
    for (let i = 0; i < 70; i++) assert.ok(claim(await page()))
    page = durableSequence(shared, "seq", db)
  }
})

// 2026-10-06: two tabs reserving in the same moment both read one ceiling from local storage
// and handed out the same block (a node model of Chromium's asynchronous cross-tab storage).
test("two tabs reserving at the same moment never hand out the same number", async () => {
  const shared = staleStorage(), db = indexedDB()
  const a = durableSequence(shared.tab(), "seq", db), b = durableSequence(shared.tab(), "seq", db)
  const handed: number[] = []
  // Both boot before either write reaches the other, then cross block boundaries together.
  for (let round = 0; round < 200; round++) {
    handed.push(...await Promise.all([a(), b()]))
    if (round % 7 === 0) shared.deliver()
  }
  const repeated = handed.filter((seq, at) => handed.indexOf(seq) !== at)
  assert.deepEqual(repeated, [], `numbers handed out twice: ${repeated.slice(0, 8).join(",")}`)
})

test("a tab crossing its block on a stale view of the other tab's ceiling does not reuse it", async () => {
  const shared = staleStorage(), db = indexedDB()
  const a = durableSequence(shared.tab(), "seq", db), b = durableSequence(shared.tab(), "seq", db)
  const handed: number[] = []
  handed.push(await a()); shared.deliver()
  handed.push(await b()); shared.deliver()
  handed.push(await a()) // not delivered to b
  for (let i = 0; i < 130; i++) handed.push(await b())
  shared.deliver()
  for (let i = 0; i < 70; i++) handed.push(await a(), await b())
  const repeated = handed.filter((seq, at) => handed.indexOf(seq) !== at)
  assert.deepEqual(repeated, [], `numbers handed out twice: ${repeated.slice(0, 8).join(",")}`)
})

test("a device with no IndexedDB refuses to number an envelope rather than guess", async () => {
  const next = durableSequence(storage(), "seq", null)
  await assert.rejects(async () => next(), (error: { code?: string }) => error.code === "no_sequence_store")
})

test("the first reservation continues above a ceiling an older page left in local storage", async () => {
  const shared = storage(), db = indexedDB()
  shared.setItem("seq", "26176")
  const page = durableSequence(shared, "seq", db)
  assert.ok(await page() >= 26176)
})
