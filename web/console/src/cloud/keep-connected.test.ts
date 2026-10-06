// The hosted console's reconnect loop (`keepConnected`, copied `cloud-boot.js`)
// and a page put in the background:
// `node --test --experimental-strip-types web/console/src/cloud/keep-connected.test.ts`.
//
// Until 2026-10-06 a page hidden for a minute (plus up to another minute for
// work still in the air) had its Cloud socket retired, said `paused`, and
// connected nothing until it was shown again; the console then read "this
// page's Cloud connection is reconnecting" and its terminals could not type.
// The person asked for that to go: a hidden page keeps its connection exactly
// as a shown one does. What stays is recovery — a page shown after a long hide
// does not trust a socket that still says `ready`, because the browser may
// have frozen it meanwhile.
import { test } from "node:test"
import assert from "node:assert/strict"
import { keepConnected } from "../legacy/js/net/cloud-boot.js"

/** Timers and a clock the test moves by hand. */
function fakeClock() {
  let at = 1_000_000
  let next = 1
  const timers = new Map<number, { due: number; fn: () => void }>()
  return {
    now: () => at,
    setTimeout: (fn: () => void, ms: number) => {
      const id = next++
      timers.set(id, { due: at + Math.max(0, ms), fn })
      return id
    },
    clearTimeout: (id: number) => { timers.delete(id) },
    async advance(ms: number) {
      const end = at + ms
      for (;;) {
        await settle()
        let soonest: [number, { due: number; fn: () => void }] | null = null
        for (const entry of timers) if (entry[1].due <= end && (!soonest || entry[1].due < soonest[1].due)) soonest = entry
        if (!soonest) break
        timers.delete(soonest[0])
        at = soonest[1].due
        soonest[1].fn()
      }
      at = end
      await settle()
    },
  }
}

async function settle() {
  for (let i = 0; i < 20; i++) await Promise.resolve()
}

/** A document and window whose visibility the test sets. */
function fakePage() {
  const docListeners = new Map<string, Array<() => void>>()
  const winListeners = new Map<string, Array<() => void>>()
  const target = (listeners: Map<string, Array<() => void>>) => ({
    addEventListener: (name: string, fn: () => void) => { listeners.set(name, [...(listeners.get(name) ?? []), fn]) },
    removeEventListener: (name: string, fn: () => void) => {
      listeners.set(name, (listeners.get(name) ?? []).filter((f) => f !== fn))
    },
  })
  const document = { hidden: false, visibilityState: "visible", ...target(docListeners) }
  const window = target(winListeners)
  const fire = (listeners: Map<string, Array<() => void>>, name: string) => (listeners.get(name) ?? []).forEach((fn) => fn())
  return {
    document,
    window,
    navigator: { onLine: true },
    hide() {
      document.hidden = true
      document.visibilityState = "hidden"
      fire(docListeners, "visibilitychange")
      fire(winListeners, "pagehide")
    },
    show() {
      document.hidden = false
      document.visibilityState = "visible"
      fire(docListeners, "visibilitychange")
    },
  }
}

interface FakeClient {
  ready: boolean
  retired: boolean
  continuityUnproven?: boolean
  lifecycle?: (reason: string) => unknown
  events(listener: (event: { type: string; state?: string }) => void): () => void
  drop(): void
  retire(): void
  stop(): void
}

function fakeClient(): FakeClient {
  const listeners = new Set<(event: { type: string; state?: string }) => void>()
  return {
    ready: true,
    retired: false,
    events(listener) { listeners.add(listener); return () => listeners.delete(listener) },
    drop() { this.ready = false; listeners.forEach((l) => l({ type: "connection", state: "offline" })) },
    retire() { this.retired = true; this.ready = false },
    stop() { this.retired = true; this.ready = false },
  }
}

/** A session whose every connect succeeds with a token that needs no renewal in the test's span. */
function fakeSession() {
  const clients: FakeClient[] = []
  return {
    clients,
    client: null as FakeClient | null,
    async connect() {
      const client = fakeClient()
      const previous = this.client
      clients.push(client)
      this.client = client
      return { state: "connected", client, previous, renewInMs: null, expiresAt: NaN }
    },
  }
}

function start() {
  const clock = fakeClock()
  const page = fakePage()
  const session = fakeSession()
  const states: string[] = []
  const line = keepConnected(session, {
    now: clock.now,
    setTimeout: clock.setTimeout,
    clearTimeout: clock.clearTimeout,
    jitter: () => 0.5,
    document: page.document,
    window: page.window,
    navigator: page.navigator,
    onState: (update: { state: string }) => states.push(update.state),
  })
  return { clock, page, session, states, line }
}

// Red before 2026-10-06: at 60 s the socket was retired and `paused` emitted.
test("a page hidden far past the old grace keeps its Cloud connection open", async () => {
  const { clock, page, session, states, line } = start()
  await clock.advance(0)
  assert.equal(session.clients.length, 1)
  const first = session.clients[0]

  page.hide()
  // The old grace (60 s) and the old wait for work in the air (60 s), well past both.
  await clock.advance(10 * 60 * 1000)

  assert.ok(!states.includes("paused"), `no paused state while hidden: ${states.join(", ")}`)
  assert.deepEqual(states, ["connected"])
  assert.equal(first.retired, false, "the hidden page's socket is not closed")
  assert.equal(first.ready, true)
  assert.equal(session.clients.length, 1, "nothing was torn down and connected again")
  assert.equal(first.lifecycle?.("demand"), true, "a read asked while hidden is told a connection is there")
  line.stop()
})

test("a hidden page whose socket drops connects again without waiting to be shown", async () => {
  const { clock, page, session, states, line } = start()
  await clock.advance(0)
  page.hide()
  await clock.advance(5 * 60 * 1000)
  session.clients[0].drop()
  await clock.advance(5_000)

  assert.equal(session.clients.length, 2, "the loop reconnected while the page was still hidden")
  assert.equal(session.clients[1].ready, true)
  assert.deepEqual(states, ["connected", "reconnecting", "connected"])
  line.stop()
})

// Kept on purpose: recovery, not saving. A phone may freeze a hidden page, so
// a socket that says `ready` after a long hide is replaced, while it serves.
test("a page shown after a long hide makes a fresh connection beside the one that says ready", async () => {
  const { clock, page, session, states, line } = start()
  await clock.advance(0)
  page.hide()
  await clock.advance(2 * 60 * 1000)
  page.show()
  await clock.advance(0)

  assert.equal(session.clients.length, 2, "a fresh connection was made on return")
  assert.equal(session.clients[0].continuityUnproven, true, "the old socket's continuity is marked unproven")
  assert.ok(!states.includes("paused") && !states.includes("reconnecting"), states.join(", "))
  line.stop()
})

test("a short hide changes nothing", async () => {
  const { clock, page, session, line } = start()
  await clock.advance(0)
  page.hide()
  await clock.advance(10_000)
  page.show()
  await clock.advance(0)
  assert.equal(session.clients.length, 1)
  assert.equal(session.clients[0].retired, false)
  line.stop()
})
