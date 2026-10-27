import { test } from "node:test"
import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
import { openVideoStream } from "../legacy/js/net/video-stream.js"
import { VIDEO_CHUNK_BYTES } from "../legacy/js/net/document-links.js"

const locator = { machine: "machine-a", session: "session-a", scope: "project", path: "clip.mp4" }
const fixture = readFileSync(new URL("./testdata/video-stream.mp4", import.meta.url))
// Generated colour/tone, with an unused mdat before its tail moov. The media
// offsets are unchanged; discovering metadata must skip that space, then return.
function movie() {
  let at = 0
  while (fixture.toString("ascii", at + 4, at + 8) !== "moov") at += fixture.readUInt32BE(at)
  const free = Buffer.alloc(VIDEO_CHUNK_BYTES * 3)
  free.writeUInt32BE(free.length); free.write("mdat", 4)
  return Buffer.concat([fixture.subarray(0, at), free, fixture.subarray(at)])
}
function reader(bytes: Buffer, asks: number[], action?: (offset: number) => void) {
  return async (offset: number, revision: string) => {
    asks.push(offset); action?.(offset)
    const data = bytes.subarray(offset, offset + VIDEO_CHUNK_BYTES)
    return { scope: "project", path: "clip.mp4", media_type: "video/mp4", total_bytes: bytes.length,
      offset, revision: revision || "r1", byte_count: data.length, data: data.toString("base64") }
  }
}

test("streaming discovers tail metadata without reading the intervening media and honors decoder backpressure", async () => {
  const bytes = movie(), asks: number[] = [], progress: number[] = []
  const answer = await openVideoStream(locator, reader(bytes, asks), { progress: (n: number) => progress.push(n) })
  assert.deepEqual(asks, [0])
  let release!: () => void
  const gate = new Promise<void>(resolve => { release = resolve })
  const parts: { id: number; buffer: ArrayBuffer }[] = []
  let initialized!: () => void
  const ready = new Promise<void>(resolve => { initialized = resolve })
  let ended = false
  const running = answer.stream({
    async initialize(tracks: { mime: string; buffer: ArrayBuffer }[], duration: number) {
      assert.equal(tracks.length, 2); assert.equal(duration, 2)
      assert.ok(tracks.every(t => t.buffer.byteLength > 0 && /^(video|audio)\/mp4; codecs="/.test(t.mime)))
      initialized(); await gate
    },
    async append(batch: typeof parts) { parts.push(...batch) },
    async finish() { ended = true },
  })
  await ready
  assert.deepEqual(asks, [0, VIDEO_CHUNK_BYTES * 3])
  assert.equal(ended, false)
  release(); await running
  assert.ok(parts.length > 0)
  assert.equal(ended, true)
  assert.deepEqual(asks, [0, VIDEO_CHUNK_BYTES * 3, VIDEO_CHUNK_BYTES, VIDEO_CHUNK_BYTES * 2])
  assert.equal(new Set(asks).size, asks.length)
  assert.equal(progress.at(-1), 100)
})

test("closing during a decoder acknowledgement prevents any further reads", async () => {
  const asks: number[] = [], controller = new AbortController()
  const answer = await openVideoStream(locator, reader(movie(), asks), { signal: controller.signal })
  await assert.rejects(answer.stream({ initialize: async () => controller.abort(), append: async () => {}, finish: async () => {} }), { name: "AbortError" })
  assert.deepEqual(asks, [0, VIDEO_CHUNK_BYTES * 3])
})

test("late revoked reads fail once without silently finishing or retrying", async () => {
  const asks: number[] = []
  const answer = await openVideoStream(locator, reader(movie(), asks, offset => {
    if (offset === VIDEO_CHUNK_BYTES) throw Object.assign(new Error("Revoked"), { code: "forbidden" })
  }))
  let finished = false
  await assert.rejects(answer.stream({ initialize: async () => {}, append: async () => {}, finish: async () => { finished = true } }), { code: "forbidden" })
  assert.deepEqual(asks, [0, VIDEO_CHUNK_BYTES * 3, VIDEO_CHUNK_BYTES])
  assert.equal(finished, false)
})

test("a decoder rejection and malformed media stop the transfer", async () => {
  const asks: number[] = []
  const answer = await openVideoStream(locator, reader(movie(), asks))
  await assert.rejects(answer.stream({ initialize: async () => { throw new Error("Decoder refused") } }), /Decoder refused/)
  assert.deepEqual(asks, [0, VIDEO_CHUNK_BYTES * 3])
  const broken = await openVideoStream(locator, reader(Buffer.from("not an mp4"), []))
  await assert.rejects(broken.stream({}), { code: "video_format_unsupported" })
})
