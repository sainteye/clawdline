// The copied client's `t/` subscriptions against a relay that answers each
// subscribe and unsubscribe in order: `node --test web/console/src/cloud/*.test.ts`.
//
// What a phone hit: the machine answered a Project read within milliseconds on
// `t/<machine>/__clawdline_machine__`, and the relay acked every answer
// `delivered fanout=0` for a minute, because it held no subscription for that
// channel on the browser's socket while the client believed it did. The client
// recorded a channel as held the moment its `subscribe` frame left, never sent
// it again, and never read the relay's `subscriptions` answer. So these tests
// hand the client a relay that loses or refuses a channel and require the read
// on that channel to finish anyway, or to fail with the relay's own word.
//
// The relay here routes by what it really holds: an answer on a `t/` channel
// reaches the page only while that channel is in its list. Sealing and opening
// are replaced on the instance — this is about which channels the socket holds,
// not about keys — and everything between is the client's own code.
import { test } from "node:test"
import assert from "node:assert/strict"
import { CloudClient } from "../legacy/js/net/cloud-client.js"
import { parseEnvelopeChannel } from "../legacy/js/net/cloud-crypto.js"
// @ts-expect-error -- node runs the source TypeScript directly.
import { RelayReader } from "./relay-reader.ts"

const MACHINE = "mac-a"
const REPLY = "t/" + MACHINE + "/__clawdline_machine__"
const SESSION = "t/" + MACHINE + "/%250"
const READ_TIMEOUT_MS = 400

type Frame = { type: string; channels?: string[]; envelope?: { ch: string; body: Record<string, unknown> } }

/** One socket's relay: answers frames in order, holds a list, delivers only to what it holds. */
class Relay {
  holds: string[] = []
  frames: Frame[] = []
  /** Called for each subscribe or unsubscribe before it is applied; may return a refusal code. */
  refuse: (frame: Frame) => string | null = () => null
  /** Called after a frame is applied: the lost-update race drops channels here. */
  lose: (frame: Frame) => string[] = () => []
  socket: FakeSocket | null = null
  private seq = 1

  subscribesFor(channel: string) {
    return this.frames.filter((frame) => frame.type === "subscribe" && frame.channels!.includes(channel)).length
  }

  receive(frame: Frame) {
    this.frames.push(frame)
    if (frame.type === "subscribe" || frame.type === "unsubscribe") {
      const code = this.refuse(frame)
      if (code) return this.say({ type: "error", code, message: "refused" })
      const next = new Set(this.holds)
      frame.channels!.forEach((channel) => frame.type === "subscribe" ? next.add(channel) : next.delete(channel))
      this.lose(frame).forEach((channel) => next.delete(channel))
      this.holds = [...next]
      return this.say({ type: "subscriptions", channels: this.holds })
    }
    if (frame.type === "publish") {
      const body = frame.envelope!.body
      const delivered = this.holds.includes(REPLY)
      this.say({ type: "ack", ch: frame.envelope!.ch, seq: this.seq, fanout: delivered ? 1 : 0, status: "delivered" })
      if (delivered) {
        this.say({ type: "envelope", envelope: { ch: REPLY, seq: this.seq, ts: Date.now(), sender: "mac-sender",
          payload: { read: "read:" + body.request, body: { answered: body.type } } } })
      }
      this.seq += 1
    }
  }

  say(frame: unknown) {
    const socket = this.socket!
    setImmediate(() => socket.onmessage?.({ data: JSON.stringify(frame) }))
  }
}

class FakeSocket {
  static relay: Relay
  readyState = 1
  onmessage: ((event: { data: string }) => void) | null = null
  onclose: ((event: { code: number }) => void) | null = null
  onerror: (() => void) | null = null
  relay: Relay
  constructor() {
    this.relay = FakeSocket.relay
    this.relay.socket = this
  }
  send(data: string) { this.relay.receive(JSON.parse(data)) }
  close() { this.readyState = 3 }
}

async function connected(relay: Relay) {
  FakeSocket.relay = relay
  let sequence = 1
  const client = new CloudClient({
    relayURL: "https://relay.example.test", deviceToken: "token", devicePrivateKey: { extractable: false },
    deviceID: "viewer-1", account: "account-1", allowWrites: true, nextSequence: async () => sequence++,
    WebSocket: FakeSocket, readTimeoutMs: READ_TIMEOUT_MS,
  }) as any
  // Sealing and opening stand aside: a publish carries its plaintext, an envelope its payload.
  client._publishCommand = async function (this: any, machine: string, type: string, body: Record<string, unknown>) {
    this._send({ type: "publish", envelope: { ch: "ctl/" + machine, body: { type, ...body } } })
  }
  client._receiveEnvelope = async function (this: any, envelope: any, realign: boolean) {
    this._applySnapshot(parseEnvelopeChannel(envelope.ch), envelope.payload, envelope, realign)
  }
  await client.start({ quiet: true })
  relay.say({ type: "ready", v: 1, role: "viewer", account: "account-1", device: "viewer-1" })
  await client.whenReady()
  return client
}

/** Every frame the relay has to answer is answered, and every answer is read. */
async function settled(client: any) {
  for (let i = 0; i < 20; i += 1) {
    await new Promise((resolve) => setImmediate(resolve))
    await client.messageChain
  }
}

function read(client: any) {
  return client._machineRequest(MACHINE, "projects", {}, "read") as Promise<{ answered: string }>
}

test("aborted reads release eight distinct channels before another read publishes", async () => {
  const relay = new Relay()
  const client = await connected(relay)
  client._publishCommand = () => new Promise(() => {})
  try {
    const controllers = Array.from({ length: 8 }, () => new AbortController())
    const reads = controllers.map((controller, index) => client._read(
      { machine: MACHINE, session: `held-${index}` }, "info", {}, "info.full", 10_000,
      { signal: controller.signal },
    ))
    await settled(client)
    assert.equal(client.subscriptionHolds.size, 8)
    controllers.forEach((controller) => controller.abort())
    await Promise.all(reads.map((pending: Promise<unknown>) => assert.rejects(pending, { name: "AbortError" })))
    assert.equal(client.subscriptionHolds.size, 0)
    const ninth = new AbortController()
    const pending = client._read({ machine: MACHINE, session: "ninth" }, "info", {}, "info.full", 10_000,
      { signal: ninth.signal })
    await settled(client)
    assert.equal(client.subscriptionHolds.size, 1)
    ninth.abort()
    await assert.rejects(pending, { name: "AbortError" })
  } finally {
    client.stop()
  }
})

test("aborting one shared machine-channel read leaves the other holder intact", async () => {
  const client = await connected(new Relay())
  client._publishCommand = () => new Promise(() => {})
  try {
    const first = new AbortController()
    const second = new AbortController()
    const a = client._machineRequest(MACHINE, "work.v2.session-todos", {}, "read", 10_000, { signal: first.signal })
    const b = client._machineRequest(MACHINE, "work.v2.human-interventions", {}, "read", 10_000, { signal: second.signal })
    await settled(client)
    assert.equal(client.subscriptionHolds.get(REPLY), 2)
    first.abort()
    await assert.rejects(a, { name: "AbortError" })
    assert.equal(client.subscriptionHolds.get(REPLY), 1)
    second.abort()
    await assert.rejects(b, { name: "AbortError" })
    assert.equal(client.subscriptionHolds.has(REPLY), false)
  } finally {
    client.stop()
  }
})

test("one coalesced caller can abort while the remaining caller receives the answer", async () => {
  const client = await connected(new Relay())
  let answer: (() => void) | undefined
  client._publishCommand = () => new Promise<void>((resolve) => { answer = resolve })
  try {
    const first = new AbortController()
    const second = new AbortController()
    const identity = { machine: MACHINE, session: "coalesced" }
    const a = client._read(identity, "info", {}, "info.full", 10_000, { signal: first.signal })
    const b = client._read(identity, "info", {}, "info.full", 10_000, { signal: second.signal })
    await settled(client)
    assert.equal(client.subscriptionHolds.size, 1)
    first.abort()
    await assert.rejects(a, { name: "AbortError" })
    assert.equal(client.subscriptionHolds.size, 1)
    client._settleRead([...client.readWaiters.keys()][0], { answered: true }, null)
    assert.deepEqual(await b, { answered: true })
    assert.equal(client.subscriptionHolds.size, 0)
    answer?.()
  } finally {
    client.stop()
  }
})

test("an aborted HTTP fetch releases its machine reply channel before a retry", async () => {
  const client = await connected(new Relay())
  client._publishCommand = () => new Promise(() => {})
  const reader = new RelayReader(MACHINE)
  reader.attach(client)
  try {
    const controller = new AbortController()
    const pending = reader.fetch("/v1/work/v2/session-todos/%251", { signal: controller.signal })
    await settled(client)
    assert.equal(client.subscriptionHolds.get(REPLY), 1)
    controller.abort()
    await assert.rejects(pending, { name: "AbortError" })
    assert.equal(client.subscriptionHolds.has(REPLY), false)
    const retry = new AbortController()
    const next = reader.fetch("/v1/work/v2/session-todos/%251", { signal: retry.signal })
    await settled(client)
    assert.equal(client.subscriptionHolds.get(REPLY), 1)
    retry.abort()
    await assert.rejects(next, { name: "AbortError" })
    assert.equal(client.subscriptionHolds.has(REPLY), false)
  } finally {
    client.stop()
  }
})

test("a channel the relay reports it no longer holds is subscribed again, and the read on it is answered", async () => {
  const relay = new Relay()
  const client = await connected(relay)
  try {
    assert.deepEqual(await read(client), { answered: "projects" })
    // The race: opening a Session's channel comes back without the reply channel.
    relay.lose = (frame) => frame.type === "subscribe" && frame.channels!.includes(SESSION) ? [REPLY] : []
    client.subscribe([SESSION])
    await settled(client)
    relay.lose = () => []
    assert.deepEqual(await read(client), { answered: "projects" })
    assert.equal(relay.subscribesFor(REPLY), 2)
    assert.deepEqual([...relay.holds].sort(), [SESSION, REPLY].sort())
  } finally {
    client.stop()
  }
})

test("a subscribe the relay refuses is not recorded as held: its read fails with the relay's word and the next read subscribes again", async () => {
  const relay = new Relay()
  const client = await connected(relay)
  try {
    relay.refuse = (frame) => frame.type === "subscribe" ? "over_capacity" : null
    const started = Date.now()
    await assert.rejects(read(client), (error: any) => error.code === "over_capacity" && error.layer === "relay")
    assert.ok(Date.now() - started < READ_TIMEOUT_MS, "failed at the relay's refusal, not at the read timeout")
    assert.equal(client.socketSubscriptions.has(REPLY), false)
    relay.refuse = () => null
    assert.deepEqual(await read(client), { answered: "projects" })
    assert.equal(relay.subscribesFor(REPLY), 2)
  } finally {
    client.stop()
  }
})

test("a relay that keeps dropping a channel is asked again a bounded number of times, not in a loop", async () => {
  const relay = new Relay()
  const client = await connected(relay)
  try {
    relay.lose = () => [REPLY]
    await assert.rejects(read(client), (error: any) => error.code === "cloud_read_timeout")
    await settled(client)
    const asked = relay.subscribesFor(REPLY)
    assert.ok(asked >= 2, "the lost channel was asked for again at least once (" + asked + ")")
    assert.ok(asked <= 4, "the lost channel was not asked for in a loop (" + asked + ")")
    // Nothing more goes out while nothing new is asked.
    await settled(client)
    assert.equal(relay.subscribesFor(REPLY), asked)
  } finally {
    client.stop()
  }
})

test("an answer to an earlier subscribe that predates a later one does not make the later channel lost", async () => {
  const relay = new Relay()
  const client = await connected(relay)
  try {
    // Two subscribes leave before either is answered; the first answer cannot name the second.
    client.subscribe([SESSION])
    const reading = read(client)
    assert.deepEqual(await reading, { answered: "projects" })
    await settled(client)
    assert.equal(relay.subscribesFor(REPLY), 1)
    assert.equal(relay.subscribesFor(SESSION), 1)
    assert.deepEqual([...client.socketSubscriptions.keys()].sort(), [SESSION, REPLY].sort())
  } finally {
    client.stop()
  }
})
