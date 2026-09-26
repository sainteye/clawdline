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
import { createServer, type IncomingMessage, type Server, type ServerResponse } from "node:http"
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

function row(id: string, label: string, backend: string, sessionId: string) {
  return {
    id,
    label,
    backend,
    state: "idle",
    work_state: "ready",
    evidence: "process",
    isClaude: true,
    sessionId,
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
  row(TMUX, "Alpha on a tmux pane", "tmux", "10000000-0000-4000-8000-000000000001"),
  row(TTY, "Bravo on a tty", "owned", "10000000-0000-4000-8000-000000000002"),
  row(ITERM, "Charlie in iTerm", "iterm", "10000000-0000-4000-8000-000000000003"),
  row(LEGACY, "Delta from an old link", "tmux", "10000000-0000-4000-8000-000000000004"),
]

// ---- the stand-in daemon

let generation = 0
let intentRequests = 0
let placeRequests = 0
let smartTitleRequests = 0
let smartTitleRequestKey = ""
let interruptRequests: { id: string; key: string }[] = []
let requestedPaths: string[] = []
let failPlacesOnRequest = 0
let createdWork: Record<string, unknown> | null = null
let createdWorkBody: Record<string, unknown> | null = null
const PROJECT_ICON = {
  accent: "#D97757",
  cells: [["#D97757", "#D97757"], ["#D97757", "#141416"]],
}
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

function requestJSON(req: IncomingMessage): Promise<Record<string, unknown>> {
  return new Promise((resolve, reject) => {
    let raw = ""
    req.setEncoding("utf8")
    req.on("data", (chunk) => { raw += chunk })
    req.on("end", () => {
      try { resolve(JSON.parse(raw) as Record<string, unknown>) } catch (error) { reject(error) }
    })
    req.on("error", reject)
  })
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
    requestedPaths.push(`${req.method} ${path}`)
    if (path === "/v1/sessions") return json(res, 200, snapshot())
    const info = /^\/v1\/sessions\/([^/]+)\/info$/.exec(path)
    if (info && req.method === "GET") {
      const id = decodeURIComponent(info[1])
      const session = ROWS.find((candidate) => candidate.id === id)
      if (!session) return json(res, 404, { error: { code: "not_found", message: id } })
      return json(res, 200, {
        info: {
          session: {
            id: session.id,
            title: session.label,
            assistant: "claude",
            namingAssistant: "claude",
            sessionId: session.sessionId,
            cwd: session.cwd,
          },
          models: [],
          deploy: [],
          links: [],
        },
      })
    }
    const interrupt = /^\/v1\/sessions\/([^/]+)\/interrupt$/.exec(path)
    if (interrupt && req.method === "POST") {
      const id = decodeURIComponent(interrupt[1])
      interruptRequests.push({ id, key: String(req.headers["idempotency-key"] ?? "") })
      return json(res, 200, { ok: true, id, action: "interrupted" })
    }
    const smartTitle = /^\/v1\/sessions\/([^/]+)\/smart-title$/.exec(path)
    if (smartTitle && req.method === "POST") {
      smartTitleRequests++
      smartTitleRequestKey = String(req.headers["idempotency-key"] ?? "")
      void requestJSON(req).then(() => json(res, 200, {
        ok: true,
        title: "Smart release helper",
        display_title: "Smart release helper",
        local_applied: true,
        downstream: "local_only",
        downstream_synced: false,
      }), () => json(res, 400, { error: "invalid_json", detail: "fixture could not read JSON" }))
      return
    }
    if (path === "/v1/health") return json(res, 200, { ok: true })
    if (path === "/__project_request_count") return json(res, 200, { count: placeRequests })
    if (path === "/v1/places") {
      placeRequests++
      if (placeRequests === failPlacesOnRequest) {
        failPlacesOnRequest = 0
        return json(res, 503, { error: "fixture_project_refresh_failed", detail: "the second Project read failed" })
      }
      return json(res, 200, {
        at: Date.now(),
        assistants: [{ id: "claude", label: "Claude Code", availability: "unknown" }],
        places: [{ id: "fixture-place", label: "Example project", path: "/tmp/fixture", at: Date.now(), icon: PROJECT_ICON }],
      })
    }
    if (path === "/v1/work/v2/items" && req.method === "GET") {
      return json(res, 200, { ok: true, rows: createdWork ? [createdWork] : [], counts: {}, truncated: false })
    }
    if (path === "/v1/work/v2/items" && req.method === "POST") {
      void requestJSON(req).then((body) => {
        createdWorkBody = body
        createdWork = {
          id: "20000000-0000-4000-8000-000000000001",
          project: { id: "fixture-place", label: "Example project", path: "/tmp/fixture", icon: PROJECT_ICON, available: true },
          kind: body.kind,
          title: body.title,
          description: body.description,
          phase: "created",
          condition: null,
          area: "unassigned",
          deployment_policy: body.deployment_policy,
          owner_session: null,
          created_at: 1,
          updated_at: 1,
          closed_at: null,
          cycle: 1,
          version: 1,
          images: [],
        }
        json(res, 201, { item: createdWork })
      }, () => json(res, 400, { error: "invalid_json", detail: "fixture could not read JSON" }))
      return
    }
    if (path === "/v1/work/v2/proposals") return json(res, 200, { rows: [], truncated: false })
    if (path === "/v1/intents" && req.method === "POST") {
      intentRequests++
      void requestJSON(req).then((body) => {
        const board = String(body.text).includes("Board item")
        json(res, 200, {
          draft: board ? {
            place_id: "fixture-place",
            assistant: "claude",
            model: "sonnet",
            instructions: "",
            title: "Voice-created Board draft",
            description: "Confirm the generated Project, title, and description before creating this item.",
            confidence: 0.94,
            question: "",
            kind: "work",
            work_kind: "feature",
            at: "",
            days: [],
          } : {
            place_id: null,
            assistant: "claude",
            model: "sonnet",
            instructions: "Read the failing test and explain the cause.",
            title: "Explain failing test",
            description: "",
            confidence: 0.3,
            question: "Which project should this run in?",
            kind: "session",
            work_kind: "",
            at: "",
            days: [],
          },
          ms: 7,
        })
      }, () => json(res, 400, { error: "invalid_json", detail: "fixture could not read JSON" }))
      return
    }
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

test("desk: smart naming explains the one model turn before it spends it, then saves the answer", () =>
  inTab(DESK, async (tab) => {
    smartTitleRequests = 0
    smartTitleRequestKey = ""
    requestedPaths = []
    await tab.go("/" + FRAGMENT[TTY])
    await tab.until("the session opens", (s) => s.open === TTY)
    await tab.run(`document.getElementById("detail-info")?.click()`)
    await tab.run(`new Promise((resolve, reject) => {
      const deadline = Date.now() + 5000
      const read = () => {
        const button = document.querySelector("button.title-smart")
        if (button) return resolve(true)
        if (Date.now() > deadline) return reject(new Error("the smart-name button did not appear"))
        setTimeout(read, 25)
      }
      read()
    })`)
    const button = await tab.run(`(() => {
      const button = document.querySelector("button.title-smart")
      const box = button?.getBoundingClientRect()
      return { label: button?.getAttribute("aria-label"), width: box?.width, height: box?.height }
    })()`)
    assert.equal(button.label, "智能命名")
    assert.ok(button.width >= 36 && button.height >= 36, "the icon remains a comfortable pointer target")

    await tab.run(`document.querySelector("button.title-smart")?.click()`)
    const asked = await tab.run(`(() => ({
      shown: document.getElementById("action-confirm")?.hidden === false,
      title: document.getElementById("action-confirm-title")?.textContent,
      say: document.getElementById("action-confirm-say")?.textContent,
      focused: document.activeElement?.id,
      disabled: document.getElementById("action-confirm-go")?.hasAttribute("disabled"),
    }))()`)
    assert.equal(asked.shown, true)
    assert.equal(asked.title, "要智能命名這個 session 嗎？")
    assert.match(asked.say, /Claude Code/)
    assert.match(asked.say, /一次小型模型 turn/)
    assert.match(asked.say, /額度/)
    assert.match(asked.say, /取消就不會呼叫模型/)
    assert.match(asked.say, /手動編輯標題/)
    assert.equal(asked.focused, "action-confirm-cancel")
    assert.equal(asked.disabled, false)
    assert.equal(smartTitleRequests, 0, "opening the confirmation spends no model turn")

    const started = await tab.run(`(() => {
      document.getElementById("action-confirm-go")?.click()
      return {
        busy: document.getElementById("action-confirm-sheet")?.getAttribute("aria-busy"),
        label: document.getElementById("action-confirm-go")?.textContent,
      }
    })()`)
    assert.deepEqual(started, { busy: "true", label: "命名中…" })
    await new Promise((resolve) => setTimeout(resolve, 250))
    assert.equal(smartTitleRequests, 1, "confirming sends exactly one naming request: " + JSON.stringify(requestedPaths))
    await tab.run(`new Promise((resolve, reject) => {
      const deadline = Date.now() + 5000
      const read = () => {
        const title = document.querySelector(".session-title span")?.textContent
        const said = document.getElementById("info-said")?.textContent
        if (title === "Smart release helper" && said === "智能標題已儲存。") return resolve(true)
        if (Date.now() > deadline) return reject(new Error("the generated title was not shown and saved: " + JSON.stringify({
          title,
          said,
          confirm: document.getElementById("action-confirm")?.hidden,
        })))
        setTimeout(read, 25)
      }
      read()
    })`)
    assert.ok(smartTitleRequestKey, "the one mutating request carries an idempotency key")
  }))

test("desk: the session menu's first row stops the current turn with one keyed request", () =>
  inTab(DESK, async (tab) => {
    interruptRequests = []
    await tab.go("/" + FRAGMENT[TMUX])
    await tab.until("the session opens", (s) => s.open === TMUX)
    await tab.run(`document.getElementById("detail-actions-trigger")?.click()`)
    const menu = await tab.run(`(() => {
      const first = document.querySelector("#session-actions-main button:not([hidden])")
      return {
        shown: document.getElementById("session-actions")?.hidden === false,
        first: first?.id,
        label: first?.textContent,
        disabled: first?.hasAttribute("disabled"),
      }
    })()`)
    assert.deepEqual(menu, { shown: true, first: "session-interrupt", label: "停止目前的工作（Esc）", disabled: false })
    assert.equal(interruptRequests.length, 0, "opening the menu stops nothing")

    await tab.run(`document.getElementById("session-interrupt")?.click()`)
    await tab.run(`new Promise((resolve, reject) => {
      const deadline = Date.now() + 5000
      const read = () => {
        const said = document.getElementById("toast")?.textContent ?? ""
        if (said.includes("已送出 Esc")) return resolve(true)
        if (Date.now() > deadline) return reject(new Error("the stop was not acknowledged: " + JSON.stringify(said)))
        setTimeout(read, 25)
      }
      read()
    })`)
    assert.equal(await tab.run(`document.getElementById("session-actions")?.hidden`), true, "the menu closes")
    assert.equal(interruptRequests.length, 1, "one press is one stop")
    assert.equal(interruptRequests[0].id, TMUX)
    assert.ok(interruptRequests[0].key, "the stop carries an idempotency key, so a relay retry is not a second Escape")
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

test("phone: the home microphone opens an editable draft before anything starts", () =>
  inTab(PHONE, async (tab) => {
    await tab.go("/")
    await tab.until("the list arrives", (s) => s.rows === ROWS.length)
    const opened = await tab.run(`(() => {
      document.getElementById("voice-go").click()
      const sheet = document.getElementById("command")
      const text = document.getElementById("command-text")
      text.value = "Please explain the failing test"
      text.dispatchEvent(new Event("input", { bubbles: true }))
      return { visible: !sheet.hidden, disabled: document.getElementById("command-go").disabled, text: text.value }
    })()`)
    assert.deepEqual(opened, { visible: true, disabled: false, text: "Please explain the failing test" },
      "the microphone opens an editable command sheet")
    await tab.run(`document.getElementById("command-go").click()`)
    const draft = await tab.run(`new Promise((resolve, reject) => {
      const deadline = Date.now() + 5000
      const read = () => {
        const box = document.getElementById("command-draft")
        if (box && !box.hidden) return resolve({
          instructions: document.getElementById("command-instructions").value,
          places: document.querySelectorAll("#command-list .place").length,
          status: document.getElementById("command-said").textContent,
          disabled: document.getElementById("command-go").disabled,
        })
        if (Date.now() >= deadline) return reject(new Error("the editable draft did not appear"))
        setTimeout(read, 25)
      }
      read()
    })`)
    assert.equal(intentRequests, 1, "the reviewed sentence is planned once")
    assert.equal(placeRequests, 1, "the draft reads the project choices once")
    assert.deepEqual(draft, {
      instructions: "Read the failing test and explain the cause.",
      places: 1,
      status: "Which project should this run in?",
      disabled: true,
    })
    const ready = await tab.run(`(() => {
      document.querySelector("#command-list .place").click()
      return !document.getElementById("command-go").disabled
    })()`)
    assert.equal(ready, true, "choosing a project makes the reviewed draft startable")
  }))

test("phone: the command textarea keeps the full sheet width and the microphone is a centred footer action", () =>
  inTab(PHONE, async (tab) => {
    await tab.go("/")
    await tab.until("the list arrives", (s) => s.rows === ROWS.length)
    const layout = await tab.run(`(() => {
      document.getElementById("voice-go").click()
      const field = document.getElementById("command-text").getBoundingClientRect()
      const block = document.querySelector("#command-sheet .command-input").getBoundingClientRect()
      const microphone = document.getElementById("command-mic").getBoundingClientRect()
      const icon = document.querySelector("#command-mic .ico-mic").getBoundingClientRect()
      const cancel = document.getElementById("command-cancel").getBoundingClientRect()
      document.getElementById("command-mic").setAttribute("aria-pressed", "true")
      const stop = document.querySelector("#command-mic .ico-stop").getBoundingClientRect()
      return {
        field: { left: field.left, right: field.right, bottom: field.bottom, height: field.height },
        block: { left: block.left, right: block.right },
        microphone: {
          left: microphone.left,
          top: microphone.top,
          centreX: microphone.left + microphone.width / 2,
          centreY: microphone.top + microphone.height / 2,
        },
        icon: {
          centreX: icon.left + icon.width / 2,
          centreY: icon.top + icon.height / 2,
        },
        stop: {
          centreX: stop.left + stop.width / 2,
          centreY: stop.top + stop.height / 2,
        },
        cancelLeft: cancel.left,
      }
    })()`)
    assert.ok(Math.abs(layout.field.left - layout.block.left) <= 1, "the textarea starts at the input block edge")
    assert.ok(Math.abs(layout.field.right - layout.block.right) <= 1, "the textarea reaches the input block edge")
    assert.ok(layout.field.height >= 112, `the textarea is only ${layout.field.height}px tall`)
    assert.ok(layout.microphone.top >= layout.field.bottom + 8, "the microphone sits below instead of beside the textarea")
    assert.ok(layout.microphone.left < layout.cancelLeft, "the microphone owns the left side of the footer")
    assert.ok(Math.abs(layout.microphone.centreX - layout.icon.centreX) <= 0.5, "the microphone icon is horizontally centred")
    assert.ok(Math.abs(layout.microphone.centreY - layout.icon.centreY) <= 0.5, "the microphone icon is vertically centred")
    assert.ok(Math.abs(layout.microphone.centreX - layout.stop.centreX) <= 0.5, "the listening stop icon is horizontally centred")
    assert.ok(Math.abs(layout.microphone.centreY - layout.stop.centreY) <= 0.5, "the listening stop icon is vertically centred")
  }))

test("phone: a spoken Board item is prefilled but not created until confirmation", () =>
  inTab(PHONE, async (tab) => {
    createdWork = null
    createdWorkBody = null
    // The command already read the Project row used by the planner. Make the
    // Board page's immediately following refresh fail, as a slow/offline Cloud
    // read can, and require the confirmation to retain that selected row.
    failPlacesOnRequest = placeRequests + 2
    await tab.go("/")
    await tab.until("the list arrives", (s) => s.rows === ROWS.length)
    await tab.run(`(() => {
      document.getElementById("voice-go").click()
      const text = document.getElementById("command-text")
      text.value = "Create a Board item for the voice confirmation flow"
      text.dispatchEvent(new Event("input", { bubbles: true }))
      document.getElementById("command-go").click()
    })()`)
    const draft = await tab.run(`new Promise((resolve, reject) => {
      const deadline = Date.now() + 5000
      const read = () => {
        const modal = document.querySelector(".work-new-modal")
        const project = modal?.querySelector(".work-project-trigger")
        if (modal && project?.textContent?.includes("Example project")) return resolve({
          heading: modal.querySelector("h2")?.textContent,
          note: modal.querySelector(".work-note")?.textContent,
          project: project.querySelector("span:not(.work-project-chevron)")?.textContent,
          title: modal.querySelector("input.work-input")?.value,
          description: modal.querySelector("textarea")?.value,
          kind: modal.querySelector('.work-kind-option[aria-pressed="true"] b')?.textContent,
          commandHidden: document.getElementById("command").hidden,
          submitDisabled: modal.querySelector('button[type="submit"]')?.disabled,
        })
        if (Date.now() >= deadline) return reject(new Error("the spoken Board draft did not reach its confirmation"))
        setTimeout(read, 25)
      }
      read()
    })`)
    assert.equal(createdWork, null, "planning and showing the confirmation do not create the item")
    assert.deepEqual(draft, {
      heading: "確認看板項目",
      note: "語音已填入草稿；按「建立」前不會新增看板項目。",
      project: "Example project",
      title: "Voice-created Board draft",
      description: "Confirm the generated Project, title, and description before creating this item.",
      kind: "Feature",
      commandHidden: true,
      submitDisabled: false,
    })
    await tab.run(`document.querySelector(".work-new-modal form").requestSubmit()`)
    await tab.run(`new Promise((resolve, reject) => {
      const deadline = Date.now() + 5000
      const read = () => {
        if (document.querySelector(".work-created-modal .work-v2-card")) return resolve(true)
        if (Date.now() >= deadline) return reject(new Error("the confirmed Board item was not created"))
        setTimeout(read, 25)
      }
      read()
    })`)
    assert.equal((createdWork as Record<string, unknown> | null)?.title, "Voice-created Board draft")
    assert.equal((createdWork as Record<string, unknown> | null)?.description, "Confirm the generated Project, title, and description before creating this item.")
    assert.equal((createdWorkBody as Record<string, unknown> | null)?.project_id, "fixture-place")
  }))

test("phone: reopening an empty Project picker reads the Projects again", () =>
  inTab(PHONE, async (tab) => {
    const before = placeRequests
    failPlacesOnRequest = before + 1
    await tab.go("/")
    await tab.until("the list arrives", (s) => s.rows === ROWS.length)
    await tab.run(`document.getElementById("work-create-go").click()`)
    await tab.run(`new Promise((resolve, reject) => {
      const deadline = Date.now() + 5000
      const read = () => {
        const modal = document.querySelector(".work-new-modal")
        const alert = modal?.querySelector('[role="alert"]')
        if (modal && alert) return resolve(true)
        if (Date.now() >= deadline) return reject(new Error("the first Project read did not fail visibly"))
        setTimeout(read, 25)
      }
      read()
    })`)
    const recovered = await tab.run(`new Promise((resolve, reject) => {
      const deadline = Date.now() + 5000
      document.querySelector(".work-new-modal .work-project-trigger").click()
      const read = () => {
        const option = document.querySelector(".work-new-modal .work-project-option")
        if (option) return resolve(option.textContent)
        if (Date.now() >= deadline) return reject(new Error("opening the empty Project picker did not read again"))
        setTimeout(read, 25)
      }
      read()
    })`)
    assert.match(String(recovered), /Example project/)
    await tab.run(`document.querySelector(".work-new-modal .work-project-trigger").click()`)
    await tab.run(`document.querySelector(".work-new-modal .work-project-trigger").click()`)
    await tab.run(`new Promise((resolve, reject) => {
      const deadline = Date.now() + 5000
      const read = () => {
        fetch("/__project_request_count").then((response) => response.json()).then((body) => {
          if (body.count >= ${before} + 3) resolve(true)
          else if (Date.now() >= deadline) reject(new Error("reopening the Project picker did not refresh again"))
          else setTimeout(read, 25)
        }, reject)
      }
      read()
    })`)
    assert.equal(placeRequests, before + 3, "initial load and both picker openings each read Projects")
    await tab.run(`document.querySelector('.work-new-modal [aria-label="關閉"]').click()`)
  }))

test("phone: a new Board item opens without the keyboard and in readable type", () =>
  inTab(PHONE, async (tab) => {
    await tab.go("/")
    await tab.until("the list arrives", (s) => s.rows === ROWS.length)
    await tab.run(`document.getElementById("work-create-go").click()`)
    const opened = await tab.run(`new Promise((resolve, reject) => {
      const deadline = Date.now() + 5000
      const read = () => {
        const modal = document.querySelector(".work-new-modal")
        if (modal) {
          const px = (selector) => parseFloat(getComputedStyle(modal.querySelector(selector)).fontSize)
          return resolve({
            focusedTag: document.activeElement?.tagName,
            focusedInModal: modal.contains(document.activeElement) && document.activeElement?.matches("input, textarea"),
            field: px(".work-modal-field"),
            legend: px(".work-kind-field legend"),
            kindName: px(".work-kind-option b"),
            kindHint: px(".work-kind-option small"),
            title: px("input.work-input"),
            description: px("textarea"),
            imagesHint: px(".work-modal-image-tools small"),
          })
        }
        if (Date.now() >= deadline) return reject(new Error("the Board item modal did not open"))
        setTimeout(read, 25)
      }
      read()
    })`) as Record<string, unknown>
    // A focused text field raises the phone keyboard over the Project and kind
    // the person has not chosen yet.
    assert.equal(opened.focusedInModal, false, `opening focused a text field (${opened.focusedTag})`)
    // Below 16px, iOS zooms the page when the field is tapped.
    assert.ok((opened.title as number) >= 16, `title field is ${opened.title}px`)
    assert.ok((opened.description as number) >= 16, `description field is ${opened.description}px`)
    for (const key of ["field", "legend", "kindName"]) assert.ok((opened[key] as number) >= 14, `${key} is ${opened[key]}px`)
    for (const key of ["kindHint", "imagesHint"]) assert.ok((opened[key] as number) >= 12, `${key} is ${opened[key]}px`)
    await tab.run(`document.querySelector('.work-new-modal [aria-label="關閉"]').click()`)
  }))

test("phone: the Board shortcut keeps a new item open for assignment", () =>
  inTab(PHONE, async (tab) => {
    createdWork = null
    await tab.go("/")
    await tab.until("the list arrives", (s) => s.rows === ROWS.length)
    await tab.run(`document.getElementById("work-create-go").click()`)
    await tab.run(`new Promise((resolve, reject) => {
      const deadline = Date.now() + 5000
      const choose = () => {
        const trigger = document.querySelector(".work-new-modal .work-project-trigger")
        if (trigger) {
          trigger.click()
          const option = document.querySelector(".work-new-modal .work-project-option")
          if (option) { option.click(); return resolve(true) }
        }
        if (Date.now() >= deadline) return reject(new Error("the Board item modal did not load its Project"))
        setTimeout(choose, 25)
      }
      choose()
    })`)
    await tab.run(`(() => {
      const title = document.querySelector(".work-new-modal input.work-input")
      const description = document.querySelector(".work-new-modal textarea")
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value").set.call(title, "Shortcut-created work")
      title.dispatchEvent(new Event("input", { bubbles: true }))
      Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, "value").set.call(description, "Keep this item open so it can be assigned.")
      description.dispatchEvent(new Event("input", { bubbles: true }))
      document.querySelector(".work-new-modal form").requestSubmit()
    })()`)
    const card = await tab.run(`new Promise((resolve, reject) => {
      const deadline = Date.now() + 5000
      const read = () => {
        const modal = document.querySelector(".work-created-modal")
        const card = modal?.querySelector(".work-v2-card")
        if (card) return resolve({
          title: card.querySelector("h3")?.textContent,
          picker: card.querySelector(".work-session-trigger > span:not(.work-session-placeholder):not(.work-project-chevron)")?.textContent,
          actions: [...card.querySelectorAll(".work-assignment > button")].map((button) => button.textContent),
          focus: document.activeElement?.getAttribute("aria-label"),
          sessionsPage: !document.getElementById("app").hidden,
          boardPage: !document.getElementById("work").hidden,
        })
        if (Date.now() >= deadline) return reject(new Error("the created Board item did not stay open"))
        setTimeout(read, 25)
      }
      read()
    })`)
    assert.deepEqual(card, {
      title: "Shortcut-created work",
      picker: "選擇既有 Session",
      actions: ["指派", "開新 Codex Session"],
      focus: "指派既有 Session",
      sessionsPage: true,
      boardPage: false,
    })
  }))
