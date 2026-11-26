import assert from "node:assert/strict"
import { createRequire } from "node:module"
import { test } from "node:test"
import { fileURLToPath } from "node:url"
import { build } from "esbuild"

const require = createRequire(import.meta.url)
const { outputFiles } = await build({
  stdin: {
    contents: 'export { ClawdlineClient } from "./client.ts"; export { FleetStore } from "./fleet.ts";',
    resolveDir: fileURLToPath(new URL(".", import.meta.url)),
  },
  bundle: true, platform: "node", format: "cjs", packages: "external", write: false,
})
const compiled = { exports: {} }
new Function("require", "module", "exports", outputFiles[0].text)(require, compiled, compiled.exports)
const { ClawdlineClient, FleetStore } = compiled.exports
const snapshot = (epoch, generation) => ({ at: 100, scan: { epoch, generation, complete: true }, sessions: [] })

test("the request deadline aborts a stalled Session body and a retry reads the Session list", async t => {
  t.mock.timers.enable({ apis: ["setTimeout"] })
  const timeoutMs = 250
  const bodyReading = Promise.withResolvers()
  let attempts = 0
  let firstSignal
  const client = new ClawdlineClient({
    timeoutMs,
    fetch: async (url, { headers, signal }) => {
      assert.equal(url, "/v1/sessions")
      if (++attempts > 1) return Response.json(snapshot(2, 1))
      assert.equal(new Headers(headers).get("X-Clawdline-Initial-Read"), "1")
      firstSignal = signal
      return new Response(new ReadableStream({
        start(controller) {
          controller.enqueue(new TextEncoder().encode('{"sessions":'))
          signal.addEventListener("abort", () => controller.error(signal.reason), { once: true })
        },
        pull() {
          // The queued chunk has been consumed, so this request is waiting for more body bytes.
          bodyReading.resolve()
        },
      }))
    },
  })
  const request = client.sessions(true)
  await Promise.race([
    bodyReading.promise,
    request.then(() => assert.fail("The Session read completed before the body stalled")),
  ])
  assert.equal(firstSignal.aborted, false)
  t.mock.timers.tick(timeoutMs)
  assert.equal(firstSignal.aborted, true, "The deadline must still abort fetch after the response headers arrive")
  await assert.rejects(request, error => {
    assert.equal(error.name, "TransportError")
    assert.equal(error.cause?.name, "AbortError")
    return true
  })
  assert.deepEqual(await client.sessions(), snapshot(2, 1))
  assert.equal(attempts, 2)
})

function fixture() {
  const initial = Promise.withResolvers()
  const streams = []
  const client = {
    url: path => path,
    sessions: startup => startup ? initial.promise : Promise.resolve(snapshot(3, 2)),
  }
  const store = new FleetStore(client, {
    open(url, handlers) {
      assert.equal(url, "/v1/events")
      const stream = { handlers, closed: false }
      streams.push(stream)
      return { close: () => { stream.closed = true } }
    },
  })
  return { initial, store, streams }
}

for (const [name, read, frames, expected] of [
  ["newer frame", snapshot(1, 2), [snapshot(1, 4), snapshot(1, 3)], snapshot(1, 4)],
  ["newer HTTP snapshot", snapshot(1, 4), [snapshot(1, 3)], snapshot(1, 4)],
  ["new daemon stream", snapshot(1, 4), [snapshot(2, 1)], snapshot(2, 1)],
  ["old daemon stream", snapshot(2, 1), [snapshot(1, 4)], snapshot(2, 1)],
  ["HTTP failure with a live snapshot", new Error("Initial read failed"), [snapshot(2, 1)], snapshot(2, 1)],
]) {
  test(`the stream opens during the initial read and the store publishes the ${name}`, async () => {
    const { initial, store, streams } = fixture()
    const seen = []
    store.subscribe(state => { if (state.snapshot) seen.push(state.snapshot.scan) })
    const startup = store.start()
    assert.equal(streams.length, 1, "The live connection must open while the initial Session read is pending")
    assert.equal(store.get().live, false)
    assert.equal(store.get().loaded, false)
    const { handlers } = streams[0]
    handlers.onOpen()
    assert.equal(store.get().live, true)
    handlers.onFrame("sessions", '{"sessions":"invalid"}')
    for (const frame of frames) handlers.onFrame("sessions", JSON.stringify(frame))
    assert.deepEqual(seen, [], "the initial visual list still arrives as one batch")
    if (read instanceof Error) initial.reject(read)
    else initial.resolve(read)
    await startup
    assert.deepEqual(store.get().snapshot, expected)
    assert.deepEqual(seen, [expected.scan])
    assert.equal(store.get().error, null)
  })
}

test("a missing initial snapshot reports its failure and the existing refresh recovers", async () => {
  const { initial, store, streams } = fixture()
  const startup = store.start()
  streams[0].handlers.onOpen()
  initial.reject(new Error("Initial read failed"))
  await startup
  assert.equal(store.get().snapshot, null)
  assert.equal(store.get().error, "Initial read failed")
  assert.equal(store.get().live, true)
  await store.refresh()
  assert.deepEqual(store.get().snapshot, snapshot(3, 2))
  assert.equal(store.get().error, null)
})

test("an older startup result cannot replace a manual refresh across daemon restarts", async () => {
  const { initial, store, streams } = fixture()
  const startup = store.start()
  streams[0].handlers.onFrame("sessions", JSON.stringify(snapshot(1, 5)))
  await store.refresh()
  initial.resolve(snapshot(1, 4))
  await startup
  assert.deepEqual(store.get().snapshot, snapshot(3, 2))
  assert.equal(store.get().error, null)
})

test("stopping during startup closes the stream and ignores its pending work", async () => {
  const { initial, store, streams } = fixture()
  const startup = store.start()
  store.stop()
  assert.equal(streams[0].closed, true)
  streams[0].handlers.onOpen()
  streams[0].handlers.onFrame("sessions", JSON.stringify(snapshot(2, 1)))
  initial.resolve(snapshot(2, 1))
  await startup
  assert.equal(store.get().live, false)
  assert.equal(store.get().snapshot, null)
  assert.equal(streams.length, 1)
})
