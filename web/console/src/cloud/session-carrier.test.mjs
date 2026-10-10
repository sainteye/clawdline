// Session reads on the viewer's direct carrier: the subscription they do not
// spend, and every way the carrier can fail ending on the relay.
import { test } from "node:test"
import assert from "node:assert/strict"
import { StatusCloudClient } from "./status-client.js"
import { bytesBase64, importMasterSecret, sealEnvelope } from "../legacy/js/net/cloud-crypto.js"

const genA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
/** Lets the reader's own promise chain run: a carrier publish and its fallback are both async. */
const flush = () => new Promise((resolve) => setImmediate(resolve))
const KEY = "m\u0000s\u0000transcript"

/** A client with the fields the carrier path touches, and a carrier it may or may not have. */
function carrierClient(carrier) {
  const client = Object.create(StatusCloudClient.prototype)
  const sent = []
  const subscribed = []
  Object.assign(client, {
    ready: true, retired: false, deviceID: "viewer", allowWrites: true, now: () => 1000,
    readTimeoutMs: 60_000, subscriptionLimit: 8, subscriptionIdleMs: 30_000,
    readWaiters: new Map(), pinnedReadProofs: new Map(), pendingBySequence: new Map(),
    carrierReads: new Map(), carrierSequences: new Map(), carrierCapabilities: new Map(),
    carrierWatched: new WeakSet(), carrierMachines: new Set(),
    machineOffline: new Map(), machineLacks: new Map(),
    orchestratorSnapshots: new Map(), machineDescriptors: new Map(), macCapabilities: new Map(),
    socketSubscriptions: new Map(), subscriptionHolds: new Map(), pendingSubscriptions: new Set(),
    resubscribes: new Map(), transcriptSnapshots: new Map(), sessionSequenceByKey: new Map(),
    listeners: new Set(), trail: { sealed() { }, step() { }, refused() { } },
    timers: [], sent, subscribed,
    setTimeout(fn, ms) { client.timers.push({ fn, ms }); return client.timers.length },
    clearTimeout(id) { client.timers[id - 1] = null },
    _sendSubscriptionFrame(type, channels) { subscribed.push({ type, channels }) },
    _observeMachine() { },
    readContentCapabilities: new Map(),
    classicSessionMachine: null, classicSessionReads: new Map(),
    // The one seam these tests stand in for: everything above it is the real reader.
    async _publishCommand(machine, type, body, envelopeClass, pending) {
      sent.push({ machine, type, body, carrier: pending?.carrier ?? null, key: pending?.key ?? null })
      if (client.publishFails) { client.publishFails = false; throw Object.assign(new Error("gone"), { code: "carrier_closed" }) }
      // What the real publisher leaves behind: a sequence registered for its ACK.
      const ref = { sender: "viewer", seq: sent.length }
      if (pending?.waiters) pending.waiters.ref = ref
      client.pendingBySequence.set(ref.seq, { ref, machine, key: pending?.key ?? null, ack: null })
    },
  })
  client.useDirectCarriers(carrier ? { for: () => carrier } : null)
  return client
}

/** A carrier that is open and records what was written to it. */
function openCarrier() {
  const down = []
  return {
    open: true, written: [], down, closed: null, generation: 1,
    whenDown(listener) { down.push(listener); return () => { } },
    send(envelope) { this.written.push(envelope) },
    async ensure() { return this.open },
    current() { return this.open ? {} : null },
    // The real carrier's close: the channel goes, and everyone waiting on it is told once.
    close(code = "carrier_closed") {
      if (this.closed) return
      this.closed = code
      this.open = false
      for (const listener of [...down]) listener(code)
    },
    failed(generation, code) { if (generation === this.generation) this.close(code) },
  }
}

/** The carrier path with real envelopes: everything but the pairing and the snapshot is the reader's own. */
async function sealedClient() {
  const signing = await crypto.subtle.generateKey({ name: "Ed25519" }, true, ["sign", "verify"])
  const raw = new Uint8Array(32).fill(7)
  const masterKey = await importMasterSecret(raw)
  const carrier = openCarrier()
  const client = carrierClient(carrier)
  const applied = []
  Object.assign(client, {
    _machinePairing: async () => ({ masterKey, keyID: "mk", senderKey: signing.publicKey, senderID: "m" }),
    _compareKeyID() { },
    _sawAuthenticatedEnvelope() { },
    _applySnapshot(channel, payload) { applied.push([channel.kind, payload?.read]) },
    _recordReceiveFailure(error) { applied.push(["failed", error.code ?? error.message]) },
  })
  const envelope = (seq, read) => sealEnvelope({ ch: "t/m/s", seq, ts: 1, class: "stream",
    key_id: "mk", sender: "m" }, JSON.stringify({ read, status: 200 }), masterKey, signing.privateKey)
  return { client, carrier, applied, envelope, masterKey, signing, raw }
}

const read = (client) => client._read({ machine: "m", session: "s" }, "transcript",
  { machine_id: "m", expected_generation: genA }, "transcript", 30_000, {})

test("a read on an open carrier spends no relay subscription channel", async () => {
  const carrier = openCarrier()
  const client = carrierClient(carrier)
  const answer = read(client)
  assert.equal(client.carrierReads.size, 1)
  await flush()
  assert.deepEqual([...client.subscribed], [], "the carrier read subscribed to a relay channel")
  assert.equal(client.subscriptionHolds.size, 0)
  assert.equal(client.socketSubscriptions.size, 0)
  assert.deepEqual(client.sent.map((row) => [row.type, row.carrier === carrier]), [["transcript", true]])
  client._settleRead(KEY, { entries: [] }, null)
  assert.deepEqual(await answer, { entries: [] })

  // Nothing is left behind: the next read of the same session is not refused as busy, and the
  // flight the carrier was given does not outlive the answer it was waiting for.
  assert.equal(client.readWaiters.size, 0)
  assert.equal(client.pendingBySequence.size, 0)
  assert.equal(client.carrierReads.size, 0)
})

test("fault injection: a carrier that dies holding a read has it asked again on the relay", async () => {
  const carrier = openCarrier()
  const client = carrierClient(carrier)
  const answer = read(client)
  await flush()
  assert.equal(client.sent.length, 1)
  const timers = client.timers.filter(Boolean).length

  // The channel is gone with the request already written to it. A read has no effect, so it is
  // asked again rather than failed: the page is never told "could not read".
  carrier.open = false
  for (const listener of carrier.down) listener("carrier_closed")
  await flush()
  assert.deepEqual([...client.subscribed], [{ type: "subscribe", channels: ["t/m/s"] }])
  assert.deepEqual(client.sent.map((row) => row.carrier === null), [false, true],
    "the second ask did not go to the relay")
  assert.equal(client.carrierReads.size, 0)
  assert.equal(client.timers.filter(Boolean).length, timers,
    "the fallback started a second timeout, so a carrier read could wait twice as long")
  assert.equal(client.pendingBySequence.size, 1, "the spent carrier sequence is still waiting for a relay ACK")
  client._settleRead(KEY, { entries: ["from the relay"] }, null)
  assert.deepEqual(await answer, { entries: ["from the relay"] })
})

test("fault injection: a carrier that refuses the write loses nothing", async () => {
  const carrier = openCarrier()
  const client = carrierClient(carrier)
  client.publishFails = true
  const answer = read(client)
  await flush()
  assert.deepEqual([...client.subscribed], [{ type: "subscribe", channels: ["t/m/s"] }])
  assert.equal(client.sent.length, 2)
  assert.equal(client.sent[1].carrier, null)
  client._settleRead(KEY, { entries: [] }, null)
  assert.deepEqual(await answer, { entries: [] })
})

test("a machine that says nothing about carriers is never offered one, and its reads take the relay", async () => {
  const carrier = openCarrier()
  carrier.open = false
  carrier.current = () => null
  const client = carrierClient(carrier)
  assert.equal(client.carrierSupported("m"), false)
  assert.equal(client._machineImplements("m", "carrier-offer"), "no")

  // An older daemon publishes no flag at all, so a new page never sends it an offer.
  client._emit({ type: "orchestrator", machine: "m", data: { at: 1, machine: { read_content_v1: true } } })
  assert.equal(client.carrierSupported("m"), false)
  client._emit({ type: "orchestrator", machine: "m", data: { at: 2, machine: { session_carrier_v1: true } } })
  assert.equal(client.carrierSupported("m"), true)
  assert.equal(client._machineImplements("m", "carrier-offer"), "yes")

  // While the carrier is not open the read goes over the relay, with its subscription.
  read(client)
  await flush()
  assert.deepEqual([...client.subscribed], [{ type: "subscribe", channels: ["t/m/s"] }])
  assert.equal(client.sent[0].carrier, null)
})

test("the carrier numbers its envelopes apart from the relay, so a low one is not a replay", async () => {
  const { client, applied, envelope, masterKey, signing, raw } = await sealedClient()

  // The relay's sender sequence is far ahead; the carrier's own starts at one.
  client.sequenceBySender = new Map([["m", 900]])
  await client.receiveCarrierEnvelope("m", await envelope(1, "transcript"), 1)
  await client.receiveCarrierEnvelope("m", await envelope(2, "info.full"), 1)
  assert.deepEqual(applied, [["transcript", "transcript"], ["transcript", "info.full"]])

  // Its own sequence still has to advance.
  applied.length = 0
  await client.receiveCarrierEnvelope("m", await envelope(2, "transcript"), 1)
  assert.deepEqual(applied, [["failed", "replay"]])

  // A channel the carrier may not carry is refused whatever its signature says.
  applied.length = 0
  await client.receiveCarrierEnvelope("m", await sealEnvelope({ ch: "s/m/s", seq: 9, ts: 1,
    class: "stream", key_id: "mk", sender: "m" }, "{}", masterKey, signing.privateKey), 1)
  assert.deepEqual(applied, [["failed", "bad_channel"]])
  assert.equal(bytesBase64(raw).length, 44)
})

test("a carrier the page replaced counts from one again, and its answers are not replays", async () => {
  const { client, applied, envelope } = await sealedClient()

  // Two answers on the carrier this page opened first.
  await client.receiveCarrierEnvelope("m", await envelope(1, "transcript"), 1)
  await client.receiveCarrierEnvelope("m", await envelope(2, "info.full"), 1)
  assert.deepEqual(applied, [["transcript", "transcript"], ["transcript", "info.full"]])

  // That channel died without this page hearing it go, and the page opened another. The machine
  // counts per peer, so the new channel's first answer is number one again. Measured on
  // 2026-10-11: a floor kept per machine read it as a replay, the answer was dropped, and the
  // read that was waiting for it timed out on a carrier that was open — which nothing falls
  // back from, because nothing had failed.
  applied.length = 0
  await client.receiveCarrierEnvelope("m", await envelope(1, "transcript"), 2)
  assert.deepEqual(applied, [["transcript", "transcript"]])

  // The channel that was replaced cannot push the floor back under the one in use.
  applied.length = 0
  await client.receiveCarrierEnvelope("m", await envelope(9, "transcript"), 1)
  assert.deepEqual(applied, [["failed", "replay"]])

  // And the one in use still has to advance.
  applied.length = 0
  await client.receiveCarrierEnvelope("m", await envelope(1, "transcript"), 2)
  assert.deepEqual(applied, [["failed", "replay"]])
})

test("fault injection: a carrier whose answer this page cannot accept is closed, and the read is asked again on the relay", async () => {
  const { client, carrier, applied, envelope } = await sealedClient()
  const answer = read(client)
  await flush()
  assert.equal(client.sent.length, 1)
  assert.equal(client.sent[0].carrier, carrier)

  // The machine's pairing is gone from under this page, so what the carrier delivers cannot be
  // opened. An open carrier whose envelopes this page refuses answers no read ever again, so it
  // is closed rather than left in place: closing it is what tells the machine, and what hands
  // every read it was holding back to the relay.
  client._machinePairing = async () => null
  await client.receiveCarrierEnvelope("m", await envelope(1, "transcript"), 1)
  await flush()
  assert.deepEqual(applied, [["failed", "machine_not_paired"]])
  assert.equal(carrier.closed, "carrier_unusable")
  assert.deepEqual([...client.subscribed], [{ type: "subscribe", channels: ["t/m/s"] }])
  assert.deepEqual(client.sent.map((row) => row.carrier === null), [false, true],
    "the read was not asked again on the relay")
  client._settleRead(KEY, { entries: ["from the relay"] }, null)
  assert.deepEqual(await answer, { entries: ["from the relay"] })
})

test("a client that renews its credentials lets go of its carrier, so the machine hears it go", () => {
  const carrier = openCarrier()
  const client = carrierClient(carrier)
  Object.assign(client, {
    handlers: null, readyWaiters: [],
    _forgetSocketSubscriptions() { }, _disarmViewerEvents() { }, _closeTabChannel() { },
    _settleReady() { }, _endSessionRecovery() { }, _failAllReads() { },
  })
  assert.ok(client._carrierFor("m"))

  // A renewal hands the page to a replacement client, which opens its own carrier. The machine
  // keeps one carrier per viewer and has no idle bound, so the retired client's channel has to
  // go with it: measured on 2026-10-11, a channel left behind by a renewal was still held by the
  // machine, and the replacement's offer was refused as busy.
  client.retire()
  assert.equal(carrier.closed, "carrier_client_retired")
  assert.equal(client.retired, true)
})
