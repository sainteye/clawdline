import { test } from "node:test"
import assert from "node:assert/strict"
import { StatusCloudClient, statusChannel } from "./status-client.js"
import { base64Bytes, envelopeSigningBytes } from "../legacy/js/net/cloud-crypto.js"

const zero = (length) => Buffer.alloc(length).toString("base64")
const envelope = (ch) => ({ v: 1, ch, seq: 1, ts: 1, class: "stream", key_id: "key", nonce: zero(12),
  ct: zero(16), sender: "sender", sig: zero(64) })
const genA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const genB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

test("ss channel keeps both opaque machine and Session ids after envelope validation", () => {
  assert.deepEqual(statusChannel(envelope("ss/machine%2Fa/%2514")), { machine: "machine/a", session: "%14" })
  assert.equal(statusChannel(envelope("s/machine/session")), null)
  assert.equal(statusChannel(envelope("ss/machine/bad/extra")), null)
})

test("a status envelope with an invalid signature never enters the projection", async () => {
  const keys = await crypto.subtle.generateKey({ name: "Ed25519" }, true, ["sign", "verify"])
  const client = Object.create(StatusCloudClient.prototype)
  client.statusSnapshots = new Map()
  client._machinePairing = async () => ({ keyID: "key", senderID: "sender", senderKey: keys.publicKey, masterKey: null })
  client._compareKeyID = () => {}
  client._recordReceiveFailure = () => {}
  await assert.rejects(() => client._receiveEnvelope(envelope("ss/m/s"), false), { code: "unreadable_envelope" })
  assert.equal(client.statusSnapshots.size, 0)
})

test("a paired signed ss envelope decrypts into only its machine and Session key", async () => {
  const keys = await crypto.subtle.generateKey({ name: "Ed25519" }, true, ["sign", "verify"])
  const masterKey = await crypto.subtle.generateKey({ name: "AES-GCM", length: 256 }, true, ["encrypt", "decrypt"])
  const nonce = crypto.getRandomValues(new Uint8Array(12))
  const payload = { machine_id: "m", session_id: "s", state: "working" }
  const ct = await crypto.subtle.encrypt({ name: "AES-GCM", iv: nonce }, masterKey,
    new TextEncoder().encode(JSON.stringify(payload)))
  const signed = { ...envelope("ss/m/s"), nonce: Buffer.from(nonce).toString("base64"),
    ct: Buffer.from(ct).toString("base64") }
  signed.sig = Buffer.from(await crypto.subtle.sign({ name: "Ed25519" }, keys.privateKey,
    envelopeSigningBytes(signed))).toString("base64")
  const client = Object.create(StatusCloudClient.prototype)
  client.statusSnapshots = new Map()
  client.statusSequences = new Map()
  client.sequenceBySender = new Map()
  client.realignSequenceByChannel = new Map()
  client._machinePairing = async () => ({ keyID: "key", senderID: "sender", senderKey: keys.publicKey, masterKey })
  client._compareKeyID = () => {}
  client._sawAuthenticatedEnvelope = () => {}
  client._recordReceiveFailure = () => {}
  const events = []
  client._emit = (event) => events.push(event)
  await client._receiveEnvelope(signed, false)
  assert.deepEqual(client.statusSnapshots.get(JSON.stringify(["m", "s"])).payload, payload)
  assert.equal(events[0].type, "session_status")
})

test("pinned transcript sends exact generation and never coalesces another generation", async () => {
  const client = Object.create(StatusCloudClient.prototype)
  client.pinnedTranscriptFlights = new Map()
  const calls = []
  let settle
  client._read = (...args) => {
    calls.push(args)
    if (calls.length === 1) return new Promise((resolve) => { settle = resolve })
    return Promise.resolve({ entries: [] })
  }
  const a = { machineID: "m", sessionID: "same", executionGeneration: genA }
  const b = { ...a, executionGeneration: genB }
  const signal = new AbortController().signal
  const first = client.transcriptForGeneration(a, signal)
  assert.equal(client.transcriptForGeneration(a, signal), first)
  const second = client.transcriptForGeneration(b, signal)
  await Promise.resolve()
  assert.equal(calls.length, 1)
  settle({ entries: [] })
  await first
  await second
  assert.equal(calls.length, 2)
  assert.deepEqual(calls.map((args) => args[2].expected_generation), [genA, genB])
  assert.deepEqual(calls.map((args) => args[2].machine_id), ["m", "m"])
  assert.equal(calls[1][5].signal, signal)
})

test("pinned info asks for full detail with the exact machine and generation", async () => {
  const client = Object.create(StatusCloudClient.prototype)
  client.pinnedInfoFlights = new Map()
  client.readWaiters = new Map()
  const calls = []
  client._read = (...args) => { calls.push(args); return Promise.resolve({ info: {} }) }
  await client.infoForGeneration({ machineID: "m", sessionID: "s", executionGeneration: genA },
    new AbortController().signal)
  assert.equal(calls[0][1], "info")
  assert.deepEqual(calls[0][2], { parts: "full", machine_id: "m", expected_generation: genA })
  assert.equal(calls[0][3], "info.full")
})

test("pinned detail refuses to join an unpinned read waiter", async () => {
  const client = Object.create(StatusCloudClient.prototype)
  client.pinnedInfoFlights = new Map()
  client.readWaiters = new Map([["m\u0000s\u0000info.full", {}]])
  client._read = () => { throw new Error("must not send") }
  await assert.rejects(() => client.infoForGeneration({ machineID: "m", sessionID: "s", executionGeneration: genA }),
    { code: "cloud_read_busy" })
})

test("read_transcript-only pinned reads pass the old local write guard", async () => {
  const client = Object.create(StatusCloudClient.prototype)
  client.allowWrites = false
  client.retired = false
  client.readWaiters = new Map()
  client._sessionIdentity = () => ({ machine: "m", session: "s" })
  client._unsupportedRefusal = () => null
  client._offlineRefusal = () => Object.assign(new Error("offline sentinel"), { code: "offline_sentinel" })
  await assert.rejects(() => client._read({ machine: "m", session: "s" }, "info",
    { parts: "full", machine_id: "m", expected_generation: genA }, "info.full"), { code: "offline_sentinel" })
  assert.equal(client.allowWrites, false)
  await assert.rejects(() => client._read({ machine: "m", session: "s" }, "send", {}, "action:one"),
    { code: "cloud_read_needs_send_prompt" })
})

test("only pinned info and transcript seal r/; send remains on ctl/", async () => {
  const keys = await crypto.subtle.generateKey({ name: "Ed25519" }, true, ["sign", "verify"])
  const masterKey = await crypto.subtle.generateKey({ name: "AES-GCM", length: 256 }, true, ["encrypt", "decrypt"])
  const client = Object.create(StatusCloudClient.prototype)
  client.allowWrites = false
  client.ready = true
  client.deviceID = "viewer"
  client.devicePrivateKey = keys.privateKey
  let sequence = 7
  client.nextSequence = async () => sequence++
  client._unsupportedRefusal = () => null
  client._offlineRefusal = () => null
  client._outboundMachinePairing = async () => ({ keyID: "key", masterKey })
  client.trail = { sealed() {} }
  client.pendingBySequence = new Map()
  const frames = []
  client._send = (frame) => frames.push(frame)
  const reply = await client._publishCommand("m", "info", {
    session: "s", parts: "full", machine_id: "m", expected_generation: genA,
  }, "ctl")
  assert.equal(reply.ch, "r/m")
  assert.equal(reply.class, "ctl")
  assert.equal(frames[0].envelope.ch, "r/m")
  assert.equal(await crypto.subtle.verify({ name: "Ed25519" }, keys.publicKey,
    base64Bytes(reply.sig, "sig"), envelopeSigningBytes(reply)), true)
  const flip = (value) => {
    const bytes = base64Bytes(value)
    bytes[0] ^= 1
    return Buffer.from(bytes).toString("base64")
  }
  for (const changed of [
    { v: 2 }, { ch: "ctl/m" }, { ch: "r/other" }, { seq: 9 }, { ts: reply.ts + 1 },
    { class: "dispatch" }, { key_id: "other" },
    { nonce: flip(reply.nonce) }, { ct: flip(reply.ct) },
  ]) {
    const tampered = { ...reply, ...changed }
    assert.equal(await crypto.subtle.verify({ name: "Ed25519" }, keys.publicKey,
      base64Bytes(tampered.sig, "sig"), envelopeSigningBytes(tampered)), false,
    "tampering with " + Object.keys(changed)[0] + " must fail")
  }
  const other = await crypto.subtle.generateKey({ name: "Ed25519" }, true, ["sign", "verify"])
  assert.equal(await crypto.subtle.verify({ name: "Ed25519" }, other.publicKey,
    base64Bytes(reply.sig, "sig"), envelopeSigningBytes({ ...reply, sender: "other" })), false,
  "another roster sender cannot use the viewer signature")
  const opened = JSON.parse(new TextDecoder().decode(await crypto.subtle.decrypt(
    { name: "AES-GCM", iv: base64Bytes(reply.nonce, "nonce") }, masterKey, base64Bytes(reply.ct, "ct"))))
  assert.deepEqual(opened, { type: "info", session: "s", parts: "full", machine_id: "m", expected_generation: genA })
  const key = "m\u0000s\u0000transcript"
  const waiters = {}
  client.readWaiters = new Map([[key, waiters]])
  const pending = { key, waiters }
  const transcript = await client._publishCommand("m", "transcript", {
    session: "s", machine_id: "m", expected_generation: genA, limit: 200,
  }, "ctl", pending)
  assert.equal(transcript.ch, "r/m")
  assert.equal(pending.registered.ref.seq, 8)
  assert.equal(client.pendingBySequence.get(8), pending.registered)
  await assert.rejects(() => client._publishCommand("m", "info", {
    session: "s", parts: "full", machine_id: "other", expected_generation: genA,
  }, "ctl"), { code: "execution_target_required" })
  await assert.rejects(() => client._publishCommand("m", "send", { session: "s" }, "ctl"),
    { code: "cloud_read_only" })
})

test("leaving a detail releases s and t immediately", () => {
  const client = Object.create(StatusCloudClient.prototype)
  client.ready = true
  client.pendingSubscriptions = new Set(["s/m/s", "t/m/s"])
  client.socketSubscriptions = new Map([["s/m/s", 1], ["t/m/s", 1]])
  client.resubscribes = new Map()
  const sent = []
  client._sendSubscriptionFrame = (type, channels) => sent.push({ type, channels })
  client.unsubscribe(["s/m/s", "t/m/s"])
  assert.deepEqual(sent, [{ type: "unsubscribe", channels: ["s/m/s", "t/m/s"] }])
  assert.equal(client.socketSubscriptions.size, 0)
})

test("only an s row received after exact detail open may supply its menu", () => {
  const client = Object.create(StatusCloudClient.prototype)
  client.openedDetails = new Set()
  client.detailSnapshots = new Map()
  client.sessionSnapshots = new Map([["m\u0000s", { menu: { question: "old" } }]])
  client.listeners = new Set()
  const destination = { machineID: "m", sessionID: "s", executionGeneration: genA }
  client.openDetail(destination)
  assert.equal(client.detailSnapshots.size, 0)
  client.sessionSnapshots.set("m\u0000s", { menu: { question: "new" } })
  client._emit({ type: "sessions", identity: { machine: "m", session: "s" } })
  assert.equal(client.detailSnapshots.get("m\u0000s").menu.question, "new")
  client.closeDetail(destination)
  assert.equal(client.detailSnapshots.size, 0)
})

test("r/ capability is learned only from this socket's full machine descriptor", () => {
  const client = Object.create(StatusCloudClient.prototype)
  client.readContentCapabilities = new Map()
  client.listeners = new Set()
  client._emit({ type: "orchestrator", machine: "m", statusOnly: true,
    data: { at: 1, machine: { read_content_v1: true } } })
  assert.equal(client.readContentCapabilities.has("m"), false)
  client._emit({ type: "orchestrator", machine: "m",
    data: { at: 2, machine: { read_content_v1: true } } })
  assert.deepEqual(client.readContentCapabilities.get("m"), { at: 2, supported: true })
  client._emit({ type: "orchestrator", machine: "m", data: { at: 3, machine: {} } })
  assert.deepEqual(client.readContentCapabilities.get("m"), { at: 3, supported: false })
})

test("an event gap requests one retained ss row and releases it after realign", async () => {
  const client = Object.create(StatusCloudClient.prototype)
  client.ready = true
  client.statusSnapshots = new Map()
  client.statusRecoveries = new Map()
  client.socketSubscriptions = new Map()
  client.pendingSubscriptions = new Set()
  client.resubscribes = new Map()
  client.subscriptionLimit = 8
  client.readTimeoutMs = 60000
  client.now = () => 0
  client._trimSubscriptions = () => {}
  client.setTimeout = () => 1
  client.clearTimeout = () => {}
  const frames = []
  client._sendSubscriptionFrame = (type, channels) => frames.push({ type, channels })
  let listener
  client.events = (callback) => { listener = callback; return () => { listener = null } }
  const recovery = client.recoverStatusRow("m", "s", genA)
  assert.deepEqual(frames, [{ type: "subscribe", channels: ["ss/m/s"] }])
  client.statusSnapshots.set(JSON.stringify(["m", "s"]), { payload: { snapshot_generation: genA } })
  listener({ type: "session_status", identity: { machine: "m", session: "s" } })
  assert.equal(await recovery, true)
  assert.deepEqual(frames[1], { type: "unsubscribe", channels: ["ss/m/s"] })
  assert.equal(client.statusRecoveries.size, 0)
})

test("a missing retained row stays a gap after the bounded wait", async () => {
  const client = Object.create(StatusCloudClient.prototype)
  client.ready = true
  client.statusSnapshots = new Map()
  client.statusRecoveries = new Map()
  client.socketSubscriptions = new Map()
  client.pendingSubscriptions = new Set()
  client.resubscribes = new Map()
  client.subscriptionLimit = 8
  client.readTimeoutMs = 60000
  client.now = () => 0
  client._trimSubscriptions = () => {}
  let timeout
  client.setTimeout = (callback) => { timeout = callback; return 1 }
  client.clearTimeout = () => {}
  client._sendSubscriptionFrame = () => {}
  client.events = () => () => {}
  const recovery = client.recoverStatusRow("m", "s", genA)
  timeout()
  assert.equal(await recovery, false)
  assert.equal(client.socketSubscriptions.size, 0)
})
