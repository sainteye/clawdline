import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- esbuild bundles the TypeScript source for Node's test runner.
import { TerminalChannelTransport, freshTerminalConnection, type TerminalCloudClient, type TerminalEnvelope } from "./terminal-transport.ts"
import { bytesBase64, base64Bytes, envelopeSigningBytes } from "../legacy/js/net/cloud-crypto.js"
// @ts-expect-error -- the focused runner bundles TypeScript before Node executes it.
import { TerminalObservation } from "./terminal-observation.ts"
import goReceipt from "./testdata/terminal-receipt-go.json" with { type: "json" }
import type { DirectChannelLike, DirectPeerFactory, DirectPeerLike } from "./terminal-direct.js"

const machine = "machine_test"
const viewer = "viewer_test"
async function fixture(peers: DirectPeerFactory | null = null) {
  const master = await crypto.subtle.generateKey({ name: "AES-GCM", length: 256 }, false, ["encrypt", "decrypt"])
  const sender = await crypto.subtle.generateKey("Ed25519", false, ["sign", "verify"])
  const machineKey = await crypto.subtle.generateKey("Ed25519", false, ["sign", "verify"])
  const events = new Set<(event: { type: string; channels?: string[]; code?: string; error?: { code?: string; subscriptionChannels?: string[] } }) => void>()
  const published: TerminalEnvelope[] = []
  const observed: unknown[] = []
  const stages: Array<{ stage: string; code?: string; channel?: string }> = []
  const observation = new TerminalObservation((row) => stages.push(row))
  let confirm = true
  let rejectDelta = false
  const client: TerminalCloudClient = {
    deviceID: viewer, devicePrivateKey: sender.privateKey, ready: true, retired: false,
    nextSequence: async () => 7, socketSubscriptions: new Map(), subscriptionHolds: new Map(),
    pendingSubscriptions: new Set(), subscriptionLimit: 8,
    _trimSubscriptions(incoming, keep) {
      while (this.socketSubscriptions.size + incoming > this.subscriptionLimit) {
        const idle = [...this.socketSubscriptions.keys()].find((ch) => !this.subscriptionHolds.has(ch) && !keep.includes(ch))
        if (!idle) break
        this.socketSubscriptions.delete(idle)
        this.pendingSubscriptions.delete(idle)
        this._sendSubscriptionFrame("unsubscribe", [idle])
      }
    },
    _outboundMachinePairing: async (id) => ({ masterKey: master, keyID: "ms-1", senderKey: machineKey.publicKey, senderID: id }),
    _send(frame) {
      if ((frame as { type?: string }).type === "publish") published.push((frame as { envelope: TerminalEnvelope }).envelope)
      if ((frame as { type?: string }).type === "terminal_frame_observed") observed.push(frame)
    },
    _sendSubscriptionFrame(_type, channels) {
      if (rejectDelta && channels[0]?.startsWith("termd/"))
        queueMicrotask(() => events.forEach((fn) => fn({ type: "error", code: "invalid_channel" })))
      else if (confirm) queueMicrotask(() => events.forEach((fn) => fn({ type: "subscriptions", channels })))
    },
    _receiveEnvelope: async () => undefined,
    events(fn) { events.add(fn); return () => { events.delete(fn) } },
  }
  const adapter = new TerminalChannelTransport(client, machine, observation, peers)
  const fresh = freshTerminalConnection()
  const seen: unknown[] = []
  const seal = async (sequence: number, plaintext: unknown, kind: "term" | "termr" = "termr", key = fresh.key,
    connection = fresh.connection, keyID = fresh.keyID, targetMachine = machine): Promise<TerminalEnvelope> => {
    const nonce = crypto.getRandomValues(new Uint8Array(12))
    const imported = await crypto.subtle.importKey("raw", key.slice().buffer as ArrayBuffer, "AES-GCM", false, ["encrypt"])
    const ct = await crypto.subtle.encrypt({ name: "AES-GCM", iv: nonce }, imported, new TextEncoder().encode(JSON.stringify(plaintext)))
    const envelope: TerminalEnvelope = { v: 1, ch: `${kind}/${targetMachine}/${viewer}/${connection}`, seq: sequence,
      ts: Date.now(), class: kind === "term" ? "stream" : "ctl", key_id: keyID, nonce: bytesBase64(nonce),
      ct: bytesBase64(new Uint8Array(ct)), sender: targetMachine, sig: "" }
    envelope.sig = bytesBase64(new Uint8Array(await crypto.subtle.sign("Ed25519", machineKey.privateKey, envelopeSigningBytes(envelope))))
    return envelope
  }
  return { adapter, client, fresh, seen, published, observed, stages, seal, observation, machineKey,
    emitRelay: (event: { type: string; ch?: string; seq?: number; code?: string; status?: string;
      error?: { code?: string; subscriptionChannels?: string[] } }) => events.forEach((fn) => fn(event)),
    setConfirm: (value: boolean) => { confirm = value },
    setRejectDelta: (value: boolean) => { rejectDelta = value }, master }
}

test("an old relay rejects only optional delta and leaves the full-frame connection usable", async () => {
  const f = await fixture()
  f.setRejectDelta(true)
  await f.adapter.subscribeTerminal(f.fresh.connection, f.fresh.keyID, f.fresh.key, (event) => f.seen.push(event))
  assert.equal(f.adapter.deltaAvailable(f.fresh.connection), false)
  assert.equal(f.seen.length, 0)
  assert.equal(f.client.subscriptionHolds.size, 2)
  await f.adapter.publishTerminal({ v: 1, type: "terminal_request", request_id: crypto.randomUUID(),
    connection: f.fresh.connection, operation: "open_connection" })
  assert.equal(f.published.length, 1)
  f.adapter.dispose()
})

test("two machines sharing a Cloud client keep their receipts after either transport closes", async () => {
  const f = await fixture()
  const second = new TerminalChannelTransport(f.client, "machine_other")
  const other = freshTerminalConnection()
  const seenOther: unknown[] = []
  await f.adapter.subscribeTerminal(f.fresh.connection, f.fresh.keyID, f.fresh.key, (event) => f.seen.push(event))
  await second.subscribeTerminal(other.connection, other.keyID, other.key, (event) => seenOther.push(event))
  const body = (connection: string) => ({ v: 1, type: "terminal_receipt", request_id: crypto.randomUUID(),
    connection, operation: "list", status: "ok" })
  await f.client._receiveEnvelope(await f.seal(1, body(f.fresh.connection)), false)
  await f.client._receiveEnvelope(await f.seal(1, body(other.connection), "termr", other.key, other.connection, other.keyID, "machine_other"), false)
  assert.equal(f.seen.length, 1)
  assert.equal(seenOther.length, 1)
  f.adapter.dispose()
  await f.client._receiveEnvelope(await f.seal(2, body(other.connection), "termr", other.key, other.connection, other.keyID, "machine_other"), false)
  assert.equal(seenOther.length, 2)
  second.dispose()
})

test("terminal subscription makes room through the client instead of exceeding the relay limit", async () => {
  const f = await fixture()
  for (let i = 0; i < 8; i++) f.client.socketSubscriptions.set(`idle/${i}`, Date.now())
  await f.adapter.subscribeTerminal(f.fresh.connection, f.fresh.keyID, f.fresh.key, (event) => f.seen.push(event))
  assert.equal(f.client.socketSubscriptions.size, 8)
  assert.equal(f.client.socketSubscriptions.has("idle/0"), false)
  assert.equal(f.client.socketSubscriptions.has("idle/1"), false)
  f.adapter.dispose()
})

test("a full socket with held channels refuses before publishing a terminal request", async () => {
  const f = await fixture()
  for (let i = 0; i < 7; i++) f.client.subscriptionHolds.set(`held/${i}`, 1)
  await assert.rejects(f.adapter.subscribeTerminal(f.fresh.connection, f.fresh.keyID, f.fresh.key, () => undefined), /cloud_read_busy/)
  assert.equal(f.client.socketSubscriptions.size, 0)
  assert.equal(f.client.subscriptionHolds.size, 7)
  f.adapter.dispose()
})

test("the no-cache receipt is subscribed before an outbound open can be published", async () => {
  const f = await fixture()
  f.setConfirm(false)
  const subscribed = f.adapter.subscribeTerminal(f.fresh.connection, f.fresh.keyID, f.fresh.key, (event) => f.seen.push(event))
  const request = { v: 1, type: "terminal_request", request_id: crypto.randomUUID(), connection: f.fresh.connection, operation: "open_connection" }
  await assert.rejects(f.adapter.publishTerminal(request), /terminal_invalid/)
  f.setConfirm(true)
  const channels = [`term/${machine}/${viewer}/${f.fresh.connection}`, `termr/${machine}/${viewer}/${f.fresh.connection}`]
  f.client._sendSubscriptionFrame("subscribe", channels)
  await subscribed
  await f.adapter.publishTerminal(request)
  assert.equal(f.published.length, 1)
  assert.deepEqual(f.stages.map((row) => row.stage), ["subscription_sent", "subscription_confirmed", "request_sent"])
  assert.deepEqual(JSON.parse(new TextDecoder().decode(await crypto.subtle.decrypt({ name: "AES-GCM", iv: base64Bytes(f.published[0]!.nonce) },
    f.master, base64Bytes(f.published[0]!.ct)))), request)
  f.adapter.dispose()
})

test("a failed signed receipt records its exact receive stage without reaching the session", async () => {
  const f = await fixture()
  await f.adapter.subscribeTerminal(f.fresh.connection, f.fresh.keyID, f.fresh.key, (event) => f.seen.push(event))
  const body = { v: 1, type: "terminal_receipt", request_id: crypto.randomUUID(), connection: f.fresh.connection,
    operation: "capture", terminal_id: "trm_test", status: "ok" }
  const valid = await f.seal(4, body)
  const wrongKey = await f.seal(3, body, "termr", crypto.getRandomValues(new Uint8Array(32)))
  await f.client._receiveEnvelope(wrongKey, false)
  await f.client._receiveEnvelope(valid, false)
  const receivedStages = f.stages.filter((row) => row.stage !== "subscription_sent" && row.stage !== "subscription_confirmed")
  assert.deepEqual(receivedStages.map((row) => row.stage),
    ["raw_received", "envelope_rejected", "raw_received", "envelope_opened"])
  assert.equal(receivedStages[1]?.code, "terminal_bad_key")
  assert.equal(receivedStages[3]?.channel, "termr")
  assert.equal(f.seen.length, 1)
  f.adapter.dispose()
})

test("a Go-sealed terminal receipt passes the browser signature and AES checks", async () => {
  // Produced with internal/domain/cloud.Seal using fixed synthetic key, seed and nonce.
  const senderKey = await crypto.subtle.importKey("raw", base64Bytes(goReceipt.sender_key), "Ed25519", false, ["verify"])
  const masterKey = await crypto.subtle.generateKey({ name: "AES-GCM", length: 256 }, false, ["encrypt"])
  const events = new Set<(event: { type: string; channels?: string[] }) => void>()
  const client: TerminalCloudClient = {
    deviceID: viewer, devicePrivateKey: null, ready: true, retired: false,
    nextSequence: async () => 1, socketSubscriptions: new Map(), subscriptionHolds: new Map(),
    pendingSubscriptions: new Set(), subscriptionLimit: 8,
    _trimSubscriptions() {},
    _outboundMachinePairing: async () => ({ masterKey, keyID: "ms-1", senderKey, senderID: machine }),
    _send: () => undefined,
    _sendSubscriptionFrame: (_type, channels) => queueMicrotask(() => events.forEach((fn) => fn({ type: "subscriptions", channels }))),
    _receiveEnvelope: async () => undefined,
    events(fn) { events.add(fn); return () => { events.delete(fn) } },
  }
  const adapter = new TerminalChannelTransport(client, machine)
  const seen: unknown[] = []
  const connection = goReceipt.plaintext.connection
  await adapter.subscribeTerminal(connection, goReceipt.envelope.key_id, base64Bytes(goReceipt.content_key),
    (event) => seen.push(event))
  await client._receiveEnvelope(goReceipt.envelope, false)
  assert.deepEqual((seen[0] as { plaintext: unknown }).plaintext, goReceipt.plaintext)
  adapter.dispose()
})

test("the machine signature, viewer route, key and sequence all gate a receipt", async () => {
  const f = await fixture()
  await f.adapter.subscribeTerminal(f.fresh.connection, f.fresh.keyID, f.fresh.key, (event) => f.seen.push(event))
  const body = { v: 1, type: "terminal_receipt", request_id: crypto.randomUUID(), connection: f.fresh.connection,
    operation: "open_connection", status: "ok" }
  const good = await f.seal(2, body)
  assert.deepEqual(await f.adapter.openTerminalEnvelope(good, f.fresh.connection), body)
  await assert.rejects(f.adapter.openTerminalEnvelope(good, f.fresh.connection), /terminal_bad_envelope/)
  await assert.rejects(f.adapter.openTerminalEnvelope({ ...(await f.seal(3, body)), ch: `termr/${machine}/wrong/${f.fresh.connection}` }, f.fresh.connection), /terminal_wrong_viewer/)
  await assert.rejects(f.adapter.openTerminalEnvelope({ ...(await f.seal(3, body)), key_id: "rk-old" }, f.fresh.connection), /terminal_bad_envelope/)
  await assert.rejects(f.adapter.openTerminalEnvelope(await f.seal(3, body, "termr", crypto.getRandomValues(new Uint8Array(32))), f.fresh.connection), /terminal_bad_key/)
  f.adapter.dispose()
})

test("a frame delivered first cannot discard a lower-sequence receipt on the other channel", async () => {
  const f = await fixture()
  await f.adapter.subscribeTerminal(f.fresh.connection, f.fresh.keyID, f.fresh.key, (event) => f.seen.push(event))
  const receipt = { v: 1, type: "terminal_receipt", request_id: crypto.randomUUID(), connection: f.fresh.connection,
    operation: "capture", terminal_id: "trm_test", status: "ok" }
  const terminalFrame = { v: 1, type: "terminal_frame", terminal_id: "trm_test", connection: f.fresh.connection,
    frame_seq: 1, captured_at: Date.now() / 1000,
    frame: { rev: "screen", at: Date.now() / 1000, cols: 80, rows: 1, lines: ["ready"],
      cursor: { x: 0, y: 0, visible: true }, modes: { app_cursor: false, app_keypad: false, mouse_sgr: false,
        alt: false, mouse: "none" } } }
  const frameEnvelope = await f.seal(12, terminalFrame, "term")
  const receiptEnvelope = await f.seal(11, receipt, "termr")
  await f.client._receiveEnvelope(frameEnvelope, false)
  await f.client._receiveEnvelope(receiptEnvelope, false)
  assert.deepEqual(f.seen.map((event) => (event as { plaintext: { type: string } }).plaintext.type),
    ["terminal_frame", "terminal_receipt"])
  await assert.rejects(f.adapter.openTerminalEnvelope(receiptEnvelope, f.fresh.connection), /terminal_bad_envelope/)
  f.adapter.dispose()
})

test("a verified term frame can be observed once on the authenticated socket", async () => {
  const f = await fixture()
  await f.adapter.subscribeTerminal(f.fresh.connection, f.fresh.keyID, f.fresh.key, (event) => f.seen.push(event))
  const invalid = await f.seal(4, { v: 1, type: "terminal_frame", connection: f.fresh.connection,
    terminal_id: "trm_test", frame_seq: 1, captured_at: Date.now() / 1000, frame: { lines: [] } }, "term")
  await f.client._receiveEnvelope(invalid, false)
  assert.throws(() => f.adapter.observeTerminalFrame(invalid), /terminal_bad_envelope/)
  const frame = await f.seal(5, { v: 1, type: "terminal_frame", connection: f.fresh.connection,
    terminal_id: "trm_test", frame_seq: 2, captured_at: Date.now() / 1000,
    frame: { rev: "full", at: Date.now() / 1000, cols: 80, rows: 1, lines: ["ready"],
      cursor: { x: 0, y: 0, visible: true }, modes: { app_cursor: false, app_keypad: false, mouse_sgr: false,
        alt: false, mouse: "none" } } }, "term")
  await f.client._receiveEnvelope(frame, false)
  assert.equal(f.observed.length, 0, "decryption alone is not acceptance by the terminal session")
  f.adapter.observeTerminalFrame(frame)
  assert.deepEqual(f.observed, [{ type: "terminal_frame_observed", machine, viewer,
    connection: f.fresh.connection, envelope_seq: 5 }])
  assert.throws(() => f.adapter.observeTerminalFrame(frame), /terminal_bad_envelope/)
  const wrong = { ...frame, seq: 6 }
  assert.throws(() => f.adapter.observeTerminalFrame(wrong), /terminal_bad_envelope/)
  f.adapter.dispose()
})

test("a revocation notice reaches the tab only through a signed current receipt route and key", async () => {
  const f = await fixture()
  await f.adapter.subscribeTerminal(f.fresh.connection, f.fresh.keyID, f.fresh.key, (event) => f.seen.push(event))
  const notice = { v: 1, type: "terminal_notice", connection: f.fresh.connection,
    code: "terminal_access_revoked", machine_incarnation: "machine-start" }
  const wrongRoute = await f.seal(3, notice)
  wrongRoute.ch = `termr/${machine}/wrong/${f.fresh.connection}`
  await f.client._receiveEnvelope(wrongRoute, false)
  const wrongKey = await f.seal(4, notice, "termr", crypto.getRandomValues(new Uint8Array(32)))
  await f.client._receiveEnvelope(wrongKey, false)
  const wrongSignature = await f.seal(5, notice)
  wrongSignature.sig = bytesBase64(crypto.getRandomValues(new Uint8Array(64)))
  await f.client._receiveEnvelope(wrongSignature, false)
  assert.equal(f.seen.length, 0, "invalid envelopes cannot change the session")
  const valid = await f.seal(6, notice)
  await f.client._receiveEnvelope(valid, false)
  assert.equal(f.seen.length, 1)
  assert.deepEqual((f.seen[0] as { plaintext: unknown }).plaintext, notice)
  f.adapter.dispose()
})

test("a typed relay subscription refusal keeps its source code", async () => {
  const f = await fixture()
  f.setConfirm(false)
  const subscribed = f.adapter.subscribeTerminal(f.fresh.connection, f.fresh.keyID, f.fresh.key, (event) => f.seen.push(event))
  await new Promise<void>((resolve) => setTimeout(resolve, 0))
  f.emitRelay({ type: "error", error: { code: "forbidden", subscriptionChannels: [
    `term/${machine}/${viewer}/${f.fresh.connection}`] } })
  await assert.rejects(subscribed, /forbidden/)
  assert.equal(f.seen.length, 0)
  f.adapter.dispose()
})

test("a forbidden error for another shared socket channel does not deny a terminal subscription", async () => {
  const f = await fixture()
  f.setConfirm(false)
  const subscribed = f.adapter.subscribeTerminal(f.fresh.connection, f.fresh.keyID, f.fresh.key, () => undefined)
  await new Promise<void>((resolve) => setTimeout(resolve, 0))
  f.emitRelay({ type: "error", error: { code: "forbidden", subscriptionChannels: ["t/other/session"] } })
  f.setConfirm(true)
  f.client._sendSubscriptionFrame("subscribe", [
    `term/${machine}/${viewer}/${f.fresh.connection}`,
    `termr/${machine}/${viewer}/${f.fresh.connection}`,
  ])
  await subscribed
  assert.deepEqual(f.stages.filter((row) => row.stage === "relay_error"), [])
  f.adapter.dispose()
})

test("a retracted terminal subscription reports the relay budget refusal promptly", async () => {
  const f = await fixture()
  f.setConfirm(false)
  const subscribed = f.adapter.subscribeTerminal(f.fresh.connection, f.fresh.keyID, f.fresh.key, () => undefined)
  await new Promise<void>((resolve) => setTimeout(resolve, 0))
  const channel = `term/${machine}/${viewer}/${f.fresh.connection}`
  f.client.socketSubscriptions.delete(channel)
  f.emitRelay({ type: "error", error: { code: "terminal_budget_exhausted", subscriptionChannels: [channel] } })
  await assert.rejects(subscribed, /terminal_budget_exhausted/)
  assert.deepEqual(f.stages.map((row) => [row.stage, row.code ?? ""]), [
    ["subscription_sent", ""], ["relay_error", "terminal_budget_exhausted"],
    ["subscription_refused", "terminal_budget_exhausted"],
  ])
  f.adapter.dispose()
})

test("a relay publish refusal identifies only its pending terminal request", async () => {
  const f = await fixture()
  await f.adapter.subscribeTerminal(f.fresh.connection, f.fresh.keyID, f.fresh.key, (event) => f.seen.push(event))
  const requestID = crypto.randomUUID()
  await f.adapter.publishTerminal({ v: 1, type: "terminal_request", request_id: requestID,
    connection: f.fresh.connection, operation: "list" })
  f.emitRelay({ type: "publish_error", ch: `termi/${machine}/${viewer}`, seq: 7, code: "rate_limited" })
  assert.deepEqual(f.seen, [{ error: "rate_limited", requestID }])
  assert.ok(f.stages.some((row) => row.stage === "publish_refused" && row.code === "rate_limited"))
  f.adapter.dispose()
})

// Offline fault injection: a receipt the relay delivered, refused at a
// distinct browser stage. Each case is read from the copyable text alone.
const receiptFaults: Array<{ name: string; code: string; inject: (f: Awaited<ReturnType<typeof fixture>>, body: Record<string, unknown>) => Promise<void> }> = [
  { name: "wrong content key", code: "terminal_bad_key", inject: async (f, body) => {
    await f.client._receiveEnvelope(await f.seal(5, body, "termr", crypto.getRandomValues(new Uint8Array(32))), false) } },
  { name: "wrong key id", code: "terminal_key_id_mismatch", inject: async (f, body) => {
    await f.client._receiveEnvelope({ ...(await f.seal(5, body)), key_id: "rk-old" }, false) } },
  { name: "out of order", code: "terminal_out_of_order", inject: async (f, body) => {
    const later = await f.seal(6, { ...body, request_id: crypto.randomUUID() })
    const earlier = await f.seal(5, body)
    await f.client._receiveEnvelope(later, false)
    await f.client._receiveEnvelope(earlier, false) } },
  { name: "relay delivered, signature refused", code: "terminal_bad_signature", inject: async (f, body) => {
    const envelope = await f.seal(5, body)
    const other = await crypto.subtle.generateKey("Ed25519", false, ["sign", "verify"])
    envelope.sig = bytesBase64(new Uint8Array(await crypto.subtle.sign("Ed25519", other.privateKey, envelopeSigningBytes(envelope))))
    await f.client._receiveEnvelope(envelope, false) } },
  { name: "relay delivered to a retired connection", code: "terminal_old_connection", inject: async (f, body) => {
    const envelope = await f.seal(5, body)
    f.adapter.unsubscribeTerminal(f.fresh.connection)
    await f.client._receiveEnvelope(envelope, false) } },
]
for (const fault of receiptFaults) {
  test(`fault injection: ${fault.name} stops at verify_decrypt with ${fault.code}`, async () => {
    const f = await fixture()
    await f.adapter.subscribeTerminal(f.fresh.connection, f.fresh.keyID, f.fresh.key, (event) => f.seen.push(event))
    const requestID = crypto.randomUUID()
    const body = { v: 1, type: "terminal_receipt", request_id: requestID, connection: f.fresh.connection,
      operation: "capture", terminal_id: "trm_test", status: "ok", result: { lines: ["secret screen"] } }
    await fault.inject(f, body)
    const text = f.observation.text()
    assert.equal(text.split("\n")[1]!.replace(/ seq=\d+$/, ""), `stopped phase=verify_decrypt stage=envelope_rejected code=${fault.code}`)
    assert.equal(f.seen.filter((event) => (event as { plaintext?: { request_id?: string } }).plaintext?.request_id === requestID).length, 0)
    for (const secret of [f.fresh.connection, f.fresh.keyID, bytesBase64(f.fresh.key), requestID, machine, viewer, "secret screen", "trm_test"]) {
      assert.equal(text.includes(secret), false, `diagnostics carry ${secret}`)
    }
    f.adapter.dispose()
  })
}

test("a turned-off or broken timeline leaves receipt delivery and refusal unchanged", async () => {
  for (const sink of [null, () => { throw new Error("sink failed") }]) {
    const f = await fixture()
    const quiet = new TerminalChannelTransport(f.client, machine, new TerminalObservation(sink))
    const seen: unknown[] = []
    await quiet.subscribeTerminal(f.fresh.connection, f.fresh.keyID, f.fresh.key, (event) => seen.push(event))
    const body = { v: 1, type: "terminal_receipt", request_id: crypto.randomUUID(), connection: f.fresh.connection,
      operation: "capture", terminal_id: "trm_test", status: "ok" }
    await f.client._receiveEnvelope(await f.seal(5, body, "termr", crypto.getRandomValues(new Uint8Array(32))), false)
    await f.client._receiveEnvelope(await f.seal(6, body), false)
    assert.deepEqual(seen.map((event) => (event as { plaintext: unknown }).plaintext), [body])
    quiet.dispose(); f.adapter.dispose()
  }
})

class FakeChannel implements DirectChannelLike {
  readyState = "connecting"
  sent: string[] = []
  onopen: ((event?: unknown) => void) | null = null
  onclose: ((event?: unknown) => void) | null = null
  onerror: ((event?: unknown) => void) | null = null
  onmessage: ((event: { data: unknown }) => void) | null = null
  send(data: string): void { this.sent.push(data) }
  close(): void { this.readyState = "closed" }
  deliver(envelope: unknown): void { this.onmessage?.({ data: JSON.stringify({ t: "env", e: envelope }) }) }
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
const settle = () => new Promise<void>((resolve) => setTimeout(resolve, 5))
const fullFrame = (connection: string, frameSeq: number) => ({ v: 1, type: "terminal_frame", connection, terminal_id: "trm_test",
  frame_seq: frameSeq, captured_at: Date.now() / 1000, frame: { rev: `r${frameSeq}`, at: Date.now() / 1000, cols: 80, rows: 1, lines: ["ready"],
    cursor: { x: 0, y: 0, visible: true }, modes: { app_cursor: false, app_keypad: false, mouse_sgr: false, alt: false, mouse: "none" } } })
async function directFixture() {
  const peer = new FakePeer()
  const f = await fixture(() => peer)
  await f.adapter.subscribeTerminal(f.fresh.connection, f.fresh.keyID, f.fresh.key, (event) => f.seen.push(event))
  const down: string[] = []
  const offer = await f.adapter.prepareDirect(f.fresh.connection, (code) => down.push(code))
  await offer.answer("v=0 answer")
  const dc = () => peer.channel.sent.map((text) => JSON.parse(text) as { t: string; e?: TerminalEnvelope; connection?: string; frame_seq?: number })
  return { ...f, peer, offer, down, dc }
}
const request = (connection: string, operation: string, body?: unknown) => ({ v: 1, type: "terminal_request",
  request_id: crypto.randomUUID(), connection, operation, ...(body ? { body } : {}) })

test("the offer is sealed under the current connection's key with its connection as additional data", async () => {
  const f = await directFixture()
  const sealed = base64Bytes(f.offer.sealed)
  const key = await crypto.subtle.importKey("raw", f.fresh.key.slice().buffer as ArrayBuffer, "AES-GCM", false, ["decrypt"])
  const sdp = await crypto.subtle.decrypt({ name: "AES-GCM", iv: sealed.slice(0, 12),
    additionalData: new TextEncoder().encode(`clawdline-direct-offer-v1/${f.fresh.connection}`) }, key, sealed.slice(12))
  assert.equal(new TextDecoder().decode(sdp), "v=0 offer")
  assert.equal(f.offer.open, true)
  f.adapter.dispose()
  assert.equal(f.offer.open, false)
})

test("carrier selection: a direct connection's requests and its switching rekey use the DC, activation and relay connections the relay", async () => {
  const f = await directFixture()
  const next = freshTerminalConnection()
  await f.adapter.subscribeTerminal(next.connection, next.keyID, next.key, () => undefined)
  await f.adapter.publishTerminal(request(next.connection, "rekey_connection", { old_connection: f.fresh.connection, carrier: "direct" }))
  assert.equal(f.published.length, 0)
  assert.deepEqual(f.dc().map((m) => [m.t, m.e?.ch, m.e?.sender]), [["env", `termi/${machine}/${viewer}`, viewer]])
  assert.equal(f.adapter.carrierOf(next.connection), "relay", "a pending rekey is not direct until its receipt says so")
  await f.adapter.publishTerminal(request(f.fresh.connection, "list"))
  assert.equal(f.published.length, 1)
  f.adapter.setCarrier(next.connection, "direct")
  assert.equal(f.adapter.carrierOf(next.connection), "direct")
  await f.adapter.publishTerminal(request(next.connection, "input", { data: "YQ==" }))
  assert.equal(f.dc().length, 2)
  await f.adapter.publishTerminal(request(next.connection, "activate_connection", { old_connection: f.fresh.connection, first_frame_seq: 1 }))
  assert.equal(f.published.length, 2)
  assert.equal(f.dc().length, 2)
  f.peer.channel.onclose?.()
  assert.deepEqual(f.down, ["terminal_direct_closed"])
  await assert.rejects(f.adapter.publishTerminal(request(next.connection, "input", { data: "YQ==" })), /terminal_direct_closed/)
  await assert.rejects(f.adapter.publishTerminal(request(f.fresh.connection, "rekey_connection", { old_connection: next.connection, carrier: "direct" })),
    /terminal_direct_closed/)
  assert.equal(f.published.length, 2, "a refused direct request never falls through to the relay")
  f.adapter.dispose()
})

test("DC frames pass the relay's verification and are acked on the DC only after the session observes them", async () => {
  const f = await directFixture()
  f.adapter.setCarrier(f.fresh.connection, "direct")
  const forged = await f.seal(1, fullFrame(f.fresh.connection, 1), "term")
  forged.sig = (await f.seal(1, fullFrame(f.fresh.connection, 9), "term")).sig
  f.peer.channel.deliver(forged)
  await settle()
  assert.equal(f.seen.length, 0)
  assert.equal(f.stages.at(-1)?.code, "terminal_bad_signature")
  assert.throws(() => f.adapter.observeTerminalFrame(forged), /terminal_bad_envelope/)
  const wrongKey = await f.seal(2, fullFrame(f.fresh.connection, 1), "term", crypto.getRandomValues(new Uint8Array(32)))
  f.peer.channel.deliver(wrongKey)
  await settle()
  assert.equal(f.seen.length, 0)
  const frame = await f.seal(3, fullFrame(f.fresh.connection, 4), "term")
  f.peer.channel.deliver(frame)
  await settle()
  assert.equal(f.seen.length, 1)
  assert.equal(f.dc().filter((m) => m.t === "ack").length, 0, "decryption alone is not an ack")
  f.adapter.observeTerminalFrame(frame)
  assert.deepEqual(f.dc().filter((m) => m.t === "ack"), [{ t: "ack", connection: f.fresh.connection, frame_seq: 4 }])
  assert.equal(f.observed.length, 0, "a DC frame never sends terminal_frame_observed to the relay")
  f.peer.channel.deliver(frame)
  await settle()
  assert.equal(f.seen.length, 1, "a replayed DC envelope is refused like a relay one")
  assert.throws(() => f.adapter.observeTerminalFrame(frame), /terminal_bad_envelope/)
  f.adapter.dispose()
})

test("a relay frame for a direct connection and a DC frame for a relay connection are dropped", async () => {
  const f = await directFixture()
  f.adapter.setCarrier(f.fresh.connection, "direct")
  await f.client._receiveEnvelope(await f.seal(1, fullFrame(f.fresh.connection, 1), "term"), false)
  assert.equal(f.seen.length, 0)
  assert.equal(f.stages.at(-1)?.code, "terminal_wrong_carrier")
  await f.client._receiveEnvelope(await f.seal(1, { v: 1, type: "terminal_receipt", request_id: crypto.randomUUID(),
    connection: f.fresh.connection, operation: "list", status: "ok" }), false)
  assert.equal(f.seen.length, 1, "receipts stay on the relay for a direct connection")
  f.adapter.setCarrier(f.fresh.connection, "relay")
  f.peer.channel.deliver(await f.seal(2, fullFrame(f.fresh.connection, 2), "term"))
  await settle()
  assert.equal(f.seen.length, 1)
  assert.equal(f.stages.at(-1)?.code, "terminal_wrong_carrier")
  f.peer.channel.deliver(await f.seal(3, { v: 1, type: "terminal_receipt", request_id: crypto.randomUUID(),
    connection: f.fresh.connection, operation: "list", status: "ok" }))
  await settle()
  assert.equal(f.seen.length, 1, "a receipt never arrives on the DC")
  assert.equal(f.stages.at(-1)?.code, "terminal_direct_wrong_channel")
  f.adapter.dispose()
})

test("a carrier probe on termr is verified and discarded", async () => {
  const f = await fixture()
  await f.adapter.subscribeTerminal(f.fresh.connection, f.fresh.keyID, f.fresh.key, (event) => f.seen.push(event))
  await f.client._receiveEnvelope(await f.seal(1, { v: 1, type: "terminal_carrier_probe", connection: f.fresh.connection, n: 1 }), false)
  assert.equal(f.seen.length, 0)
  assert.equal(f.stages.some((row) => row.stage === "envelope_rejected"), false)
  const forged = await f.seal(2, { v: 1, type: "terminal_carrier_probe", connection: f.fresh.connection, n: 2 })
  forged.sig = (await f.seal(2, { v: 1, type: "terminal_carrier_probe", connection: f.fresh.connection, n: 3 })).sig
  await f.client._receiveEnvelope(forged, false)
  assert.equal(f.stages.at(-1)?.code, "terminal_bad_signature")
  await f.client._receiveEnvelope(await f.seal(3, { v: 1, type: "terminal_carrier_probe", connection: "other", n: 4 }), false)
  assert.equal(f.stages.at(-1)?.code, "terminal_bad_probe")
  assert.equal(f.seen.length, 0)
  f.adapter.dispose()
})
