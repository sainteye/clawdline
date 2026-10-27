import { test } from "node:test"
import assert from "node:assert/strict"
import { videoChunkAnswer, loadVideo } from "../legacy/js/net/video.js"
import { VIDEO_CHUNK_BYTES, VIDEO_MAX_BYTES, documentHash, documentLocatorFromHash, documentListing } from "../legacy/js/net/document-links.js"

const locator = { machine: "machine-a", session: "session-a", scope: "project", path: "clip.mp4" }
function chunk(offset = 0, total = 3, revision = "rev-1") {
  const data = Buffer.alloc(Math.min(VIDEO_CHUNK_BYTES, total - offset), 23)
  return { scope: "project", path: locator.path, media_type: "video/mp4", total_bytes: total, offset,
    revision, byte_count: data.length, data: data.toString("base64") }
}

test("MP4 locators and listings keep their explicit Cloud identity and separate file cap", () => {
  assert.deepEqual(documentLocatorFromHash(documentHash(locator)), locator)
  assert.equal(documentListing({ documents: [{ scope: "project", path: "clip.mp4", bytes: VIDEO_MAX_BYTES, modified: 1 }] },
    { machine: locator.machine, session: locator.session }).documents[0].bytes, VIDEO_MAX_BYTES)
  assert.throws(() => documentListing({ documents: [{ scope: "project", path: "text.md", bytes: VIDEO_MAX_BYTES, modified: 1 }] },
    { machine: locator.machine, session: locator.session }))
})

test("video chunks reject mismatched identities, range, format, version and encoded size", () => {
  assert.equal(videoChunkAnswer(locator, 0, "", chunk()).bytes.length, 3)
  for (const change of [{ path: "other.mp4" }, { scope: "task" }, { task: "extra" }, { offset: 1 },
    { byte_count: 2 }, { total_bytes: VIDEO_MAX_BYTES + 1 }, { data: "%%%" }, { data: "Fx cX" },
    { revision: "" }, { media_type: "text/html" }, { extra: true }]) {
    assert.throws(() => videoChunkAnswer(locator, 0, "", { ...chunk(), ...change }), JSON.stringify(change))
  }
  assert.throws(() => videoChunkAnswer(locator, VIDEO_CHUNK_BYTES, "old", chunk(VIDEO_CHUNK_BYTES, VIDEO_CHUNK_BYTES + 3)))
})

test("video transfer requests sequential chunks once and assembles bounded bytes", async () => {
  const asks: [number, string][] = [], progress: number[] = []
  const total = VIDEO_CHUNK_BYTES + 7
  const result = await loadVideo(locator, async (offset: number, revision: string) => {
    asks.push([offset, revision]); return chunk(offset, total)
  }, { progress: (n: number) => progress.push(n) })
  assert.deepEqual(asks, [[0, ""], [VIDEO_CHUNK_BYTES, "rev-1"]])
  assert.equal(result.byte_count, total)
  assert.equal(result.blob.type, "video/mp4")
  assert.equal((await result.blob.arrayBuffer()).byteLength, total)
  assert.equal(progress.at(-1), 100)
})

test("navigation cancellation and a failed chunk never start another read or retry", async () => {
  const abort = new AbortController()
  let asks = 0
  await assert.rejects(loadVideo(locator, async () => {
    asks++; abort.abort(); return chunk(0, VIDEO_CHUNK_BYTES + 3)
  }, { signal: abort.signal }), { name: "AbortError" })
  assert.equal(asks, 1)
  asks = 0
  await assert.rejects(loadVideo(locator, async () => { asks++; throw new Error("revoked") }), /revoked/)
  assert.equal(asks, 1)
  asks = 0
  await assert.rejects(loadVideo(locator, async (offset: number) => {
    asks++; return chunk(offset, VIDEO_CHUNK_BYTES + (offset ? 4 : 3))
  }))
  assert.equal(asks, 2)
})
