import test from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node strip-types runs the source module.
import { filterOutputs, outputCategory, outputSessionKey, outputChunk, outputListing, readOutput, shareOutputLink, type OutputFile } from "./output-files-api.ts"
const file: OutputFile = { id: "a".repeat(64), name: "plan.md", path: "reports/plan.md", absolute_path: "/workspace/reports/plan.md", kind: "created", first_at: 1, updated_at: 2, updates: 2, bytes: 5, state: "available", media_type: "text/markdown", source: "native", anchor: 100 }
const chunk = { id: file.id, name: file.name, media_type: file.media_type, total_bytes: 5, offset: 0, revision: "b".repeat(64), data: btoa("hello") }
test("output listing rejects mixed identities, unknown state and fabricated counts", () => {
  const listing = { files: [file], more: false, partial: false, coverage: "native_and_published" }
  assert.deepEqual(outputListing(listing), listing)
  for (const files of [[file, file], [{ ...file, updates: 0 }], [{ ...file, state: "complete" }], [{ ...file, id: "path/to/secret" }]]) assert.throws(() => outputListing({ ...listing, files }))
})
test("file search distinguishes paths and keeps media filters accurate", () => {
  const picture = { ...file, id: "c".repeat(64), name: "cover.png", path: "images/cover.png", media_type: "image/png" }
  assert.deepEqual(filterOutputs([file, picture], "all", "REPORTS/"), [file])
  assert.deepEqual(filterOutputs([file, picture], "images", ""), [picture])
  assert.deepEqual(filterOutputs([file, picture], "other", ""), [])
  assert.equal(outputCategory(file), "documents")
})
test("chunks bind ID, offset, revision and exact byte count", () => {
  assert.equal(new TextDecoder().decode(outputChunk(chunk, file, 0, "").bytes), "hello")
  for (const bad of [{ ...chunk, id: "c".repeat(64) }, { ...chunk, offset: 1 }, { ...chunk, data: btoa("cut") }, { ...chunk, total_bytes: 33 * 1024 * 1024 }]) assert.throws(() => outputChunk(bad, file, 0, ""))
  assert.throws(() => outputChunk(chunk, file, 0, "c".repeat(64)))
  assert.throws(() => outputChunk(chunk, file, 0, "", true))
})
test("file transfer reads exact registered IDs and stops on cancellation", async () => {
  const previous = globalThis.fetch
  const calls: string[] = []
  globalThis.fetch = async input => { calls.push(String(input)); return new Response(JSON.stringify(chunk), { status: 200 }) }
  try {
    const abort = new AbortController(), progress: number[] = []
    const blob = await readOutput("%19", file, abort.signal, p => progress.push(p))
    assert.equal(await blob.text(), "hello"); assert.deepEqual(progress, [100]); assert.match(calls[0], /^\/v1\/sessions\/%2519\/outputs\/[a-f0-9]{64}\?offset=0&revision=&size=$/)
    await assert.rejects(() => readOutput("%19", file, abort.signal, () => abort.abort()), { name: "AbortError" })
  } finally { globalThis.fetch = previous }
})

test("reusing a terminal or selecting another machine changes the output panel identity", () => {
  const row = {id:"%19",sessionId:"conversation-a",machine:"machine-a",execution_generation:"a".repeat(32)}
  assert.notEqual(outputSessionKey(row),outputSessionKey({...row,sessionId:"conversation-b"}))
  assert.notEqual(outputSessionKey(row),outputSessionKey({...row,machine:"machine-b"}))
})

test("a refused system share copies the exact pinned link, while cancellation stays cancelled", async () => {
  const link = "https://app.clawdline.com/#machine=machine-a&session=%2519&generation=" + "a".repeat(32) + "&output=" + file.id
  const copies: string[] = []
  const copy = async (text: string) => { copies.push(text) }
  assert.equal(await shareOutputLink(file.name, link, { share: async () => { throw new DOMException("Unavailable", "NotAllowedError") }, copy }), "copied")
  assert.deepEqual(copies, [link])
  copies.length = 0
  assert.equal(await shareOutputLink(file.name, link, { share: async data => { assert.deepEqual(data, { title: file.name, url: link }) }, copy }), "shared")
  assert.equal(await shareOutputLink(file.name, link, { share: async () => { throw new DOMException("Cancelled", "AbortError") }, copy }), "cancelled")
  assert.deepEqual(copies, [])
  assert.equal(await shareOutputLink(file.name, link, { copy }), "copied")
  await assert.rejects(() => shareOutputLink(file.name, link, { copy: async () => { throw new Error("Clipboard denied") } }), /Clipboard denied/)
})
