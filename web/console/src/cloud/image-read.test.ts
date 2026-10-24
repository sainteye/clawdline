import { test } from "node:test"
import assert from "node:assert/strict"
import { CloudClient } from "../legacy/js/net/cloud-client.js"

// The Go image route and Cloud answer both preserve a photograph as JPEG.
// Exercise the copied client's public image read, where the phone rejected it.
function clientWithAnswer(body: Record<string, unknown>) {
  const client = new CloudClient({ relayURL: "wss://relay.example.test/v1/connect", deviceToken: "test" }) as any
  client._read = async () => body
  return client
}

const identity = { machine: "machine-1", session: "session-1" }
const id = "image-1"

test("a JPEG artifact crosses Cloud with its bytes and media type", async () => {
  const bytes = [0xff, 0xd8, 0xff, 0xd9]
  const client = clientWithAnswer({ id, media_type: "image/jpeg", byte_count: bytes.length,
    data: Buffer.from(bytes).toString("base64") })
  const answer = await client.image(identity, id)
  assert.equal(answer.media_type, "image/jpeg")
  assert.deepEqual([...answer.bytes], bytes)
})

test("a PNG artifact still crosses Cloud", async () => {
  const bytes = [137, 80, 78, 71]
  const client = clientWithAnswer({ id, media_type: "image/png", byte_count: bytes.length,
    data: Buffer.from(bytes).toString("base64") })
  assert.equal((await client.image(identity, id)).media_type, "image/png")
})

test("an unsupported image type remains a typed bad payload", async () => {
  const client = clientWithAnswer({ id, media_type: "image/svg+xml", byte_count: 4,
    data: Buffer.from("<svg").toString("base64") })
  await assert.rejects(client.image(identity, id), { code: "bad_payload" })
})


// Exercise the tile with events, independent of React's markup or a browser's network.
import { connectArtifactTile, releaseArtifactTile } from "../legacy/js/view/transcript-images.js"

class ImageNode extends EventTarget {
  hidden = true
  src = ""
  textContent = ""
  attrs = new Map<string, string>()
  setAttribute(key: string, value: string) { this.attrs.set(key, value) }
  removeAttribute(key: string) { this.attrs.delete(key); if (key === "src") this.src = "" }
}
class ImageTile extends ImageNode {
  disabled = true
  dataset: Record<string, string> = {}
  image = new ImageNode()
  status = new ImageNode()
  querySelector(selector: string) { return selector === ".message-image" ? this.image : this.status }
}
const picture = { id: "12000000-0000-4000-8000-000000000001", media_type: "image/png", byte_count: 100,
  width: 1600, height: 900, expires_at: 4102444800 }
const tick = () => new Promise<void>((resolve) => setImmediate(resolve))

test("a picture waits until visible, reads a thumbnail, and reads its original only on a press", async () => {
  const descriptor = Object.getOwnPropertyDescriptor(globalThis, "IntersectionObserver")
  let visible!: (entries: { isIntersecting: boolean }[]) => void
  let disconnected = false
  Object.defineProperty(globalThis, "IntersectionObserver", { configurable: true, value: class {
    constructor(callback: typeof visible) { visible = callback }
    observe() {}
    disconnect() { disconnected = true }
  } })
  try {
    const tile = new ImageTile()
    const reads: string[] = []
    const opened: string[] = []
    const released: string[] = []
    const source = (name: string) => async () => {
      reads.push(name)
      return { url: "blob:" + name, release: () => released.push(name) }
    }
    const binding = connectArtifactTile(tile, picture, { lazy: true, source: source("thumb"),
      fullSource: source("original"), open: (_tile: unknown, url: string) => opened.push(url) })
    await tick()
    assert.deepEqual(reads, [])
    visible([{ isIntersecting: false }]); await tick()
    assert.deepEqual(reads, [])
    visible([{ isIntersecting: true }]); await binding.carried
    assert.equal(disconnected, true)
    assert.deepEqual(reads, ["thumb"])
    tile.image.dispatchEvent(new Event("load"))
    tile.dispatchEvent(new Event("click")); await tick()
    assert.deepEqual(reads, ["thumb", "original"])
    assert.deepEqual(opened, ["blob:original"])
    releaseArtifactTile(tile)
    assert.deepEqual(released, ["thumb", "original"])
  } finally {
    if (descriptor) Object.defineProperty(globalThis, "IntersectionObserver", descriptor)
    else Reflect.deleteProperty(globalThis, "IntersectionObserver")
  }
})

test("a removed picture releases a late answer and a failed read can be retried", async () => {
  const gone = new ImageTile()
  const released: string[] = []
  let resolve!: (value: { url: string; release: () => void }) => void
  const late = connectArtifactTile(gone, picture, { source: () => new Promise((r) => { resolve = r }) })
  await tick()
  releaseArtifactTile(gone)
  resolve({ url: "blob:late", release: () => released.push("late") })
  await late.carried
  assert.deepEqual(released, ["late"])
  assert.equal(gone.image.src, "")

  const tile = new ImageTile()
  let count = 0
  const bound = connectArtifactTile(tile, picture, { source: async () => {
    if (++count === 1) throw Object.assign(new Error("busy"), { code: "cloud_read_busy" })
    return { url: "blob:retry" }
  }, describeFailure: () => "Try again" })
  await bound.carried
  assert.equal(tile.dataset.imageState, "unavailable")
  assert.equal(tile.disabled, false)
  tile.dispatchEvent(new Event("click")); await tick()
  assert.equal(tile.image.src, "blob:retry")
  tile.image.dispatchEvent(new Event("load"))
  assert.equal(tile.dataset.imageState, "live")
})
