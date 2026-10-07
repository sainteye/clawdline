import { test } from "node:test"
import assert from "node:assert/strict"
import { authenticatedRefusalKey, CatalogCloudClient, withCatalogRefusals } from "./refusal-client.js"

class FakeClient {
  constructor() { this.settled = []; this.events = [] }
  _applySnapshot(channel, payload, envelope, realign) {
    const error = { code: payload.error?.code, message: payload.error?.message, detail: { reason: "preserved" } }
    if (payload.before) payload.before()
    this._settleRead(`machine\u0000session\u0000${payload.read}`, null, error)
    this._emit({ type: "read", read: payload.read, error, envelope, realign })
    return error
  }
  _settleRead(key, body, error) { this.settled.push({ key, body, error }) }
  _emit(event) { this.events.push(event) }
}

const Client = withCatalogRefusals(FakeClient)
const channel = { kind: "transcript" }
const refusal = (read, detail_key = "http.1234567890abcdef") => ({
  read, error: { code: "forbidden", message: "This Project is read only.", detail_key },
})

test("an authenticated transcript's fixed key reaches its waiter and event", () => {
  const client = new Client()
  const envelope = { seq: 4 }
  const error = client._applySnapshot(channel, refusal("read:request-1"), envelope, false)
  assert.equal(error.detailKey, "http.1234567890abcdef")
  assert.equal(authenticatedRefusalKey(error), "http.1234567890abcdef")
  assert.equal(error.wireDetail, "This Project is read only.")
  assert.deepEqual(error.detail, { reason: "preserved" })
  assert.equal(client.settled[0].error, error)
  assert.equal(client.events[0].error, error)
  assert.equal(client._catalogRefusal, undefined)
})

test("an invalid or absent key preserves raw words without claiming a translation", () => {
  const client = new Client()
  const invalid = client._applySnapshot(channel, refusal("read:bad", "http.untrusted"), { seq: 5 }, false)
  const absent = client._applySnapshot(channel, refusal("read:absent", null), { seq: 6 }, true)
  const nonstring = client._applySnapshot(channel, refusal("read:nonstring", Symbol("bad")), { seq: 7 }, false)
  assert.equal(invalid.detailKey, undefined)
  assert.equal(absent.detailKey, undefined)
  assert.equal(nonstring.detailKey, undefined)
  assert.equal(authenticatedRefusalKey(invalid), null)
  assert.equal(authenticatedRefusalKey(absent), null)
  assert.equal(invalid.wireDetail, "This Project is read only.")
  assert.equal(absent.wireDetail, "This Project is read only.")
  assert.equal(client.events[1].realign, true)
})

test("a nested synchronous read cannot borrow its parent's key", () => {
  const client = new Client()
  const parent = refusal("read:parent", "http.aaaaaaaaaaaaaaaa")
  parent.before = () => {
    const child = client._applySnapshot(channel, refusal("read:child", "http.bbbbbbbbbbbbbbbb"), { seq: 8 }, false)
    assert.equal(child.detailKey, "http.bbbbbbbbbbbbbbbb")
  }
  const outer = client._applySnapshot(channel, parent, { seq: 7 }, false)
  assert.equal(outer.detailKey, "http.aaaaaaaaaaaaaaaa")
  assert.equal(client._catalogRefusal, undefined)
  const unrelated = { code: "forbidden", message: "This Project is read only." }
  client._settleRead("machine\u0000session\u0000read:parent", null, unrelated)
  assert.equal(unrelated.detailKey, undefined)
  assert.equal(unrelated.wireDetail, undefined)
  assert.equal(authenticatedRefusalKey(unrelated), null)
})

test("other channels and unrelated events cannot attach the transcript key", () => {
  const client = new Client()
  const other = client._applySnapshot({ kind: "orchestrator" }, refusal("read:other"), { seq: 9 }, false)
  assert.equal(other.detailKey, undefined)
  const envelope = { seq: 10 }
  const payload = refusal("read:matched")
  payload.before = () => {
    const wrong = { code: "forbidden", message: "This Project is read only." }
    client._emit({ type: "read", read: "read:elsewhere", error: wrong, envelope })
    assert.equal(wrong.detailKey, undefined)
  }
  client._applySnapshot(channel, payload, envelope, false)
})

test("a rejected snapshot clears its temporary key", () => {
  class RejectingClient {
    _applySnapshot() { throw new Error("invalid transcript snapshot") }
  }
  const Client = withCatalogRefusals(RejectingClient)
  const client = new Client()
  assert.throws(() => client._applySnapshot(channel, refusal("read:bad"), { seq: 11 }, false), /invalid transcript snapshot/u)
  assert.equal(client._catalogRefusal, undefined)
})

test("the copied CloudClient carries a producer key through its real transcript parser", () => {
  const client = new CatalogCloudClient({ relayURL: "wss://relay.example.test/v1/connect", deviceToken: "fixture" })
  const events = []
  client.events(event => events.push(event))
  const envelope = { seq: 9, ts: "2026-10-07T00:00:00Z" }
  client._applySnapshot({ kind: "transcript", machine: "mac-a", session: "s1" }, {
    read: "read:document-1",
    error: { code: "not_found", status: 404, layer: "mac_route", message: "No document named that.", detail_key: "http.1e31c72f0db9eb5b" },
  }, envelope, false)
  const answer = events.find(event => event.type === "read")
  assert.ok(answer)
  assert.equal(answer.error.code, "not_found")
  assert.equal(answer.error.message, "No document named that.")
  assert.equal(answer.error.detailKey, "http.1e31c72f0db9eb5b")
  assert.equal(authenticatedRefusalKey(answer.error), "http.1e31c72f0db9eb5b")
  assert.equal(client._catalogRefusal, undefined)
})
