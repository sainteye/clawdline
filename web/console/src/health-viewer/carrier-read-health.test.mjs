// Reads that travel on the viewer's direct carrier record the same connection-health trace as a
// relay read (docs/admin-connection-health-contract.md §11.2): a carrier that takes the request
// and never answers is a timed-out read, a carrier that gives the read up is a carrier
// replacement under the read's correlation, and an envelope the carrier delivered that would not
// open is a decrypt-apply failure. None of it depends on the relay socket having noticed.
import { test } from "node:test"
import assert from "node:assert/strict"
import { installViewerHealthObserver } from "../legacy/js/net/viewer-health-hook.js"
import { StatusCloudClient } from "../cloud/status-client.js"
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
    carrierRowSequences: new Map(),
    carrierWatched: new WeakMap(), carrierMachines: new Set(),
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
    _applySnapshot(channel, payload, envelope, realign) {
      if (channel.kind === "session") { applied.push(["row", channel.session, realign]); return }
      applied.push([channel.kind, payload?.read])
    },
    _recordReceiveFailure(error) { applied.push(["failed", error.code ?? error.message]) },
  })
  const envelope = (seq, read) => sealEnvelope({ ch: "t/m/s", seq, ts: 1, class: "stream",
    key_id: "mk", sender: "m" }, JSON.stringify({ read, status: 200 }), masterKey, signing.privateKey)
  /** A rich Session row as the relay would deliver it, with the relay's own envelope number. */
  const row = (session, seq, body = { session: { id: session, label: "a row" }, at: 1 }) =>
    sealEnvelope({ ch: "s/m/" + session, seq, ts: 1, class: "stream", key_id: "mk", sender: "m" },
      JSON.stringify(body), masterKey, signing.privateKey)
  return { client, carrier, applied, envelope, row, masterKey, signing, raw }
}

const read = (client) => client._read({ machine: "m", session: "s" }, "transcript",
  { machine_id: "m", expected_generation: genA }, "transcript", 30_000, {})

function observe() {
  const seen = []
  let n = 0
  const remove = installViewerHealthObserver({
    record: (o) => { seen.push(o) },
    correlation: () => `vt-${(++n).toString(16).padStart(12, "0")}`,
    monotonic: () => 0,
  })
  return { seen, remove }
}
const short = (o) => [o.event, o.stage, o.class, o.error ?? o.outcome]

test("a read answered on the carrier is issued, received and applied under one correlation", async () => {
  const { seen, remove } = observe()
  try {
    const client = carrierClient(openCarrier())
    const answer = read(client)
    await flush()
    client._settleRead(KEY, { entries: [] }, null)
    await answer
    assert.deepEqual(seen.map(short), [
      ["viewer_stage_started", "viewer_request_issued", "read", undefined],
      ["viewer_stage_completed", "viewer_response_received", "read", "ok"],
      ["viewer_stage_completed", "viewer_decrypt_apply", "read", "ok"],
    ])
    assert.equal(new Set(seen.map((o) => o.correlation)).size, 1)
  } finally { remove() }
})

test("fault injection: a carrier that takes the request and stalls is a timed-out read, not a success", async () => {
  const { seen, remove } = observe()
  try {
    const client = carrierClient(openCarrier())
    const answer = read(client).catch((error) => error)
    await flush()
    // The carrier stays open and says nothing; the read's own bound runs out.
    client.timers.filter(Boolean).at(-1).fn()
    const error = await answer
    assert.equal(error.code, "cloud_read_timeout")
    assert.deepEqual(seen.map(short).at(-1), ["viewer_stage_failed", "viewer_response_received", "read", "timeout"])
    assert.ok(!seen.some((o) => o.outcome === "ok"), "a stalled carrier read was recorded as ok")
  } finally { remove() }
})

test("fault injection: a carrier that dies holding a read records the replacement, then the relay's answer", async () => {
  const { seen, remove } = observe()
  try {
    const carrier = openCarrier()
    const client = carrierClient(carrier)
    const answer = read(client)
    await flush()
    carrier.open = false
    for (const listener of carrier.down) listener("carrier_closed")
    await flush()
    client._settleRead(KEY, { entries: ["from the relay"] }, null)
    await answer
    assert.deepEqual(seen.map(short), [
      ["viewer_stage_started", "viewer_request_issued", "read", undefined],
      ["viewer_stage_failed", "viewer_carrier_replace", "read", "carrier_closed"],
      ["viewer_stage_completed", "viewer_response_received", "read", "ok"],
      ["viewer_stage_completed", "viewer_decrypt_apply", "read", "ok"],
    ])
    assert.equal(new Set(seen.map((o) => o.correlation)).size, 1)
  } finally { remove() }
})

test("fault injection: an envelope the carrier delivered that would not open is a decrypt-apply failure", async () => {
  const { seen, remove } = observe()
  try {
    const { client, envelope } = await sealedClient()
    const answer = read(client)
    await flush()
    client._machinePairing = async () => null
    await client.receiveCarrierEnvelope("m", await envelope(1, "transcript"), 1)
    await flush()
    assert.ok(seen.some((o) => o.event === "viewer_stage_failed" && o.stage === "viewer_decrypt_apply" &&
      o.error === "decrypt_failed" && o.class === "read"), JSON.stringify(seen.map(short)))
    // Nothing from the channel, the envelope or the sender reaches a record.
    const leaked = JSON.stringify(seen)
    assert.ok(!leaked.includes("t/m/s") && !leaked.includes("mk") && !leaked.includes("transcript"))
    client._settleRead(KEY, { entries: [] }, null)
    await answer
  } finally { remove() }
})

test("fault injection: a replayed carrier envelope is replay_rejected", async () => {
  const { seen, remove } = observe()
  try {
    const { client, envelope } = await sealedClient()
    await client.receiveCarrierEnvelope("m", await envelope(5, "transcript"), 1)
    seen.length = 0
    await client.receiveCarrierEnvelope("m", await envelope(5, "transcript"), 1)
    assert.deepEqual(seen.map(short), [["viewer_stage_failed", "viewer_decrypt_apply", "read", "replay_rejected"]])
  } finally { remove() }
})
