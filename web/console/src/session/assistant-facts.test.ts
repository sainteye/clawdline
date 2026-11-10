import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- Node's strip-types runner needs the source extension.
import { AssistantFactsRequest, knowQueue, type AssistantFactsReading } from "./assistant-facts.ts"

const at = 2000
const ready = { at, assistants: [{ id: "antigravity", installed: true, integrationState: "active", authState: "ready",
  capabilities: { start: true, send: true, menus: true, resume: true, transcript: true } }] }
const response = (body: unknown) => new Response(JSON.stringify(body), { headers: { "Content-Type": "application/json" } })

test("selected-machine inventory keeps false, absent fields, freshness and old-server states", async () => {
  const owner = new AssistantFactsRequest()
  const readings: AssistantFactsReading[] = []
  const calls: string[] = []
  const read = async (body: unknown) => owner.read("machine fixture", async (url, init) => {
    calls.push(String(url)); assert.equal(init?.cache, "no-store"); return response(body)
  }, value => readings.push(value), () => at * 1000)
  await read(ready)
  assert.equal(readings.at(-1)?.kind, "ready")
  assert.equal(readings.at(-1)?.assistants[0].capabilities?.send, true)
  await read({ ...ready, assistants: [{ id: "antigravity", installed: false }] })
  assert.equal(readings.at(-1)?.assistants[0].installed, false)
  assert.equal(readings.at(-1)?.assistants[0].authState, undefined)
  await read({ ...ready, at: 0 }); assert.equal(readings.at(-1)?.kind, "stale")
  await read({ assistants: ready.assistants }); assert.equal(readings.at(-1)?.kind, "stale")
  await read({ at }); assert.equal(readings.at(-1)?.kind, "unknown")
  assert.ok(calls.every(path => path === "/v1/places?machine=machine%20fixture"))
})

test("switch, execution replacement and cancellation discard delayed ready results even when transport ignores abort", async () => {
  const owner = new AssistantFactsRequest()
  const readings: AssistantFactsReading[] = []
  let finish!: (response: Response) => void
  let signal: AbortSignal | null | undefined
  const old = owner.read("first", async (_, init) => { signal = init?.signal; return new Promise(ok => finish = ok) },
    value => readings.push(value), () => at * 1000)
  await owner.read("second", async () => response({ at, assistants: [{ id: "antigravity", installed: false }] }),
    value => readings.push(value), () => at * 1000)
  assert.equal(signal?.aborted, true)
  finish(response(ready)); await old
  assert.equal(readings.at(-1)?.assistants[0].installed, false)
  const pending = owner.read("second", async () => new Promise(ok => finish = ok), value => readings.push(value), () => at * 1000)
  owner.cancel(); const count = readings.length; finish(response(ready)); await pending
  assert.equal(readings.length, count)
})

test("disconnect and a refused machine read produce error without old capabilities", async () => {
  for (const request of [async () => { throw new Error("offline") }, async () => new Response("{}", { status: 503 })]) {
    const readings: AssistantFactsReading[] = []
    await new AssistantFactsRequest().read("fixture", request, value => readings.push(value))
    assert.equal(readings.at(-1)?.kind, "error"); assert.deepEqual(readings.at(-1)?.assistants, [])
  }
})


test("an unanswered transport that ignores cancellation reaches an error instead of an endless loading state", async t => {
  t.mock.timers.enable({ apis: ["setTimeout"] })
  const readings: AssistantFactsReading[] = []
  const pending = new AssistantFactsRequest().read("fixture", async () => new Promise(() => {}), value => readings.push(value))
  t.mock.timers.tick(15_000)
  await pending
  assert.equal(readings.at(-1)?.kind, "error")
})

test("a machine's queue capability outlives its reading's freshness but not its machine or execution", () => {
  const queues = (on: boolean): AssistantFactsReading => ({ kind: "ready", assistants: [{ id: "grok", capabilities: { queue: on } } as never] })
  let known = knowQueue(null, "alpha:a", { kind: "loading", assistants: [] }, "grok")
  assert.equal(known.queues, false, "nothing read yet keeps the browser queue")
  known = knowQueue(known, "alpha:a", queues(true), "grok")
  assert.equal(known.queues, true)
  known = knowQueue(known, "alpha:a", { kind: "stale", assistants: [] }, "grok")
  assert.equal(known.queues, true, "an expired reading does not send the next message back into the browser")
  known = knowQueue(known, "alpha:a", { kind: "loading", assistants: [] }, "grok")
  assert.equal(known.queues, true, "a re-read in flight keeps what was known")
  assert.equal(knowQueue(known, "beta:a", { kind: "loading", assistants: [] }, "grok").queues, false, "another machine starts unknown")
  assert.equal(knowQueue(known, "alpha:a", queues(false), "grok").queues, false, "a ready reading without the capability withdraws it")
  assert.equal(knowQueue(null, "alpha:a", queues(true), "antigravity").queues, false, "another assistant's facts do not count")
})
