// The personas a start may name, read over Cloud end to end: `RelayReader` in
// front of the copied client itself, and a relay that answers the way the
// machine does. `node --test web/console/src/cloud/*.test.ts`.
//
// The start sheet asks `GET /v1/personas` and draws no choice when that fails.
// What must not happen is the read leaving for a machine from before personas
// and the sheet waiting out the relay's read timeout: such a machine's
// descriptor does not list the word, and the copied client refuses a word the
// descriptor does not list before anything is sealed (`_unsupportedRefusal`).
// `relay-reader.test.ts` cannot see that: its client is a fake that records
// whatever it is handed.
//
// The machine here is `internal/app/cloudops/ops.go`'s `personas`: the exact
// key set, answered on the machine reply channel under `read:<request>`.
// Sealing and opening stand aside, as in `subscriptions.test.ts`.
import { test } from "node:test"
import assert from "node:assert/strict"
import { CloudClient } from "../legacy/js/net/cloud-client.js"
import { parseEnvelopeChannel } from "../legacy/js/net/cloud-crypto.js"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { RelayReader, type CloudReadClient } from "./relay-reader.ts"

const MACHINE = "mac-a"
const REPLY = "t/" + MACHINE + "/__clawdline_machine__"
const READ_TIMEOUT_MS = 300
const CATALOG = { personas: [{ id: "architect" }], license: "MIT" }

type Frame = { type: string; channels?: string[]; envelope?: { ch: string; body: Record<string, unknown> } }

class Machine {
  socket: FakeSocket | null = null
  /** Every publish, and whether `ops.go` would have decoded it. */
  asked: { body: Record<string, unknown>; decoded: boolean }[] = []
  private seq = 1

  receive(frame: Frame) {
    if (frame.type === "subscribe" || frame.type === "unsubscribe") {
      return this.say({ type: "subscriptions", channels: frame.type === "subscribe" ? frame.channels : [] })
    }
    if (frame.type !== "publish") return
    const body = frame.envelope!.body
    const keys = Object.keys(body).sort()
    const decoded = body.type === "personas" && keys.join(",") === "request,session,type" &&
      body.session === "__clawdline_machine__" && typeof body.request === "string" && !!body.request
    this.asked.push({ body, decoded })
    this.say({ type: "ack", ch: frame.envelope!.ch, seq: this.seq, fanout: 1, status: "delivered" })
    if (decoded) {
      this.say({ type: "envelope", envelope: {
        ch: REPLY, seq: this.seq, ts: Date.now(), sender: "mac-sender",
        payload: { read: "read:" + body.request, body: CATALOG },
      } })
    }
    this.seq += 1
  }

  /** The machine's descriptor, as its `orch/` snapshot carries it. */
  describe(commands: string[]) {
    this.say({ type: "envelope", envelope: {
      ch: "orch/" + MACHINE, seq: this.seq++, ts: Date.now(), sender: "mac-sender",
      payload: { machine: { platform: "linux", commands } },
    } })
  }

  say(frame: unknown) {
    const socket = this.socket!
    setImmediate(() => socket.onmessage?.({ data: JSON.stringify(frame) }))
  }
}

class FakeSocket {
  static machine: Machine
  readyState = 1
  onmessage: ((event: { data: string }) => void) | null = null
  onclose: ((event: { code: number }) => void) | null = null
  onerror: (() => void) | null = null
  constructor() {
    FakeSocket.machine.socket = this
  }
  send(data: string) { FakeSocket.machine.receive(JSON.parse(data)) }
  close() { this.readyState = 3 }
}

async function connected(machine: Machine, commands: string[]) {
  FakeSocket.machine = machine
  let sequence = 1
  const client = new CloudClient({
    relayURL: "https://relay.example.test", deviceToken: "token", devicePrivateKey: { extractable: false },
    deviceID: "viewer-1", account: "account-1", allowWrites: true, nextSequence: async () => sequence++,
    WebSocket: FakeSocket, readTimeoutMs: READ_TIMEOUT_MS,
  }) as any
  client._publishCommand = async function (this: any, target: string, type: string, body: Record<string, unknown>) {
    this._send({ type: "publish", envelope: { ch: "ctl/" + target, body: { type, ...body } } })
  }
  client._receiveEnvelope = async function (this: any, envelope: any, realign: boolean) {
    this._applySnapshot(parseEnvelopeChannel(envelope.ch), envelope.payload, envelope, realign)
  }
  await client.start({ quiet: true })
  machine.say({ type: "ready", v: 1, role: "viewer", account: "account-1", device: "viewer-1" })
  await client.whenReady()
  machine.describe(commands)
  for (let i = 0; i < 20 && !client.machineDescriptor(MACHINE); i += 1) {
    await new Promise((resolve) => setImmediate(resolve))
    await client.messageChain
  }
  assert.deepEqual(client.machineDescriptor(MACHINE)?.machine?.commands, commands, "the descriptor arrived")
  return client
}

function reader(client: CloudReadClient) {
  const r = new RelayReader(MACHINE, { now: () => 1000 })
  r.attach(client)
  return r
}

test("the personas are a machine read the machine decodes, answered with its catalog", async () => {
  const machine = new Machine()
  const client = await connected(machine, ["places", "start", "personas"])
  try {
    const answer = await reader(client).fetch("/v1/personas")
    assert.equal(answer.status, 200)
    assert.deepEqual(await answer.json(), CATALOG)
    assert.equal(machine.asked.length, 1)
    assert.equal(machine.asked[0].decoded, true, JSON.stringify(machine.asked[0].body))
  } finally {
    client.stop()
  }
})

test("a machine whose descriptor does not list `personas` is sent nothing, and the page hears so at once", async () => {
  const machine = new Machine()
  const client = await connected(machine, ["places", "start"])
  try {
    const started = Date.now()
    const answer = await reader(client).fetch("/v1/personas")
    assert.ok(Date.now() - started < READ_TIMEOUT_MS, "refused before the read timeout, not by it")
    // The copied client's refusal carries no status, so the seam answers its
    // generic 502 with the client's own code, which is what a page reads.
    assert.equal(answer.ok, false)
    const body = await answer.json() as { error: string }
    assert.equal(body.error, "cloud_machine_unsupported")
    assert.deepEqual(machine.asked, [], "nothing was published to the machine")
  } finally {
    client.stop()
  }
})
