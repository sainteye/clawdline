import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- esbuild bundles the TypeScript source for Node's test runner.
import { TerminalChannelTransport, type TerminalCloudClient, type TerminalEnvelope } from "./terminal-transport.ts"
// @ts-expect-error -- esbuild bundles the TypeScript source for Node's test runner.
import { CloudTerminalSession } from "./terminal-session.ts"
// @ts-expect-error -- esbuild bundles the TypeScript source for Node's test runner.
import { TerminalObservation } from "./terminal-observation.ts"
// @ts-expect-error -- esbuild bundles the TypeScript source for Node's test runner.
import { settled, until } from "./until.ts"
import { bytesBase64, base64Bytes, envelopeSigningBytes } from "../legacy/js/net/cloud-crypto.js"
import type { DirectChannelLike, DirectPeerLike } from "./terminal-direct.js"

// The session and the transport together, against a machine played by the test: the real
// verification, carrier and ack paths, with only the relay socket and the DC faked.

const machine = "machine_test"
const viewer = "viewer_test"
const terminalID = "trm_test"

class FakeChannel implements DirectChannelLike {
  readyState = "connecting"
  sent: string[] = []
  onopen: ((event?: unknown) => void) | null = null
  onclose: ((event?: unknown) => void) | null = null
  onerror: ((event?: unknown) => void) | null = null
  onmessage: ((event: { data: unknown }) => void) | null = null
  send(data: string): void { this.sent.push(data) }
  close(): void { this.readyState = "closed" }
}
class FakePeer implements DirectPeerLike {
  localDescription: { sdp: string } | null = null
  iceGatheringState = "complete"
  channel = new FakeChannel()
  onicegatheringstatechange: ((event?: unknown) => void) | null = null
  onconnectionstatechange: ((event?: unknown) => void) | null = null
  createDataChannel(): DirectChannelLike { return this.channel }
  async createOffer(): Promise<{ type: string; sdp?: string }> { return { type: "offer", sdp: "v=0 offer" } }
  async setLocalDescription(description: { type: string; sdp?: string }): Promise<void> { this.localDescription = { sdp: description.sdp ?? "" } }
  async setRemoteDescription(): Promise<void> { queueMicrotask(() => { this.channel.readyState = "open"; this.channel.onopen?.() }) }
  close(): void { this.channel.close() }
}

type Request = { request_id: string; connection: string; operation: string; terminal_id?: string; key?: string; key_id?: string;
  body?: Record<string, unknown> }

async function machineFixture() {
  const master = await crypto.subtle.generateKey({ name: "AES-GCM", length: 256 }, false, ["encrypt", "decrypt"])
  const sender = await crypto.subtle.generateKey("Ed25519", false, ["sign", "verify"])
  const machineKey = await crypto.subtle.generateKey("Ed25519", false, ["sign", "verify"])
  const events = new Set<(event: { type: string; channels?: string[] }) => void>()
  const relayRequests: TerminalEnvelope[] = []
  const peer = new FakePeer()
  const client: TerminalCloudClient = {
    deviceID: viewer, devicePrivateKey: sender.privateKey, ready: true, retired: false,
    nextSequence: (() => { let n = 0; return async () => ++n })(), socketSubscriptions: new Map(), subscriptionHolds: new Map(),
    pendingSubscriptions: new Set(), subscriptionLimit: 16,
    _trimSubscriptions() {},
    _outboundMachinePairing: async (id) => ({ masterKey: master, keyID: "ms-1", senderKey: machineKey.publicKey, senderID: id }),
    _send(frame) { if ((frame as { type?: string }).type === "publish") relayRequests.push((frame as { envelope: TerminalEnvelope }).envelope) },
    _sendSubscriptionFrame(_type, channels) { queueMicrotask(() => events.forEach((fn) => fn({ type: "subscriptions", channels }))) },
    _receiveEnvelope: async () => undefined,
    events(fn) { events.add(fn); return () => { events.delete(fn) } },
  }
  const stages: string[] = []
  const observation = new TerminalObservation((row) => stages.push(`${row.stage}:${row.code ?? ""}`))
  const transport = new TerminalChannelTransport(client, machine, observation, () => peer)
  const keys = new Map<string, { key: Uint8Array; keyID: string }>()
  let seq = 0
  const seal = async (kind: "term" | "termr", connection: string, plaintext: unknown): Promise<TerminalEnvelope> => {
    const { key, keyID } = keys.get(connection)!
    const nonce = crypto.getRandomValues(new Uint8Array(12))
    const imported = await crypto.subtle.importKey("raw", key.slice().buffer as ArrayBuffer, "AES-GCM", false, ["encrypt"])
    const ct = await crypto.subtle.encrypt({ name: "AES-GCM", iv: nonce }, imported, new TextEncoder().encode(JSON.stringify(plaintext)))
    const envelope: TerminalEnvelope = { v: 1, ch: `${kind}/${machine}/${viewer}/${connection}`, seq: ++seq,
      ts: Date.now(), class: kind === "term" ? "stream" : "ctl", key_id: keyID, nonce: bytesBase64(nonce),
      ct: bytesBase64(new Uint8Array(ct)), sender: machine, sig: "" }
    envelope.sig = bytesBase64(new Uint8Array(await crypto.subtle.sign("Ed25519", machineKey.privateKey, envelopeSigningBytes(envelope))))
    return envelope
  }
  const open = async (envelope: TerminalEnvelope): Promise<Request> => {
    const plain = await crypto.subtle.decrypt({ name: "AES-GCM", iv: base64Bytes(envelope.nonce) }, master, base64Bytes(envelope.ct))
    return JSON.parse(new TextDecoder().decode(plain)) as Request
  }
  const sent: Request[] = []
  let relaySeen = 0
  let dcSeen = 0
  /** Every request the tab has sent since the last call, from either carrier. */
  const requests = async (): Promise<Request[]> => {
    const out: Request[] = []
    for (; relaySeen < relayRequests.length; relaySeen++) out.push(await open(relayRequests[relaySeen]))
    const dc = peer.channel.sent.map((text) => JSON.parse(text) as { t: string; e?: TerminalEnvelope })
    for (; dcSeen < dc.length; dcSeen++) if (dc[dcSeen].t === "env") out.push(await open(dc[dcSeen].e!))
    for (const request of out) if (request.key && request.key_id) keys.set(request.connection, { key: base64Bytes(request.key), keyID: request.key_id })
    sent.push(...out)
    return out
  }
  const acks = () => peer.channel.sent.map((text) => JSON.parse(text) as { t: string; frame_seq?: number }).filter((m) => m.t === "ack")
  const control = { held: true, holder: { same_client: true, same_device: true }, epoch: 1, applied_through: 0,
    expires_at: Date.now() / 1000 + 60 }
  const result = (request: Request): Record<string, unknown> => {
    switch (request.operation) {
    case "open_connection": case "rekey_connection":
      return { connection: request.connection, key_id: request.key_id, expires_at: Date.now() / 1000 + 300,
        machine_incarnation: "inc", ...(request.body?.carrier === "direct" ? { carrier: "direct" } : {}) }
    case "control": return request.body ? { control } : { control, machine_incarnation: "inc" }
    case "direct_offer": return { sdp: "v=0 answer", direct_receipts: true }
    case "activate_connection": return { connection: request.connection, retired_connection: request.body?.old_connection }
    case "input": return { applied_through: (request as { seq?: number }).seq }
    default: return {}
    }
  }
  /** Answers one request on the relay's `termr`, as the machine does on either carrier. */
  const answer = async (request: Request, refused = false) => {
    await client._receiveEnvelope(await seal("termr", request.connection, { v: 1, type: "terminal_receipt",
      request_id: request.request_id, connection: request.connection, operation: request.operation,
      ...(request.terminal_id ? { terminal_id: request.terminal_id } : {}),
      ...(refused ? { status: "refused", error: "terminal_invalid" } : { status: "ok", result: result(request) }) }), false)
  }
  let frameSeq = new Map<string, number>()
  const frame = async (connection: string, overDC: boolean, agoSeconds = 0) => {
    const n = (frameSeq.get(connection) ?? 0) + 1
    frameSeq.set(connection, n)
    const envelope = await seal("term", connection, { v: 1, type: "terminal_frame", connection, terminal_id: terminalID,
      frame_seq: n, captured_at: Date.now() / 1000 - agoSeconds, frame: { rev: `r${n}`, at: Date.now() / 1000, cols: 80, rows: 1, lines: ["ready"],
        cursor: { x: 0, y: 0, visible: true }, modes: { app_cursor: false, app_keypad: false, mouse_sgr: false, alt: false, mouse: "none" } } })
    if (overDC) peer.channel.onmessage?.({ data: JSON.stringify({ t: "env", e: envelope }) })
    else await client._receiveEnvelope(envelope, false)
  }
  const probe = async (connection: string, n: number) => client._receiveEnvelope(await seal("termr", connection,
    { v: 1, type: "terminal_carrier_probe", connection, n }), false)
  /** The connections whose relay channels the tab still listens on. */
  const listening = () => [...(transport as unknown as { listeners: Map<string, unknown> }).listeners.keys()]
  return { transport, requests, answer, frame, probe, acks, stages, observation, peer, listening, sent }
}

const settle = () => new Promise<void>((resolve) => setTimeout(resolve, 10))

type Held = Array<{ request_id: string; connection: string; operation: string }>

/**
 * Plays the machine, at least one round, until `done` holds, answering every request except
 * those `hold` keeps. Sealing and verifying finish off the event loop, so the bound is real
 * time, not a number of rounds.
 */
async function serve(m: Awaited<ReturnType<typeof machineFixture>>, hold: (request: { operation: string; connection: string }) => boolean,
  done: (held: Held) => boolean, refuse: (request: { operation: string; connection: string; body?: Record<string, unknown> }) => boolean = () => false) {
  const held: Held = []
  await until(async () => {
    for (const request of await m.requests()) {
      if (hold(request)) held.push(request)
      else await m.answer(request as never, refuse(request))
    }
    return done(held)
  }, 10_000)
  return held
}

test("the direct connection's first frame, arriving before the lease check returns, is acknowledged and activates", async () => {
  const m = await machineFixture()
  const session = new CloudTerminalSession(m.transport, "stable-tab", m.observation)
  try {
    const starting = session.start()
    await serve(m, () => false, settled(starting))
    await starting
    const attaching = session.attach(terminalID)
    await serve(m, () => false, settled(attaching))
    await attaching
    const acquiring = session.acquire("acquire")
    await serve(m, () => false, settled(acquiring))
    await acquiring
    const relay = (session as unknown as { connection: string }).connection
    await m.frame(relay, false)
    // The upgrade: answer the offer and the direct rekey, then hold the lease check of the new connection.
    let direct = ""
    const held = await serve(m, (request) => {
      if (request.operation === "rekey_connection") direct = request.connection
      return request.operation === "control" && request.connection === direct
    }, (held) => held.length === 1)
    assert.ok(direct, "the tab rekeyed onto the DC")
    const rekey = m.sent.find((request) => request.operation === "rekey_connection" && request.connection === direct)
    assert.equal((rekey?.body as { direct_receipts?: unknown } | undefined)?.direct_receipts, true,
      "a machine that offered DC receipts is asked for them")
    assert.equal(held.length, 1, "the lease check is out")
    const opened = () => m.stages.filter((stage) => stage.startsWith("envelope_opened")).length
    const openedBefore = opened()
    await m.frame(direct, true)
    await m.probe(direct, 1)
    await until(() => opened() > openedBefore)
    await settle()
    for (const request of held) await m.answer(request as never)
    const after = await serve(m, () => false, () => m.acks().length === 1 && session.snapshot.carrier === "direct" && session.snapshot.canType)
    assert.deepEqual(after, [])
    assert.equal(m.acks().length, 1, `the direct frame is acknowledged (stages: ${m.stages.filter((s) => !s.startsWith("raw") && !s.startsWith("request_") && !s.startsWith("pending") && !s.startsWith("session_") && !s.startsWith("relay_ack")).join(" ")})`)
    assert.equal(session.snapshot.carrier, "direct")
    assert.equal(session.snapshot.canType, true)
  } finally { session.dispose(); m.transport.dispose() }
})

test("a key typed while the tab moves to the direct path waits, then goes out once it can type", async () => {
  const m = await machineFixture()
  const session = new CloudTerminalSession(m.transport, "stable-tab", m.observation)
  try {
    const starting = session.start()
    await serve(m, () => false, settled(starting))
    await starting
    const attaching = session.attach(terminalID)
    await serve(m, () => false, settled(attaching))
    await attaching
    const acquiring = session.acquire("acquire")
    await serve(m, () => false, settled(acquiring))
    await acquiring
    const relay = (session as unknown as { connection: string }).connection
    await m.frame(relay, false)
    let direct = ""
    const held = await serve(m, (request) => {
      if (request.operation === "rekey_connection") direct = request.connection
      return request.operation === "control" && request.connection === direct
    }, (held) => held.length === 1)
    assert.equal(session.snapshot.canType, false, "the move pauses typing")
    assert.equal(session.snapshot.typeAhead, true, "but only for a moment the tab expects to end")
    const typing = session.input(new TextEncoder().encode("a"))
    await settle()
    await serve(m, () => false, () => true)
    assert.deepEqual(m.sent.filter((r) => r.operation === "input"), [], "nothing is sent while paused")
    await m.frame(direct, true)
    for (const request of held) await m.answer(request as never)
    await serve(m, () => false, settled(typing))
    await typing
    const inputs = m.sent.filter((r) => r.operation === "input")
    assert.equal(inputs.length, 1)
    assert.equal(inputs[0].connection, direct, "the key went out on the direct connection")
    assert.equal(session.snapshot.canType, true)
  } finally { session.dispose(); m.transport.dispose() }
})

test("a direct connection the machine retires before it activates falls back to the relay and lets go of both old connections", async () => {
  const m = await machineFixture()
  const session = new CloudTerminalSession(m.transport, "stable-tab", m.observation)
  try {
    const starting = session.start()
    await serve(m, () => false, settled(starting))
    await starting
    const attaching = session.attach(terminalID)
    await serve(m, () => false, settled(attaching))
    await attaching
    const acquiring = session.acquire("acquire")
    await serve(m, () => false, settled(acquiring))
    await acquiring
    const relay = (session as unknown as { connection: string }).connection
    await m.frame(relay, false)
    let direct = ""
    await serve(m, (request) => {
      if (request.operation === "rekey_connection") direct = request.connection
      return false
    }, () => session.snapshot.carrier === "direct" && !(session as unknown as { upgrading: boolean }).upgrading)
    assert.ok(direct, "the tab rekeyed onto the DC")
    assert.equal(session.snapshot.carrier, "direct")
    // The direct frame never arrives; the machine retires the connection and closes the peer.
    m.peer.channel.readyState = "closed"
    m.peer.channel.onclose?.()
    const before = m.sent.length
    // The relay rekey naming the retired connection is refused, so the tab opens afresh.
    const connection = () => (session as unknown as { connection: string }).connection
    await serve(m, () => false, () => connection() !== direct && connection() !== relay &&
      m.sent.slice(before).some((r) => r.operation === "capture" && r.connection === connection()),
    (request) => request.operation === "rekey_connection" && request.body?.old_connection === direct)
    const current = connection()
    await m.frame(current, false)
    await serve(m, () => false, () => session.snapshot.carrier === "relay" && m.listening().join() === current)
    // A last round after a settle collects anything else the tab sent, for the checks below.
    await settle()
    await serve(m, () => false, () => true)
    assert.equal(session.snapshot.carrier, "relay")
    assert.notEqual(current, relay)
    assert.notEqual(current, direct)
    assert.deepEqual(m.listening(), [current], "only the new relay connection is still subscribed")
    assert.deepEqual(m.sent.slice(before).filter((r) => r.operation === "activate_connection"), [],
      "a fresh connection activates nothing: there is no rotation left to finish")
    assert.notEqual(session.snapshot.state, "unknown")
  } finally { session.dispose(); m.transport.dispose() }
})

test("a frame set aside without drawing leaves a row saying why", async () => {
  const m = await machineFixture()
  const session = new CloudTerminalSession(m.transport, "stable-tab", m.observation)
  try {
    const starting = session.start()
    await serve(m, () => false, settled(starting))
    await starting
    const attaching = session.attach(terminalID)
    await serve(m, () => false, settled(attaching))
    await attaching
    const relay = (session as unknown as { connection: string }).connection
    await m.frame(relay, false, 10)
    await until(() => m.stages.includes("frame_dropped:terminal_frame_stale"))
    assert.ok(m.stages.includes("frame_dropped:terminal_frame_stale"), m.stages.join(" "))
    assert.equal(session.snapshot.frame, null)
  } finally { session.dispose(); m.transport.dispose() }
})
