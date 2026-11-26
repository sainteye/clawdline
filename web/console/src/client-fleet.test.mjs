import assert from "node:assert/strict"
import { setImmediate } from "node:timers/promises"
import { test } from "node:test"
import { fileURLToPath } from "node:url"
import { build } from "esbuild"

const { outputFiles } = await build({
  entryPoints: [fileURLToPath(new URL("./client.ts", import.meta.url))],
  bundle: true,
  platform: "browser",
  format: "esm",
  write: false,
})
const clientModule = `data:text/javascript;base64,${Buffer.from(outputFiles[0].text).toString("base64")}`

function fakeLocks() {
  const held = new Map()
  return {
    async request(name, options, callback) {
      let current = held.get(name)
      if (current && options.ifAvailable) return callback(null)
      while (current) {
        await current
        current = held.get(name)
      }
      options.signal?.throwIfAborted()
      const lease = Promise.withResolvers()
      held.set(name, lease.promise)
      try {
        return await callback({ name, mode: "exclusive" })
      } finally {
        held.delete(name)
        lease.resolve()
      }
    },
  }
}

function mockBrowserGlobal(t, name, value) {
  const original = Object.getOwnPropertyDescriptor(globalThis, name)
  Object.defineProperty(globalThis, name, { configurable: true, value })
  t.after(() => {
    if (original) Object.defineProperty(globalThis, name, original)
    else Reflect.deleteProperty(globalThis, name)
  })
}

let tabNumber = 0

function fleetBrowser(t, locks) {
  const traffic = []
  const handles = new Set()
  mockBrowserGlobal(t, "navigator", locks ? { locks } : {})
  mockBrowserGlobal(t, "EventSource", class {
    constructor(url) { traffic.push(`stream ${url}`) }
    addEventListener() {}
    close() { traffic.push("close stream") }
  })
  t.mock.method(globalThis, "fetch", async input => {
    traffic.push(`read ${input}`)
    return Response.json({ sessions: [] })
  })
  t.after(() => { for (const handle of handles) handle.close() })
  return {
    traffic,
    async openTab() {
      // A separate module instance gives each tab its own quiet state and stream.
      const tab = await import(`${clientModule}#tab-${++tabNumber}`)
      const transport = tab.fleetTransport()
      assert.ok(transport)
      const handle = transport.open("/v1/events", { onFrame() {} })
      handles.add(handle)
      return {
        quietFleetStream: tab.quietFleetStream,
        close() {
          handle.close()
          handles.delete(handle)
        },
      }
    },
  }
}

test("a fleet tab opens a stream when it acquires the lock", async t => {
  const browser = fleetBrowser(t, fakeLocks())
  await browser.openTab()
  await setImmediate()
  assert.deepEqual(browser.traffic, ["stream /v1/events"])
})

test("a fleet tab polls while another tab holds the lock and streams when it is released", async t => {
  const browser = fleetBrowser(t, fakeLocks())
  const leader = await browser.openTab()
  await browser.openTab()
  await setImmediate()
  assert.deepEqual(browser.traffic, ["stream /v1/events", "read /v1/sessions"])

  leader.close()
  await setImmediate()
  assert.deepEqual(browser.traffic.slice(2), ["close stream", "stream /v1/events"])
})

test("opening a terminal releases the fleet lock and closing it rejoins the queue", async t => {
  const browser = fleetBrowser(t, fakeLocks())
  const terminalTab = await browser.openTab()
  const otherTab = await browser.openTab()
  await setImmediate()
  assert.deepEqual(browser.traffic, ["stream /v1/events", "read /v1/sessions"])

  const resume = terminalTab.quietFleetStream()
  await setImmediate()
  assert.deepEqual(browser.traffic.slice(2), ["close stream", "read /v1/sessions", "stream /v1/events"])

  resume()
  await setImmediate()
  assert.equal(browser.traffic.length, 5)
  otherTab.close()
  await setImmediate()
  assert.deepEqual(browser.traffic.slice(5), ["close stream", "stream /v1/events"])
})

test("a failed fleet lock request falls back to the stream", async t => {
  const browser = fleetBrowser(t, {
    async request() { throw new DOMException("Web Locks unavailable", "SecurityError") },
  })
  await browser.openTab()
  await setImmediate()
  assert.deepEqual(browser.traffic, ["stream /v1/events"])
})

test("a fleet tab opens a stream when the browser has no Web Locks", async t => {
  const browser = fleetBrowser(t)
  await browser.openTab()
  await setImmediate()
  assert.deepEqual(browser.traffic, ["stream /v1/events"])
})
