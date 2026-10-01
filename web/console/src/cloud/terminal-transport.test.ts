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
  const events = new Set<(event: { type: string; channels?: string[]; code?: string }) => void>()
  const published: TerminalEnvelope[] = []
  let confirm = true
  const client: TerminalCloudClient = {
    deviceID: viewer, devicePrivateKey: sender.privateKey, ready: true, retired: false,
    nextSequence: async () => 7, socketSubscriptions: new Map(), subscriptionHolds: new Map(),
    _outboundMachinePairing: async () => ({ masterKey: master, keyID: "ms-1", senderKey: machineKey.publicKey, senderID: machine }),
    _send(frame) { if ((frame as { type?: string }).type === "publish") published.push((frame as { envelope: TerminalEnvelope }).envelope) },
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
  return { adapter, client, fresh, seen, published, seal, setConfirm: (value: boolean) => { confirm = value }, master }
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
