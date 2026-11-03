import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error Node executes source extensions.
import { browserDraftStorage, composerDraftKey, createComposerDrafts, type DraftStorage } from "./composer-drafts.ts"

const row = (machine = "machine-a", sessionId = "conversation-a", execution_generation = "first") =>
  ({ id: "terminal", machine, sessionId, execution_generation }) as unknown as Parameters<typeof composerDraftKey>[0]
function disk() {
  const rows = new Map<string, string>()
  const storage: DraftStorage = {
    read: async key => rows.get(key) ?? "",
    write: async (key, text) => {
      if (text) rows.set(key, text)
      else rows.delete(key)
    },
  }
  return { rows, storage }
}

test("a text draft remains readable while IndexedDB cannot open", async () => {
  const idb = Object.getOwnPropertyDescriptor(globalThis, "indexedDB")
  const local = Object.getOwnPropertyDescriptor(globalThis, "localStorage")
  let opens = 0
  Object.defineProperty(globalThis, "indexedDB", { configurable: true, value: {
    open() { opens++; throw new Error("Injected IndexedDB failure") },
  } })
  Object.defineProperty(globalThis, "localStorage", { configurable: true, value: {
    getItem: () => "Existing text", setItem() {}, removeItem() {},
  } })
  try {
    const drafts = createComposerDrafts(browserDraftStorage())
    assert.equal((await drafts.read("a")).text, "Existing text")
    await drafts.text("a", "Updated text")
    assert.equal(opens, 0, "opening and editing a composer never touches IndexedDB")
  } finally {
    if (idb) Object.defineProperty(globalThis, "indexedDB", idb)
    else Reflect.deleteProperty(globalThis, "indexedDB")
    if (local) Object.defineProperty(globalThis, "localStorage", local)
    else Reflect.deleteProperty(globalThis, "localStorage")
  }
})

test("draft identity separates machines and conversations while keeping execution refreshes", () => {
  assert.notEqual(composerDraftKey(row()), composerDraftKey(row("machine-b")))
  assert.notEqual(composerDraftKey(row()), composerDraftKey(row("machine-a", "conversation-b")))
  assert.equal(composerDraftKey(row()), composerDraftKey(row("machine-a", "conversation-a", "resumed")))
  assert.equal(composerDraftKey(null), "")
})

test("leaving, reopening and reloading restore and clear only that Session's text", async () => {
  const { storage, rows } = disk()
  const drafts = createComposerDrafts(storage)
  await drafts.text("a", "First line\n第二行")
  await drafts.text("b", "Separate draft")
  const reloaded = createComposerDrafts(storage)
  assert.deepEqual(await reloaded.read("a"), { text: "First line\n第二行", durable: true })
  assert.equal((await reloaded.read("b")).text, "Separate draft")
  await reloaded.text("a", "")
  assert.equal(rows.has("a"), false)
  assert.equal((await reloaded.read("a")).text, "")
  assert.equal((await reloaded.read("b")).text, "Separate draft")
})

test("typing stays in memory and leaving flushes only the final text", async () => {
  const { storage } = disk()
  const calls: Array<string | string[]> = []
  const write = storage.write
  storage.write = (key, value) => { calls.push(value); return write(key, value) }
  const drafts = createComposerDrafts(storage)
  for (let i = 1; i <= 1000; i++) drafts.stageText("a", "x".repeat(i))
  assert.equal(calls.length, 0, "one thousand input events do no persistent writes")
  assert.equal((await drafts.read("a")).text.length, 1000)
  await drafts.flushText("a")
  assert.equal(calls.length, 1)
  assert.equal(calls[0].length, 1000)
  await drafts.flushText("a")
  assert.equal(calls.length, 1, "unchanged background and exit events do no extra writes")
})

test("a delayed read cannot replace newer unsaved edits", async () => {
  let answer!: (value: string) => void
  const drafts = createComposerDrafts({
    read: () => new Promise(resolve => { answer = resolve }),
    write: () => new Promise(() => {}),
  })
  const reading = drafts.read("a")
  void drafts.text("a", "newer")
  answer("stale")
  assert.equal((await reading).text, "newer")
})

test("storage refusal retains separate in-page drafts and reports the loss of durability", async () => {
  const refused = async () => { throw new Error("QuotaExceededError") }
  const drafts = createComposerDrafts({ read: refused, write: refused })
  await assert.rejects(drafts.text("a", "Keep this"))
  await assert.rejects(drafts.text("b", "Other Session"))
  assert.deepEqual(await drafts.read("a"), { text: "Keep this", durable: false })
  await assert.rejects(drafts.text("a", ""))
  assert.deepEqual(await drafts.read("a"), { text: "", durable: false })
  assert.equal((await drafts.read("b")).text, "Other Session")
})
