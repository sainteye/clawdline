import assert from "node:assert/strict"
import test from "node:test"
import { base64Bytes } from "../legacy/js/net/cloud-crypto.js"
// @ts-expect-error -- Node's strip-types runner loads the TypeScript source.
import { DIRECT_CHUNK_MS, DIRECT_CHUNK_UNITS, DIRECT_LABEL, DIRECT_MESSAGE_BYTES, DIRECT_NEGOTIATE_MS, DIRECT_STUN, DirectLink, DirectReassembler, directAttemptRecorder, directAttemptStage, directCandidateCounts, directMessages, directSelectedPair, sealDirectOffer } from "./terminal-direct.ts"
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

// A real-shaped offer and answer: private host, mDNS, public srflx IPv4 and IPv6, a TCP line, and
// the credentials every SDP carries. None of it may reach a trace. The private address is built at run
// time so no address outside the documentation ranges sits in the repository.
const traceHome = [192, 168, 1, 23].join(".")
const traceOffer = [
  "v=0", "o=- 4611731400430051336 2 IN IP4 127.0.0.1", "s=-",
  "a=fingerprint:sha-256 4A:AD:B9:B1:3F:82:18:3B:54:02:12:DF:3E:5D:49:6B",
  "m=application 9 UDP/DTLS/SCTP webrtc-datachannel", "c=IN IP4 0.0.0.0",
  "a=ice-ufrag:Qx7v", "a=ice-pwd:k2b0Jz5PqWrYcVdXtLmNnFhG",
  `a=candidate:1 1 udp 2122260223 ${traceHome} 51234 typ host generation 0`,
  "a=candidate:2 1 udp 2122260223 0f1e2d3c.local 51235 typ host generation 0",
  `a=candidate:3 1 udp 1686052607 203.0.113.77 61001 typ srflx raddr ${traceHome} rport 51234 generation 0`,
  "a=candidate:4 1 udp 1686052351 2001:db8:85a3::8a2e:370:7334 61002 typ srflx raddr :: rport 0 generation 0",
  "a=candidate:5 1 tcp 1518280447 198.51.100.9 9 typ host tcptype active generation 0",
].join("\r\n") + "\r\n"
const traceAnswer = "v=0\r\na=ice-ufrag:Mm9q\r\na=ice-pwd:Z8yXwVuTsRqPoNmLkJiHgFeD\r\n" +
  "a=candidate:1 1 udp 1694498815 198.51.100.40 62000 typ srflx raddr 0.0.0.0 rport 0\r\n"
const traceSecrets = [traceHome, "203.0.113.77", "2001:db8", "198.51.100", "0f1e2d3c", ".local", "51234", "51235",
  "61001", "61002", "62000", "Qx7v", "k2b0Jz5PqWrYcVdXtLmNnFhG", "Mm9q", "Z8yXwVuTsRqPoNmLkJiHgFeD", "4A:AD:B9",
  "fingerprint", "ice-ufrag", "ice-pwd", "a=candidate", "v=0", "IN IP4"]

class TracedPeer extends FakePeer {
  iceConnectionState = "new"
  oniceconnectionstatechange: ((event?: unknown) => void) | null = null
  stats: Array<Record<string, unknown>> = []
  async createOffer(): Promise<{ type: string; sdp?: string }> { return { type: "offer", sdp: traceOffer } }
  ice(state: string): void { this.iceConnectionState = state; this.oniceconnectionstatechange?.() }
  async getStats() { if (this.closed) throw new Error("closed"); return new Map(this.stats.map((entry) => [entry.id, entry])) }
}

test("candidate counts read only a candidate's type and the family of its address", () => {
  const counts = directCandidateCounts(traceOffer)
  assert.deepEqual(counts, { "host/ipv4": 2, "host/mdns": 1, "srflx/ipv4": 1, "srflx/ipv6": 1 })
  const text = JSON.stringify(counts)
  for (const secret of traceSecrets) assert.ok(!text.includes(secret), `counts carry ${secret}`)
})

test("an attempt that opens reports one summary with its pair and no address, port or SDP", async () => {
  let peer!: TracedPeer
  const reports: Array<Record<string, unknown>> = []
  const link = new DirectLink((config) => (peer = new TracedPeer(config)), () => undefined, () => undefined,
    { attempt: "AAAAAAAAAAAAAAAAAAAAAA", kind: "terminal", report: (data) => reports.push(data) })
  peer.stats = [
    { id: "T1", type: "transport", selectedCandidatePairId: "P1" },
    { id: "P1", type: "candidate-pair", localCandidateId: "L1", remoteCandidateId: "R1", state: "succeeded", nominated: true },
    { id: "L1", type: "local-candidate", candidateType: "srflx", address: "203.0.113.77", port: 61001, protocol: "udp" },
    { id: "R1", type: "remote-candidate", candidateType: "srflx", address: "198.51.100.40", port: 62000, protocol: "udp" },
  ]
  await link.offer()
  peer.ice("checking")
  peer.connectionState = "connecting"; peer.onconnectionstatechange?.()
  peer.ice("connected")
  await link.answer(traceAnswer)
  await new Promise((resolve) => setTimeout(resolve, 0))
  link.close("terminal_direct_released")
  await new Promise((resolve) => setTimeout(resolve, 0))
  assert.equal(reports.length, 1)
  const report = reports[0]
  assert.equal(report.attempt, "AAAAAAAAAAAAAAAAAAAAAA")
  assert.equal(report.kind, "terminal")
  assert.equal(report.stage, "open")
  assert.equal(report.answer, "received")
  assert.equal(report.offer_cands, "host/ipv4:2,host/mdns:1,srflx/ipv4:1,srflx/ipv6:1")
  assert.equal(report.answer_cands, "srflx/ipv4:1")
  assert.equal(report.gather, "complete")
  assert.equal(report.browser_srflx, true)
  assert.equal(report.pair, "srflx/ipv4>srflx/ipv4")
  assert.match(String(report.ice), /^checking@\d+ms,connected@\d+ms$/)
  assert.match(String(report.pc), /^connecting@\d+ms$/)
  const text = JSON.stringify(report)
  for (const secret of traceSecrets) assert.ok(!text.includes(secret), `the summary carries ${secret}: ${text}`)
})

test("an attempt that never opens reports where it stopped", async (t) => {
  t.mock.timers.enable({ apis: ["setTimeout"] })
  let peer!: TracedPeer
  const reports: Array<Record<string, unknown>> = []
  const link = new DirectLink((config) => { peer = new TracedPeer(config); peer.autoOpen = false; return peer },
    () => undefined, () => undefined, { attempt: "BBBBBBBBBBBBBBBBBBBBBB", kind: "carrier", report: (data) => reports.push(data) })
  const answered = link.answer(traceAnswer)
  peer.ice("checking")
  await Promise.resolve()
  t.mock.timers.tick(DIRECT_NEGOTIATE_MS)
  await assert.rejects(answered, /terminal_direct_ice_failed/)
  t.mock.timers.reset()
  await new Promise((resolve) => setTimeout(resolve, 0))
  assert.equal(reports.length, 1)
  assert.equal(reports[0].stage, "timed_out")
  assert.equal(reports[0].code, "terminal_direct_ice_failed")
  assert.equal(reports[0].pair, null)
})

test("an attempt closed before any answer is no_answer, and a link without a trace reports nothing", async () => {
  const reports: Array<Record<string, unknown>> = []
  const traced = new DirectLink((config) => new TracedPeer(config), () => undefined, () => undefined,
    { attempt: "CCCCCCCCCCCCCCCCCCCCCC", kind: "carrier", report: (data) => reports.push(data) })
  await traced.offer()
  traced.close("carrier_failed")
  await new Promise((resolve) => setTimeout(resolve, 0))
  assert.deepEqual(reports.map((report) => [report.stage, report.code, report.answer]), [["no_answer", "carrier_failed", "none"]])
  new DirectLink((config) => new TracedPeer(config), () => undefined, () => undefined).close()
  assert.equal(reports.length, 1)
})

test("the stage names each way an attempt ends, in the machine's order", () => {
  const cases: Array<[string, Parameters<typeof directAttemptStage>[0], string]> = [
    ["open", { opened: true, answered: true, remote: 1, ice: ["checking", "connected"], timedOut: false }, "open"],
    ["no answer", { opened: false, answered: false, remote: 0, ice: [], timedOut: false }, "no_answer"],
    ["connected without a channel", { opened: false, answered: true, remote: 1, ice: ["checking", "connected"], timedOut: true }, "connected_no_channel"],
    ["connected through a peer-reflexive pair", { opened: false, answered: true, remote: 0, ice: ["completed"], timedOut: true }, "connected_no_channel"],
    ["nothing to check against", { opened: false, answered: true, remote: 0, ice: ["checking", "failed"], timedOut: true }, "no_remote_candidates"],
    ["checks failed", { opened: false, answered: true, remote: 2, ice: ["checking", "failed"], timedOut: false }, "checks_failed"],
    ["timed out while checking", { opened: false, answered: true, remote: 2, ice: ["checking"], timedOut: true }, "timed_out"],
    ["closed for another reason", { opened: false, answered: true, remote: 2, ice: ["checking"], timedOut: false }, "closed_early"],
  ]
  for (const [name, facts, want] of cases) assert.equal(directAttemptStage(facts), want, name)
})

test("the selected pair falls back to a nominated succeeded pair and reads families only", () => {
  assert.equal(directSelectedPair([
    { id: "P1", type: "candidate-pair", localCandidateId: "L", remoteCandidateId: "R", state: "in-progress" },
    { id: "P2", type: "candidate-pair", localCandidateId: "L", remoteCandidateId: "R6", state: "succeeded", nominated: true },
    { id: "L", type: "local-candidate", candidateType: "host", address: "fd00::1" },
    { id: "R6", type: "remote-candidate", candidateType: "prflx", ip: "2001:db8::9" },
  ]), "host/ipv6>prflx/ipv6")
  assert.equal(directSelectedPair([{ id: "P1", type: "candidate-pair", state: "failed" }]), null)
})

test("the recorder writes one cloud.direct.attempt row naming its machine, keyed by kind and stage", () => {
  const rows: Array<[string, Record<string, unknown>, string | undefined]> = []
  const record = directAttemptRecorder({ viewerEvents: { record: (event: string, data: Record<string, unknown>, key?: string) => rows.push([event, data, key]) } }, "machine_test")
  record!({ kind: "terminal", stage: "timed_out", attempt: "A" })
  assert.deepEqual(rows, [["cloud.direct.attempt", { kind: "terminal", stage: "timed_out", attempt: "A", machine: "machine_test" }, "cloud.direct.attempt|terminal|timed_out"]])
  assert.equal(directAttemptRecorder({}, "machine_test"), null)
})


test("the authenticated carrier can refuse pairing without an encrypted content envelope", () => {
  const r = new DirectReassembler(() => {})
  assert.throws(() => r.push(JSON.stringify({t: "read_error", code: "machine_pairing_required"})), /machine_pairing_required/)
  for (const value of [
    {t: "read_error", code: "revoked"},
    {t: "read_error", code: "machine_pairing_required", key: "untrusted"},
    {t: "read_error", code: "unknown"},
  ]) assert.throws(() => r.push(JSON.stringify(value)), /terminal_direct_violation/)
})
