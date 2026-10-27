import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error Node executes source extensions.
import { composerDraftKey, createComposerDrafts, type ComposerDraft, type DraftStorage } from "./composer-drafts.ts"

const row = (machine = "machine-a", sessionId = "conversation-a", execution_generation = "first") =>
  ({ id: "terminal", machine, sessionId, execution_generation }) as unknown as Parameters<typeof composerDraftKey>[0]
const empty = (): ComposerDraft => ({ text: "", images: [] })
function disk() {
  const rows = new Map<string, ComposerDraft>()
  const storage: DraftStorage = {
    read: async key => structuredClone(rows.get(key) ?? empty()),
    write: async (key, part, value) => {
      const draft = rows.get(key) ?? empty()
      Object.assign(draft, { [part]: structuredClone(value) })
      if (!draft.text && !draft.images.length) rows.delete(key)
      else rows.set(key, draft)
    },
  }
  return { rows, storage }
}

test("draft identity separates machines and conversations while keeping execution refreshes", () => {
  assert.notEqual(composerDraftKey(row()), composerDraftKey(row("machine-b")))
  assert.notEqual(composerDraftKey(row()), composerDraftKey(row("machine-a", "conversation-b")))
  assert.equal(composerDraftKey(row()), composerDraftKey(row("machine-a", "conversation-a", "resumed")))
  assert.equal(composerDraftKey(null), "")
})

test("leaving, reopening and reloading keep only that Session's text and pictures", async () => {
  const { storage, rows } = disk()
  const drafts = createComposerDrafts(storage)
  await drafts.text("a", "First line\n第二行")
  await drafts.images("a", ["data:image/png;base64,fixture"])
  await drafts.text("b", "Separate draft")
  const reloaded = createComposerDrafts(storage)
  assert.deepEqual(await reloaded.read("a"), { text: "First line\n第二行", images: ["data:image/png;base64,fixture"], durable: true })
  assert.equal((await reloaded.read("b")).text, "Separate draft")
  await reloaded.text("a", "")
  await reloaded.images("a", [])
  assert.equal(rows.has("a"), false)
  assert.equal((await reloaded.read("a")).text, "")
  assert.equal((await reloaded.read("b")).text, "Separate draft")
})

test("typing writes no picture bytes", async () => {
  const { storage } = disk()
  const calls: string[] = []
  const write = storage.write
  storage.write = (key, part, value) => { calls.push(part); return write(key, part, value) }
  const drafts = createComposerDrafts(storage)
  await drafts.images("a", ["data:image/png;base64,fixture"])
  await drafts.text("a", "h")
  await drafts.text("a", "hi")
  assert.deepEqual(calls, ["images", "text", "text"])
})

test("typing stays in memory and leaving flushes only the final text", async () => {
  const { storage } = disk()
  const calls: Array<string | string[]> = []
  const write = storage.write
  storage.write = (key, part, value) => { calls.push(value); return write(key, part, value) }
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
  let answer!: (value: ComposerDraft) => void
  const drafts = createComposerDrafts({
    read: () => new Promise(resolve => { answer = resolve }),
    write: () => new Promise(() => {}),
  })
  const reading = drafts.read("a")
  void drafts.text("a", "newer")
  answer({ text: "stale", images: [] })
  assert.equal((await reading).text, "newer")
})

test("storage refusal retains separate in-page drafts and reports the loss of durability", async () => {
  const refused = async () => { throw new Error("QuotaExceededError") }
  const drafts = createComposerDrafts({ read: refused, write: refused })
  await assert.rejects(drafts.text("a", "Keep this"))
  await assert.rejects(drafts.images("a", ["data:image/png;base64,fixture"]))
  await assert.rejects(drafts.text("b", "Other Session"))
  assert.deepEqual(await drafts.read("a"), { text: "Keep this", images: ["data:image/png;base64,fixture"], durable: false })
  await assert.rejects(drafts.text("a", ""))
  await assert.rejects(drafts.images("a", []))
  assert.deepEqual(await drafts.read("a"), { ...empty(), durable: false })
  assert.equal((await drafts.read("b")).text, "Other Session")
})
