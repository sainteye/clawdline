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
  const browser = { answer: 200, sessions: [] }
  t.mock.method(globalThis, "fetch", async input => {
    traffic.push(`read ${input}`)
    const status = browser.nextRead ? await browser.nextRead() : browser.answer
    if (status === 200) return Response.json({ sessions: browser.sessions })
    const code = { 401: "unauthorized", 403: "forbidden", 503: "unavailable" }[status]
    return Response.json({ error: { code, detail: "refused" } }, { status })
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
      const frames = []
      const handle = transport.open("/v1/events", {
        onFrame(event, data) { if (event === "sessions") frames.push(JSON.parse(data)) },
      })
      handles.add(handle)
      return {
        show,
        frames,
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
  assert.deepEqual(browser.traffic, ["tab1 stream /v1/events", "tab1 stream refused", "read /v1/sessions"])

  t.mock.timers.tick(only.module.FLEET_RECONNECT_FIRST_MS)
  await settle()
  assert.deepEqual(browser.traffic.slice(3), ["read /v1/sessions", "tab1 stream /v1/events"])
})

test("a visible holder whose stream is refused takes it back from a hidden tab after the backoff", async t => {
  t.mock.timers.enable({ apis: ["setTimeout"] })
  const browser = fleetBrowser(t, fakeLocks())
  const holder = await browser.openTab()
  await settle()
  await browser.openTab("hidden")
  await settle()
  assert.deepEqual(browser.traffic, ["tab1 stream /v1/events"])

  holder.refuseStream()
  await settle()
  assert.deepEqual(browser.traffic.slice(1), ["tab1 stream refused", "read /v1/sessions", "tab2 stream /v1/events"])

  // The page the person sees steals the stream back; the hidden one pauses.
  t.mock.timers.tick(holder.module.FLEET_RECONNECT_FIRST_MS)
  await settle()
  assert.deepEqual(browser.traffic.slice(4), ["read /v1/sessions", "tab1 stream /v1/events", "tab2 close stream"])

  t.mock.timers.tick(holder.module.FLEET_RECONNECT_CEILING_MS)
  await settle()
  assert.deepEqual(browser.traffic.slice(7), [])
})

test("a refused tab takes the stream back from a visible tab, which polls and does not steal it again", async t => {
  t.mock.timers.enable({ apis: ["setTimeout"] })
  const browser = fleetBrowser(t, fakeLocks())
  await browser.openTab()
  await settle()
  const holder = await browser.openTab()
  await settle()
  assert.deepEqual(browser.traffic, ["tab1 stream /v1/events", "tab2 stream /v1/events", "tab1 close stream", "read /v1/sessions"])

  holder.refuseStream()
  await settle()
  assert.deepEqual(browser.traffic.slice(4), ["tab2 stream refused", "read /v1/sessions", "tab1 stream /v1/events"])

  t.mock.timers.tick(holder.module.FLEET_RECONNECT_FIRST_MS)
  await settle()
  assert.deepEqual(browser.traffic.slice(7), ["read /v1/sessions", "tab2 stream /v1/events", "tab1 close stream", "read /v1/sessions"])

  const before = browser.traffic.length
  for (let i = 0; i < 6; i++) {
    t.mock.timers.tick(holder.module.FLEET_RECONNECT_CEILING_MS)
    await settle()
  }
  const later = browser.traffic.slice(before)
  assert.ok(later.length > 0 && later.every(line => line === "read /v1/sessions"), JSON.stringify(later))
})

test("successive stream refusals keep reconnecting at the 30-second ceiling", async t => {
  t.mock.timers.enable({ apis: ["setTimeout"] })
  const browser = fleetBrowser(t, fakeLocks())
  const only = await browser.openTab()
  await settle()
  const opened = () => browser.traffic.filter(line => line === "tab1 stream /v1/events").length
  for (const [attempt, wait] of [1_000, 2_000, 4_000, 8_000, 16_000, 30_000, 30_000, 30_000].entries()) {
    only.refuseStream()
    await settle()
    t.mock.timers.tick(wait - 1)
    await settle()
    assert.equal(opened(), attempt + 1)
    t.mock.timers.tick(1)
    await settle()
    assert.equal(opened(), attempt + 2)
  }
})

test("successive 503 probes still retry at the ceiling and reopen when reads recover", async t => {
  t.mock.timers.enable({ apis: ["setTimeout"] })
  const browser = fleetBrowser(t, fakeLocks())
  const only = await browser.openTab("hidden")
  await settle()
  browser.answer = 503
  only.refuseStream()
  await settle()
  const reads = () => browser.traffic.filter(line => line === "read /v1/sessions").length
  for (const [attempt, wait] of [1_000, 2_000, 4_000, 8_000, 16_000, 30_000, 30_000].entries()) {
    t.mock.timers.tick(wait - 1)
    await settle()
    assert.equal(reads(), attempt)
    t.mock.timers.tick(1)
    await settle()
    assert.equal(reads(), attempt + 1)
  }
  browser.answer = 200
  t.mock.timers.tick(30_000)
  await settle()
  assert.equal(browser.traffic.at(-1), "tab1 stream /v1/events")
})

for (const status of [401, 403]) {
  test(`a refused stream whose read answers ${status} stops until the page is shown again`, async t => {
    t.mock.timers.enable({ apis: ["setTimeout"] })
    const browser = fleetBrowser(t, fakeLocks())
    const only = await browser.openTab()
    await settle()
    browser.answer = status
    only.refuseStream()
    await settle()
    t.mock.timers.tick(1_000)
    await settle()
    assert.deepEqual(browser.traffic, ["tab1 stream /v1/events", "tab1 stream refused", "read /v1/sessions"])

    browser.answer = 200
    t.mock.timers.tick(30_000)
    await settle()
    assert.equal(browser.traffic.length, 3)

    only.show("hidden")
    await settle()
    only.show("visible")
    await settle()
    assert.deepEqual(browser.traffic.slice(3), ["tab1 stream /v1/events"])
  })
}

test("pressing the connection light retry reopens now and resets the first wait", async t => {
  t.mock.timers.enable({ apis: ["setTimeout"] })
  const browser = fleetBrowser(t, fakeLocks())
  const only = await browser.openTab()
  await settle()
  only.refuseStream()
  await settle()
  t.mock.timers.tick(1_000)
  await settle()
  only.refuseStream()
  await settle()
  const before = browser.traffic.length
  only.module.retryFleetStream?.()
  await only.module.client.sessions()
  await settle()
  assert.deepEqual(browser.traffic.slice(before), ["read /v1/sessions", "tab1 stream /v1/events"])

  only.refuseStream()
  await settle()
  t.mock.timers.tick(999)
  await settle()
  assert.equal(browser.traffic.filter(line => line === "tab1 stream /v1/events").length, 3)
  t.mock.timers.tick(1)
  await settle()
  assert.equal(browser.traffic.filter(line => line === "tab1 stream /v1/events").length, 4)
  const resumed = browser.traffic.length
  t.mock.timers.tick(1_000)
  await settle()
  assert.equal(browser.traffic.length, resumed)
})

test("a visible tab reads new sessions every five seconds during stream backoff", async t => {
  t.mock.timers.enable({ apis: ["setTimeout"] })
  const browser = fleetBrowser(t, fakeLocks())
  const only = await browser.openTab()
  await settle()
  for (const wait of [1_000, 2_000, 4_000, 8_000, 16_000]) {
    only.refuseStream()
    await settle()
    t.mock.timers.tick(wait)
    await settle()
  }
  browser.sessions = [{ id: "initial" }]
  only.refuseStream()
  await settle()
  const before = browser.traffic.length
  for (const id of ["first update", "second update"]) {
    browser.sessions = [{ id }]
    t.mock.timers.tick(4_999)
    await settle()
    assert.notDeepEqual(only.frames.at(-1), { sessions: [{ id }] })
    t.mock.timers.tick(1)
    await settle()
    assert.deepEqual(only.frames.at(-1), { sessions: [{ id }] })
  }
  assert.deepEqual(browser.traffic.slice(before), ["read /v1/sessions", "read /v1/sessions"])

  only.show("hidden")
  browser.sessions = [{ id: "while hidden" }]
  t.mock.timers.tick(5_000)
  await settle()
  assert.equal(only.frames.at(-1).sessions[0].id, "second update")
  only.show("visible")
  await settle()
  assert.equal(only.frames.at(-1).sessions[0].id, "while hidden")
})

test("an access refusal from a probe before a manual retry cannot stop the new attempt", async t => {
  t.mock.timers.enable({ apis: ["setTimeout"] })
  const browser = fleetBrowser(t, fakeLocks())
  const only = await browser.openTab()
  await settle()
  only.refuseStream()
  await settle()

  const { promise, resolve } = Promise.withResolvers()
  browser.nextRead = () => promise
  t.mock.timers.tick(1_000)
  await settle()
  browser.nextRead = undefined

  only.module.retryFleetStream()
  await settle()
  only.refuseStream()
  await settle()
  resolve(401)
  await settle()

  t.mock.timers.tick(1_000)
  await settle()
  assert.equal(browser.traffic.filter(line => line === "tab1 stream /v1/events").length, 3)
})
