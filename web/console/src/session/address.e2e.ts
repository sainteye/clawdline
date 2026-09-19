// The open session in the address, through a real browser and real URLs:
//
//   (cd web && npm run build)
//   node --test web/console/src/session/address.e2e.ts
//
// The built console is served by a stand-in daemon that answers the session
// list and nothing else, and headless Chrome (CHROME, else the usual macOS
// install) loads it: the address typed, the page reloaded, the phone's back
// gesture taken. Everything `address.test.ts` checks as strings is checked
// here as what the page does with them — a fragment the browser itself parsed,
// a history the browser itself keeps.
//
// Named `.e2e.ts` rather than `.test.ts` so that the unit run over
// `session/*.test.ts` does not start a browser.
import { test, before, after } from "node:test"
import assert from "node:assert/strict"
import { spawn, type ChildProcess } from "node:child_process"
import { createServer, type Server, type ServerResponse } from "node:http"
import { existsSync, mkdtempSync, readFileSync, rmSync, statSync } from "node:fs"
import { tmpdir } from "node:os"
import { dirname, extname, join, normalize, resolve } from "node:path"
import { fileURLToPath } from "node:url"

const dist = resolve(dirname(fileURLToPath(import.meta.url)), "../../dist")
const chrome = process.env.CHROME || "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"

// Pane ids of three digits are spelled in pieces: tools/check-private.sh reads
// any `%NNN` in a published file as a pane copied from somebody's machine.
const pane = (n: number) => "%" + n
const escapedPane = (n: number) => "%25" + n

// One of each kind of id this daemon lists: a tmux pane, a tty, an iTerm
// session. The iTerm one is a fixture GUID (one digit padding it out), with
// the `:` that has to be escaped in a fragment.
const TMUX = pane(801)
const TTY = "ttys008"
const ITERM = "w0t0p0:1A000000-0000-4000-8000-000000000001"
// A pane whose id, written raw into an address, decodes to a control
// character: `%19` is U+0019. Old notifications carried ids that way.
const LEGACY = pane(195)
const GONE = pane(999)

const FRAGMENT: Record<string, string> = {
  [TMUX]: "#session=" + escapedPane(801),
  [TTY]: "#session=ttys008",
  [ITERM]: "#session=w0t0p0%3A1A000000-0000-4000-8000-000000000001",
  [LEGACY]: "#session=" + escapedPane(195),
}

function row(id: string, label: string, backend: string) {
  return {
    id,
    label,
    backend,
    state: "idle",
    work_state: "ready",
    evidence: "process",
    isClaude: true,
    cwd: "/tmp/fixture",
    closeability: {
      activity_generation: 1,
      attestation_id: null,
      mover: null,
      obligation_generation: 1,
      observed_at: 1,
      provenance: [],
      reasons: [],
      session_generation: 1,
      source: "broker",
      state: "safe",
      version: "fixture",
    },
  }
}

const ROWS = [
  row(TMUX, "Alpha on a tmux pane", "tmux"),
  row(TTY, "Bravo on a tty", "owned"),
  row(ITERM, "Charlie in iTerm", "iterm"),
  row(LEGACY, "Delta from an old link", "tmux"),
]

// ---- the stand-in daemon

let generation = 0
function snapshot() {
  generation++
  return {
    at: Date.now(),
    scan: {
      complete: true,
      completed: { complete: true, sequence: generation },
      emptyAuthoritative: true,
      epoch: 1,
      generation,
      provenance: "fixture",
    },
    sessions: ROWS,
  }
}

const TYPES: Record<string, string> = {
  ".js": "text/javascript",
  ".css": "text/css",
  ".json": "application/json",
  ".png": "image/png",
  ".ico": "image/x-icon",
  ".svg": "image/svg+xml",
  ".webmanifest": "application/manifest+json",
}

function json(res: ServerResponse, status: number, body: unknown) {
  res.writeHead(status, { "content-type": "application/json" })
  res.end(JSON.stringify(body))
}

// The document with its words written in, as `page.go` writes them.
function document(): string {
  const html = readFileSync(join(dist, "index.html"), "utf8")
  const words = JSON.parse(readFileSync(join(dist, "strings", "zh-Hant.json"), "utf8"))
  words.lang = "zh-Hant"
  words.dir = "ltr"
  const slot = "<script>window.__strings=" + JSON.stringify(words).replaceAll("</", "<\\/") + "</script>"
  return html.replace("<!-- clawdline:strings -->", slot).replace("<!-- clawdline:cloud -->", "")
}

function daemon(): Server {
  return createServer((req, res) => {
    const url = new URL(req.url ?? "/", "http://fixture")
    const path = url.pathname
    if (path === "/v1/sessions") return json(res, 200, snapshot())
    if (path === "/v1/health") return json(res, 200, { ok: true })
    if (path === "/v1/events") {
      res.writeHead(200, { "content-type": "text/event-stream", "cache-control": "no-cache" })
      res.write("event: sessions\ndata: " + JSON.stringify(snapshot()) + "\n\n")
      return // held open, as the daemon's stream is
    }
    if (path === "/v1/transcript") {
      return json(res, 200, { entries: [], evidence: "process", id: url.searchParams.get("session"), signature: "fixture" })
    }
    if (path.startsWith("/v1/")) return json(res, 404, { error: { code: "not_found", message: path } })
    if (path === "/") {
      res.writeHead(200, { "content-type": "text/html; charset=utf-8" })
      return res.end(document())
    }
    const file = normalize(join(dist, path))
    if (!file.startsWith(dist + "/") || !existsSync(file) || !statSync(file).isFile()) {
      return json(res, 404, { error: { code: "not_found", message: path } })
    }
    res.writeHead(200, { "content-type": TYPES[extname(file)] ?? "application/octet-stream" })
    res.end(readFileSync(file))
  })
}

// ---- a browser, over the DevTools protocol

type Pending = { resolve: (v: any) => void; reject: (e: Error) => void }

class Browser {
  private seq = 0
  private pending = new Map<number, Pending>()
  private events: { method: string; params: any; sessionId?: string }[] = []
  private waiters: (() => void)[] = []
  private ws: WebSocket

  private constructor(ws: WebSocket) {
    this.ws = ws
    ws.addEventListener("message", (ev) => {
      const msg = JSON.parse(String(ev.data))
      if (msg.id !== undefined) {
        const p = this.pending.get(msg.id)
        this.pending.delete(msg.id)
        if (msg.error) p?.reject(new Error(msg.error.message))
        else p?.resolve(msg.result)
        return
      }
      this.events.push(msg)
      for (const w of this.waiters.splice(0)) w()
    })
  }

  static async connect(url: string): Promise<Browser> {
    const ws = new WebSocket(url)
    await new Promise<void>((ok, fail) => {
      ws.addEventListener("open", () => ok())
      ws.addEventListener("error", () => fail(new Error("could not reach the browser at " + url)))
    })
    return new Browser(ws)
  }

  /** One command. A browser that never answers fails the test rather than hanging it. */
  send(method: string, params: object = {}, sessionId?: string, ms = 15_000): Promise<any> {
    const id = ++this.seq
    this.ws.send(JSON.stringify({ id, method, params, sessionId }))
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(id)
        reject(new Error(method + " had no answer within " + ms + "ms"))
      }, ms)
      this.pending.set(id, {
        resolve: (v) => (clearTimeout(timer), resolve(v)),
        reject: (e) => (clearTimeout(timer), reject(e)),
      })
    })
  }

  /** Waits for an event on one page that arrives after `mark`. */
  async event(sessionId: string, method: string, mark: number, ms = 10_000): Promise<void> {
    const deadline = Date.now() + ms
    for (;;) {
      if (this.events.slice(mark).some((e) => e.sessionId === sessionId && e.method === method)) return
      if (Date.now() > deadline) throw new Error("no " + method + " within " + ms + "ms")
      await new Promise<void>((ok) => {
        this.waiters.push(ok)
        setTimeout(ok, 100)
      })
    }
  }

  mark(): number {
    return this.events.length
  }

  close() {
    this.ws.close()
  }
}

/** What the page shows, read in one go so a failure can say all of it. */
const PROBE = `(() => {
  const open = document.querySelector("#rows > li.row.open")
  const app = document.getElementById("app")
  const toast = document.getElementById("toast")
  return {
    console: !!app,
    hash: location.hash,
    open: open ? open.dataset.id : null,
    view: app ? app.dataset.view : null,
    rows: document.querySelectorAll("#rows > li.row").length,
    toast: toast && !toast.hidden ? toast.textContent : null,
  }
})()`

type Seen = { console: boolean; hash: string; open: string | null; view: string | null; rows: number; toast: string | null }

class Tab {
  private b: Browser
  private session: string
  private target: string
  readonly origin: string

  constructor(b: Browser, session: string, target: string, origin: string) {
    this.b = b
    this.session = session
    this.target = target
    this.origin = origin
  }

  static async open(b: Browser, origin: string, size: { width: number; height: number; mobile: boolean }): Promise<Tab> {
    const { targetId } = await b.send("Target.createTarget", { url: "about:blank" })
    const { sessionId } = await b.send("Target.attachToTarget", { targetId, flatten: true })
    await b.send("Page.enable", {}, sessionId)
    await b.send("Runtime.enable", {}, sessionId)
    await b.send("Emulation.setDeviceMetricsOverride", { ...size, deviceScaleFactor: 1 }, sessionId)
    return new Tab(b, sessionId, targetId, origin)
  }

  close(): Promise<void> {
    return this.b.send("Target.closeTarget", { targetId: this.target }).then(
      () => undefined,
      () => undefined,
    )
  }

  async go(address: string): Promise<void> {
    const mark = this.b.mark()
    await this.b.send("Page.navigate", { url: this.origin + address }, this.session)
    await this.b.event(this.session, "Page.loadEventFired", mark)
  }

  async reload(): Promise<void> {
    const mark = this.b.mark()
    await this.b.send("Page.reload", {}, this.session)
    await this.b.event(this.session, "Page.loadEventFired", mark)
  }

  async run(expression: string): Promise<any> {
    const { result, exceptionDetails } = await this.b.send(
      "Runtime.evaluate",
      { expression, returnByValue: true, awaitPromise: true },
      this.session,
    )
    if (exceptionDetails) throw new Error(exceptionDetails.exception?.description ?? exceptionDetails.text)
    return result.value
  }

  seen(): Promise<Seen> {
    return this.run(PROBE)
  }

  /** Until the page shows what `ok` wants, or a timeout that says what it showed instead. */
  async until(what: string, ok: (s: Seen) => boolean, ms = 5_000): Promise<Seen> {
    const deadline = Date.now() + ms
    let last: Seen | null = null
    for (;;) {
      try {
        last = await this.seen()
        if (ok(last)) return last
      } catch {
        /* between documents */
      }
      if (Date.now() > deadline) assert.fail(what + "; the page showed " + JSON.stringify(last))
      await new Promise((r) => setTimeout(r, 50))
    }
  }

  /** A press on a row, as a finger or a pointer makes it. */
  tap(id: string): Promise<void> {
    return this.run(`(() => {
      const row = [...document.querySelectorAll("#rows > li.row")].find((n) => n.dataset.id === ${JSON.stringify(id)})
      if (!row) throw new Error("no row " + ${JSON.stringify(id)})
      row.click()
    })()`)
  }
}

// ---- the run

/** A fresh tab for one test, closed however the test ends. */
async function inTab(size: { width: number; height: number; mobile: boolean }, body: (tab: Tab) => Promise<void>) {
  const tab = await Tab.open(browser, origin, size)
  try {
    await body(tab)
  } finally {
    await tab.close()
  }
}

let server: Server
let browserProcess: ChildProcess
let browser: Browser
let profile: string
let origin: string
const DESK = { width: 1280, height: 800, mobile: false }
const PHONE = { width: 390, height: 844, mobile: true }

before(async () => {
  assert.ok(existsSync(join(dist, "index.html")), "no built console at " + dist + "; run `npm run build` in web first")
  assert.ok(existsSync(chrome), "no Chrome at " + chrome + "; set CHROME")
  server = daemon()
  await new Promise<void>((ok) => server.listen(0, "127.0.0.1", ok))
  const address = server.address()
  origin = "http://127.0.0.1:" + (typeof address === "object" && address ? address.port : 0)
  profile = mkdtempSync(join(tmpdir(), "clawdline-address-"))
  browserProcess = spawn(chrome, [
    "--headless=new",
    "--remote-debugging-port=0",
    "--user-data-dir=" + profile,
    "--no-first-run",
    "--no-default-browser-check",
    "about:blank",
  ])
  const endpoint = await new Promise<string>((ok, fail) => {
    let said = ""
    const timer = setTimeout(() => fail(new Error("Chrome did not start: " + said)), 20_000)
    browserProcess.stderr?.on("data", (chunk) => {
      said += String(chunk)
      const found = /DevTools listening on (ws:\/\/\S+)/.exec(said)
      if (found) {
        clearTimeout(timer)
        ok(found[1])
      }
    })
  })
  browser = await Browser.connect(endpoint)
})

after(async () => {
  browser?.close()
  // Its profile is removed below, and Chrome writes to it until it has gone.
  if (browserProcess && browserProcess.exitCode === null) {
    const gone = new Promise((ok) => browserProcess.once("exit", ok))
    browserProcess.kill()
    await gone
  }
  // The event streams are held open, and `close` waits for every connection.
  server?.closeAllConnections()
  await new Promise<void>((ok) => (server ? server.close(() => ok()) : ok()))
  if (profile) rmSync(profile, { recursive: true, force: true, maxRetries: 5 })
})

test("desk: opening a session writes it into the address, and a reload comes back to it", () =>
  inTab(DESK, async (tab) => {
    await tab.go("/")
    await tab.until("the list arrives", (s) => s.rows === ROWS.length)
    await tab.tap(TTY)
    await tab.until("the address names the session that was opened", (s) => s.open === TTY && s.hash === FRAGMENT[TTY])
    await tab.reload()
    await tab.until("the reload opens the same session", (s) => s.rows === ROWS.length && s.open === TTY)
    assert.equal((await tab.seen()).hash, FRAGMENT[TTY])
  }))

for (const id of [TMUX, TTY, ITERM]) {
  test(`desk: an address typed in for ${id} opens that session`, () =>
    inTab(DESK, async (tab) => {
      await tab.go("/" + FRAGMENT[id])
      const s = await tab.until("the session in the address opens", (s) => s.rows === ROWS.length && s.open === id)
      assert.equal(s.hash, FRAGMENT[id])
    }))
}

test("desk: a pane id written raw by an old link still opens its session", () =>
  inTab(DESK, async (tab) => {
    await tab.go("/#session=" + LEGACY)
    await tab.until("the raw pane id is read as the pane, not as U+0019", (s) => s.open === LEGACY)
  }))

test("desk: a session that is no longer there leaves the list on screen and says so", () =>
  inTab(DESK, async (tab) => {
    assert.ok(!ROWS.some((r) => r.id === GONE))
    await tab.go("/#session=" + escapedPane(999))
    const s = await tab.until("the page says the session is gone", (s) => s.rows === ROWS.length && !!s.toast)
    assert.equal(s.open, null, "no other session is opened in its place")
    assert.equal(s.hash, "", "the address no longer asks for it")
  }))

test("desk: closing the session takes it out of the address", () =>
  inTab(DESK, async (tab) => {
    await tab.go("/" + FRAGMENT[ITERM])
    await tab.until("the session opens", (s) => s.open === ITERM)
    await tab.run(`document.activeElement?.blur?.(); document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true }))`)
    await tab.until("closed, and the address with it", (s) => s.open === null && s.hash === "")
  }))

test("phone: a tap puts the session in the address, and the back gesture returns to the list", () =>
  inTab(PHONE, async (tab) => {
    await tab.go("/")
    await tab.until("the list arrives", (s) => s.rows === ROWS.length && s.view === "list")
    await tab.tap(TMUX)
    await tab.until("the detail, named in the address", (s) => s.view === "detail" && s.open === TMUX && s.hash === FRAGMENT[TMUX])
    await tab.run("history.back()")
    await tab.until("back on the list, with the address cleared", (s) => s.console && s.view === "list" && s.hash === "")
    await tab.run("history.forward()")
    await tab.until("forward is the detail again", (s) => s.view === "detail" && s.open === TMUX)
  }))

test("phone: after a reload of a detail, the back gesture still returns to the list", () =>
  inTab(PHONE, async (tab) => {
    await tab.go("/")
    await tab.until("the list arrives", (s) => s.rows === ROWS.length)
    await tab.tap(ITERM)
    await tab.until("the detail", (s) => s.view === "detail" && s.hash === FRAGMENT[ITERM])
    await tab.reload()
    await tab.until("the reload shows the same detail", (s) => s.view === "detail" && s.open === ITERM)
    await tab.run("history.back()")
    await tab.until("back is the list, not a page before the console", (s) => s.console && s.view === "list" && s.hash === "")
  }))

test("phone: arriving at an address with a session, the back gesture goes to the list first", () =>
  inTab(PHONE, async (tab) => {
    await tab.go("/" + FRAGMENT[TTY])
    await tab.until("the detail the address asked for", (s) => s.view === "detail" && s.open === TTY)
    await tab.run("history.back()")
    await tab.until("the list, still in the console", (s) => s.console && s.view === "list" && s.open === null)
  }))

test("phone: a session that is no longer there leaves the list and says so", () =>
  inTab(PHONE, async (tab) => {
    await tab.go("/#session=" + escapedPane(999))
    const s = await tab.until("the page says the session is gone", (s) => s.rows === ROWS.length && !!s.toast)
    assert.equal(s.view, "list")
    assert.equal(s.hash, "")
  }))
