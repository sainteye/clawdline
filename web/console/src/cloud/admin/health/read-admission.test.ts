import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- node loads TypeScript directly.
import { runReads, healthGet, HealthReadBusy, retryAfterMs } from "./cloud.ts"
// @ts-expect-error -- node loads TypeScript directly.
import { readAttention, attentionWatch } from "./attention.ts"
// @ts-expect-error -- node loads TypeScript directly.
import { fixtureAnswer } from "./fixture.ts"

test("page reads and attention share admission even against the previous two-slot API", async () => {
  const origin = "https://read-admission.example.invalid"
  let active = 0, peak = 0, refused = 0
  let release!: () => void, started!: () => void
  const hold = new Promise<void>(resolve => { release = resolve })
  const twoStarted = new Promise<void>(resolve => { started = resolve })
  const calls: string[] = []
  const get = (async (input: string) => {
    const path = new URL(input).pathname
    calls.push(path)
    if (active === 2) { refused++; return new Response("{}", { status: 429 }) }
    active++; peak = Math.max(peak, active)
    if (active === 2) started()
    try {
      await hold
      if (path.endsWith("/attention")) return Response.json({ schema: "clawdline.health_attention.v1", status: "ok", items: [], complete: true })
      return fixtureAnswer("normal", input, Date.parse("2026-10-20T12:00:00Z"))
    } finally { active-- }
  }) as unknown as typeof fetch
  const page = runReads(get, origin, [
    { part: "overview", path: "overview", params: { from: "2026-10-20T11:00:00.000Z", to: "2026-10-20T12:00:00.000Z" } },
    { part: "outcomes", path: "outcomes", params: { from: "2026-10-20T11:00:00.000Z", to: "2026-10-20T12:00:00.000Z" } },
  ], new AbortController().signal)
  await twoStarted
  const attention = readAttention(origin, get)
  await Promise.resolve()
  const beforeRelease = calls.length
  release()
  const [p, a] = await Promise.all([page, attention])
  assert.equal(beforeRelease, 2, "attention must wait instead of competing for a third slot")
  assert.equal(refused, 0)
  assert.equal(peak, 2)
  assert.equal(p.kind, "parts")
  if (p.kind === "parts") assert.ok(Object.values(p.parts).every(part => part?.ok))
  assert.equal(a.state, "ok")
})

const flush = () => new Promise<void>(resolve => setImmediate(resolve))

// Exercise observable fetch admission, not the scheduler's internal map.
test("queued cancellation is removed, remaining reads are FIFO, and overflow is typed", async () => {
  const origin = "https://fifo.example.invalid"
  const calls: string[] = []
  let release!: () => void
  const hold = new Promise<void>(resolve => { release = resolve })
  const get = (async (url: string) => { calls.push(new URL(url).pathname); await hold; return new Response("{}"); }) as unknown as typeof fetch
  const controllers = Array.from({ length: 10 }, () => new AbortController())
  const reads = controllers.map((ctl, i) => healthGet(get, `${origin}/${i}`, { signal: ctl.signal }).then(() => "ok", error => error))
  await flush()
  assert.deepEqual(calls, ["/0", "/1"])
  await assert.rejects(healthGet(get, `${origin}/overflow`, {}), HealthReadBusy)
  controllers[3].abort()
  assert.equal((await reads[3] as Error).name, "AbortError")
  release()
  await Promise.all(reads)
  await flush()
  assert.deepEqual(calls, ["/0", "/1", "/2", "/4", "/5", "/6", "/7", "/8", "/9"])
  const aborted = new AbortController(); aborted.abort()
  await assert.rejects(healthGet(get, `${origin}/already-aborted`, { signal: aborted.signal }), { name: "AbortError" })
  assert.equal(calls.length, 9)
})

test("timing out a load does not release uncooperative active fetches or send cancelled queued reads", async () => {
  const origin = "https://timeout.example.invalid"
  const calls: string[] = []
  let release!: () => void
  const hold = new Promise<void>(resolve => { release = resolve })
  const get = (async (url: string) => { calls.push(url); await hold; return fixtureAnswer("normal", url, Date.parse("2026-10-20T12:00:00Z")); }) as unknown as typeof fetch
  const reads = [{ part: "overview" as const, path: "overview", params: { from: "2026-10-20T11:00:00.000Z", to: "2026-10-20T12:00:00.000Z" } }, { part: "outcomes" as const, path: "outcomes", params: { from: "2026-10-20T11:00:00.000Z", to: "2026-10-20T12:00:00.000Z" } }]
  const timed = await runReads(get, origin, reads, new AbortController().signal, { requestMs: 10 })
  assert.equal(timed.kind, "parts")
  if (timed.kind === "parts") assert.ok(Object.values(timed.parts).every(p => p && !p.ok && p.code === "timeout"))
  const queued = await runReads(get, origin, reads, new AbortController().signal, { requestMs: 10 })
  assert.equal(queued.kind, "parts")
  assert.equal(calls.length, 2, "timed out underlying work still owns both slots")
  release(); await flush()
  const recovered = await runReads(get, origin, reads, new AbortController().signal)
  assert.equal(calls.length, 4, "only the fresh load runs after recovery")
  assert.equal(recovered.kind, "parts")
  if (recovered.kind === "parts") assert.ok(Object.values(recovered.parts).every(p => p?.ok))
})

test("origins do not share admission and synchronous failures release their slot", async () => {
  let release!: () => void
  const hold = new Promise<void>(resolve => { release = resolve })
  const blocked = (async () => { await hold; return new Response("{}"); }) as typeof fetch
  const a = healthGet(blocked, "https://blocked.example.invalid/a", {})
  const b = healthGet(blocked, "https://blocked.example.invalid/b", {})
  await assert.rejects(healthGet((() => { throw new Error("offline") }) as typeof fetch, "https://other.example.invalid/fail", {}), /offline/)
  const response = await healthGet((async () => new Response("ok")) as typeof fetch, "https://other.example.invalid/ok", {})
  assert.equal(await response.text(), "ok")
  release(); await Promise.all([a, b]); await flush()
})

test("429 stays typed and Retry-After is bounded without automatic retries", async () => {
  let calls = 0
  const get = (async () => { calls++; return new Response("{}", { status: 429, headers: { "retry-after": "30" } }); }) as typeof fetch
  const a = await readAttention("https://busy.example.invalid", get)
  assert.deepEqual(a, { state: "failed", code: "rate_limited", retryAfterMs: 30_000 })
  const page = await runReads(get, "https://busy.example.invalid", [{ part: "overview", path: "overview", params: { from: "2026-10-20T11:00:00.000Z", to: "2026-10-20T12:00:00.000Z" } }], new AbortController().signal)
  assert.deepEqual(page, { kind: "parts", parts: { overview: { ok: false, code: "rate_limited", retryAfterMs: 30_000 } } })
  assert.equal(calls, 2)
  const hint = (value: string) => retryAfterMs(new Response(null, { headers: { "retry-after": value } }), Date.parse("2026-11-09T00:00:00Z"))
  assert.equal(hint("Wed, 09 Nov 2026 00:00:20 GMT"), 20_000)
  assert.equal(hint("99999"), 60_000)
  assert.equal(hint("0"), 0)
  assert.equal(hint("invalid"), undefined)
})

test("attention refresh observes Retry-After even when the regular cadence is shorter", async t => {
  t.mock.timers.enable({ apis: ["setTimeout", "Date"], now: 1_000_000 })
  let calls = 0
  const get = (async () => { calls++; return new Response("{}", { status: 429, headers: { "retry-after": "30" } }); }) as typeof fetch
  const watch = attentionWatch("https://refresh.example.invalid", get, null, 15_000)
  const stop = watch.subscribe(() => {})
  try {
    t.mock.timers.tick(0); await flush()
    assert.equal(calls, 1)
    t.mock.timers.tick(29_999); await flush()
    assert.equal(calls, 1)
    t.mock.timers.tick(1); await flush()
    assert.equal(calls, 2)
  } finally { stop(); t.mock.timers.reset() }
})
