import { test } from "node:test"
import assert from "node:assert/strict"
import { StatusCloudClient, applyTranscriptDelta, pinnedReplyMatches, statusChannel } from "./status-client.js"
import { base64Bytes, envelopeSigningBytes } from "../legacy/js/net/cloud-crypto.js"

const zero = (length) => Buffer.alloc(length).toString("base64")
const envelope = (ch) => ({ v: 1, ch, seq: 1, ts: 1, class: "stream", key_id: "key", nonce: zero(12),
  ct: zero(16), sender: "sender", sig: zero(64) })
const genA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const genB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

function machineCountClient() {
  const client = Object.create(StatusCloudClient.prototype)
  client._knownMachines = () => ["mac", "linux"]
  client.orchestratorSnapshots = new Map(["mac", "linux"].map(id => [id, {
    at: Date.now() / 1000, machine: { platform: id === "mac" ? "darwin" : "linux" },
  }]))
  client.machineDescriptors = new Map()
  client.machineObservedAt = new Map()
  client.unpairedMachines = new Set()
  client.viewerVerified = new Map([["mac", {}], ["linux", {}]])
  client.pairingSeen = new Set()
  client.sessionSnapshots = new Map()
  client.statusSnapshots = new Map()
  return client
}

function holdCountInventory(client, machine, sessions, overrides = {}) {
  const identity = { machine, session: "__clawdline_inventory_v1__" }
  const marker = { identity, observedAt: Date.now(), sequence: 1,
    payload: { at: Date.now() / 1000, complete: true, snapshot_generation: genA,
      inventory: { version: 1, sessions }, ...overrides } }
  client.statusSnapshots.set(JSON.stringify([machine, identity.session]), marker)
  return marker
}

test("machine counts follow the complete status inventory before any Session detail opens", async () => {
  const client = machineCountClient()
  holdCountInventory(client, "mac", ["root", "child", "idle"])
  holdCountInventory(client, "linux", ["root"])
  const counts = async () => (await client.machines()).machines.map(row => [row.id, row.sessions])
  assert.deepEqual(await counts(), [["mac", 3], ["linux", 1]])
  client.sessionSnapshots.set("mac\u0000root", { machine: "mac", session: "root" })
  client.sessionSnapshots.set("mac\u0000removed", { machine: "mac", session: "removed" })
  assert.deepEqual(await counts(), [["mac", 3], ["linux", 1]], "opened or obsolete details are not a census")
  holdCountInventory(client, "mac", ["root"])
  assert.deepEqual(await counts(), [["mac", 1], ["linux", 1]], "a newer complete set replaces the count")
  holdCountInventory(client, "mac", [])
  assert.deepEqual(await counts(), [["mac", 0], ["linux", 1]], "a complete empty set proves zero")
})

test("missing, partial, expired or malformed status inventories never invent a machine count", async () => {
  const client = machineCountClient()
  client.sessionSnapshots.set("mac\u0000opened", { machine: "mac", session: "opened" })
  assert.equal((await client.machines()).machines[0].sessions, undefined)
  for (const overrides of [
    { complete: false }, { at: undefined }, { at: Date.now() / 1000 - 301 },
    { at: Date.now() / 1000 + 301 }, { snapshot_generation: "invalid" },
    { inventory: { version: 1, sessions: ["same", "same"] } },
    { inventory: { version: 1, sessions: ["__clawdline_inventory_v1__"] } },
    { inventory: { version: 2, sessions: [] } },
  ]) {
    holdCountInventory(client, "mac", [], overrides)
    assert.equal((await client.machines()).machines[0].sessions, undefined, JSON.stringify(overrides))
  }
  const marker = holdCountInventory(client, "mac", [])
  marker.identity = { machine: "linux", session: marker.identity.session }
  assert.equal((await client.machines()).machines[0].sessions, undefined, "held identity must match its machine")
})

test("authenticated inventory replay updates the picker count without subscribing to rich detail", async () => {
  const keys = await crypto.subtle.generateKey({ name: "Ed25519" }, true, ["sign", "verify"])
  const masterKey = await crypto.subtle.generateKey({ name: "AES-GCM", length: 256 }, true, ["encrypt", "decrypt"])
  const client = machineCountClient()
  client.statusSequences = new Map()
  client.sequenceBySender = new Map()
  client.realignSequenceByChannel = new Map()
  client._machinePairing = async () => ({ keyID: "key", senderID: "sender", senderKey: keys.publicKey, masterKey })
  client._compareKeyID = () => {}
  client._sawAuthenticatedEnvelope = () => {}
  client._recordReceiveFailure = () => {}
  const readings = []
  client._emit = () => readings.push(client._machineRows()[0].sessions)
  for (const [seq, sessions] of [[1, ["root", "child", "idle"]], [2, []]]) {
    const nonce = crypto.getRandomValues(new Uint8Array(12))
    const payload = { at: Date.now() / 1000, complete: true, snapshot_generation: genA,
      inventory: { version: 1, sessions } }
    const ct = await crypto.subtle.encrypt({ name: "AES-GCM", iv: nonce }, masterKey,
      new TextEncoder().encode(JSON.stringify(payload)))
    const signed = { ...envelope("ss/mac/__clawdline_inventory_v1__"), seq, ts: Date.now(),
      nonce: Buffer.from(nonce).toString("base64"), ct: Buffer.from(ct).toString("base64") }
    signed.sig = Buffer.from(await crypto.subtle.sign({ name: "Ed25519" }, keys.privateKey,
      envelopeSigningBytes(signed))).toString("base64")
    const signature = base64Bytes(signed.sig, "sig")
    signature[0] ^= 1
    await assert.rejects(client._receiveEnvelope({ ...signed, sig: Buffer.from(signature).toString("base64") }, true),
      { code: "unreadable_envelope" })
    await client._receiveEnvelope(signed, true)
  }
  assert.deepEqual(readings, [3, 0])
  assert.equal(client.sessionSnapshots.size, 0)
  assert.equal((await client.machines()).machines[1].sessions, undefined, "another machine remains unknown")
})

test("fleet diagnostics choose a paired Mac even when a Linux test machine sorts first", () => {
  const client = Object.create(StatusCloudClient.prototype)
  client.viewerVerified = new Map([
    ["linux-a", { sender: "agent-a" }], ["mac-z", { sender: "agent-z" }],
  ])
  client._machineImplements = () => "yes"
  client._evidentMac = (machine) => machine === "mac-z"
  const remembered = []
  client.viewerEvents = { rememberTarget: (target) => remembered.push(target) }
  assert.equal(client._viewerEventTarget(true).machine, "mac-z")
  assert.equal(remembered[0].machine, "mac-z")
  client.viewerVerified.delete("mac-z")
  assert.equal(client._viewerEventTarget(false).machine, "linux-a",
    "a Linux-only account still delivers its diagnostics")
})

test("one schedule machine paints without waiting for another machine", async () => {
  const client = Object.create(StatusCloudClient.prototype)
  client.retired = false
  client.now = () => 1_700_000
  client.orchestratorSnapshots = new Map()
  client._machinesFor = () => ({ rows: [{ id: "mac-a" }, { id: "linux-b" }],
    capable: [{ id: "mac-a" }, { id: "linux-b" }], pairable: [] })
  const asked = []
  client._machineRequest = async (machine) => {
    asked.push(machine)
    if (machine !== "mac-a") throw new Error("another machine is slow")
    return { schedules: [{ id: "first" }], at: 1_700 }
  }
  const answer = await client.schedules({ fresh: true, machine: "mac-a" })
  assert.deepEqual(answer.schedules, [{ id: "first", machine: "mac-a" }])
  assert.deepEqual(asked, ["mac-a"])
  assert.deepEqual(client.orchestratorSnapshots.get("mac-a").schedules, [{ id: "first" }])
})

test("a cold browser asks a named paired schedule machine before its descriptor arrives", async () => {
  const client = Object.create(StatusCloudClient.prototype)
  client.retired = false
  client.now = () => 1_700_000
  client.orchestratorSnapshots = new Map()
  client._machinesFor = () => ({ rows: [{ id: "mac-a" }, { id: "linux-b" }], capable: [], pairable: [] })
  client._machineImplements = () => "unknown"
  const asked = []
  client._machineRequest = async (machine) => {
    asked.push(machine)
    return { schedules: [{ id: "first" }], at: 1_700 }
  }
  const answer = await client.schedules({ fresh: true, machine: "mac-a" })
  assert.deepEqual(answer.schedules, [{ id: "first", machine: "mac-a" }])
  assert.deepEqual(asked, ["mac-a"])
})

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

test("a signed descriptor persists the complete command list across reconnects", () => {
  const stored = new Map()
  const storage = { getItem: (key) => stored.get(key) ?? null, setItem: (key, value) => stored.set(key, value) }
  const client = Object.create(StatusCloudClient.prototype)
  client.machineDescriptors = new Map()
  client.descriptorStorage = storage
  client._descriptorStorageKey = () => "descriptors"
  client.viewerEvents = { now: () => 1 }
  const commands = Array.from({ length: 151 }, (_, i) => `word-${i}`)
  commands[149] = "end"
  commands[150] = "session-receipt"
  client._rememberDescriptor("m", { machine: { platform: "darwin", commands } })
  const reopened = Object.create(StatusCloudClient.prototype)
  reopened.descriptorStorage = storage
  reopened._descriptorStorageKey = () => "descriptors"
  reopened.machineDescriptors = reopened._loadMachineDescriptors()
  reopened.orchestratorSnapshots = new Map()
  reopened.machineLacks = new Map()
  reopened.macCapabilities = new Map()
  assert.equal(reopened.machineDescriptor("m").machine.commands.length, 151)
  assert.equal(reopened._machineImplements("m", "end"), "yes")
  assert.equal(reopened._machineImplements("m", "session-receipt"), "yes")
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

test("a paired Mac remains the voice host while its cached command list is incomplete", async () => {
  const client = Object.create(StatusCloudClient.prototype)
  const cached = Array.from({ length: 64 }, (_, i) => `word-${i}`)
  client._machineRows = () => [
    { id: "mac", pairing: "paired", freshness: "current" },
    { id: "linux", pairing: "paired", freshness: "current" },
  ]
  client.orchestratorSnapshots = new Map()
  client.machineDescriptors = new Map([
    ["mac", { machine: { platform: "darwin", commands: cached } }],
    ["linux", { machine: { platform: "linux", commands: ["send"] } }],
  ])
  client.machineLacks = new Map()
  client.macCapabilities = new Map()
  client.account = "account"
  client.voiceHostStorage = null
  client.voiceHostChoice = null
  const host = await client.voiceHost()
  assert.equal(host.machine, "mac")
  const asked = []
  client._machineRequest = async (machine, word) => {
    asked.push([machine, word])
    return { text: "heard" }
  }
  assert.deepEqual(await client.voice("audio", 16000), { text: "heard" })
  assert.deepEqual(asked, [["mac", "voice"]])

  // A complete live descriptor that omits voice is an actual refusal.
  client.orchestratorSnapshots.set("mac", { machine: { platform: "darwin", commands: ["send"] } })
  await assert.rejects(client.voiceHost(), { code: "cloud_voice_host_unavailable" })
})

test("voice follows the current Session machine even when an old saved host names another machine", async () => {
  const client = Object.create(StatusCloudClient.prototype)
  client._machineRows = () => [
    { id: "mac", pairing: "paired", freshness: "current" },
    { id: "linux", pairing: "paired", freshness: "current" },
  ]
  client.orchestratorSnapshots = new Map([
    ["mac", { machine: { platform: "darwin", commands: ["voice"] } }],
    ["linux", { machine: { platform: "linux", commands: ["voice"] } }],
  ])
  client.machineDescriptors = new Map()
  client.machineLacks = new Map()
  client.macCapabilities = new Map()
  client.account = "account"
  client.voiceHostStorage = null
  client.voiceHostChoice = null
  const calls = []
  client._machineRequest = (...args) => {
    calls.push(args)
    return Promise.resolve({ text: "hello" })
  }

  assert.deepEqual(await client.voiceHostFor("linux"), {
    machine: "linux", chosen: false, candidates: ["mac", "linux"],
  })
  assert.deepEqual(await client.voiceHostFor("mac"), {
    machine: "mac", chosen: false, candidates: ["mac", "linux"],
  })
  assert.deepEqual(await client.voiceFor("linux", "AAAA", 16000), { text: "hello" })
  assert.deepEqual(calls, [["linux", "voice", { audio: "AAAA", rate: 16000 }, "action", 360_000]])

  client.voiceHostChoice = "mac"
  assert.deepEqual(await client.voiceHostFor("linux"), {
    machine: "linux", chosen: false, candidates: ["mac", "linux"],
  })
})

test("macOS is the fallback when the current Session machine cannot transcribe", async () => {
  const client = Object.create(StatusCloudClient.prototype)
  client._machineRows = () => [
    { id: "mac", pairing: "paired", freshness: "current" },
    { id: "linux", pairing: "paired", freshness: "current" },
  ]
  client.orchestratorSnapshots = new Map([
    ["mac", { machine: { platform: "macos", commands: ["voice"] } }],
    ["linux", { machine: { platform: "linux", commands: [] } }],
  ])
  client.machineDescriptors = new Map()
  client.machineLacks = new Map()
  client.macCapabilities = new Map()
  client.account = "account"
  client.voiceHostStorage = null
  client.voiceHostChoice = null

  assert.equal((await client.voiceHostFor("linux")).machine, "mac")
})

test("offline voice hosts cannot win as a saved choice, sole candidate, or Session owner", async () => {
  const client = Object.create(StatusCloudClient.prototype)
  let rows = [
    { id: "mac", pairing: "paired", freshness: "stale" },
    { id: "linux", pairing: "paired", freshness: "current" },
  ]
  client._machineRows = () => rows
  client.orchestratorSnapshots = new Map([
    ["mac", { machine: { platform: "darwin", commands: ["voice"] } }],
    ["linux", { machine: { platform: "linux", commands: ["voice"] } }],
  ])
  client.machineDescriptors = new Map()
  client.machineLacks = new Map()
  client.macCapabilities = new Map()
  client.account = "account"
  client.voiceHostStorage = null
  client.voiceHostChoice = "mac"
  const calls = []
  client._machineRequest = async (machine) => { calls.push(machine); return { text: "heard" } }

  assert.equal((await client.voiceHost()).machine, "linux")
  assert.equal((await client.voiceHostFor("mac")).machine, "linux")
  await client.voiceFor("mac", "AAAA", 16000)
  assert.deepEqual(calls, ["linux"])
  await assert.rejects(client.setVoiceHost("mac"), { code: "cloud_voice_host_unavailable" })
  rows = rows.slice(0, 1)
  await assert.rejects(client.voiceHost(), { code: "cloud_voice_host_unavailable" })
  client.voiceHostChoice = null
  await assert.rejects(client.voiceHostFor("mac"), { code: "cloud_voice_host_unavailable" })
  rows[0] = { ...rows[0], freshness: "unknown" }
  await assert.rejects(client.voice("AAAA", 16000), { code: "cloud_voice_host_unavailable" })
  assert.deepEqual(calls, ["linux"], "no audio leaves for an unavailable host")
  rows[0] = { ...rows[0], freshness: "current" }
  assert.equal((await client.voiceHost()).machine, "mac", "a fresh report restores eligibility")
})

test("relay-confirmed offline overrides a fresh inventory even after retry backoff expires", async (t) => {
  t.mock.timers.enable({ apis: ["Date"], now: 1_700_000_000_000 })
  const client = Object.create(StatusCloudClient.prototype)
  client._knownMachines = () => ["mac"]
  client.orchestratorSnapshots = new Map([["mac", { at: Date.now() / 1000,
    machine: { platform: "darwin", commands: ["voice"] } }]])
  client.machineDescriptors = new Map()
  client.machineObservedAt = new Map()
  client.unpairedMachines = new Set()
  client.viewerVerified = new Map([["mac", {}]])
  client.pairingSeen = new Set()
  client.sessionSnapshots = new Map()
  client.machineOffline = new Map()
  client.now = () => 1000
  assert.equal((await client.machines()).machines[0].freshness, "current")
  client._rememberMachineOffline("mac")
  let row = (await client.machines()).machines[0]
  assert.equal(row.freshness, "stale")
  assert.equal(row.autoSelectable, false)
  assert.ok(row.observedAt, "keep the last-seen time for the offline explanation")
  client.now = () => 1_000_000
  assert.equal(client._offlineRefusal("mac"), null, "retry becomes possible without declaring recovery")
  assert.equal((await client.machines()).machines[0].freshness, "stale")
  client.pendingBySequence = new Map()
  client.trail = { step() {} }
  client._emit = () => {}
  client._relayAnswered({ seq: 1, ch: "ctl/mac", status: "delivered" }, null)
  assert.equal((await client.machines()).machines[0].freshness, "current")
  t.mock.timers.tick(5 * 60 * 1000 + 1)
  assert.equal((await client.machines()).machines[0].freshness, "stale",
    "silence expires presence without another machine sending an event")
  client._observeMachine("mac", Date.now())
  assert.equal((await client.machines()).machines[0].freshness, "current")
})

test("a Linux no_whisper refusal retries the same recording on macOS and remembers the working source", async () => {
  const client = Object.create(StatusCloudClient.prototype)
  client._machineRows = () => [
    { id: "mac", pairing: "paired", freshness: "current" },
    { id: "linux", pairing: "paired", freshness: "current" },
  ]
  client.orchestratorSnapshots = new Map([
    ["mac", { machine: { platform: "darwin", commands: ["voice"] } }],
    ["linux", { machine: { platform: "linux", commands: ["voice"] } }],
  ])
  client.machineDescriptors = new Map()
  client.machineLacks = new Map()
  client.macCapabilities = new Map()
  client.account = "account"
  client.voiceHostStorage = null
  client.voiceHostChoice = null
  client.voiceUnavailable = new Set()
  const calls = []
  client._machineRequest = (machine, word, body) => {
    calls.push([machine, word, body])
    return machine === "linux"
      ? Promise.reject({ code: "no_whisper", reason: "no_binary" })
      : Promise.resolve({ text: "heard on the Mac" })
  }

  assert.deepEqual(await client.voiceFor("linux", "AAAA", 16000), { text: "heard on the Mac" })
  assert.deepEqual(calls, [
    ["linux", "voice", { audio: "AAAA", rate: 16000 }],
    ["mac", "voice", { audio: "AAAA", rate: 16000 }],
  ])
  assert.equal((await client.voiceHostFor("linux")).machine, "mac")
})

test("no_whisper reaches the page only after every capable voice host refuses it", async () => {
  const client = Object.create(StatusCloudClient.prototype)
  client._machineRows = () => [
    { id: "mac", pairing: "paired", freshness: "current" },
    { id: "linux", pairing: "paired", freshness: "current" },
  ]
  client.orchestratorSnapshots = new Map([
    ["mac", { machine: { platform: "darwin", commands: ["voice"] } }],
    ["linux", { machine: { platform: "linux", commands: ["voice"] } }],
  ])
  client.machineDescriptors = new Map()
  client.machineLacks = new Map()
  client.macCapabilities = new Map()
  client.account = "account"
  client.voiceHostStorage = null
  client.voiceHostChoice = null
  client.voiceUnavailable = new Set()
  const calls = []
  client._machineRequest = (machine) => {
    calls.push(machine)
    return Promise.reject({ code: "no_whisper", reason: machine === "linux" ? "no_binary" : "no_model" })
  }

  await assert.rejects(client.voiceFor("linux", "AAAA", 16000), { code: "no_whisper", reason: "no_model" })
  assert.deepEqual(calls, ["linux", "mac"])
})

test("a missing Whisper refusal records the selected machine without recording speech", async () => {
  const client = Object.create(StatusCloudClient.prototype)
  const rows = []
  client._voiceHost = () => ({ machine: "mac-b", chosen: true, candidates: ["mac-a", "mac-b"] })
  client._machineRequest = () => Promise.reject({ code: "no_whisper", detail: { reason: "no_binary" } })
  client.viewerEvents = { record: (event, data) => rows.push({ event, data }) }
  client._scheduleViewerEvents = () => {}
  client.webBuild = "build"
  await assert.rejects(client.voice("private-audio", 16000), { code: "no_whisper" })
  assert.deepEqual(rows, [{ event: "cloud.voice.whisper_missing", data: {
    target_machine: "mac-b", chosen: true, candidates: ["mac-a", "mac-b"],
    reason: "no_binary", web_build: "build",
  } }])
  assert.doesNotMatch(JSON.stringify(rows), /private-audio/)
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
  client.machineDescriptor = () => null
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
  client.machineDescriptor = () => null
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

test("same-offset native pages on different revisions own separate read names", async () => {
  const client = Object.create(StatusCloudClient.prototype)
  client.machineDescriptor = () => null
  const calls = []
  client._read = (...args) => { calls.push(args); return Promise.resolve({ entries: [] }) }
  const destination = { machineID: "m", sessionID: "s", executionGeneration: genA }
  for (const revision of ["agy1:1:123:abc", "agy1:2:123:def"]) {
    await client.transcriptPageForGeneration(destination, 123, new AbortController().signal, revision)
  }
  assert.deepEqual(calls.map((args) => args[2].revision), ["agy1:1:123:abc", "agy1:2:123:def"])
  assert.deepEqual(calls.map((args) => args[3]), ["transcript.before.123.revision.agy1:1:123:abc",
    "transcript.before.123.revision.agy1:2:123:def"])
})

test("native older pages keep their negotiated view through relay and carrier publication", async (t) => {
  const keys = await crypto.subtle.generateKey({ name: "Ed25519" }, false, ["sign", "verify"])
  const masterKey = await crypto.subtle.generateKey({ name: "AES-GCM", length: 256 }, false, ["encrypt", "decrypt"])
  for (const transport of ["relay", "carrier"]) {
    for (const view of [null, "tools-v1", "tools-v2"]) {
      await t.test(`${transport}: ${view || "original"}`, async () => {
        let sequence = 10
        const client = new StatusCloudClient({ relayURL: "wss://relay.example.test", deviceToken: "fixture",
          deviceID: "viewer", devicePrivateKey: keys.privateKey, nextSequence: async () => sequence++ })
        client.ready = true
        client.machineDescriptor = () => ({ machine: { transcript_tools_v1: !!view, transcript_tools_v2: view === "tools-v2" } })
        client._unsupportedRefusal = () => null
        client._offlineRefusal = () => null
        client._outboundMachinePairing = async () => ({ keyID: "key", masterKey })
        client._subscriptionRoom = () => null
        client.subscribe = () => {}
        client._releaseSubscription = () => {}
        let published
        const publication = new Promise(resolve => { published = resolve })
        client._send = frame => published(frame.envelope)
        client._carrierFor = () => transport === "carrier" ? { open: true, send: published } : null
        const revision = "native1:123:fixture"
        const read = "transcript.before.123.revision." + revision +
          (view ? ".view." + view + (typeof DecompressionStream === "function" ? "z" : "") : "")
        const key = "m\u0000s\u0000" + read
        const controller = new AbortController()
        const result = client.transcriptPageForGeneration({ machineID: "m", sessionID: "s", executionGeneration: genA },
          123, controller.signal, revision)
        try {
          const envelope = await Promise.race([publication, result.then(() => assert.fail("read settled before publication"))])
          const fields = JSON.parse(new TextDecoder().decode(await crypto.subtle.decrypt(
            { name: "AES-GCM", iv: base64Bytes(envelope.nonce, "nonce") }, masterKey, base64Bytes(envelope.ct, "ct"))))
          assert.equal(envelope.ch, "r/m")
          assert.equal(fields.revision, revision)
          assert.equal(fields.view ?? null, view ? view + (typeof DecompressionStream === "function" ? "z" : "") : null)
          assert.equal(client.readWaiters.size, 1)
          assert.ok(client.readWaiters.has(key))
          const body = { entries: [{ role: "assistant", text: "An older message" }], nextBefore: 40 }
          client._applySnapshot({ kind: "transcript", machine: "m", session: "s" }, {
            read, machine_id: "m", session_id: "s", expected_generation: genA, seq: envelope.seq, body,
          }, { seq: 50, ts: Date.now() }, false)
          assert.deepEqual(await result, body)
          assert.equal(client.readWaiters.size, 0)
          assert.equal(client.pinnedReadProofs.size, 0)
          assert.equal(client.pendingBySequence.size, 0)
          assert.equal(client.transcriptSnapshots.size, 0)
        } finally {
          controller.abort()
          await result.catch(() => {})
        }
      })
    }
  }
})

test("Graphify reads cross the real pinned client on relay and carrier without write access", async (t) => {
  const keys = await crypto.subtle.generateKey({ name: "Ed25519" }, false, ["sign", "verify"])
  const masterKey = await crypto.subtle.generateKey({ name: "AES-GCM", length: 256 }, false, ["encrypt", "decrypt"])
  for (const transport of ["relay", "carrier"]) {
    await t.test(transport, async () => {
      let sequence = 10
      const client = new StatusCloudClient({ relayURL: "wss://relay.example.test", deviceToken: "fixture",
        deviceID: "viewer", devicePrivateKey: keys.privateKey, nextSequence: async () => sequence++ })
      client.ready = true
      client.allowWrites = false
      client.machineDescriptor = () => null
      client._unsupportedRefusal = () => null
      client._offlineRefusal = () => null
      client._outboundMachinePairing = async () => ({ keyID: "key", masterKey })
      client._subscriptionRoom = () => null
      client.subscribe = () => {}
      client._releaseSubscription = () => {}
      let published
      const publication = new Promise(resolve => { published = resolve })
      client._send = frame => published(frame.envelope)
      client._carrierFor = () => transport === "carrier" ? { open: true, send: published } : null
      const controller = new AbortController()
      const result = client.readForGeneration({ machineID: "m", sessionID: "s", executionGeneration: genA },
        "session-graphify", {}, controller.signal)
      try {
        const envelope = await Promise.race([publication, result.then(() => assert.fail("read settled before publication"))])
        const fields = JSON.parse(new TextDecoder().decode(await crypto.subtle.decrypt(
          { name: "AES-GCM", iv: base64Bytes(envelope.nonce, "nonce") }, masterKey, base64Bytes(envelope.ct, "ct"))))
        assert.equal(envelope.ch, "r/m")
        assert.equal(fields.type, "session-graphify")
        assert.equal(fields.machine_id, "m")
        assert.equal(fields.session, "s")
        assert.equal(fields.expected_generation, genA)
        assert.equal(client.allowWrites, false)
        const key = "m\u0000s\u0000session-graphify"
        assert.ok(client.readWaiters.has(key))
        assert.ok(client.pinnedReadProofs.has(key))
        const body = { state: "index_missing", nodes: [], links: [] }
        client._applySnapshot({ kind: "transcript", machine: "m", session: "s" }, {
          read: "session-graphify", machine_id: "m", session_id: "s", expected_generation: genA,
          seq: envelope.seq, body,
        }, { seq: 50, ts: Date.now() }, false)
        assert.deepEqual(await result, body)
        assert.equal(client.readWaiters.size, 0)
        assert.equal(client.pinnedReadProofs.size, 0)
        assert.equal(client.pendingBySequence.size, 0)
        await assert.rejects(() => client.readForGeneration({ machineID: "m", sessionID: "s", executionGeneration: genA },
          "session-graphify-build", {}, controller.signal), { code: "read_only_channel" })
        assert.equal(client.allowWrites, false)
      } finally {
        controller.abort()
        await result.catch(() => {})
      }
    })
  }
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
  await client.listPresentationsForMachine("m", signal, { fresh: true })
  assert.equal(calls[1][2].fresh, true)
})

test("opening one Session asks the machine for its fresh row", async () => {
  const client = Object.create(StatusCloudClient.prototype)
  const calls = []
  client._machineRequest = (...args) => { calls.push(args); return Promise.resolve({ ids: ["s"] }) }
  const signal = new AbortController().signal
  await client.refreshSessionStatus("m", "s", signal)
  assert.deepEqual(calls[0].slice(0, 4), ["m", "sessions.row", { row: "s" }, "read"])
  assert.equal(calls[0][5].signal, signal)
})

test("a transcript picture uses an exact image waiter and rejects a missing execution", async () => {
  const client = Object.create(StatusCloudClient.prototype)
  const calls = []
  client.imageReadQueue = []
  client.imageReadsRunning = 0
  client.imageReadsInFlight = 3
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

test("pinned transcript pictures respect the three-read image lane", async () => {
  const client = Object.create(StatusCloudClient.prototype)
  const calls = []
  const releases = []
  client.imageReadQueue = []
  client.imageReadsRunning = 0
  client.imageReadsInFlight = 3
  client._read = (...args) => {
    calls.push(args)
    return new Promise((resolve) => releases.push(resolve))
  }
  const destination = { machineID: "m", sessionID: "s", executionGeneration: genA }
  const images = Array.from({ length: 6 }, (_, index) => client.imageForGeneration(destination, `picture-${index}`))
  await new Promise((resolve) => setImmediate(resolve))
  assert.equal(calls.length, 3, "six images bypassed the image lane and filled the Mac's read slots")
  releases.shift()({ id: "picture-0" })
  await new Promise((resolve) => setImmediate(resolve))
  assert.equal(calls.length, 4, "a freed image slot did not start the next picture")
  for (const release of releases) release({})
  await new Promise((resolve) => setImmediate(resolve))
  for (const release of releases.slice(3)) release({})
  await Promise.all(images)
})

test("original detail panels retain their read names on the exact r/ target", async () => {
  const client = Object.create(StatusCloudClient.prototype)
  const calls = []
  client._read = (...args) => { calls.push(args); return Promise.resolve({}) }
  const target = { machineID: "m", sessionID: "s", executionGeneration: genA }
  for (const [word, fields, answer] of [
    ["info", {parts:"full"}, "info.full"], ["info", {parts:"remote-permission"}, "info.remote-permission"],
    ["git", {}, "git"], ["git-diff", { request: "diff-1", path: "README.md" }, "read:diff-1"],
    ["outputs", {}, "outputs"], ["output-file", { request: "file-1", id: "a".repeat(64), offset: "0", revision: "", size: "thumb" }, "read:file-1"],
    ["screen", {}, "screen"], ["agent", { agent: "a", limit: 100 }, "agent:a"],
    ["agent", { agent: "a", limit: 100, before: 17 }, "agent:a.before.17"],
    ["shell", { shell: "sh", bytes: 4096 }, "shell:sh"], ["documents", {}, "documents"],
    ["document", { request: "doc-1", scope: "project", task: "", path: "README.md" }, "read:doc-1"],
    ["video", { request: "video-1", scope: "project", task: "", path: "clip.mp4", offset: 0, revision: "" }, "read:video-1"],
  ]) {
    await client.readForGeneration(target, word, fields)
    const sent = calls.at(-1)
    assert.deepEqual(sent[0], { machine: "m", session: "s" })
    assert.equal(sent[1], word)
    assert.deepEqual(sent[2], { ...fields, machine_id: "m", expected_generation: genA })
    assert.equal(sent[3], answer)
  }
  assert.equal(calls.length, 13)
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
  client.machineDescriptor=()=>null
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
  const revision = "agy1:synthetic:123:abc"
  const olderName = "transcript.before.123.revision." + revision
  const olderKey = "m\u0000s\u0000" + olderName
  const olderWaiters = {}
  client.readWaiters.set(olderKey, olderWaiters)
  client.pinnedReadProofs.set(olderKey, { machineID: "m", sessionID: "s", generation: genA,
    read: olderName, seq: null, waiters: olderWaiters })
  const olderEnvelope = await client._publishCommand("m", "transcript", {
    session: "s", machine_id: "m", expected_generation: genA, limit: 200, before: 123, revision,
  }, "ctl", { key: olderKey, waiters: olderWaiters })
  assert.equal(await crypto.subtle.verify({ name: "Ed25519" }, keys.publicKey,
    base64Bytes(olderEnvelope.sig, "sig"), envelopeSigningBytes(olderEnvelope)), true)
  const olderBody = JSON.parse(new TextDecoder().decode(await crypto.subtle.decrypt(
    { name: "AES-GCM", iv: base64Bytes(olderEnvelope.nonce, "nonce") }, masterKey, base64Bytes(olderEnvelope.ct, "ct"))))
  assert.deepEqual(olderBody, { type: "transcript", session: "s", machine_id: "m", expected_generation: genA,
    limit: 200, before: 123, revision })
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

test("replayed or disconnected older replies cannot settle a new revision at the same offset", () => {
  const client = Object.create(StatusCloudClient.prototype)
  const read = "transcript.before.123.revision.agy1:new"
  const key = "m\u0000s\u0000" + read
  const waiters = {}
  client.readWaiters = new Map([[key, waiters]])
  client.pinnedReadProofs = new Map([[key, { machineID: "m", sessionID: "s", generation: genA,
    read, seq: 52, waiters }]])
  client._observeMachine = () => {}
  client.transcriptSnapshots = new Map()
  client._emit = () => {}
  const settlements = []
  client._settleRead = (...args) => { if (client.readWaiters.has(args[0])) settlements.push(args) }
  const channel = { kind: "transcript", machine: "m", session: "s" }
  const payload = { read, machine_id: "m", session_id: "s", expected_generation: genA, seq: 52,
    body: { entries: [], signature: "agy1:new" } }
  client._applySnapshot(channel, { ...payload, read: "transcript.before.123.revision.agy1:old", seq: 51 }, {}, false)
  client._applySnapshot(channel, payload, {}, true)
  assert.equal(settlements.length, 0)
  client._applySnapshot(channel, payload, {}, false)
  assert.equal(settlements.length, 1)
  assert.equal(settlements[0][0], key)
  client.readWaiters.clear()
  client.pinnedReadProofs.clear()
  client._applySnapshot(channel, payload, {}, false)
  assert.equal(settlements.length, 1, "an answer after disconnect has no waiter to fill")
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

// On 2026-10-12 a Session stopped on a question was open on the page, and the
// machine logged its transcript channel delivered every minute and its rich-row
// channel to nobody: the next transcript read had trimmed the quiet s/ as idle,
// and the row carrying the question never arrived.
test("an opened detail's rich row is not trimmed as idle by the next read, and is let go on close", () => {
  const client = Object.create(StatusCloudClient.prototype)
  let now = 0
  client.now = () => now
  client.ready = true
  client.subscriptionLimit = 8
  client.subscriptionIdleMs = 120_000
  client.subscriptionHolds = new Map()
  client.pendingSubscriptions = new Set()
  client.socketSubscriptions = new Map()
  client.subscriptionFrames = []
  client.resubscribes = new Map()
  client.openedDetails = new Set()
  client.detailSnapshots = new Map()
  const sent = []
  client._send = (frame) => sent.push(frame)
  const destination = { machineID: "m", sessionID: "s", executionGeneration: genA }
  client.openDetail(destination)
  client.subscribe(["s/m/s", "t/m/s"])
  now = 10 * 60_000
  client.subscribe(["t/m/other"])
  assert.ok(client.socketSubscriptions.has("s/m/s"), "the opened rich row was trimmed")
  assert.ok(!sent.some((f) => f.type === "unsubscribe" && f.channels.includes("s/m/s")))
  client.openDetail(destination)
  client.closeDetail(destination)
  assert.equal(client.subscriptionHolds.has("s/m/s"), false, "a second open of the same detail held it twice")
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

test("a cold browser replays one missing inventory marker and shares the pending read", async () => {
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
  const delays = []
  client.setTimeout = (_callback, ms) => { delays.push(ms); return 1 }
  client.clearTimeout = () => {}
  const frames = []
  client._sendSubscriptionFrame = (type, channels) => frames.push({ type, channels })
  let listener
  client.events = (callback) => { listener = callback; return () => { listener = null } }
  const first = client.recoverStatusMarker("m")
  const second = client.recoverStatusMarker("m")
  assert.equal(first, second)
  assert.deepEqual(delays, [5_000])
  assert.deepEqual(frames, [{ type: "subscribe", channels: ["ss/m/__clawdline_inventory_v1__"] }])
  client.statusSnapshots.set(JSON.stringify(["m", "__clawdline_inventory_v1__"]), { payload: { inventory: { version: 1 } } })
  listener({ type: "session_status", identity: { machine: "m", session: "__clawdline_inventory_v1__" } })
  assert.equal(await first, true)
  assert.deepEqual(frames[1], { type: "unsubscribe", channels: ["ss/m/__clawdline_inventory_v1__"] })
  assert.equal(await client.recoverStatusMarker("m"), true)
  assert.equal(frames.length, 2)
})

test("a pinned machine list has a shorter timeout than an ordinary content read", async () => {
  const client = Object.create(StatusCloudClient.prototype)
  let received
  client._read = (...args) => { received = args; return Promise.resolve({ complete: true, sessions: [] }) }
  await client.listPresentationsForMachine("m", new AbortController().signal)
  assert.equal(received[4], 10_000)
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

// A schedules read keeps its rows in the snapshot map before any orch/
// envelope has come. That entry is not the descriptor: the page still asks the
// machine for it, which is where its version is. Taking the entry for the
// snapshot left a Linux machine on v1.0.9 reading "version not provided".
test("a schedules read does not stand in for the machine's own descriptor", async () => {
  const client = Object.create(StatusCloudClient.prototype)
  client.retired = false
  client.now = () => 1_700_000
  client.orchestratorSnapshots = new Map()
  client.orchestratorReceived = new Set()
  client.orchestratorHeld = new Set()
  client._machinesFor = () => ({ rows: [{ id: "linux-b" }], capable: [{ id: "linux-b" }], pairable: [] })
  client._machineRequest = async () => ({ schedules: [{ id: "daily" }], at: 1_700 })
  await client.schedules({ fresh: true, machine: "linux-b" })
  assert.ok(client.orchestratorSnapshots.has("linux-b"), "the rows are kept")
  assert.equal(client._holdsOrchestrator("linux-b"), false, "the descriptor is still asked for")
  client.orchestratorReceived.add("linux-b")
  assert.equal(client._holdsOrchestrator("linux-b"), true)
})

// 2026-10-16: the machine's log showed an opened waiting Session's s/ row going
// out with `fanout=0` and `fanout=1` by turns. The list's row recovery asked for
// the same channel as the opened detail and let go of it when the row came, or
// when it gave up waiting, though the opened detail still held it.
test("the list's row recovery does not let go of the channel an opened detail holds", () => {
  for (const ending of ["row", "timeout"]) {
    const client = Object.create(StatusCloudClient.prototype)
    client.statusSnapshots = new Map([
      [JSON.stringify(["m", "__clawdline_inventory_v1__"]), { payload: {
        complete: true, snapshot_generation: genA, inventory: { version: 1, sessions: ["s"] } } }],
      [JSON.stringify(["m", "s"]), { payload: { snapshot_generation: genA, execution_generation: genA } }],
    ])
    client.sessionSnapshots = new Map()
    client.classicSessionMachine = null
    client.classicSessionReads = new Map()
    client.classicSessionAttempted = new Map()
    client.pendingSubscriptions = new Set()
    client.socketSubscriptions = new Map()
    client.subscriptionHolds = new Map()
    client.resubscribes = new Map()
    client.openedDetails = new Set()
    client.detailSnapshots = new Map()
    client.listeners = new Set()
    client.subscriptionLimit = 8
    client.ready = true
    client.now = () => 100
    let fire = null
    client.setTimeout = (fn) => { fire = fn; return 1 }
    client.clearTimeout = () => {}
    const frames = []
    client._sendSubscriptionFrame = (type, channels) => frames.push({ type, channels })
    client.openDetail({ machineID: "m", sessionID: "s", executionGeneration: genA })
    client.enableClassicSessionView("m", "s")
    assert.deepEqual(frames, [{ type: "subscribe", channels: ["s/m/s"] }])
    if (ending === "row") {
      client.sessionSnapshots.set("m\u0000s", { execution_generation: genA })
      client._emit({ type: "sessions", identity: { machine: "m", session: "s" } })
    } else fire()
    assert.ok(client.socketSubscriptions.has("s/m/s"), `the opened detail's channel was let go on ${ending}`)
    assert.ok(!frames.some((f) => f.type === "unsubscribe"), ending)
    assert.equal(client.classicSessionReads.size, 0, "the recovery's own read is still finished")
  }
})

// The row a `sessions.row` answer carries is taken as its s/ envelope would
// be, and copied to the opened detail — unless an envelope moved the row while
// the ask was out, it names another Session, or the machine's marker no
// longer lists it.
test("an answered Session row is taken only when nothing newer came", () => {
  const make = () => {
    const client = Object.create(StatusCloudClient.prototype)
    client.sessionSnapshots = new Map()
    client.sessionSequenceByKey = new Map()
    client.sessionInventoryByMachine = new Map([["m", { sequence: 5, ids: new Set(["m\u0000s"]) }]])
    client.sessionKeysHeard = new Set()
    client.sessionRecovery = new Map()
    client.orchestratorSnapshots = new Map()
    client.machineDescriptors = new Map()
    client.openedDetails = new Set(["s/m/s"])
    client.detailSnapshots = new Map()
    client.listeners = new Set()
    client.events = []
    client.listeners.add((event) => client.events.push(event))
    return client
  }
  const payload = { session: { id: "s", state: "waiting", menu: { options: [{ n: 1, label: "Yes" }] } }, at: 1, scan: {} }
  const client = make()
  const mark = client._sessionRowMark("m", "s")
  assert.equal(client._applyAnsweredSessionRow("m", "s", payload, mark), true)
  assert.deepEqual(client.detailSnapshots.get("m\u0000s")?.menu, payload.session.menu)
  assert.equal(client.events.at(-1)?.type, "sessions")
  assert.deepEqual(client.events.at(-1)?.identity, { machine: "m", session: "s" })

  const newer = make()
  const before = newer._sessionRowMark("m", "s")
  newer.sessionSequenceByKey.set("m\u0000s", 9)
  newer.sessionSnapshots.set("m\u0000s", { id: "s", state: "working" })
  assert.equal(newer._applyAnsweredSessionRow("m", "s", payload, before), false)
  assert.equal(newer.sessionSnapshots.get("m\u0000s").state, "working", "an envelope delivered meanwhile wins")

  const other = make()
  assert.equal(other._applyAnsweredSessionRow("m", "s", { session: { id: "t" } }, 0), false)
  const gone = make()
  gone.sessionInventoryByMachine.set("m", { sequence: 6, ids: new Set() })
  assert.equal(gone._applyAnsweredSessionRow("m", "s", payload, 0), false)
  assert.equal(gone.sessionSnapshots.size, 0)
})

// A short-lived viewer token makes a new socket and a new client every few
// minutes. The page holds the machine's descriptor across that renewal, so the
// new client does not ask for it again — and it must therefore carry it, or
// the version the sidebar reads from it reads as still coming, forever.
test("a renewed client carries the descriptor it will not ask for again", () => {
  const options = { relayURL: "https://relay.test", deviceToken: "t", account: "acct", deviceID: "dev" }
  const prior = new StatusCloudClient(options)
  const descriptor = { app: { version: "v1.0.9", api_level: 7 }, machine: { commands: ["transcript"] } }
  prior.orchestratorSnapshots.set("mac-a", descriptor)
  prior.orchestratorReceived.add("mac-a")
  prior.orchestratorSnapshots.set("linux-b", { schedules: [] })
  const next = new StatusCloudClient({ ...options, resumeFrom: prior })
  assert.equal(next._holdsOrchestrator("mac-a"), true, "the descriptor is not asked for again")
  assert.equal(next.orchestratorSnapshots.get("mac-a"), descriptor, "so it is carried")
  assert.ok(next.orchestratorReceived.has("mac-a"))
  assert.equal(next._holdsOrchestrator("linux-b"), false, "a schedules entry is still not a descriptor")
  assert.equal(next.orchestratorSnapshots.has("linux-b"), false)
})

test("the selected classic Session stays subscribed after recovery, including an already held row", () => {
  for (const cached of [false, true]) {
    const client = Object.create(StatusCloudClient.prototype)
    client.statusSnapshots = new Map([
      [JSON.stringify(["m", "__clawdline_inventory_v1__"]), { payload: {
        complete: true, snapshot_generation: genA, inventory: { version: 1, sessions: ["s"] } } }],
      [JSON.stringify(["m", "s"]), { payload: { snapshot_generation: genA, execution_generation: genA } }],
    ])
    client.sessionSnapshots = new Map(cached ? [["m\u0000s", { execution_generation: genA }]] : [])
    client.classicSessionReads = new Map()
    client.classicSessionAttempted = new Map()
    client.pendingSubscriptions = new Set()
    client.socketSubscriptions = new Map()
    client.subscriptionHolds = new Map()
    client.resubscribes = new Map()
    client.openedDetails = new Set()
    client.detailSnapshots = new Map()
    client.listeners = new Set()
    client.subscriptionLimit = 8
    client.ready = true
    client.now = () => 100
    client.setTimeout = () => 1
    client.clearTimeout = () => {}
    const frames = []
    client._sendSubscriptionFrame = (type, channels) => frames.push({ type, channels })
    client.enableClassicSessionView("m", "s")
    assert.equal(client.subscriptionHolds.get("s/m/s"), 1)
    client._emit({ type: "sessions", identity: { machine: "m", session: "s" } })
    assert.equal(client.socketSubscriptions.has("s/m/s"), true, "the Effort event still has a receiver")
    assert.equal(frames.some((frame) => frame.type === "unsubscribe"), false)
    client.disableClassicSessionView()
    assert.equal(client.subscriptionHolds.has("s/m/s"), false)
    assert.equal(client.socketSubscriptions.has("s/m/s"), false)
  }
})


test("thumbnails have a separate exact waiter and older machines keep their original image read", async () => {
  const client = Object.create(StatusCloudClient.prototype)
  Object.assign(client, { imageReadQueue: [], imageReadsRunning: 0, imageReadsInFlight: 3 })
  const calls = []
  client.machineDescriptor = () => ({ machine: { image_thumbnails_v1: true } })
  client._read = (...args) => { calls.push(args); return Promise.resolve({}) }
  const target = { machineID: "m", sessionID: "s", executionGeneration: genA }
  await client.imageForGeneration(target, "p", undefined, "thumb")
  assert.equal(calls[0][2].size, "thumb")
  assert.equal(calls[0][3], "image.p.thumb")
  await client.imageForGeneration(target, "p")
  assert.equal(calls[1][2].size, undefined)
  assert.equal(calls[1][3], "image.p")
  client.machineDescriptor = () => ({ machine: {} })
  await client.imageForGeneration(target, "p", undefined, "thumb")
  assert.equal(calls[2][2].size, undefined)
  assert.equal(calls[2][3], "image.p")
})


test("an older carrier's pin requirement falls back only its reads and stops new direct attempts", async () => {
  const c = Object.create(StatusCloudClient.prototype)
  const identity = {machine: "m", session: "s"}
  const waiters = {waiting: [], ref: null}
  c.carrierReads = new Map([["one", {identity, type: "transcript", extra: {}, waiters}], ["two", {identity: {machine: "other"}}]])
  c.readWaiters = new Map([["one", waiters]])
  c.subscriptionHolds = new Map()
  c._subscriptionRoom = () => null
  c.subscribe = () => {}
  const published = [], settled = []
  c._publishCommand = async (...args) => {published.push(args)}
  c._settleRead = (key, body, error) => settled.push([key, error?.code])
  c._emit = () => assert.fail("a carrier refusal is not a failed relay read")
  c.directCarriers = { for() { assert.fail("must keep this client on relay") } }
  c._carrierDown("m", "machine_pairing_required")
  await new Promise(resolve => setImmediate(resolve))
  assert.equal(published.length, 1)
  assert.equal(published[0][0], "m")
  assert.equal(published[0][1], "transcript")
  assert.equal(c.readWaiters.get("one"), waiters, "the original caller still awaits its relay answer")
  assert.equal(c.carrierReads.has("two"), true)
  assert.equal(c._carrierFor("m"), null)
  assert.deepEqual(settled, [])
})

test("versioned tool views require an authenticated machine capability and reuse only matching pages", async () => {
  const client = Object.create(StatusCloudClient.prototype)
  client.pinnedTranscriptFlights = new Map()
  let supported = false
  client.machineDescriptor = () => ({machine:{transcript_tools_v1:supported}})
  const asks = []
  const held = {id:"s",version:"a".repeat(64),entries:[{role:"assistant",text:"held"}]}
  client._read = async (_identity,_type,fields,answer) => {
    asks.push({fields,answer})
    return fields.view?.includes(held.version) ? {unchanged:true,version:held.version} : held
  }
  const target = {machineID:"m",sessionID:"s",executionGeneration:genA}
  assert.equal(await client.transcriptForGeneration(target,undefined,held),held)
  assert.equal(asks[0].fields.view,undefined)
  supported = true
  assert.equal(await client.transcriptForGeneration(target,undefined,held),held)
  assert.match(asks[1].fields.view,/^tools-v1z?:a{64}$/)
  assert.equal(asks[1].answer,"transcript.view."+asks[1].fields.view)
  assert.equal(asks[1].fields.expected_generation,genA)
  client._read = async () => ({unchanged:true,version:"b".repeat(64)})
  await assert.rejects(client.transcriptForGeneration(target,undefined,held),error=>error.code==="transcript_cursor_stale")
})

test("a stale capability falls back once without dropping the execution pin", async () => {
  const client = Object.create(StatusCloudClient.prototype)
  client.pinnedTranscriptFlights = new Map()
  client.machineDescriptor = () => ({machine:{transcript_tools_v1:true}})
  const asks = []
  client._read = async (_identity,_type,fields) => {
    asks.push(fields)
    if(fields.view) throw Object.assign(new Error("old machine"),{code:"cloud_read_timeout"})
    return {entries:[]}
  }
  await client.transcriptForGeneration({machineID:"m",sessionID:"s",executionGeneration:genA})
  assert.equal(asks.length,2)
  assert.equal(asks[1].view,undefined)
  assert.equal(asks[1].expected_generation,genA)
})

test("long tool bodies opt into v2 only on a supporting machine, including older pages",async()=>{
 for(const supported of [false,true]) {
  const client=Object.create(StatusCloudClient.prototype)
  client.pinnedTranscriptFlights=new Map()
  client.machineDescriptor=()=>({machine:{transcript_tools_v1:true,transcript_tools_v2:supported}})
  const asks=[]
  client._read=async(_identity,_type,fields,answer)=>{asks.push({fields,answer});return {entries:[]}}
  const target={machineID:"m",sessionID:"s",executionGeneration:genA}
  await client.transcriptForGeneration(target)
  await client.transcriptPageForGeneration(target,200,undefined,"held-revision")
  assert.equal(asks.length,2)
  for(const ask of asks) {
   assert.match(ask.fields.view,supported?/^tools-v2z?$/:/^tools-v1z?$/)
   assert.equal(ask.fields.expected_generation,genA)
   assert.ok(ask.answer.endsWith(".view."+ask.fields.view))
  }
  assert.equal(asks[1].fields.revision,"held-revision")
 }
})

test("a stale v2 descriptor retries one v1 read with the same execution and history cursor",async()=>{
 const client=Object.create(StatusCloudClient.prototype)
 client.pinnedTranscriptFlights=new Map()
 client.machineDescriptor=()=>({machine:{transcript_tools_v1:true,transcript_tools_v2:true}})
 const asks=[]
 client._read=async(_identity,_type,fields,answer)=>{
  asks.push({fields,answer})
  if(fields.view.startsWith("tools-v2")) throw Object.assign(new Error("downgraded"),{code:"invalid_transcript_view"})
  return {entries:[]}
 }
 const target={machineID:"m",sessionID:"s",executionGeneration:genA}
 await client.transcriptForGeneration(target)
 await client.transcriptPageForGeneration(target,200,undefined,"revision")
 assert.equal(asks.length,4)
 for(const index of [1,3]) {
  assert.match(asks[index].fields.view,/^tools-v1z?$/)
  assert.equal(asks[index].fields.expected_generation,genA)
 }
 assert.equal(asks[3].fields.before,200)
 assert.equal(asks[3].fields.revision,"revision")
})

test("transcript deltas replace revisions and preserve repeated entries atomically",()=>{
 const row=text=>({role:"tool",text})
 const held={id:"s",version:"a".repeat(64),signature:"old",evidence:"transcript",nextBefore:10,entries:[row("same"),row("pending"),row("same")]}
 const reply={baseVersion:held.version,entryCount:4,page:{id:"s",version:"b".repeat(64),signature:"new",evidence:"transcript",entries:[]},delta:[{start:1,deleteCount:1,entries:[row("complete"),row("same")]}]}
 const result=applyTranscriptDelta(held,reply,"s",200)
 assert.deepEqual(result.entries.map(x=>x.text),["same","complete","same","same"])
 assert.equal(result.signature,"new")
 assert.equal(result.nextBefore,undefined,"current metadata replaces stale metadata")
 assert.equal(held.entries[1].text,"pending","the held page is immutable until the update passes")
})

test("malformed or foreign transcript deltas never change the held page",()=>{
 const held={id:"s",version:"a".repeat(64),entries:[{role:"assistant",text:"kept"}]}
 const good={baseVersion:held.version,entryCount:2,page:{id:"s",version:"b".repeat(64),signature:"new",evidence:"transcript",entries:[]},delta:[{start:1,deleteCount:0,entries:[{role:"assistant",text:"new"}]}]}
 const bad=[{...good,baseVersion:"c".repeat(64)}, {...good,page:{...good.page,id:"other"}},
  {...good,delta:[{...good.delta[0],start:-1}]}, {...good,delta:[{...good.delta[0],start:2}]},
  {...good,delta:[{...good.delta[0],deleteCount:1}]}, {...good,delta:[{...good.delta[0],entries:null}]},
  {...good,delta:[{...good.delta[0],entries:[{role:"tool"}]}]}, {...good,page:{...good.page,entries:[{role:"tool",text:"ambiguous"}]}}]
 for(const reply of bad) assert.throws(()=>applyTranscriptDelta(held,reply,"s",200),error=>error.code==="transcript_cursor_stale")
 assert.throws(()=>applyTranscriptDelta(held,{...good,entryCount:1},"s"),error=>error.code==="transcript_cursor_stale")
 assert.deepEqual(held.entries,[{role:"assistant",text:"kept"}])
})

test("incremental transcript reads require a negotiated capability and merge before publishing",async()=>{
 for(const supported of [false,true]) {
  const client=Object.create(StatusCloudClient.prototype)
  client.pinnedTranscriptFlights=new Map()
  client.machineDescriptor=()=>({machine:{transcript_tools_v1:true,transcript_tools_v2:true,transcript_delta_v1:supported}})
  const held={id:"s",version:"a".repeat(64),entries:[{role:"assistant",text:"kept"}]}
  const full={id:"s",version:"b".repeat(64),signature:"next",evidence:"transcript",entries:[...held.entries,{role:"assistant",text:"new"}]}
  const asks=[]
  client._read=async(_identity,_type,fields,read,_x,options)=>{
   asks.push({fields,read,signal:options.signal})
   return fields.view.startsWith("delta-")?{baseVersion:held.version,entryCount:full.entries.length,page:{...full,entries:[]},delta:[{start:1,deleteCount:0,entries:[full.entries[1]]}]}:full
  }
  const controller=new AbortController(),target={machineID:"m",sessionID:"s",executionGeneration:genA}
  assert.deepEqual(await client.transcriptForGeneration(target,controller.signal,held),full)
  assert.match(asks[0].fields.view,supported?/^delta-v1z?:a{64}$/:/^tools-v2z?:a{64}$/)
  assert.equal(asks[0].read,"transcript.view."+asks[0].fields.view)
  assert.equal(asks[0].fields.machine_id,"m")
  assert.equal(asks[0].fields.expected_generation,genA)
  assert.equal(asks[0].signal,controller.signal)
  await client.transcriptForGeneration(target)
  assert.match(asks[1].fields.view,/^tools-v2z?$/,"the first page is always full")
 }
})

test("a stale delta recovers once with a full compact read and keeps cancellation and scope",async()=>{
 const client=Object.create(StatusCloudClient.prototype)
 client.pinnedTranscriptFlights=new Map()
 client.machineDescriptor=()=>({machine:{transcript_tools_v1:true,transcript_tools_v2:true,transcript_delta_v1:true}})
 const held={id:"s",version:"a".repeat(64),entries:[]},full={id:"s",entries:[{role:"assistant",text:"recovered"}]}
 const asks=[],controller=new AbortController(),target={machineID:"m",sessionID:"s",executionGeneration:genA}
 client._read=async(_identity,_type,fields,_read,_x,options)=>{
  asks.push({fields,signal:options.signal})
  return fields.view.startsWith("delta-")?{baseVersion:"wrong",delta:{}}:full
 }
 assert.equal(await client.transcriptForGeneration(target,controller.signal,held),full)
 assert.equal(asks.length,2)
 assert.match(asks[1].fields.view,/^tools-v2z?$/)
 assert.equal(asks[1].fields.expected_generation,genA)
 assert.equal(asks[1].signal,controller.signal)
 asks.length=0
 client._read=async(_identity,_type,fields)=>{asks.push(fields);throw Object.assign(new Error("stale"),{code:"transcript_cursor_stale"})}
 await assert.rejects(client.transcriptForGeneration(target,undefined,held),error=>error.code==="transcript_cursor_stale")
 assert.equal(asks.length,2,"recovery cannot amplify into an unbounded retry")
})

test("an execution switch waits out an old delta and cannot publish it as the new answer",async()=>{
 const client=Object.create(StatusCloudClient.prototype)
 client.pinnedTranscriptFlights=new Map()
 client.machineDescriptor=()=>({machine:{transcript_tools_v1:true,transcript_tools_v2:true,transcript_delta_v1:true}})
 const held={id:"s",version:"a".repeat(64),entries:[]},asks=[]
 let release
 client._read=async(_identity,_type,fields)=>{
  asks.push(fields)
  if(fields.expected_generation===genA) return new Promise(resolve=>release=resolve)
  return {id:"s",entries:[{role:"assistant",text:"new execution"}]}
 }
 const old=client.transcriptForGeneration({machineID:"m",sessionID:"s",executionGeneration:genA},undefined,held)
 const next=client.transcriptForGeneration({machineID:"m",sessionID:"s",executionGeneration:genB},undefined,held)
 assert.equal(asks.length,1)
 release({unchanged:true,version:held.version})
 assert.equal(await old,held)
 assert.equal((await next).entries[0].text,"new execution")
 assert.equal(asks[1].expected_generation,genB)
})

test("rolling windows and provider expansions preserve all retained and repeated rows",()=>{
 const rows=Array.from({length:201},(_,i)=>({role:"tool",text:"row-"+i}))
 const held={id:"s",version:"a".repeat(64),entries:rows.slice(0,200)}
 const page={id:"s",version:"b".repeat(64),signature:"new",evidence:"transcript",entries:[]}
 const rolling={baseVersion:held.version,entryCount:200,page,delta:[{start:0,deleteCount:1,entries:[]},{start:200,deleteCount:0,entries:[rows[200]]}]}
 assert.deepEqual(applyTranscriptDelta(held,rolling,"s").entries,rows.slice(1))
 const expanded={...rolling,entryCount:201,delta:[rolling.delta[1]]}
 assert.deepEqual(applyTranscriptDelta(held,expanded,"s").entries,rows)
 assert.throws(()=>applyTranscriptDelta(held,{...rolling,delta:[rolling.delta[1],rolling.delta[0]]},"s"),error=>error.code==="transcript_cursor_stale")
})

test("generic JSON compression is negotiated only for capable read peers",async()=>{
 for(const capable of [false,true]) {
  const client=Object.create(StatusCloudClient.prototype)
  client.machineDescriptor=()=>({machine:{read_compression_v1:capable}})
  const calls=[]
  client._readRepresentation=async(...args)=>{calls.push(args);return {items:[]}}
  const signal=new AbortController().signal,identity={machine:"m",session:"__clawdline_machine__"}
  await client._read(identity,"work.v2.cards",{request:"fixture"},"read:fixture",123,{signal})
  assert.equal(calls[0][2].accept_encoding,capable?"gzip-v1":undefined)
  assert.equal(calls[0][3],capable?"read:fixture.encoding.gzip-v1":"read:fixture")
  assert.equal(calls[0][5].signal,signal)
  await client._read(identity,"work.v2.edit",{request:"write"},"action:write")
  await client._read(identity,"image",{id:"picture"},"image.picture")
  assert.equal(calls[1][2].accept_encoding,undefined)
  assert.equal(calls[2][2].accept_encoding,undefined)
 }
})

test("a stale compression capability falls back once and disables repeat failures",async()=>{
 const client=Object.create(StatusCloudClient.prototype)
 client.machineDescriptor=()=>({machine:{read_compression_v1:true}})
 const calls=[],identity={machine:"m",session:"s"},fields={machine_id:"m",expected_generation:genA}
 client._readRepresentation=async(...args)=>{
  calls.push(args)
  if(args[2].accept_encoding) throw Object.assign(new Error("old peer"),{code:"malformed_read"})
  return {skills:[]}
 }
 await client._read(identity,"skills",fields,"skills")
 await client._read(identity,"skills",fields,"skills")
 assert.equal(calls.length,3)
 assert.equal(calls[1][3],"skills")
 assert.equal(calls[1][2].expected_generation,genA)
 assert.equal(calls[2][2].accept_encoding,undefined)
 client.machineLacks.clear();calls.length=0
 client._readRepresentation=async(...args)=>{calls.push(args);throw Object.assign(new Error("denied"),{code:"forbidden"})}
 await assert.rejects(client._read(identity,"skills",fields,"skills"),error=>error.code==="forbidden")
 assert.equal(calls.length,1,"authorization failures must not trigger another read")
})

test("negotiated pinned reads preserve the reply proof and abort without fallback",async()=>{
 const client=Object.create(StatusCloudClient.prototype)
 client.machineDescriptor=()=>({machine:{read_compression_v1:true}})
 Object.assign(client,{allowWrites:false,retired:false,readWaiters:new Map(),pinnedReadProofs:new Map(),_unsupportedRefusal:()=>null,
  _offlineRefusal:()=>Object.assign(new Error("offline"),{code:"sentinel"})})
 await assert.rejects(client._read({machine:"m",session:"s"},"skills",{machine_id:"m",expected_generation:genA},"skills"),error=>error.code==="sentinel")
 assert.equal(client.allowWrites,false)
 const controller=new AbortController();let calls=0
 client._readRepresentation=async()=>{calls++;controller.abort();throw Object.assign(new Error("cancelled"),{code:"cloud_read_timeout"})}
 await assert.rejects(client._read({machine:"m",session:"s"},"skills",{},"skills",123,{signal:controller.signal}),error=>error.code==="cloud_read_timeout")
 assert.equal(calls,1)
})
