import assert from "node:assert/strict"
import test from "node:test"
import type { DirectChannelLike, DirectPeerLike } from "../cloud/terminal-direct.js"
// @ts-expect-error -- a `.ts` path, so Node's strip-types runner can load it.
import { TerminalChannelTransport, freshTerminalConnection, type TerminalCloudClient } from "../cloud/terminal-transport.ts"
// @ts-expect-error -- a `.ts` path, so Node's strip-types runner can load it.
import { DirectCarrier } from "../cloud/direct-carrier.ts"
import { installViewerHealthObserver } from "../legacy/js/net/viewer-health-hook.js"

// A terminal that negotiates its own direct channel (`TerminalChannelTransport.prepareDirect`)
// records the same `viewer_carrier_replace` stage a Session read's negotiation does
// (docs/admin-connection-health-contract.md §11.2): one random correlation per negotiation,
// fixed words, no connection id, SDP or sealed offer in any record.

class FakeChannel implements DirectChannelLike {
  readyState = "connecting"
  onopen: ((event?: unknown) => void) | null = null
  onclose: ((event?: unknown) => void) | null = null
  onerror: ((event?: unknown) => void) | null = null
  onmessage: ((event: { data: unknown }) => void) | null = null
  send(): void {}
  close(): void { this.readyState = "closed" }
}
class FakePeer implements DirectPeerLike {
  localDescription: { sdp: string } | null = null
  iceGatheringState = "complete"
  channel = new FakeChannel()
  connectionState = "new"
  onicegatheringstatechange: ((event?: unknown) => void) | null = null
  onconnectionstatechange: ((event?: unknown) => void) | null = null
  private readonly opens: boolean
  constructor(opens = true) { this.opens = opens }
  createDataChannel(): DirectChannelLike { return this.channel }
  async createOffer(): Promise<{ type: string; sdp?: string }> { return { type: "offer", sdp: "v=0 terminal-offer" } }
  async setLocalDescription(d: { type: string; sdp?: string }): Promise<void> { this.localDescription = { sdp: d.sdp ?? "" } }
  async setRemoteDescription(): Promise<void> {
    queueMicrotask(() => {
      if (this.opens) { this.channel.readyState = "open"; this.channel.onopen?.() }
      else { this.connectionState = "failed"; this.onconnectionstatechange?.() }
    })
  }
  close(): void { this.channel.close() }
}

type Observation = { event: string; stage: string; class?: string; correlation?: string; error?: string; outcome?: string }
function observe(options: { throws?: boolean } = {}): { seen: Observation[]; remove: () => void } {
  const seen: Observation[] = []
  let n = 0
  const remove = installViewerHealthObserver({
    record: (o: Observation) => {
      seen.push(o)
      if (options.throws) throw new Error("observer broke")
    },
    correlation: () => `vt-${(++n).toString(16).padStart(12, "0")}`,
    monotonic: () => 0,
  })
  return { seen, remove }
}
const short = (o: Observation) => [o.event, o.stage, o.class, o.error ?? o.outcome]

let machines = 0
async function terminal(peers: () => FakePeer, options: { carrier?: boolean } = {}) {
  const machine = `machine_terminal_health_${++machines}`
  const sender = await crypto.subtle.generateKey("Ed25519", false, ["sign", "verify"])
  const master = await crypto.subtle.generateKey({ name: "AES-GCM", length: 256 }, false, ["encrypt", "decrypt"])
  const events = new Set<(event: { type: string; channels?: string[] }) => void>()
  const carrierClient = {
    deviceID: "viewer", ready: true, retired: false,
    publishCarrierOffer: async () => { throw Object.assign(new Error("unused"), { code: "carrier_failed" }) },
    receiveCarrierEnvelope: () => {}, carrierSupported: () => false,
  }
  const carrier = options.carrier ? DirectCarrier.for(carrierClient, machine, () => new FakePeer()) : null
  const client: TerminalCloudClient = {
    deviceID: "viewer", devicePrivateKey: sender.privateKey, ready: true, retired: false,
    nextSequence: async () => 1, socketSubscriptions: new Map(), subscriptionHolds: new Map(),
    pendingSubscriptions: new Set(), subscriptionLimit: 8,
    _trimSubscriptions() {},
    _outboundMachinePairing: async (id) => ({ masterKey: master, keyID: "ms-1", senderKey: sender.publicKey, senderID: id }),
    _send() {},
    _sendSubscriptionFrame(_type, channels) { queueMicrotask(() => events.forEach((fn) => fn({ type: "subscriptions", channels }))) },
    _receiveEnvelope: async () => undefined,
    events(fn) { events.add(fn); return () => { events.delete(fn) } },
    ...(carrier ? { _carrierFor: () => carrier } : {}),
  }
  const transport = new TerminalChannelTransport(client, machine, undefined, peers)
  const fresh = freshTerminalConnection()
  await transport.subscribeTerminal(fresh.connection, fresh.keyID, fresh.key, () => {})
  return { transport, fresh, carrier }
}

test("a terminal's own negotiation is one carrier replacement under one random correlation, with nothing of the offer in it", async () => {
  const { seen, remove } = observe()
  try {
    const t = await terminal(() => new FakePeer())
    const offer = await t.transport.prepareDirect(t.fresh.connection, () => {})
    await offer.answer("v=0 machine-answer")
    assert.equal(offer.open, true)
    assert.deepEqual(seen.map(short), [
      ["viewer_stage_started", "viewer_carrier_replace", "transport", undefined],
      ["viewer_stage_completed", "viewer_carrier_replace", "transport", "ok"],
    ])
    assert.equal(seen[0]!.correlation, seen[1]!.correlation)
    const leaked = JSON.stringify(seen)
    for (const secret of [t.fresh.connection, t.fresh.keyID, offer.sealed, "v=0", "machine-answer", "terminal-offer"]) {
      assert.ok(!leaked.includes(secret), `a record names ${secret}`)
    }
    // A terminal that lets its channel go on purpose records nothing more.
    offer.close()
    assert.equal(seen.length, 2)
    t.transport.dispose()
  } finally { remove() }
})

test("fault injection: ICE that finds no path is a failed replacement, and the next negotiation's recovery is its own", async () => {
  const { seen, remove } = observe()
  try {
    let opens = false
    const t = await terminal(() => new FakePeer(opens))
    const first = await t.transport.prepareDirect(t.fresh.connection, () => {})
    await assert.rejects(first.answer("v=0 machine-answer"))
    first.close()
    opens = true
    const second = await t.transport.prepareDirect(t.fresh.connection, () => {})
    await second.answer("v=0 machine-answer")
    assert.deepEqual(seen.map(short), [
      ["viewer_stage_started", "viewer_carrier_replace", "transport", undefined],
      ["viewer_stage_failed", "viewer_carrier_replace", "transport", "network_unreachable"],
      ["viewer_stage_started", "viewer_carrier_replace", "transport", undefined],
      ["viewer_stage_completed", "viewer_carrier_replace", "transport", "ok"],
    ])
    assert.notEqual(seen[0]!.correlation, seen[2]!.correlation)
    t.transport.dispose()
  } finally { remove() }
})

test("fault injection: a direct_offer that never got its answer applied is aborted, not a guessed failure", async () => {
  const { seen, remove } = observe()
  try {
    const t = await terminal(() => new FakePeer())
    const offer = await t.transport.prepareDirect(t.fresh.connection, () => {})
    // terminal-session's `direct_offer` request was refused or timed out: it closes the offer.
    offer.close()
    offer.close()
    assert.deepEqual(seen.map(short).slice(1), [["viewer_stage_completed", "viewer_carrier_replace", "transport", "aborted"]])
    t.transport.dispose()
  } finally { remove() }
})

test("fault injection: a terminal-held channel that stops carrying is a failure; the replaced one is not", async () => {
  const { seen, remove } = observe()
  try {
    const peers: FakePeer[] = []
    const t = await terminal(() => { const p = new FakePeer(); peers.push(p); return p })
    const down: string[] = []
    const offer = await t.transport.prepareDirect(t.fresh.connection, (code) => down.push(code))
    await offer.answer("v=0 machine-answer")
    seen.length = 0
    peers[0]!.channel.onclose?.()
    assert.deepEqual(down, ["terminal_direct_closed"])
    assert.deepEqual(seen.map(short), [["viewer_stage_failed", "viewer_carrier_replace", "transport", "carrier_closed"]])
    // Recovery: a new negotiation, and replacing it with yet another, records no failure.
    seen.length = 0
    const next = await t.transport.prepareDirect(t.fresh.connection, () => {})
    await next.answer("v=0 machine-answer")
    await t.transport.prepareDirect(t.fresh.connection, () => {})
    assert.deepEqual(seen.map(short).filter((s) => s[0] !== "viewer_stage_started"), [
      ["viewer_stage_completed", "viewer_carrier_replace", "transport", "ok"],
    ])
    t.transport.dispose()
  } finally { remove() }
})

test("a legacy terminal channel the page adopted reports its loss once", async () => {
  const { seen, remove } = observe()
  try {
    const peers: FakePeer[] = []
    const t = await terminal(() => { const p = new FakePeer(); peers.push(p); return p }, { carrier: true })
    const offer = await t.transport.prepareDirect(t.fresh.connection, () => {})
    await offer.answer("v=0 machine-answer")
    assert.equal(t.carrier!.open, true)
    seen.length = 0
    peers[0]!.channel.onclose?.()
    assert.equal(t.carrier!.open, false)
    assert.deepEqual(seen.map(short), [["viewer_stage_failed", "viewer_carrier_replace", "transport", "carrier_closed"]])
    t.transport.dispose()
  } finally { remove() }
})

test("an observer that throws never fails a terminal's negotiation", async () => {
  const { seen, remove } = observe({ throws: true })
  try {
    const t = await terminal(() => new FakePeer())
    const offer = await t.transport.prepareDirect(t.fresh.connection, () => {})
    await offer.answer("v=0 machine-answer")
    assert.equal(offer.open, true)
    assert.equal(seen.length, 2)
    t.transport.dispose()
  } finally { remove() }
})
