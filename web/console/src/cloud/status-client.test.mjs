import { test } from "node:test"
import assert from "node:assert/strict"
import { StatusCloudClient, pinnedReplyMatches, statusChannel } from "./status-client.js"
import { base64Bytes, envelopeSigningBytes } from "../legacy/js/net/cloud-crypto.js"

const zero = (length) => Buffer.alloc(length).toString("base64")
const envelope = (ch) => ({ v: 1, ch, seq: 1, ts: 1, class: "stream", key_id: "key", nonce: zero(12),
  ct: zero(16), sender: "sender", sig: zero(64) })
const genA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const genB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

test("the original one-machine page subscribes only exact rich rows named by that machine's status", () => {
  const client = Object.create(StatusCloudClient.prototype)
  client.statusSnapshots = new Map([
    [JSON.stringify(["m", "__clawdline_inventory_v1__"]), { payload: {
      complete: true, snapshot_generation: genA, inventory: { version: 1, sessions: ["s1", "s2"] },
    } }],
    [JSON.stringify(["m", "s1"]), { payload: { snapshot_generation: genA, execution_generation: genA } }],
    [JSON.stringify(["m", "s2"]), { payload: { snapshot_generation: genA, execution_generation: genB } }],
    [JSON.stringify(["other", "s1"]), { payload: { snapshot_generation: genA, execution_generation: genA } }],
  ])
  client.sessionSnapshots = new Map([["m\u0000s1", { execution_generation: genA }]])
  client.classicSessionMachine = null
  client.classicSessionReads = new Map()
  client.classicSessionAttempted = new Map()
  client.pendingSubscriptions = new Set()
  client.socketSubscriptions = new Map()
  client.subscriptionHolds = new Map()
  client.resubscribes = new Map()
  client.subscriptionLimit = 8
  client.ready = true
  client.now = () => 100
  client.setTimeout = () => 1
  client.clearTimeout = () => {}
  const frames = []
  client._sendSubscriptionFrame = (type, channels) => frames.push({ type, channels })
  client.enableClassicSessionView("m")
  assert.deepEqual(frames, [{ type: "subscribe", channels: ["s/m/s2"] }])
  assert.deepEqual([...client.socketSubscriptions.keys()], ["s/m/s2"])
  client.ready = false
  client.disableClassicSessionView()
  assert.equal(client.socketSubscriptions.size, 0)
  client.sessionSnapshots.clear()
  client.ready = true
  client.enableClassicSessionView("m", "s2")
  assert.deepEqual(frames.at(-1), { type: "subscribe", channels: ["s/m/s2"] })
  assert.deepEqual([...client.socketSubscriptions.keys()], ["s/m/s2"],
    "opening one fleet Session does not recover every rich row on its machine")
})

test("the original list recovers more rows when other idle Cloud channels fill the relay budget", () => {
  const client = Object.create(StatusCloudClient.prototype)
  client.statusSnapshots = new Map([[JSON.stringify(["m", "__clawdline_inventory_v1__"]), { payload: {
    complete: true, snapshot_generation: genA, inventory: { version: 1, sessions: ["s1", "s2", "s3"] },
  } }]])
  for (const id of ["s1", "s2", "s3"]) client.statusSnapshots.set(JSON.stringify(["m", id]),
    { payload: { snapshot_generation: genA, execution_generation: genA } })
  client.sessionSnapshots = new Map()
  client.classicSessionMachine = null
  client.classicSessionReads = new Map()
  client.classicSessionAttempted = new Map()
  client.pendingSubscriptions = new Set(Array.from({ length: 8 }, (_, i) => `t/m/old${i}`))
  client.socketSubscriptions = new Map([...client.pendingSubscriptions].map((channel) => [channel, 100]))
  client.subscriptionHolds = new Map()
  client.resubscribes = new Map()
  client.subscriptionLimit = 8
  client.ready = true
  client.now = () => 100
  client.setTimeout = () => 1
  client.clearTimeout = () => {}
  const frames = []
  client._sendSubscriptionFrame = (type, channels) => frames.push({ type, channels })
  client.enableClassicSessionView("m")
  assert.deepEqual(frames.filter((frame) => frame.type === "subscribe"), [
    { type: "subscribe", channels: ["s/m/s1"] }, { type: "subscribe", channels: ["s/m/s2"] },
  ])
  assert.equal(client.socketSubscriptions.size, 8)
  client.sessionSnapshots.set("m\u0000s1", { execution_generation: genA })
  client.unsubscribe(["s/m/s1"])
  client.classicSessionReads.delete("s/m/s1")
  client._recoverClassicSessionRows()
  assert.deepEqual(frames.filter((frame) => frame.type === "subscribe").at(-1),
    { type: "subscribe", channels: ["s/m/s3"] })
})

test("the live signed machine descriptor keeps commands beyond the archived cache bound", () => {
  const client = Object.create(StatusCloudClient.prototype)
  const words = Array.from({ length: 65 }, (_, i) => `word-${i}`)
  words.push("send", "session-receipt")
  client.machineDescriptors = new Map([["m", { machine: { commands: words.slice(0, 64) }, build: "old" }]])
  client.orchestratorSnapshots = new Map([["m", { machine: { commands: words }, app: { build: "current" } }]])
  assert.equal(client.machineDescriptor("m").machine.commands.includes("session-receipt"), true)
  assert.equal(client.machineDescriptor("m").machine.commands.includes("send"), true)
  client.orchestratorSnapshots.clear()
  assert.equal(client.machineDescriptor("m").machine.commands.includes("session-receipt"), false)
})

test("a cut remembered Linux command list cannot refuse a named transcript read", () => {
  const client = Object.create(StatusCloudClient.prototype)
  client.machineDescriptors = new Map([["m", { machine: {
    platform: "linux", commands: Array.from({ length: 64 }, (_, i) => `word-${i}`),
  } }]])
  client.orchestratorSnapshots = new Map()
  client.machineLacks = new Map()
  client.macCapabilities = new Map()
  assert.equal(client._machineImplements("m", "transcript"), "unknown")
  client.orchestratorSnapshots.set("m", { machine: { platform: "linux", commands: ["transcript"] } })
  assert.equal(client._machineImplements("m", "transcript"), "yes")
  client.orchestratorSnapshots.clear()
  client.machineLacks.set("m", new Set(["transcript"]))
  assert.equal(client._machineImplements("m", "transcript"), "no")
})

test("status recovery frees idle relay channels before declaring an event gap", async () => {
  const client = Object.create(StatusCloudClient.prototype)
  client.ready = true
  client.statusSnapshots = new Map()
  client.statusRecoveries = new Map()
  client.pendingSubscriptions = new Set(Array.from({ length: 8 }, (_, i) => `t/m/old${i}`))
  client.socketSubscriptions = new Map([...client.pendingSubscriptions].map((channel) => [channel, 100]))
  client.subscriptionHolds = new Map()
  client.resubscribes = new Map()
  client.subscriptionLimit = 8
  client.subscriptionIdleMs = 60_000
  client.readTimeoutMs = 60_000
  client.now = () => 100
  client.setTimeout = () => 1
  client.clearTimeout = () => {}
  client.events = () => () => {}
  const frames = []
  client._sendSubscriptionFrame = (type, channels) => frames.push({ type, channels })
  const recovery = client.recoverStatusRow("m", "s", genA)
  assert.deepEqual(frames, [
    { type: "unsubscribe", channels: ["t/m/old0"] },
    { type: "subscribe", channels: ["ss/m/s"] },
  ])
  assert.equal(client.socketSubscriptions.size, 8)
  client.cancelStatusRecoveries()
  assert.equal(await recovery, false)
})

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
  const signed = { ...envelope("ss/m/s"), ts: Date.now(), nonce: Buffer.from(nonce).toString("base64"),
    ct: Buffer.from(ct).toString("base64") }
  signed.sig = Buffer.from(await crypto.subtle.sign({ name: "Ed25519" }, keys.privateKey,
    envelopeSigningBytes(signed))).toString("base64")
  const client = Object.create(StatusCloudClient.prototype)
  client.statusSnapshots = new Map()
  client.statusSequences = new Map()
  client.machineOffline = new Map([["m", { until: Date.now() + 1000 }]])
  client.machineObservedAt = new Map()
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
  assert.equal(client.machineOffline.has("m"), false)
  assert.equal(client.machineObservedAt.get("m"), signed.ts)
  assert.equal(events[0].type, "session_status")
  client.machineOffline.set("m", { until: Date.now() + 1000 })
  client.machineObservedAt.clear()
  const retained = { ...signed, seq: 2, ts: signed.ts - 600_000 }
  retained.sig = Buffer.from(await crypto.subtle.sign({ name: "Ed25519" }, keys.privateKey,
    envelopeSigningBytes(retained))).toString("base64")
  await client._receiveEnvelope(retained, true)
  assert.equal(client.machineOffline.has("m"), true, "a retained row does not prove the machine is online")
  assert.equal(client.machineObservedAt.get("m"), retained.ts, "retention preserves only the signed observation time")
  assert.ok(Date.now() - client.machineObservedAt.get("m") > 300_000,
    "an old retained row cannot claim current machine presence")
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

test("an older transcript page names its cursor in the read waiter and preserves its pin", async () => {
  const client = Object.create(StatusCloudClient.prototype)
  const calls = []
  client._read = (...args) => { calls.push(args); return Promise.resolve({ entries: [], nextBefore: 40 }) }
  const destination = { machineID: "m", sessionID: "s", executionGeneration: genA }
  const signal = new AbortController().signal
  await client.transcriptPageForGeneration(destination, 123, signal)
  assert.deepEqual(calls[0][2], { limit: 200, before: 123, priority: "foreground", machine_id: "m",
    expected_generation: genA })
  assert.equal(calls[0][3], "transcript.before.123")
  assert.equal(calls[0][5].signal, signal)
  await assert.rejects(() => client.transcriptPageForGeneration(destination, 0, signal),
    { code: "execution_target_required" })
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

test("one machine list read uses a read-only machine reply waiter", async () => {
  const client = Object.create(StatusCloudClient.prototype)
  const calls = []
  client._read = (...args) => { calls.push(args); return Promise.resolve({ sessions: [] }) }
  const signal = new AbortController().signal
  await client.listPresentationsForMachine("m", signal)
  assert.deepEqual(calls[0][0], { machine: "m", session: "__clawdline_machine__" })
  assert.equal(calls[0][1], "sessions.list")
  assert.equal(calls[0][2].machine_id, "m")
  assert.equal(calls[0][3], "read:" + calls[0][2].request)
  assert.equal(calls[0][5].signal, signal)
})

test("a transcript picture uses an exact image waiter and rejects a missing execution", async () => {
  const client = Object.create(StatusCloudClient.prototype)
  const calls = []
  client._read = (...args) => { calls.push(args); return Promise.resolve({ id: "picture-1" }) }
  const destination = { machineID: "m", sessionID: "s", executionGeneration: genA }
  const signal = new AbortController().signal
  await client.imageForGeneration(destination, "picture-1", signal)
  assert.equal(calls[0][1], "image")
  assert.deepEqual(calls[0][2], { id: "picture-1", machine_id: "m", expected_generation: genA })
  assert.equal(calls[0][3], "image.picture-1")
  assert.equal(calls[0][5].signal, signal)
  await assert.rejects(() => client.imageForGeneration({ ...destination, executionGeneration: "" }, "picture-1", signal),
    { code: "execution_target_required" })
})

test("original detail panels retain their read names on the exact r/ target", async () => {
  const client = Object.create(StatusCloudClient.prototype)
  const calls = []
  client._read = (...args) => { calls.push(args); return Promise.resolve({}) }
  const target = { machineID: "m", sessionID: "s", executionGeneration: genA }
  for (const [word, fields, answer] of [
    ["git", {}, "git"], ["git-diff", { request: "diff-1", path: "README.md" }, "read:diff-1"],
    ["screen", {}, "screen"], ["agent", { agent: "a", limit: 100 }, "agent:a"],
    ["agent", { agent: "a", limit: 100, before: 17 }, "agent:a.before.17"],
    ["shell", { shell: "sh", bytes: 4096 }, "shell:sh"], ["documents", {}, "documents"],
    ["document", { request: "doc-1", scope: "project", task: "", path: "README.md" }, "read:doc-1"],
  ]) {
    await client.readForGeneration(target, word, fields)
    const sent = calls.at(-1)
    assert.deepEqual(sent[0], { machine: "m", session: "s" })
    assert.equal(sent[1], word)
    assert.deepEqual(sent[2], { ...fields, machine_id: "m", expected_generation: genA })
    assert.equal(sent[3], answer)
  }
  assert.equal(calls.length, 8)
  await assert.rejects(() => client.readForGeneration(target, "send", { request: "write-1" }),
    { code: "read_only_channel" })
  await assert.rejects(() => client.readForGeneration({ ...target, executionGeneration: "" }, "git"),
    { code: "execution_target_required" })
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
  client.pinnedReadProofs = new Map()
  client._sessionIdentity = () => ({ machine: "m", session: "s" })
  client._unsupportedRefusal = () => null
  client._offlineRefusal = () => Object.assign(new Error("offline sentinel"), { code: "offline_sentinel" })
  await assert.rejects(() => client._read({ machine: "m", session: "s" }, "info",
    { parts: "full", machine_id: "m", expected_generation: genA }, "info.full"), { code: "offline_sentinel" })
  assert.equal(client.allowWrites, false)
  await assert.rejects(() => client._read({ machine: "m", session: "s" }, "send", {}, "action:one"),
    { code: "cloud_read_needs_send_prompt" })
})

test("only pinned Session content reads seal r/; send remains on ctl/", async () => {
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
  client.pinnedReadProofs = new Map([[key, { machineID: "m", sessionID: "s", generation: genA,
    read: "transcript", seq: null, waiters }]])
  const pending = { key, waiters }
  const transcript = await client._publishCommand("m", "transcript", {
    session: "s", machine_id: "m", expected_generation: genA, limit: 200,
  }, "ctl", pending)
  assert.equal(transcript.ch, "r/m")
  assert.equal(pending.registered.ref.seq, 8)
  assert.equal(client.pinnedReadProofs.get(key).seq, 8)
  assert.equal(client.pendingBySequence.get(8), pending.registered)
  const skills = await client._publishCommand("m", "skills", {
    session: "s", machine_id: "m", expected_generation: genA,
  }, "ctl")
  assert.equal(skills.ch, "r/m")
  const skillsBody = JSON.parse(new TextDecoder().decode(await crypto.subtle.decrypt(
    { name: "AES-GCM", iv: base64Bytes(skills.nonce, "nonce") }, masterKey, base64Bytes(skills.ct, "ct"))))
  assert.deepEqual(skillsBody, { type: "skills", session: "s", machine_id: "m", expected_generation: genA })
  const inbox = await client._publishCommand("m", "peer-inbox", {
    session: "s", machine_id: "m", expected_generation: genA, request: "inbox-1",
  }, "ctl")
  assert.equal(inbox.ch, "r/m")
  const listKey = "m\u0000__clawdline_machine__\u0000read:batch-1"
  const listWaiters = {}
  client.readWaiters.set(listKey, listWaiters)
  client.pinnedReadProofs.set(listKey, { machineID: "m", sessionID: "__clawdline_machine__",
    generation: null, read: "read:batch-1", seq: null, waiters: listWaiters })
  const list = await client._publishCommand("m", "sessions.list", {
    session: "__clawdline_machine__", machine_id: "m", request: "batch-1",
  }, "ctl", { key: listKey, waiters: listWaiters })
  assert.equal(list.ch, "r/m")
  const listBody = JSON.parse(new TextDecoder().decode(await crypto.subtle.decrypt(
    { name: "AES-GCM", iv: base64Bytes(list.nonce, "nonce") }, masterKey, base64Bytes(list.ct, "ct"))))
  assert.deepEqual(listBody, { type: "sessions.list", session: "__clawdline_machine__",
    machine_id: "m", request: "batch-1" })
  await assert.rejects(() => client._publishCommand("m", "peer-inbox", {
    session: "s", machine_id: "m", request: "inbox-2",
  }, "ctl"), { code: "execution_target_required" })
  await assert.rejects(() => client._publishCommand("m", "info", {
    session: "s", parts: "full", machine_id: "other", expected_generation: genA,
  }, "ctl"), { code: "execution_target_required" })
  await assert.rejects(() => client._publishCommand("m", "send", { session: "s" }, "ctl"),
    { code: "cloud_read_only" })
})

test("pinned t/ replies need the machine, Session, generation and original request sequence", () => {
  const proof = { machineID: "m", sessionID: "s", generation: genA, read: "info.full", seq: 42 }
  const answer = { read: "info.full", machine_id: "m", session_id: "s", expected_generation: genA, seq: 42,
    body: { info: {} } }
  assert.equal(pinnedReplyMatches(answer, proof), true)
  for (const [field, replacement] of [
    ["machine_id", "elsewhere"], ["session_id", "reused"], ["expected_generation", genB],
    ["seq", 41], ["read", "transcript"],
  ]) {
    assert.equal(pinnedReplyMatches({ ...answer, [field]: replacement }, proof), false, field)
    const missing = { ...answer }
    delete missing[field]
    assert.equal(pinnedReplyMatches(missing, proof), false, "missing " + field)
  }
})

test("machine list replies use request sequence proof without pretending to pin a Session", () => {
  const proof = { machineID: "m", sessionID: "__clawdline_machine__", generation: null,
    read: "read:batch-1", seq: 8 }
  const reply = { read: "read:batch-1", machine_id: "m", session_id: "__clawdline_machine__", seq: 8 }
  assert.equal(pinnedReplyMatches(reply, proof), true)
  assert.equal(pinnedReplyMatches({ ...reply, seq: 7 }, proof), false)
  assert.equal(pinnedReplyMatches({ ...reply, expected_generation: genA }, proof), false)
})

test("a retained or mismatched t/ row cannot settle a new pinned execution", () => {
  const client = Object.create(StatusCloudClient.prototype)
  const key = "m\u0000s\u0000transcript"
  const waiters = {}
  client.readWaiters = new Map([[key, waiters]])
  client.pinnedReadProofs = new Map([[key, { machineID: "m", sessionID: "s", generation: genB,
    read: "transcript", seq: 18, waiters }]])
  client._observeMachine = () => {}
  client.transcriptSnapshots = new Map()
  client.sessionSequenceByKey = new Map()
  client._emit = () => {}
  const settlements = []
  client._settleRead = (readKey, body, error) => settlements.push({ readKey, body, error })
  const channel = { kind: "transcript", machine: "m", session: "s" }
  const old = { read: "transcript", machine_id: "m", session_id: "s", expected_generation: genA,
    seq: 17, body: { entries: [{ text: "old content" }] } }
  client._applySnapshot(channel, old, { seq: 23, ts: Date.now() }, true)
  assert.equal(settlements.length, 0)
  assert.equal(client.transcriptSnapshots.size, 0)
  client._applySnapshot(channel, old, { seq: 24, ts: Date.now() }, false)
  assert.equal(settlements.length, 1)
  assert.equal(settlements[0].error.code, "read_reply_mismatch")
  assert.equal(client.transcriptSnapshots.size, 0)
  settlements.length = 0
  const current = { ...old, expected_generation: genB, seq: 18, body: { entries: [] } }
  client._applySnapshot(channel, current, { seq: 25, ts: Date.now() }, false)
  assert.deepEqual(settlements, [{ readKey: key, body: current.body, error: null }])
})

test("peer inbox refuses a reply from another request or execution before settling content", () => {
  const client = Object.create(StatusCloudClient.prototype)
  const key = "m\u0000s\u0000read:inbox-1"
  const waiters = { type: "peer-inbox", ref: { seq: 7 } }
  client.readWaiters = new Map([[key, waiters]])
  client.pinnedReadProofs = new Map([[key, { machineID: "m", sessionID: "s", generation: genA,
    read: "read:inbox-1", seq: 7, waiters }]])
  client.transcriptSnapshots = new Map()
  client.sessionSequenceByKey = new Map()
  client._observeMachine = () => {}
  client._emit = () => {}
  const settled = []
  client._settleRead = (name, body, error) => settled.push({ name, body, code: error?.code })
  const channel = { kind: "transcript", machine: "m", session: "s" }
  const old =
    { read: "read:inbox-1", machine_id: "m", session_id: "s", expected_generation: genA, seq: 6,
      body: { items: [{ body: "private" }] } }
  client._applySnapshot(channel, old, {}, false)
  assert.deepEqual(settled, [{ name: key, body: null, code: "read_reply_mismatch" }])
  settled.length = 0
  const current = { ...old, seq: 7, body: { items: [] } }
  client._applySnapshot(channel, current, {}, true)
  assert.equal(settled.length, 0)
  client._applySnapshot(channel, current, {}, false)
  assert.deepEqual(settled, [{ name: key, body: current.body, code: undefined }])
})

test("an older page reply settles only the waiter for its exact cursor and request sequence", () => {
  const client = Object.create(StatusCloudClient.prototype)
  const read = "transcript.before.123"
  const key = "m\u0000s\u0000" + read
  const waiters = {}
  client.readWaiters = new Map([[key, waiters]])
  client.pinnedReadProofs = new Map([[key, { machineID: "m", sessionID: "s", generation: genA,
    read, seq: 52, waiters }]])
  client._observeMachine = () => {}
  client.transcriptSnapshots = new Map()
  client._emit = () => {}
  const settlements = []
  client._settleRead = (readKey, body, error) => settlements.push({ readKey, body, error })
  const channel = { kind: "transcript", machine: "m", session: "s" }
  const older = { read, machine_id: "m", session_id: "s", expected_generation: genA, seq: 51,
    body: { entries: [{ role: "assistant", text: "old" }] } }
  client._applySnapshot(channel, older, { seq: 60, ts: Date.now() }, false)
  assert.equal(settlements[0].error.code, "read_reply_mismatch")
  settlements.length = 0
  client._applySnapshot(channel, { ...older, seq: 52 }, { seq: 61, ts: Date.now() }, false)
  assert.deepEqual(settlements, [{ readKey: key, body: older.body, error: null }])
  assert.equal(client.transcriptSnapshots.size, 0, "an older page never replaces the newest transcript cache")
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

test("only the exact pinned r/ ACK can report Relay-confirmed machine offline", () => {
  const client = Object.create(StatusCloudClient.prototype)
  const key = "m\u0000s\u0000transcript"
  const pending = { machine: "m", key, ref: { sender: "viewer", seq: 42 }, ack: null }
  client.pendingBySequence = new Map([[42, pending]])
  client.pinnedReadProofs = new Map([[key, { seq: 42, machineID: "m" }]])
  client.machineOffline = new Map()
  client.now = () => 1000
  client.trail = { step() {}, refused() {} }
  const settled = []
  const events = []
  client._settleRead = (...args) => settled.push(args)
  client._emit = (event) => events.push(event)
  client._relayAnswered({ seq: 42, ch: "r/other", status: "machine_offline" }, null)
  assert.equal(client.machineOffline.size, 0)
  client._relayAnswered({ seq: 42, ch: "r/m", status: "machine_offline" }, null)
  assert.ok(client.machineOffline.get("m")?.until > 1000)
  assert.equal(settled[0][0], key)
  assert.deepEqual(events, [{ type: "machine_reachability", machine: "m" }])
  client.pendingBySequence.set(43, { ...pending, ref: { sender: "viewer", seq: 43 } })
  client.pinnedReadProofs.set(key, { seq: 43, machineID: "m" })
  client._relayAnswered({ seq: 43, ch: "r/m", status: "delivered" }, null)
  assert.equal(client.machineOffline.has("m"), false)
  assert.equal(events.length, 2)
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

test("a status row arriving before recovery is used without a timeout or subscription", async () => {
  const client = Object.create(StatusCloudClient.prototype)
  client.ready = true
  client.statusSnapshots = new Map([[JSON.stringify(["m", "s"]),
    { payload: { snapshot_generation: genA } }]])
  client.statusRecoveries = new Map()
  const frames = []
  client._sendSubscriptionFrame = (type, channels) => frames.push({ type, channels })
  assert.equal(await client.recoverStatusRow("m", "s", genA), true)
  assert.deepEqual(frames, [])
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
