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
