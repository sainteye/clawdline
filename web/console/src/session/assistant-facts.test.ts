import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- Node's strip-types runner needs the source extension.
import { AssistantFactsRequest, type AssistantFactsReading } from "./assistant-facts.ts"

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
