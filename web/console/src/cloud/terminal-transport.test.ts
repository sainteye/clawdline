import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- esbuild bundles the TypeScript source for Node's test runner.
import { TerminalChannelTransport, freshTerminalConnection, type TerminalCloudClient, type TerminalEnvelope } from "./terminal-transport.ts"
import { bytesBase64, base64Bytes, envelopeSigningBytes } from "../legacy/js/net/cloud-crypto.js"

const machine = "machine_test"
const viewer = "viewer_test"
async function fixture() {
  const master = await crypto.subtle.generateKey({ name: "AES-GCM", length: 256 }, false, ["encrypt", "decrypt"])
  const sender = await crypto.subtle.generateKey("Ed25519", false, ["sign", "verify"])
  const machineKey = await crypto.subtle.generateKey("Ed25519", false, ["sign", "verify"])
  const events = new Set<(event: { type: string; channels?: string[]; code?: string; error?: { code?: string } }) => void>()
  const published: TerminalEnvelope[] = []
  const observed: unknown[] = []
  let confirm = true
  const client: TerminalCloudClient = {
    deviceID: viewer, devicePrivateKey: sender.privateKey, ready: true, retired: false,
    nextSequence: async () => 7, socketSubscriptions: new Map(), subscriptionHolds: new Map(),
    _outboundMachinePairing: async () => ({ masterKey: master, keyID: "ms-1", senderKey: machineKey.publicKey, senderID: machine }),
    _send(frame) {
      if ((frame as { type?: string }).type === "publish") published.push((frame as { envelope: TerminalEnvelope }).envelope)
      if ((frame as { type?: string }).type === "terminal_frame_observed") observed.push(frame)
    },
    _sendSubscriptionFrame(_type, channels) { if (confirm) queueMicrotask(() => events.forEach((fn) => fn({ type: "subscriptions", channels }))) },
    _receiveEnvelope: async () => undefined,
    events(fn) { events.add(fn); return () => { events.delete(fn) } },
  }
  const adapter = new TerminalChannelTransport(client, machine)
  const fresh = freshTerminalConnection()
  const seen: unknown[] = []
  const seal = async (sequence: number, plaintext: unknown, kind: "term" | "termr" = "termr", key = fresh.key): Promise<TerminalEnvelope> => {
    const nonce = crypto.getRandomValues(new Uint8Array(12))
    const imported = await crypto.subtle.importKey("raw", key.slice().buffer as ArrayBuffer, "AES-GCM", false, ["encrypt"])
    const ct = await crypto.subtle.encrypt({ name: "AES-GCM", iv: nonce }, imported, new TextEncoder().encode(JSON.stringify(plaintext)))
    const envelope: TerminalEnvelope = { v: 1, ch: `${kind}/${machine}/${viewer}/${fresh.connection}`, seq: sequence,
      ts: Date.now(), class: kind === "term" ? "stream" : "ctl", key_id: fresh.keyID, nonce: bytesBase64(nonce),
      ct: bytesBase64(new Uint8Array(ct)), sender: machine, sig: "" }
    envelope.sig = bytesBase64(new Uint8Array(await crypto.subtle.sign("Ed25519", machineKey.privateKey, envelopeSigningBytes(envelope))))
    return envelope
  }
  return { adapter, client, fresh, seen, published, observed, seal,
    emitRelay: (event: { type: string; error?: { code?: string } }) => events.forEach((fn) => fn(event)),
    setConfirm: (value: boolean) => { confirm = value }, master }
}

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
  assert.deepEqual(JSON.parse(new TextDecoder().decode(await crypto.subtle.decrypt({ name: "AES-GCM", iv: base64Bytes(f.published[0]!.nonce) },
    f.master, base64Bytes(f.published[0]!.ct)))), request)
  f.adapter.dispose()
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
  f.emitRelay({ type: "error", error: { code: "forbidden" } })
  await assert.rejects(subscribed, /forbidden/)
  assert.equal(f.seen.length, 0)
  f.adapter.dispose()
})
