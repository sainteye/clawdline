import assert from "node:assert/strict"
import test from "node:test"
import { RefusalError } from "@clawdline/core/refusal"
// @ts-expect-error -- `.ts` paths let Node's strip-types runner execute this test.
import { CLOUD_BUSY_BACKOFF_MS, createReferenceImageLoader, referenceImageURL } from "./reference-images.ts"

/** A fetch whose answers the test hands out one at a time, counting what is open. */
function heldFetch() {
  const open: { url: string; answer: (response: Response) => void }[] = []
  const urls: string[] = []
  let inFlight = 0
  let most = 0
  const fetch = (url: string) => {
    urls.push(url)
    inFlight++
    most = Math.max(most, inFlight)
    return new Promise<Response>((resolve) => {
      open.push({ url, answer: (response) => { inFlight--; resolve(response) } })
    })
  }
  return { fetch, open, urls, most: () => most }
}

const png = () => new Response(new Uint8Array([137, 80, 78, 71]), { status: 200, headers: { "content-type": "image/png" } })
const busy = () => new Response(JSON.stringify({ error: { code: "cloud_read_busy", message: "the channel is full", layer: "mac_transport" } }),
  { status: 429, headers: { "content-type": "application/json" } })
const settle = () => new Promise((resolve) => setImmediate(resolve))

test("the thumbnail route asks for the small copy, and the original asks for nothing extra", () => {
  assert.equal(referenceImageURL("img-1", "thumb"), "/v1/work/v2/images/img-1?size=thumb")
  assert.equal(referenceImageURL("img-1", "full"), "/v1/work/v2/images/img-1")
  assert.equal(referenceImageURL("a/b c", "thumb"), "/v1/work/v2/images/a%2Fb%20c?size=thumb")
})

test("through Cloud a page never has more than two picture reads in flight", async () => {
  const held = heldFetch()
  const loader = createReferenceImageLoader({ limited: true, fetch: held.fetch, sleep: async () => {} })
  const loads = ["a", "b", "c", "d", "e"].map((id) => loader.load(id, "thumb"))
  await settle()
  assert.equal(held.open.length, 2)
  while (held.open.length) {
    held.open.shift()!.answer(png())
    await settle()
    assert.ok(held.open.length <= 2, `${held.open.length} reads were open at once`)
  }
  await Promise.all(loads)
  assert.equal(held.most(), 2)
  assert.equal(held.urls.length, 5)
})

test("read from the machine itself nothing is held back", async () => {
  const held = heldFetch()
  const loader = createReferenceImageLoader({ limited: false, fetch: held.fetch })
  const loads = ["a", "b", "c", "d", "e"].map((id) => loader.load(id, "thumb"))
  await settle()
  assert.equal(held.open.length, 5)
  for (const read of held.open.splice(0)) read.answer(png())
  await Promise.all(loads)
})

test("a read refused cloud_read_busy is tried after 1 s, 2 s and 4 s, and then the failure is the machine's", async () => {
  const waits: number[] = []
  let tries = 0
  const loader = createReferenceImageLoader({
    limited: true,
    fetch: async () => { tries++; return busy() },
    sleep: async (ms) => { waits.push(ms) },
  })
  await assert.rejects(loader.load("img-1", "thumb"), (error: unknown) =>
    error instanceof RefusalError && error.code === "cloud_read_busy" && error.status === 429)
  assert.deepEqual(waits, [1000, 2000, 4000])
  assert.deepEqual(waits, [...CLOUD_BUSY_BACKOFF_MS])
  assert.equal(tries, 4)
})

test("a busy read that clears on a retry answers the picture", async () => {
  const waits: number[] = []
  const answers = [busy(), busy(), png()]
  const loader = createReferenceImageLoader({ limited: true, fetch: async () => answers.shift()!, sleep: async (ms) => { waits.push(ms) } })
  const blob = await loader.load("img-1", "thumb")
  assert.equal(blob.size, 4)
  assert.deepEqual(waits, [1000, 2000])
})

test("a refusal that is not busy is not tried again", async () => {
  const waits: number[] = []
  let tries = 0
  const loader = createReferenceImageLoader({
    limited: true,
    fetch: async () => {
      tries++
      return new Response(JSON.stringify({ error: { code: "not_found", message: "no such picture" } }), { status: 404 })
    },
    sleep: async (ms) => { waits.push(ms) },
  })
  await assert.rejects(loader.load("img-1", "thumb"), (error: unknown) => error instanceof RefusalError && error.code === "not_found")
  assert.equal(tries, 1)
  assert.equal(waits.length, 0)
  // A 429 with another code is somebody else's limit, not the reply channel.
  tries = 0
  const other = createReferenceImageLoader({
    limited: true,
    fetch: async () => { tries++; return new Response(JSON.stringify({ error: "rate_limited" }), { status: 429 }) },
    sleep: async (ms) => { waits.push(ms) },
  })
  await assert.rejects(other.load("img-1", "thumb"), /rate_limited/)
  assert.equal(tries, 1)
  assert.deepEqual(waits, [])
})

test("read from the machine itself a busy answer is not retried either", async () => {
  let tries = 0
  const loader = createReferenceImageLoader({ limited: false, fetch: async () => { tries++; return busy() }, sleep: async () => {} })
  await assert.rejects(loader.load("img-1", "thumb"), /cloud_read_busy/)
  assert.equal(tries, 1)
})

test("a card that goes away leaves the queue and frees nobody's slot", async () => {
  const held = heldFetch()
  const loader = createReferenceImageLoader({ limited: true, fetch: held.fetch, sleep: async () => {} })
  const first = loader.load("a", "thumb")
  const second = loader.load("b", "thumb")
  const stop = new AbortController()
  const gone = loader.load("c", "thumb", stop.signal)
  const fourth = loader.load("d", "thumb")
  await settle()
  stop.abort()
  await assert.rejects(gone, { name: "AbortError" })
  held.open.shift()!.answer(png())
  await settle()
  assert.deepEqual(held.urls, ["/v1/work/v2/images/a?size=thumb", "/v1/work/v2/images/b?size=thumb", "/v1/work/v2/images/d?size=thumb"])
  for (const read of held.open.splice(0)) read.answer(png())
  await Promise.all([first, second, fourth])
})
