// A subagent's conversation and a background command's output, read over
// Cloud end to end: `RelayReader` in front of the copied client itself, and a
// relay that answers the way the machine does. `node --test web/console/src/cloud/*.test.ts`.
//
// What the person saw on 2026-09-27: opening a Claude subagent on
// app.clawdline.com drew the transcript skeleton and nothing else. The machine
// logged `answered nobody: session=unknown status=400 code=malformed_read` for
// each try. `relay-reader.ts` sent these two words through `_machineRequest`,
// which adds a `request` and waits on the machine's reply channel; the machine
// decodes `agent` and `shell` against an exact key set and answers on the
// session's own channel, so the body was malformed and the answer, had there
// been one, would have gone where nobody waited. `relay-reader.test.ts` could
// not see it: its client is a fake that records whatever it is handed.
//
// So the machine here is a copy of the machine's rule and nothing kinder:
// `internal/app/cloudops/ops.go` (`agent`, `shell`) — the exact key set, the
// answer on `t/<machine>/<session>` named `<word>:<id>`, and for a body that does
// not decode, no answer at all (`cloudops.go`, `malformed_read` with no plan).
// Sealing and opening stand aside, as in `subscriptions.test.ts`.
import { test } from "node:test"
import assert from "node:assert/strict"
import { CatalogCloudClient } from "./refusal-client.js"
import { parseEnvelopeChannel } from "../legacy/js/net/cloud-crypto.js"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { RelayReader, type CloudReadClient } from "./relay-reader.ts"
// @ts-expect-error -- Node runs this TypeScript source test directly.
import { RelayWriter, writeRoute } from "./relay-writer.ts"

const MACHINE = "mac-a"
const READ_TIMEOUT_MS = 300

/** The machine's decoders for the two words, as `ops.go` spells them. */
const WORDS: Record<string, { keys: string[]; id: string }> = {
  agent: { keys: ["type", "session", "agent", "limit"], id: "agent" },
  shell: { keys: ["type", "session", "shell", "bytes"], id: "shell" },
  "work.decision-answer": { keys: ["type", "session", "request", "id", "answer"], id: "id" },
}

type Frame = { type: string; channels?: string[]; envelope?: { ch: string; body: Record<string, unknown> } }

class Machine {
  socket: FakeSocket | null = null
  refusal: { code: string; message: string; detail_key?: string } | null = null
  /** What each publish carried, and whether it decoded. */
  asked: { body: Record<string, unknown>; decoded: boolean }[] = []
  private seq = 1

  receive(frame: Frame) {
    if (frame.type === "subscribe" || frame.type === "unsubscribe") {
      return this.say({ type: "subscriptions", channels: frame.type === "subscribe" ? frame.channels : [] })
    }
    if (frame.type !== "publish") return
    const body = frame.envelope!.body
    const word = WORDS[String(body.type)]
    const keys = Object.keys(body).sort()
    const action = body.type === "work.decision-answer"
    const decoded = !!word && keys.length === word.keys.length && word.keys.every((key) => key in body) &&
      (action || (typeof body.session === "string" && !!body.session)) && typeof body[word.id] === "string" && !!body[word.id]
    this.asked.push({ body, decoded })
    this.say({ type: "ack", ch: frame.envelope!.ch, seq: this.seq, fanout: 1, status: "delivered" })
    if (decoded) {
      const id = String(body[word.id])
      this.say({ type: "envelope", envelope: {
        ch: "t/" + MACHINE + "/" + (action ? "__clawdline_machine__" : encodeURIComponent(String(body.session))), seq: this.seq, ts: Date.now(),
        sender: "mac-sender",
        payload: this.refusal
          ? { read: action ? "action:" + body.request : body.type + ":" + id, error: this.refusal, status: 409 }
          : { read: action ? "action:" + body.request : body.type + ":" + id, body: { [word.id]: id, entries: [{ role: "assistant", text: "found it" }], signature: "1" } },
      } })
    }
    this.seq += 1
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

async function connected(machine: Machine) {
  FakeSocket.machine = machine
  let sequence = 1
  const client = new CatalogCloudClient({
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
  return client
}

function reader(client: CloudReadClient) {
  const r = new RelayReader(MACHINE, { now: () => 1000 })
  r.attach(client)
  return r
}

test("a subagent's conversation opened over Cloud is a read the machine decodes, and its entries arrive", async () => {
  const machine = new Machine()
  const client = await connected(machine)
  try {
    const answer = await reader(client).fetch("/v1/sessions/S1/agents/a7?limit=200")
    assert.equal(answer.status, 200)
    const page = await answer.json() as { agent: string; entries: { text: string }[] }
    assert.equal(page.agent, "a7")
    assert.equal(page.entries[0].text, "found it")
    assert.deepEqual(machine.asked, [{ body: { type: "agent", session: "S1", agent: "a7", limit: 200 }, decoded: true }])
  } finally {
    client.stop()
  }
})

test("a background command's output opened over Cloud is a read the machine decodes", async () => {
  const machine = new Machine()
  const client = await connected(machine)
  try {
    const answer = await reader(client).fetch("/v1/sessions/S1/shells/b0aau3e6s?bytes=65536")
    assert.equal(answer.status, 200)
    assert.deepEqual(machine.asked, [{ body: { type: "shell", session: "S1", shell: "b0aau3e6s", bytes: 65536 }, decoded: true }])
  } finally {
    client.stop()
  }
})

test("a machine refusal keeps its explicit key through the copied client and Cloud read seam", async () => {
  const machine = new Machine()
  const message = "This agent is no longer available."
  machine.refusal = { code: "agent_not_found", message, detail_key: "http.1234567890abcdef" }
  const client = await connected(machine)
  try {
    const response = await reader(client).fetch("/v1/sessions/S1/agents/a7?limit=200")
    assert.equal(response.status, 409, JSON.stringify(machine.asked))
    const body = await response.json() as { error: string; detail: string; detail_key?: string }
    assert.deepEqual([body.error, body.detail, body.detail_key], ["agent_not_found", message, "http.1234567890abcdef"])
    machine.refusal = { code: "agent_not_found", message }
    const raw = await reader(client).fetch("/v1/sessions/S1/agents/a7?limit=200")
    const rawBody = await raw.json() as { detail: string; detail_key?: string }
    assert.equal(rawBody.detail, message)
    assert.equal(rawBody.detail_key, undefined, "the same raw English sentence cannot borrow the earlier key")
  } finally {
    client.stop()
  }
})

test("a machine refusal keeps its explicit key through the copied client and nested Cloud write seam", async () => {
  const machine = new Machine()
  const message = "This decision was already answered."
  machine.refusal = { code: "decision_already_answered", message, detail_key: "http.1234567890abcdef" }
  const client = await connected(machine)
  try {
    const relay = new RelayReader(MACHINE, { now: () => 1000 })
    const writer = new RelayWriter(relay.writeHost, { now: () => 1000, requestID: () => "request-1" })
    relay.carryWrites({ route: writeRoute, answer: (route, method, url, init) => writer.answer(route, method, url, init) })
    relay.attach(client)
    const response = await relay.fetch("/v1/work/decisions/10000000-0000-4000-8000-000000000001", {
      method: "POST", headers: { "Content-Type": "application/json", "Idempotency-Key": "request-1" },
      body: JSON.stringify({ answer: "Proceed" }),
    })
    assert.equal(response.status, 409, JSON.stringify(machine.asked))
    const body = await response.json() as { error: { code: string; message: string; detail_key?: string } }
    assert.deepEqual([body.error.code, body.error.message, body.error.detail_key], ["decision_already_answered", message, "http.1234567890abcdef"])
  } finally {
    client.stop()
  }
})
