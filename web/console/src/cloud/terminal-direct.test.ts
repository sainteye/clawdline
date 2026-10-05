import assert from "node:assert/strict"
import test from "node:test"
import { base64Bytes } from "../legacy/js/net/cloud-crypto.js"
// @ts-expect-error -- Node's strip-types runner loads the TypeScript source.
import { DIRECT_CHUNK_MS, DIRECT_CHUNK_UNITS, DIRECT_LABEL, DIRECT_MESSAGE_BYTES, DIRECT_NEGOTIATE_MS, DIRECT_STUN, DirectLink, DirectReassembler, directMessages, sealDirectOffer } from "./terminal-direct.ts"
import type { DirectChannelLike, DirectPeerLike } from "./terminal-direct.js"
import type { TerminalEnvelope } from "./terminal-transport.js"

const envelope = (ct = "c"): TerminalEnvelope => ({ v: 1, ch: "term/machine_test/viewer_test/AAAAAAAAAAAAAAAAAAAAAA", seq: 1, ts: 1,
  class: "stream", key_id: "rk-AAAAAAAAAAAAAAAAAAAAAA", nonce: "n", ct, sender: "machine_test", sig: "s" })
const chunk = (id: number, i: number, n: number, d: string) => JSON.stringify({ t: "chunk", id, i, n, d })

class FakeChannel implements DirectChannelLike {
  readyState = "connecting"
  sent: string[] = []
  closed = false
  onopen: ((event?: unknown) => void) | null = null
  onclose: ((event?: unknown) => void) | null = null
  onerror: ((event?: unknown) => void) | null = null
  onmessage: ((event: { data: unknown }) => void) | null = null
  send(data: string): void { this.sent.push(data) }
  close(): void { this.closed = true; this.readyState = "closed" }
  open(): void { this.readyState = "open"; this.onopen?.() }
  deliver(data: unknown): void { this.onmessage?.({ data }) }
}
class FakePeer implements DirectPeerLike {
  localDescription: { sdp: string } | null = null
  iceGatheringState = "new"
  connectionState = "new"
  channel!: FakeChannel
  label = ""
  remote = ""
  closed = false
  autoOpen = true
  config: unknown
  onicegatheringstatechange: ((event?: unknown) => void) | null = null
  onconnectionstatechange: ((event?: unknown) => void) | null = null
  constructor(config: unknown) { this.config = config }
  createDataChannel(label: string): DirectChannelLike { this.label = label; this.channel = new FakeChannel(); return this.channel }
  async createOffer(): Promise<{ type: string; sdp?: string }> { return { type: "offer", sdp: "v=0 offer" } }
  async setLocalDescription(description: { type: string; sdp?: string }): Promise<void> {
    this.localDescription = { sdp: description.sdp ?? "" }
    queueMicrotask(() => { this.iceGatheringState = "complete"; this.onicegatheringstatechange?.() })
  }
  async setRemoteDescription(description: { type: "answer"; sdp: string }): Promise<void> {
    this.remote = description.sdp
    if (this.autoOpen) queueMicrotask(() => this.channel.open())
  }
  close(): void { this.closed = true }
}

test("a small envelope is one env message and a large one is contiguous ordered chunks", () => {
  assert.deepEqual(directMessages(envelope(), 0).map((text: string) => JSON.parse(text)), [{ t: "env", e: envelope() }])
  const big = envelope("x".repeat(DIRECT_CHUNK_UNITS * 2 + 10))
  const pieces = directMessages(big, 7).map((text: string) => JSON.parse(text))
  assert.equal(pieces.length, 3)
  assert.deepEqual(pieces.map((piece: { id: number; i: number; n: number }) => [piece.id, piece.i, piece.n]), [[7, 0, 3], [7, 1, 3], [7, 2, 3]])
  assert.ok(pieces.every((piece: { d: string }) => piece.d.length <= DIRECT_CHUNK_UNITS))
  assert.equal(pieces.map((piece: { d: string }) => piece.d).join(""), JSON.stringify({ t: "env", e: big }))
})

test("chunk reassembly rebuilds the envelope a sender split", () => {
  const big = envelope("y".repeat(DIRECT_CHUNK_UNITS * 3))
  const reassembler = new DirectReassembler(() => assert.fail("no timeout"))
  const pieces = directMessages(big, 1)
  for (const piece of pieces.slice(0, -1)) assert.equal(reassembler.push(piece), null)
  assert.deepEqual(reassembler.push(pieces.at(-1)), big)
  assert.deepEqual(reassembler.push(directMessages(envelope(), 2)[0]), envelope())
})

test("every DC message violation is refused", () => {
  const text = JSON.stringify({ t: "env", e: envelope() })
  const half = Math.floor(text.length / 2)
  const cases: Array<[string, unknown[]]> = [
    ["a piece out of order", [chunk(1, 1, 2, text.slice(half))]],
    ["a skipped piece", [chunk(1, 0, 3, text.slice(0, 5)), chunk(1, 2, 3, text.slice(5))]],
    ["a piece of another id before the message completes", [chunk(1, 0, 2, text.slice(0, half)), chunk(2, 1, 2, text.slice(half))]],
    ["another message inside a chunked one", [chunk(1, 0, 2, text.slice(0, half)), text]],
    ["a piece whose count changes", [chunk(1, 0, 2, text.slice(0, half)), chunk(1, 1, 3, text.slice(half))]],
    ["a piece longer than its bound", [chunk(1, 0, 2, "z".repeat(DIRECT_CHUNK_UNITS + 1))]],
    ["a message past the reassembly bound", [chunk(1, 0, 1000, "z".repeat(DIRECT_CHUNK_UNITS)),
      ...Array.from({ length: Math.ceil(DIRECT_MESSAGE_BYTES / DIRECT_CHUNK_UNITS) }, (_, i) => chunk(1, i + 1, 1000, "z".repeat(DIRECT_CHUNK_UNITS)))]],
    ["an ack from the machine", [JSON.stringify({ t: "ack", connection: "c", frame_seq: 1 })]],
    ["an unknown message", [JSON.stringify({ t: "hello" })]],
    ["binary data", [new Uint8Array(4)]],
    ["text that is not JSON", ["{"]],
    ["an env without an envelope", [JSON.stringify({ t: "env", e: null })]],
    ["reassembled text that is not an env", [chunk(1, 0, 1, JSON.stringify({ t: "ack" }))]],
  ]
  for (const [name, messages] of cases) {
    const reassembler = new DirectReassembler(() => undefined)
    assert.throws(() => { for (const message of messages) reassembler.push(message) }, /terminal_direct_violation/, name)
  }
})

test("a partial message still incomplete after the chunk limit times out", (t) => {
  t.mock.timers.enable({ apis: ["setTimeout"] })
  let timedOut = 0
  const reassembler = new DirectReassembler(() => timedOut++)
  reassembler.push(chunk(1, 0, 2, "{"))
  t.mock.timers.tick(DIRECT_CHUNK_MS - 1)
  assert.equal(timedOut, 0)
  t.mock.timers.tick(1)
  assert.equal(timedOut, 1)
})

test("the link offers one ordered channel over STUN only and closes its DC on each violation", async () => {
  let peer!: FakePeer
  const down: string[] = []
  const seen: TerminalEnvelope[] = []
  const link = new DirectLink((config) => (peer = new FakePeer(config)), (e) => seen.push(e), (code) => down.push(code))
  assert.deepEqual(peer.config, { iceServers: DIRECT_STUN.map((urls: string) => ({ urls })) })
  assert.equal(peer.label, DIRECT_LABEL)
  assert.equal(await link.offer(), "v=0 offer")
  await link.answer("v=0 answer")
  assert.equal(peer.remote, "v=0 answer")
  assert.equal(link.open, true)
  peer.channel.deliver(JSON.stringify({ t: "env", e: envelope() }))
  assert.deepEqual(seen, [envelope()])
  peer.channel.deliver(chunk(1, 1, 2, "x"))
  assert.deepEqual(down, ["terminal_direct_violation"])
  assert.equal(peer.channel.closed, true)
  assert.equal(peer.closed, true)
  assert.equal(link.open, false)
  peer.channel.deliver(JSON.stringify({ t: "env", e: envelope() }))
  assert.equal(seen.length, 1)
})

test("a chunk timeout on the link closes the DC", async (t) => {
  t.mock.timers.enable({ apis: ["setTimeout"] })
  let peer!: FakePeer
  const down: string[] = []
  const link = new DirectLink((config) => (peer = new FakePeer(config)), () => undefined, (code) => down.push(code))
  peer.channel.open()
  peer.channel.deliver(chunk(3, 0, 2, "{"))
  t.mock.timers.tick(DIRECT_CHUNK_MS)
  assert.deepEqual(down, ["terminal_direct_chunk_timeout"])
  assert.equal(link.open, false)
})

test("ICE that never opens the DC within the negotiate limit fails the link", async (t) => {
  t.mock.timers.enable({ apis: ["setTimeout"] })
  let peer!: FakePeer
  const down: string[] = []
  const link = new DirectLink((config) => { peer = new FakePeer(config); peer.autoOpen = false; return peer }, () => undefined, (code) => down.push(code))
  const answered = link.answer("v=0 answer")
  await Promise.resolve()
  t.mock.timers.tick(DIRECT_NEGOTIATE_MS)
  await assert.rejects(answered, /terminal_direct_ice_failed/)
  assert.deepEqual(down, ["terminal_direct_ice_failed"])
  assert.equal(peer.closed, true)
})

test("a DC close or a failed peer reports the link down once", async () => {
  let peer!: FakePeer
  const down: string[] = []
  new DirectLink((config) => (peer = new FakePeer(config)), () => undefined, (code) => down.push(code))
  peer.channel.open()
  peer.connectionState = "failed"
  peer.onconnectionstatechange?.()
  peer.channel.onclose?.()
  assert.deepEqual(down, ["terminal_direct_ice_failed"])
})

test("the offer is sealed under the connection key with its connection as additional data", async () => {
  const raw = crypto.getRandomValues(new Uint8Array(32))
  const key = await crypto.subtle.importKey("raw", raw, "AES-GCM", false, ["encrypt", "decrypt"])
  const sealed = base64Bytes(await sealDirectOffer(key, "conn", "v=0 offer"))
  const open = (aad: string) => crypto.subtle.decrypt({ name: "AES-GCM", iv: sealed.slice(0, 12),
    additionalData: new TextEncoder().encode(aad) }, key, sealed.slice(12))
  assert.equal(new TextDecoder().decode(await open("clawdline-direct-offer-v1/conn")), "v=0 offer")
  await assert.rejects(open("clawdline-direct-offer-v1/other"))
  await assert.rejects(sealDirectOffer(key, "conn", "a".repeat(16 * 1024 + 1)), /terminal_direct_sdp_too_large/)
})
