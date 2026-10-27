import { test } from "node:test"
import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
import { createFile } from "mp4box"
import { openVideoPosters } from "../legacy/js/net/video-posters.js"
import { VIDEO_CHUNK_BYTES } from "../legacy/js/net/document-links.js"

const locator = { machine: "machine-a", session: "session-a", scope: "project", path: "clip.mp4" }
const fixture = readFileSync(new URL("./testdata/video-poster.mp4", import.meta.url))
function reader(asks: number[]) {
  return async (offset: number, revision: string) => {
    asks.push(offset)
    const bytes = fixture.subarray(offset, offset + VIDEO_CHUNK_BYTES)
    return { scope: "project", path: "clip.mp4", media_type: "video/mp4", total_bytes: fixture.length,
      offset, revision: revision || "r1", byte_count: bytes.length, data: bytes.toString("base64") }
  }
}
test("later random-access frames share one pinned read and omit audio", async () => {
  const asks: number[] = [], times: number[] = []
  const answer = await openVideoPosters(locator, reader(asks), { signal: new AbortController().signal, byteBudget: VIDEO_CHUNK_BYTES })
  await answer.frames(async (stream: any) => {
    let first = true
    await stream.stream({
      initialize: async (tracks: any[], _duration: number, target: number) => { assert.ok(target > 0); assert.equal(tracks.length, 1); assert.match(tracks[0].mime, /^video\//) },
      append: async (parts: any[]) => {
        if (!first) return
        first = false
        const parsed: any = createFile(false)
        const buffer = parts[0].buffer.slice(0); buffer.fileStart = 0
        parsed.appendBuffer(buffer)
        times.push(parsed.moofs[0].trafs[0].tfdt.baseMediaDecodeTime)
      },
      finish: async () => {},
    })
  })
  assert.equal(times.length, 4)
  assert.ok(times[0] > 0, "start from a later keyframe, rather than the opening frame")
  assert.ok(new Set(times).size > 1, "sample different parts of the movie")
  assert.deepEqual(asks, [0])
})
test("external cancellation stops subsequent candidates without another read", async () => {
  const asks: number[] = [], controller = new AbortController()
  const answer = await openVideoPosters(locator, reader(asks), { signal: controller.signal, byteBudget: VIDEO_CHUNK_BYTES })
  let count = 0
  await assert.rejects(answer.frames(async () => { count++; controller.abort() }), { name: "AbortError" })
  assert.equal(count, 1); assert.deepEqual(asks, [0])
})
test("a source read is refused before exceeding the shared candidate budget", async () => {
  const asks: number[] = []
  await assert.rejects(openVideoPosters(locator, reader(asks), { signal: new AbortController().signal, byteBudget: 0 }), { code: "video_preview_unavailable" })
  assert.deepEqual(asks, [])
})

test("a later budget refusal preserves a completed candidate and is not retried", async () => {
  const asks: number[] = []
  const answer = await openVideoPosters(locator, reader(asks), { signal: new AbortController().signal, byteBudget: VIDEO_CHUNK_BYTES })
  let count = 0
  await answer.frames(async () => {
    if (++count === 2) throw Object.assign(new Error("Budget exhausted"), { code: "video_preview_unavailable" })
  })
  assert.equal(count, 2); assert.deepEqual(asks, [0])
})
test("an authorization refusal is propagated even after a completed candidate", async () => {
  const answer = await openVideoPosters(locator, reader([]), { signal: new AbortController().signal, byteBudget: VIDEO_CHUNK_BYTES })
  let count = 0
  await assert.rejects(answer.frames(async () => {
    if (++count === 2) throw Object.assign(new Error("Revoked"), { code: "forbidden" })
  }), { code: "forbidden" })
  assert.equal(count, 2)
})

test("a sampling attempt cannot be replayed after its four candidates", async () => {
  const asks: number[] = []
  const answer = await openVideoPosters(locator, reader(asks), { signal: new AbortController().signal, byteBudget: VIDEO_CHUNK_BYTES })
  await answer.frames(async () => {})
  await assert.rejects(answer.frames(async () => assert.fail("must not sample again")), { code: "video_preview_unavailable" })
  assert.deepEqual(asks, [0])
})
