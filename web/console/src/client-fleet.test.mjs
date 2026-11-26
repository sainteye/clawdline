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

// Follows the Web Locks request queue: first come first served, ifAvailable
// only when nothing holds or waits, and steal preempts the holder, whose
// request then rejects with AbortError.
function fakeLocks() {
  const holders = new Map()
  const queues = new Map()
  const aborted = () => new DOMException("The lock request is aborted", "AbortError")
  const grant = (name, entry) => {
    holders.set(name, entry)
    Promise.resolve().then(() => entry.callback({ name, mode: "exclusive" })).then(
      value => {
        if (holders.get(name) !== entry) return
        holders.delete(name)
        next(name)
        entry.resolve(value)
      },
      cause => {
        if (holders.get(name) !== entry) return
        holders.delete(name)
        next(name)
        entry.reject(cause)
      },
    )
  }
  const next = name => {
    const queue = queues.get(name) ?? []
    const entry = queue.shift()
    if (entry) grant(name, entry)
  }
  return {
    request(name, options, callback) {
      return new Promise((resolve, reject) => {
        const entry = { callback, resolve, reject }
        const queue = queues.get(name) ?? []
        queues.set(name, queue)
        if (options.signal?.aborted) { reject(aborted()); return }
        if (options.steal) {
          const held = holders.get(name)
          holders.delete(name)
          held?.reject(aborted())
          grant(name, entry)
          return
        }
        if (!holders.has(name) && queue.length === 0) { grant(name, entry); return }
        if (options.ifAvailable) {
          Promise.resolve().then(() => callback(null)).then(resolve, reject)
          return
        }
        queue.push(entry)
        options.signal?.addEventListener("abort", () => {
          const at = queue.indexOf(entry)
          if (at < 0) return
          queue.splice(at, 1)
          reject(aborted())
        })
      })
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

function fakePage(visibility) {
  const page = new EventTarget()
  page.visibilityState = visibility
  return {
    page,
    show(state) {
      page.visibilityState = state
      page.dispatchEvent(new Event("visibilitychange"))
    },
  }
}

async function settle() {
  for (let i = 0; i < 5; i++) await setImmediate()
}

function fleetBrowser(t, locks) {
  const traffic = []
  const handles = new Set()
  let tabs = 0
  mockBrowserGlobal(t, "navigator", locks ? { locks } : {})
  const browser = { answer: 200 }
  t.mock.method(globalThis, "fetch", async input => {
    traffic.push(`read ${input}`)
    if (browser.answer === 200) return Response.json({ sessions: [] })
    return Response.json({ error: { code: browser.answer === 401 ? "unauthorized" : "unavailable", detail: "refused" } }, { status: browser.answer })
  })
  t.after(() => { for (const handle of handles) handle.close() })
  return Object.assign(browser, {
    traffic,
    async openTab(visibility = "visible") {
      const name = `tab${++tabs}`
      // A module reads its page and EventSource once, as a browser tab would.
      const { page, show } = fakePage(visibility)
      mockBrowserGlobal(t, "document", page)
      const sources = []
      mockBrowserGlobal(t, "EventSource", class {
        constructor(url) {
          traffic.push(`${name} stream ${url}`)
          this.readyState = 0
          sources.push(this)
        }
        addEventListener() {}
        close() {
          if (this.readyState === 2) return
          this.readyState = 2
          traffic.push(`${name} close stream`)
        }
      })
      // A separate module instance gives each tab its own quiet state and stream.
      const tab = await import(`${clientModule}#tab-${++tabNumber}`)
      const transport = tab.fleetTransport()
      assert.ok(transport)
      const handle = transport.open("/v1/events", { onFrame() {} })
      handles.add(handle)
      return {
        show,
        module: tab,
        quietFleetStream: tab.quietFleetStream,
        // The browser answers this tab's open stream with an HTTP error: the
        // EventSource fires error and stays CLOSED, as a 401 or 503 leaves it.
        refuseStream() {
          const source = sources.at(-1)
          assert.ok(source && source.readyState !== 2, `${name} has no open stream to refuse`)
          source.readyState = 2
          traffic.push(`${name} stream refused`)
          source.onerror?.({ type: "error", target: source })
        },
        close() {
          handle.close()
          handles.delete(handle)
        },
      }
    },
  })
}

test("a fleet tab opens a stream when it acquires the lock", async t => {
  const browser = fleetBrowser(t, fakeLocks())
  await browser.openTab()
  await settle()
  assert.deepEqual(browser.traffic, ["tab1 stream /v1/events"])
})

test("the tab opened last takes the stream and a visible follower polls until it is released", async t => {
  const browser = fleetBrowser(t, fakeLocks())
  const first = await browser.openTab()
  await settle()
  const second = await browser.openTab()
  await settle()
  assert.deepEqual(browser.traffic, ["tab1 stream /v1/events", "tab2 stream /v1/events", "tab1 close stream", "read /v1/sessions"])

  second.close()
  await settle()
  assert.deepEqual(browser.traffic.slice(4), ["tab2 close stream", "tab1 stream /v1/events"])
  first.close()
})

test("a hidden waiting tab does not poll and takes the stream as soon as it is shown", async t => {
  const browser = fleetBrowser(t, fakeLocks())
  await browser.openTab()
  await settle()
  const background = await browser.openTab("hidden")
  await settle()
  assert.deepEqual(browser.traffic, ["tab1 stream /v1/events"])

  background.show("visible")
  await settle()
  assert.deepEqual(browser.traffic.slice(1), ["tab2 stream /v1/events", "tab1 close stream", "read /v1/sessions"])
})

test("a holder that is hidden hands the stream to a visible tab that waits", async t => {
  const browser = fleetBrowser(t, fakeLocks())
  await browser.openTab()
  await settle()
  const front = await browser.openTab()
  await settle()
  assert.deepEqual(browser.traffic, ["tab1 stream /v1/events", "tab2 stream /v1/events", "tab1 close stream", "read /v1/sessions"])

  front.show("hidden")
  await settle()
  assert.deepEqual(browser.traffic.slice(4), ["tab1 stream /v1/events", "tab2 close stream"])
})

test("a hidden tab alone keeps the stream without reconnecting", async t => {
  const browser = fleetBrowser(t, fakeLocks())
  const only = await browser.openTab()
  await settle()
  only.show("hidden")
  await settle()
  only.show("visible")
  await settle()
  assert.deepEqual(browser.traffic, ["tab1 stream /v1/events"])
})

test("opening a terminal releases the fleet lock and closing it takes the stream back", async t => {
  const browser = fleetBrowser(t, fakeLocks())
  const terminalTab = await browser.openTab()
  await settle()
  await browser.openTab("hidden")
  await settle()
  assert.deepEqual(browser.traffic, ["tab1 stream /v1/events"])

  const resume = terminalTab.quietFleetStream()
  await settle()
  assert.deepEqual(browser.traffic.slice(1), ["tab1 close stream", "read /v1/sessions", "tab2 stream /v1/events"])

  resume()
  await settle()
  assert.deepEqual(browser.traffic.slice(4), ["tab1 stream /v1/events", "tab2 close stream"])
})

test("a hidden terminal tab stops reading and reads once when it is shown", async t => {
  const browser = fleetBrowser(t, fakeLocks())
  const terminalTab = await browser.openTab()
  await settle()
  terminalTab.quietFleetStream()
  await settle()
  assert.deepEqual(browser.traffic, ["tab1 stream /v1/events", "tab1 close stream", "read /v1/sessions"])

  terminalTab.show("hidden")
  await settle()
  terminalTab.show("visible")
  await settle()
  assert.deepEqual(browser.traffic.slice(3), ["read /v1/sessions"])
})

test("a failed fleet lock request falls back to the stream", async t => {
  const browser = fleetBrowser(t, {
    async request() { throw new DOMException("Web Locks unavailable", "SecurityError") },
  })
  await browser.openTab()
  await settle()
  assert.deepEqual(browser.traffic, ["tab1 stream /v1/events"])
})

test("a fleet tab opens a stream when the browser has no Web Locks", async t => {
  const browser = fleetBrowser(t)
  await browser.openTab()
  await settle()
  assert.deepEqual(browser.traffic, ["tab1 stream /v1/events"])
})

// Failure injection: the holder's stream is answered with an HTTP error. The
// browser closes such an EventSource for good and never retries it itself.

test("a held stream refused with 503 reopens after a backoff", async t => {
  t.mock.timers.enable({ apis: ["setTimeout"] })
  const browser = fleetBrowser(t, fakeLocks())
  const only = await browser.openTab()
  await settle()
  only.refuseStream()
  await settle()
  assert.deepEqual(browser.traffic, ["tab1 stream /v1/events", "tab1 stream refused"])

  t.mock.timers.tick(only.module.FLEET_RECONNECT_FIRST_MS)
  await settle()
  assert.deepEqual(browser.traffic.slice(2), ["read /v1/sessions", "tab1 stream /v1/events"])
})

test("a holder whose stream is refused hands the lock to a waiting tab", async t => {
  t.mock.timers.enable({ apis: ["setTimeout"] })
  const browser = fleetBrowser(t, fakeLocks())
  const holder = await browser.openTab()
  await settle()
  await browser.openTab("hidden")
  await settle()
  assert.deepEqual(browser.traffic, ["tab1 stream /v1/events"])

  holder.refuseStream()
  await settle()
  assert.deepEqual(browser.traffic.slice(1), ["tab1 stream refused", "tab2 stream /v1/events"])

  // Its own retry queues behind the new holder and reads instead of stealing.
  t.mock.timers.tick(holder.module.FLEET_RECONNECT_FIRST_MS)
  await settle()
  assert.deepEqual(browser.traffic.slice(3), ["read /v1/sessions", "read /v1/sessions"])
})

test("a stream that keeps being refused stops reconnecting at the limit", async t => {
  t.mock.timers.enable({ apis: ["setTimeout"] })
  const browser = fleetBrowser(t, fakeLocks())
  const only = await browser.openTab()
  await settle()
  const { FLEET_RECONNECT_ATTEMPTS, FLEET_RECONNECT_CEILING_MS } = only.module
  assert.ok(FLEET_RECONNECT_ATTEMPTS > 0)
  const opened = () => browser.traffic.filter(line => line === "tab1 stream /v1/events").length
  for (let attempt = 0; attempt < FLEET_RECONNECT_ATTEMPTS; attempt++) {
    only.refuseStream()
    await settle()
    t.mock.timers.tick(FLEET_RECONNECT_CEILING_MS)
    await settle()
    assert.equal(opened(), attempt + 2)
  }
  only.refuseStream()
  await settle()
  const stopped = browser.traffic.length
  t.mock.timers.tick(FLEET_RECONNECT_CEILING_MS * 10)
  await settle()
  assert.deepEqual(browser.traffic.slice(stopped), [])
  assert.equal(opened(), FLEET_RECONNECT_ATTEMPTS + 1)
})

test("a refused stream whose read answers 401 stops at once and a shown page tries again", async t => {
  t.mock.timers.enable({ apis: ["setTimeout"] })
  const browser = fleetBrowser(t, fakeLocks())
  const only = await browser.openTab()
  await settle()
  browser.answer = 401
  only.refuseStream()
  await settle()
  t.mock.timers.tick(only.module.FLEET_RECONNECT_FIRST_MS)
  await settle()
  assert.deepEqual(browser.traffic, ["tab1 stream /v1/events", "tab1 stream refused", "read /v1/sessions"])

  t.mock.timers.tick(only.module.FLEET_RECONNECT_CEILING_MS * 10)
  await settle()
  assert.equal(browser.traffic.length, 3)

  // Signed in again and back on the page: the person's return is a new try.
  browser.answer = 200
  only.show("hidden")
  await settle()
  only.show("visible")
  await settle()
  assert.deepEqual(browser.traffic.slice(3), ["tab1 stream /v1/events"])
})
