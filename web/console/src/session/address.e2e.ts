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
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, statSync, writeFileSync } from "node:fs"
import { tmpdir } from "node:os"
import { dirname, extname, join, normalize, resolve } from "node:path"
import { fileURLToPath } from "node:url"

const dist = resolve(dirname(fileURLToPath(import.meta.url)), "../../dist")
const shots = process.env.CLAWDLINE_SHOTS || ""
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
let machineSettings: Record<string, unknown> = {
  codex_default_model: "",
  claude_default_model: "",
  models: {
    codex: [
      { value: "gpt-6-sol", label: "GPT-6 Sol" },
      { value: "gpt-5.6-sol", label: "GPT-5.6 Sol" },
    ],
    claude: [
      { value: "opus", label: "Opus 5.5" },
      { value: "fable", label: "Fable 5.1" },
      { value: "sonnet", label: "Sonnet 5" },
      { value: "haiku", label: "Haiku 4.5" },
      { value: "claude-fable-5", label: "Fable 5" },
      { value: "opus5", label: "Opus 5" },
      { value: "opus48", label: "Opus 4.8" },
      { value: "opus47", label: "Opus 4.7" },
      { value: "opus46", label: "Opus 4.6" },
      { value: "opus45", label: "Opus 4.5" },
    ],
  },
}
let refuseDefaultModelWrite = false
let smartTitleRequestKey = ""
let smartTitleRefusal = ""
let namingAssistant = "claude"
let smartTitleNamedBy = ""
let interruptRequests: { id: string; key: string }[] = []
let requestedPaths: string[] = []
let failPlacesOnRequest = 0
// The machine's persona catalog, as many roles as the daemon compiles in and
// with its longest names: the assignment row lays every one of them out on a
// phone. The created item's words and kind never preselect a role.
const PERSONA_NAMES: [string, string, string][] = [
  ["architect", "Architect", "架構師"],
  ["backend", "Backend Engineer", "後端工程師"],
  ["frontend", "Frontend Engineer", "前端工程師"],
  ["security", "Security Engineer", "資安工程師"],
  ["code-reviewer", "Code Reviewer", "程式碼審查員"],
  ["reality-checker", "Reality Checker", "驗證員"],
  ["technical-writer", "Technical Writer", "技術文件寫手"],
  ["minimal-change", "Minimal-Change Engineer", "最小改動工程師"],
]
const PERSONAS = PERSONA_NAMES.map(([id, en, zh]) => ({
  id,
  name: { en, "zh-Hant": zh },
  summary: { en, "zh-Hant": zh },
  icon: { accent: "#e07a5f", cells: Array.from({ length: 7 }, () => Array.from({ length: 8 }, () => "#e07a5f")) },
  source: "fixture",
  suggested_kinds: [],
}))
let createdWork: Record<string, unknown> | null = null
let createdWorkBody: Record<string, unknown> | null = null
let personaSuggestionRequests = 0
let personaSuggestionKey = ""
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
    if (path === "/v1/settings/default-models" && req.method === "GET") return json(res, 200, machineSettings)
    if (path === "/v1/settings/default-models" && req.method === "POST") {
      void requestJSON(req).then((change) => {
        if (refuseDefaultModelWrite) {
          refuseDefaultModelWrite = false
          return json(res, 503, { error: "settings_unavailable", detail: "The settings file is unavailable." })
        }
        const model = Object.values(change)[0]
        if (typeof model !== "string" || (model !== "" && !/^[a-z0-9][a-z0-9._-]{0,63}$/.test(model))) {
          return json(res, 400, { error: "invalid_default_model", detail: "That is not a model name." })
        }
        machineSettings = { ...machineSettings, ...change }
        return json(res, 200, machineSettings)
      }, () => json(res, 400, { error: "bad_request", detail: "That is not JSON." }))
      return
    }
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
            namingAssistant,
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
      if (smartTitleRefusal) {
        return json(res, 503, { error: smartTitleRefusal, detail: "fixture naming assistant refused" })
      }
      void requestJSON(req).then(() => json(res, 200, {
        ok: true,
        title: "Smart release helper",
        display_title: "Smart release helper",
        local_applied: true,
        downstream: "local_only",
        downstream_synced: false,
        ...(smartTitleNamedBy ? { named_by: smartTitleNamedBy } : {}),
      }), () => json(res, 400, { error: "invalid_json", detail: "fixture could not read JSON" }))
      return
    }
    if (path === "/v1/health") return json(res, 200, { ok: true })
    if (path === "/v1/personas") return json(res, 200, { license: "MIT", personas: PERSONAS })
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
        places: [{
          id: "fixture-place", label: "Example project", path: "/tmp/fixture", at: Date.now(), icon: PROJECT_ICON,
          repo: "github.com/example/project",
          setup: { icon: "generated", deploy: "ready", deploy_activity: "running", servers: "missing", server_count: 0, sync: "ready" },
        }],
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
    if (/^\/v1\/work\/v2\/items\/[^/]+\/persona-suggestion$/.test(path) && req.method === "POST") {
      personaSuggestionRequests++
      personaSuggestionKey = String(req.headers["idempotency-key"] ?? "")
      void requestJSON(req).then((body) => {
        if (body.expected_version !== createdWork?.version) {
          return json(res, 409, { error: "version_conflict", detail: "fixture item changed" })
        }
        json(res, 200, { ok: true, outcome: "recommend", persona_id: "backend", provider: "codex" })
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

  async press(key: string, code = key): Promise<void> {
    const virtual = key === "Tab" ? 9 : key === "Enter" ? 13 : 0
    const event = { key, code, windowsVirtualKeyCode: virtual, nativeVirtualKeyCode: virtual }
    await this.b.send("Input.dispatchKeyEvent", { type: "rawKeyDown", ...event }, this.session)
    await this.b.send("Input.dispatchKeyEvent", { type: "keyUp", ...event }, this.session)
  }

  async screenshot(path: string): Promise<void> {
    const { data } = await this.b.send("Page.captureScreenshot", { format: "png", fromSurface: true }, this.session)
    writeFileSync(path, Buffer.from(data, "base64"))
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

  async view(size: { width: number; height: number; mobile: boolean }, scheme: "light" | "dark"): Promise<void> {
    await this.b.send("Emulation.setDeviceMetricsOverride", { ...size, deviceScaleFactor: 1 }, this.session)
    await this.b.send("Emulation.setEmulatedMedia", {
      features: [{ name: "prefers-color-scheme", value: scheme }],
    }, this.session)
  }

  async shot(name: string): Promise<void> {
    if (!shots) return
    await new Promise((r) => setTimeout(r, 150))
    const { data } = await this.b.send("Page.captureScreenshot", { format: "png" }, this.session)
    writeFileSync(join(shots, name + ".png"), Buffer.from(data, "base64"))
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

test("phone: Settings changes, remembers and restores the browser's text size", async () => {
  const evidence = process.env.CLAWDLINE_FONT_SCREENSHOT_DIR || ""
  if (evidence) mkdirSync(evidence, { recursive: true })

  await inTab(PHONE, async (tab) => {
    await tab.go("/#page=settings")
    await tab.run(`new Promise((resolve, reject) => {
      const deadline = Date.now() + 5000
      const read = () => {
        const page = document.getElementById("settings")
        const value = document.querySelector(".font-scale-value")
        if (page && !page.hidden && value?.textContent === "100%") return resolve(true)
        if (Date.now() > deadline) return reject(new Error("the text-size setting did not appear"))
        setTimeout(read, 25)
      }
      read()
    })`)
    const initial = await tab.run(`(() => {
      const title = document.getElementById("settings-font-scale-title")
      const smaller = document.getElementById("settings-font-scale-smaller")
      const larger = document.getElementById("settings-font-scale-larger")
      const reset = document.getElementById("settings-font-scale-reset")
      const box = (element) => element.getBoundingClientRect()
      return {
        titleHeight: box(title).height,
        adjust: getComputedStyle(document.body).webkitTextSizeAdjust,
        controls: [smaller, larger, reset].map((element) => ({ width: box(element).width, height: box(element).height })),
        scrollsSideways: document.documentElement.scrollWidth > document.documentElement.clientWidth,
      }
    })()`)
    assert.equal(initial.adjust, "100%")
    assert.equal(initial.scrollsSideways, false)
    for (const control of initial.controls) {
      assert.ok(control.width >= 44, `a text-size control is only ${control.width}px wide`)
      assert.ok(control.height >= 44, `a text-size control is only ${control.height}px tall`)
    }

    await tab.run(`document.getElementById("settings-font-scale-smaller").focus()`)
    await tab.press("Tab")
    assert.equal(await tab.run(`document.activeElement?.id`), "settings-font-scale-larger")
    await tab.run(`document.activeElement?.click()`)
    assert.equal(await tab.run(`document.querySelector(".font-scale-value")?.textContent`), "110%")
    await tab.press("Tab")
    assert.equal(await tab.run(`document.activeElement?.id`), "settings-font-scale-reset")

    await tab.run(`(() => {
      const plus = document.getElementById("settings-font-scale-larger")
      for (let percent = 120; percent <= 150; percent += 10) plus.click()
    })()`)
    const largest = await tab.run(`(() => {
      const page = document.getElementById("settings")
      const title = document.getElementById("settings-font-scale-title")
      const plus = document.getElementById("settings-font-scale-larger")
      const row = document.querySelector(".font-scale-row")
      const rowBox = row.getBoundingClientRect()
      return {
        value: document.querySelector(".font-scale-value")?.textContent,
        stored: localStorage.getItem("clawdline.font-scale"),
        adjust: getComputedStyle(document.body).webkitTextSizeAdjust,
        titleHeight: title.getBoundingClientRect().height,
        plusDisabled: plus.disabled,
        pageScrollsSideways: page.scrollWidth > page.clientWidth,
        documentScrollsSideways: document.documentElement.scrollWidth > document.documentElement.clientWidth,
        rowInsideViewport: rowBox.left >= 0 && rowBox.right <= innerWidth,
      }
    })()`)
    assert.deepEqual(largest, {
      value: "150%",
      stored: "150",
      adjust: "150%",
      titleHeight: largest.titleHeight,
      plusDisabled: true,
      pageScrollsSideways: false,
      documentScrollsSideways: false,
      rowInsideViewport: true,
    })
    assert.ok(largest.titleHeight > initial.titleHeight, "150% makes the setting's words visibly taller")
    if (evidence) await tab.screenshot(join(evidence, "phone-390-text-150.png"))

    await tab.reload()
    await tab.run(`new Promise((resolve, reject) => {
      const deadline = Date.now() + 5000
      const read = () => {
        const value = document.querySelector(".font-scale-value")?.textContent
        if (value === "150%") return resolve(true)
        if (Date.now() > deadline) return reject(new Error("the remembered text size did not return: " + value))
        setTimeout(read, 25)
      }
      read()
    })`)
    assert.equal(await tab.run(`getComputedStyle(document.body).webkitTextSizeAdjust`), "150%")
    await tab.run(`document.getElementById("settings-font-scale-reset").click()`)
  })

  await inTab(DESK, async (tab) => {
    await tab.go("/#page=settings")
    await tab.run(`new Promise((resolve, reject) => {
      const deadline = Date.now() + 5000
      const read = () => {
        const value = document.querySelector(".font-scale-value")?.textContent
        if (value === "100%") return resolve(true)
        if (Date.now() > deadline) return reject(new Error("the desktop setting did not appear: " + value))
        setTimeout(read, 25)
      }
      read()
    })`)
    const layout = await tab.run(`(() => ({
      scrollsSideways: document.documentElement.scrollWidth > document.documentElement.clientWidth,
      background: getComputedStyle(document.body).backgroundColor,
      focused: document.activeElement?.id,
      fontScaleHidden: getComputedStyle(document.getElementById("settings-font-scale")).display === "none",
    }))()`)
    assert.equal(layout.scrollsSideways, false)
    assert.equal(layout.background, "rgb(14, 14, 17)")
    assert.equal(layout.focused, "settings-close")
    assert.equal(layout.fontScaleHidden, true, "the mobile-only setting does not promise an effect on a desk")
    if (evidence) await tab.screenshot(join(evidence, "desktop-1280-text-100.png"))
  })
})

test("Settings selects provider model defaults, fits a phone, and restores a refused choice", async () => {
  const evidence = process.env.CLAWDLINE_DEFAULT_MODEL_SCREENSHOT_DIR || ""
  if (evidence) mkdirSync(evidence, { recursive: true })
  machineSettings = {
    codex_default_model: "",
    claude_default_model: "",
    models: {
      codex: [
        { value: "gpt-6-sol", label: "GPT-6 Sol" },
        { value: "gpt-5.6-sol", label: "GPT-5.6 Sol" },
      ],
      claude: [
        { value: "opus", label: "Opus 5.5" },
        { value: "fable", label: "Fable 5.1" },
        { value: "sonnet", label: "Sonnet 5" },
        { value: "haiku", label: "Haiku 4.5" },
        { value: "claude-fable-5", label: "Fable 5" },
        { value: "opus5", label: "Opus 5" },
        { value: "opus48", label: "Opus 4.8" },
        { value: "opus47", label: "Opus 4.7" },
        { value: "opus46", label: "Opus 4.6" },
        { value: "opus45", label: "Opus 4.5" },
      ],
    },
  }

  await inTab(PHONE, async (tab) => {
    await tab.go("/#page=settings")
    await tab.run(`new Promise((resolve, reject) => {
      const deadline = Date.now() + 5000
      const read = () => {
        const select = document.getElementById("settings-codex-default-model")
        if (select && !select.disabled) return resolve(true)
        if (Date.now() > deadline) return reject(new Error("the default-model setting did not appear"))
        setTimeout(read, 25)
      }
      read()
    })`)
    const phone = await tab.run(`(() => {
      const block = document.getElementById("settings-default-models")
      const codex = document.getElementById("settings-codex-default-model")
      const claude = document.getElementById("settings-claude-default-model")
      return {
        scrollsSideways: block.scrollWidth > block.clientWidth || document.documentElement.scrollWidth > innerWidth,
        boardAIConsentControl: !!document.getElementById("settings-board-ai-toggle"),
        fields: [codex, claude].map((select) => ({
          tag: select.tagName,
          width: select.getBoundingClientRect().width,
          fontSize: getComputedStyle(select).fontSize,
          label: document.querySelector('label[for="' + select.id + '"]')?.textContent,
          options: [...select.options].map((option) => [option.value, option.textContent]),
        })),
      }
    })()`)
    assert.equal(phone.scrollsSideways, false)
    assert.equal(phone.boardAIConsentControl, false)
    assert.equal(phone.fields.length, 2)
    for (const field of phone.fields) {
      assert.equal(field.tag, "SELECT")
      assert.ok(field.width <= PHONE.width, `a model field is ${field.width}px wide`)
      assert.equal(field.fontSize, "16px")
      assert.ok(field.label, "every model field has a visible label")
    }
    assert.deepEqual(phone.fields[0].options, [
      ["", "由助理決定"],
      ["gpt-6-sol", "GPT-6 Sol"],
      ["gpt-5.6-sol", "GPT-5.6 Sol"],
    ])
    assert.deepEqual(phone.fields[1].options, [
      ["", "由助理決定"],
      ["opus", "Opus 5.5"],
      ["fable", "Fable 5.1"],
      ["sonnet", "Sonnet 5"],
      ["haiku", "Haiku 4.5"],
      ["claude-fable-5", "Fable 5"],
      ["opus5", "Opus 5"],
      ["opus48", "Opus 4.8"],
      ["opus47", "Opus 4.7"],
      ["opus46", "Opus 4.6"],
      ["opus45", "Opus 4.5"],
    ])

    await tab.run(`(() => {
      const select = document.getElementById("settings-codex-default-model")
      select.value = "gpt-6-sol"
      select.dispatchEvent(new Event("change", { bubbles: true }))
    })()`)
    await tab.run(`new Promise((resolve, reject) => {
      const deadline = Date.now() + 5000
      const read = () => {
        const said = document.querySelector("#settings-default-models .said")?.textContent || ""
        if (said.includes("已儲存")) return resolve(true)
        if (Date.now() > deadline) return reject(new Error("the saved state did not arrive: " + said))
        setTimeout(read, 25)
      }
      read()
    })`)
    assert.equal(machineSettings.codex_default_model, "gpt-6-sol")

    refuseDefaultModelWrite = true
    await tab.run(`(() => {
      const select = document.getElementById("settings-codex-default-model")
      select.value = "gpt-5.6-sol"
      select.dispatchEvent(new Event("change", { bubbles: true }))
    })()`)
    await tab.run(`new Promise((resolve, reject) => {
      const deadline = Date.now() + 5000
      const read = () => {
        const select = document.getElementById("settings-codex-default-model")
        const said = document.querySelector("#settings-default-models .said")?.textContent || ""
        if (select.value === "gpt-6-sol" && said.includes("失敗")) return resolve(true)
        if (Date.now() > deadline) return reject(new Error("the refused value was not restored: " + select.value + " / " + said))
        setTimeout(read, 25)
      }
      read()
    })`)
    if (evidence) await tab.screenshot(join(evidence, "phone-default-models.png"))
  })

  await inTab(DESK, async (tab) => {
    await tab.go("/#page=settings")
    await tab.run(`new Promise((resolve, reject) => {
      const deadline = Date.now() + 5000
      const read = () => {
        const select = document.getElementById("settings-codex-default-model")
        if (select && !select.disabled) return resolve(true)
        if (Date.now() > deadline) return reject(new Error("the desktop default-model setting did not appear"))
        setTimeout(read, 25)
      }
      read()
    })`)
    const desk = await tab.run(`(() => ({
      value: document.getElementById("settings-codex-default-model").value,
      focused: document.activeElement?.id,
      scrollsSideways: document.documentElement.scrollWidth > innerWidth,
    }))()`)
    assert.deepEqual(desk, { value: "gpt-6-sol", focused: "settings-close", scrollsSideways: false })
    if (evidence) await tab.screenshot(join(evidence, "desktop-default-models.png"))
  })
})

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
        spinning: !!document.querySelector("#action-confirm-go .busy canvas"),
      }
    })()`)
    assert.deepEqual(started, { busy: "true", label: "命名中…", spinning: true })
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

test("desk: an exhausted naming account is named, not reported as an unreadable session", () =>
  inTab(DESK, async (tab) => {
    smartTitleRequests = 0
    smartTitleRefusal = "namer_out_of_quota"
    try {
      await tab.go("/" + FRAGMENT[TTY])
      await tab.until("the session opens", (s) => s.open === TTY)
      await tab.run(`document.getElementById("detail-info")?.click()`)
      await tab.run(`new Promise((resolve, reject) => {
        const deadline = Date.now() + 5000
        const read = () => {
          const button = document.querySelector("button.title-smart")
          if (button && !button.disabled) return resolve(true)
          if (Date.now() > deadline) return reject(new Error("the smart-name button did not appear"))
          setTimeout(read, 25)
        }
        read()
      })`)
      await tab.run(`document.querySelector("button.title-smart")?.click()`)
      await tab.run(`document.getElementById("action-confirm-go")?.click()`)
      const said = await tab.run(`new Promise((resolve, reject) => {
        const deadline = Date.now() + 5000
        const read = () => {
          const said = document.getElementById("info-said")?.textContent || ""
          if (said.includes("namer_out_of_quota")) return resolve(said)
          if (Date.now() > deadline) return reject(new Error("the refusal was not shown: " + JSON.stringify(said)))
          setTimeout(read, 25)
        }
        read()
      })`)
      assert.equal(smartTitleRequests, 1)
      assert.match(said, /Claude Code 的額度已用完/)
      assert.match(said, /自動命名新的 session/)
      assert.doesNotMatch(said, /讀不到這個 session 的資訊/)
    } finally {
      smartTitleRefusal = ""
    }
  }))

/** Opens Session Info, presses smart naming and confirms; returns the confirmation and what the card then said. */
async function smartNameOnce(tab: Tab, until: string): Promise<{ say: string; said: string }> {
  await tab.go("/" + FRAGMENT[TTY])
  await tab.until("the session opens", (s) => s.open === TTY)
  await tab.run(`document.getElementById("detail-info")?.click()`)
  await tab.run(`new Promise((resolve, reject) => {
    const deadline = Date.now() + 5000
    const read = () => {
      const button = document.querySelector("button.title-smart")
      if (button && !button.disabled) return resolve(true)
      if (Date.now() > deadline) return reject(new Error("the smart-name button did not appear"))
      setTimeout(read, 25)
    }
    read()
  })`)
  await tab.run(`document.querySelector("button.title-smart")?.click()`)
  const say = await tab.run(`document.getElementById("action-confirm-say")?.textContent || ""`)
  await tab.run(`document.getElementById("action-confirm-go")?.click()`)
  const said = await tab.run(`new Promise((resolve, reject) => {
    const deadline = Date.now() + 5000
    const read = () => {
      const said = document.getElementById("info-said")?.textContent || ""
      if (said.includes(${JSON.stringify(until)})) return resolve(said)
      if (Date.now() > deadline) return reject(new Error("the card did not say it: " + JSON.stringify(said)))
      setTimeout(read, 25)
    }
    read()
  })`)
  return { say, said }
}

test("desk: automatic naming says it may use Codex, and names who answered", () =>
  inTab(DESK, async (tab) => {
    namingAssistant = "auto"
    smartTitleNamedBy = "codex"
    try {
      const { say, said } = await smartNameOnce(tab, "由 Codex 命名")
      assert.match(say, /Claude Code/)
      assert.match(say, /額度用完或沒安裝時，才改交給 Codex/)
      assert.equal(said, "智能標題已儲存（由 Codex 命名）。")
    } finally {
      namingAssistant = "claude"
      smartTitleNamedBy = ""
    }
  }))

test("desk: automatic naming with both accounts exhausted names both", () =>
  inTab(DESK, async (tab) => {
    namingAssistant = "auto"
    smartTitleRefusal = "namer_out_of_quota"
    try {
      const { said } = await smartNameOnce(tab, "namer_out_of_quota")
      assert.match(said, /Claude Code 和 Codex 的額度都已用完/)
      assert.doesNotMatch(said, /讀不到這個 session 的資訊/)
    } finally {
      namingAssistant = "claude"
      smartTitleRefusal = ""
    }
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

test("phone: Projects opens a reviewable setup Session without using the intent planner", () =>
  inTab(PHONE, async (tab) => {
    const plannedBefore = intentRequests
    await tab.go("/#page=projects")
    const reviewed = await tab.run(`new Promise((resolve, reject) => {
      const deadline = Date.now() + 5000
      const open = () => {
        const management = document.querySelector(".project-tools")
        const launcher = document.querySelector(".project-setup")
        const trigger = launcher?.querySelector(".project-setup-open")
        const dialog = launcher?.querySelector(".project-setup-dialog")
        const readiness = dialog?.querySelector(".project-readiness-card")
        const button = readiness?.querySelector(".project-readiness-action")
        if (!management || !launcher || !trigger || !dialog || !readiness || !button) {
          if (Date.now() >= deadline) return reject(new Error("the Project setup launcher did not load its Projects"))
          return setTimeout(open, 25)
        }
        const managementBox = management.getBoundingClientRect()
        const managementWasClosed = !management.open
        management.open = true
        const launcherBox = launcher.getBoundingClientRect()
        const groupedTools = management.querySelectorAll(":scope > .project-tools-body > .project-setup, :scope > .project-tools-body > .project-sync, :scope > .project-tools-body > .project-icon-copy").length
        const topLevelGroups = document.querySelectorAll("#project-icon-copy-host > .project-tools").length
        const inlineCards = launcher.querySelectorAll(":scope > .project-readiness-list").length
        trigger.click()
        setTimeout(() => {
          const dialogBox = dialog.getBoundingClientRect()
          const close = dialog.querySelector(".project-setup-close")
          const closeStyle = getComputedStyle(close)
          const actionBox = readiness.getBoundingClientRect()
          const buttonBox = button.getBoundingClientRect()
          const capabilities = readiness.querySelectorAll(".project-readiness-capability").length
          const initialFocus = document.activeElement === dialog
          close.click()
          const closed = () => {
            if (dialog.open || document.activeElement !== trigger) {
              if (Date.now() >= deadline) return reject(new Error("the Project setup dialog did not close"))
              return setTimeout(closed, 25)
            }
            trigger.click()
            button.click()
            waitForDraft(true, initialFocus, closeStyle.outlineStyle, dialogBox, actionBox, buttonBox, capabilities, launcherBox, inlineCards)
          }
          closed()
        }, 0)
        const waitForDraft = (returnedFocus, initialFocus, closeOutlineStyle, dialogBox, actionBox, buttonBox, capabilities, launcherBox, inlineCards) => {
          const wait = () => {
            const draft = document.getElementById("command-draft")
            if (draft && !draft.hidden) return resolve({
              pageHidden: document.getElementById("projects").hidden,
              instructions: document.getElementById("command-instructions").value,
              picked: document.querySelector('#command-list .place[aria-pressed="true"]')?.dataset.id,
              focused: document.activeElement?.id,
              buttonWidth: buttonBox.width,
              cardWidth: actionBox.width,
              capabilities,
              launcherHeight: launcherBox.height,
              managementHeight: managementBox.height,
              managementWasClosed,
              groupedTools,
              topLevelGroups,
              inlineCards,
              returnedFocus,
              initialFocus,
              closeOutlineStyle,
              dialog: {
                left: dialogBox.left,
                top: dialogBox.top,
                right: dialogBox.right,
                bottom: dialogBox.bottom,
              },
              viewport: { width: innerWidth, height: innerHeight },
            })
            if (Date.now() >= deadline) return reject(new Error("the Project setup review did not open"))
            setTimeout(wait, 25)
          }
          wait()
        }
      }
      open()
    })`)
    assert.equal(intentRequests, plannedBefore, "a known Project and task do not need the paid intent planner")
    assert.equal(reviewed.pageHidden, false)
    assert.equal(reviewed.picked, "fixture-place")
    assert.equal(reviewed.focused, "command-instructions", "keyboard review starts on the editable instructions")
    assert.match(reviewed.instructions, /clawdline guide zh-TW project/)
    assert.match(reviewed.instructions, /deploy／CI/)
    assert.match(reviewed.instructions, /\.devstack\.json/)
    assert.equal(reviewed.managementWasClosed, true, "Project maintenance is secondary on arrival")
    assert.ok(reviewed.managementHeight <= 82, `the collapsed management group is ${reviewed.managementHeight}px tall`)
    assert.equal(reviewed.topLevelGroups, 1, "the three maintenance tools have one top-level group")
    assert.equal(reviewed.groupedTools, 3, "health, sync and icon tools stay together inside the group")
    assert.equal(reviewed.inlineCards, 0, "the Project page does not repeat the readiness list in its main flow")
    assert.ok(reviewed.launcherHeight <= 110, `the compact launcher is ${reviewed.launcherHeight}px tall`)
    assert.equal(reviewed.initialFocus, true, "opening the audit focuses its labelled dialog instead of outlining Close")
    assert.equal(reviewed.closeOutlineStyle, "none", "the touch-opened Close control has no accent frame")
    assert.equal(reviewed.returnedFocus, true, "closing the audit returns keyboard focus to its launcher")
    assert.ok(reviewed.dialog.left <= 1 && reviewed.dialog.top <= 1, "the phone audit starts at the viewport edge")
    assert.ok(reviewed.dialog.right >= reviewed.viewport.width - 1, "the phone audit spans the viewport width")
    assert.ok(reviewed.dialog.bottom >= reviewed.viewport.height - 1, "the phone audit spans the viewport height")
    assert.equal(reviewed.capabilities, 4, "the phone shows each configuration layer")
    assert.ok(reviewed.buttonWidth >= reviewed.cardWidth - 30, "the primary phone action keeps the card width")
  }))

test("phone: a spoken Board item is prefilled but not created until confirmation", () =>
  inTab(PHONE, async (tab) => {
    createdWork = null
    personaSuggestionRequests = 0
    personaSuggestionKey = ""
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

test("phone and desktop: the Board shortcut keeps a new item open for assignment", () =>
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
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value").set.call(title, "React console shortcut")
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
        if (card) {
          const roles = card.querySelector(".work-new-persona")
          // RoleRow fills its native buttons in an effect after the card's
          // first paint. Measure only once that accessible row is complete.
          if (roles?.querySelectorAll(".chip").length !== ${PERSONAS.length + 1}) {
            if (Date.now() >= deadline) return reject(new Error("the created Board item's roles did not load"))
            return setTimeout(read, 25)
          }
          return resolve({
            fits: (() => {
            // Every edge the person sees stays inside the viewport: the
            // panel, and each control of the assignment row inside the panel.
            // The role chips scroll sideways within their own row, so the row
            // is measured, not the chips it has scrolled out of view.
            const panel = modal.querySelector(".work-created-panel").getBoundingClientRect()
            const firstRole = roles?.firstElementChild?.getBoundingClientRect()
            const roleRow = roles?.getBoundingClientRect()
            const outside = [...card.querySelectorAll(".work-assignment-action, .work-persona-ai-button, .work-assignment-route > .work-new-session")]
              .map((el) => el.getBoundingClientRect())
              .filter((box) => box.left < panel.left - 0.5 || box.right > panel.right + 0.5)
            return {
              panelInView: panel.left >= 0 && panel.right <= innerWidth,
              modalScrollsSideways: modal.scrollWidth > modal.clientWidth,
              controlsOutsidePanel: outside.length,
              personaChips: roles.querySelectorAll(".chip").length,
              rolesScrollSideways: roles.scrollWidth > roles.clientWidth,
              roleEdgeRoom: firstRole && roleRow ? Math.round(firstRole.left - roleRow.left) : 0,
            }
            })(),
            title: card.querySelector("h3")?.textContent,
            roleDescription: roles.getAttribute("aria-describedby"),
            chosenRole: roles.querySelector('[aria-checked="true"] span')?.textContent,
            picker: card.querySelector(".work-session-trigger > span:not(.work-session-placeholder):not(.work-project-chevron)")?.textContent,
            paths: [...card.querySelectorAll(".work-assignment-route h4")].map((heading) => heading.textContent),
            actions: [...card.querySelectorAll(".work-assignment-action")].map((button) => button.textContent),
            actionsUseChips: [...card.querySelectorAll(".work-assignment-action")].every((button) =>
              button.classList.contains("chip") && getComputedStyle(button).backgroundColor ===
                getComputedStyle(card.querySelector(".work-persona-ai-button")).backgroundColor),
            focus: document.activeElement?.getAttribute("aria-label"),
            progressBeforeStart: !!card.querySelector(".work-milestones"),
            sessionsPage: !document.getElementById("app").hidden,
            boardPage: !document.getElementById("work").hidden,
          })
        }
        if (Date.now() >= deadline) return reject(new Error("the created Board item did not stay open"))
        setTimeout(read, 25)
      }
      read()
    })`)
    assert.deepEqual(card, {
      fits: { panelInView: true, modalScrollsSideways: false, controlsOutsidePanel: 0, personaChips: PERSONAS.length + 1,
        rolesScrollSideways: true, roleEdgeRoom: 7 },
      title: "React console shortcut",
      roleDescription: null,
      chosenRole: "不指定",
      picker: "選擇既有 Session",
      paths: ["指派給既有 Session", "開啟新 Session"],
      actions: ["開新 Codex Session"],
      actionsUseChips: true,
      focus: "指派既有 Session",
      progressBeforeStart: false,
      sessionsPage: true,
      boardPage: false,
    })
    assert.equal(personaSuggestionRequests, 0, "opening and rendering the item must not call AI")
    await tab.shot("smart-role-phone-light")
    await tab.view(PHONE, "dark")
    await tab.shot("smart-role-phone-dark")
    await tab.view(DESK, "dark")
    assert.deepEqual(await tab.run(`(() => {
      const modal = document.querySelector(".work-created-modal")
      const panel = modal.querySelector(".work-created-panel").getBoundingClientRect()
      const actions = [...modal.querySelectorAll(".work-assignment-action")].map((button) => button.getBoundingClientRect())
      const roles = modal.querySelector(".work-new-persona")
      const roleRow = roles.getBoundingClientRect()
      const firstRole = roles.firstElementChild.getBoundingClientRect()
      return {
        panelInView: panel.left >= 0 && panel.right <= innerWidth,
        modalScrollsSideways: modal.scrollWidth > modal.clientWidth,
        actionsInPanel: actions.every((box) => box.left >= panel.left && box.right <= panel.right),
        actionsTallEnough: actions.every((box) => box.height >= 36),
        roleEdgeRoom: Math.round(firstRole.left - roleRow.left),
        progressBeforeStart: !!modal.querySelector(".work-milestones"),
      }
    })()`), { panelInView: true, modalScrollsSideways: false, actionsInPanel: true, actionsTallEnough: true,
      roleEdgeRoom: 7, progressBeforeStart: false })
    await tab.shot("smart-role-desktop-dark")
    await tab.view(DESK, "light")
    await tab.shot("smart-role-desktop-light")

    // With no Session chosen, its confirmation is absent from the keyboard
    // path. The person can choose a role before asking AI.
    const keyboard: string[] = []
    for (let index = 0; index < 4; index++) {
      await tab.press("Tab")
      keyboard.push(await tab.run(`document.activeElement?.getAttribute("aria-label") || document.activeElement?.textContent?.trim()`))
    }
    assert.deepEqual(keyboard, ["Codex", "Claude Code", "AI 建議", "不指定"], "keyboard order")
    assert.deepEqual(await tab.run(`(() => {
      const role = document.querySelector(".work-created-modal .work-new-persona .chip")
      return { active: document.activeElement === role, disabled: role.disabled, text: role.textContent }
    })()`), { active: true, disabled: false, text: "不指定" })
    await tab.run(`(() => {
      const role = document.querySelector('.work-created-modal [data-persona-id="frontend"]')
      role.focus()
      role.click()
    })()`)
    assert.deepEqual(await tab.run(`(() => {
      const card = document.querySelector(".work-created-modal .work-v2-card")
      const roles = card.querySelector(".work-new-persona")
      return {
        focused: document.activeElement?.textContent,
        chosenRole: roles.querySelector('[aria-checked="true"] span')?.textContent,
        actions: [...card.querySelectorAll(".work-assignment-action")].map((button) => button.textContent),
      }
    })()`), {
      focused: "前端工程師",
      chosenRole: "前端工程師",
      actions: ["開新 Codex Session（前端工程師）"],
    })

    // One explicit press sends one receipted request, selects the catalog id
    // AI returned, and still permits a manual override afterward.
    await tab.run(`document.querySelector(".work-created-modal .work-persona-ai-button").click()`)
    const ai = await tab.run(`new Promise((resolve, reject) => {
      const deadline = Date.now() + 5000
      const read = () => {
        const card = document.querySelector(".work-created-modal .work-v2-card")
        const status = card?.querySelector(".work-persona-ai-result[role=status]")
        const roles = card?.querySelector(".work-new-persona")
        const chosenRole = roles?.querySelector('[aria-checked="true"] span')?.textContent
        if (status && chosenRole === "後端工程師") {
          return resolve({
            status: status.textContent,
            chosenRole,
            action: [...card.querySelectorAll(".work-assignment-action")].at(-1)?.textContent,
          })
        }
        if (Date.now() >= deadline) return reject(new Error("AI role suggestion did not arrive"))
        setTimeout(read, 25)
      }
      read()
    })`)
    assert.deepEqual(ai, {
      status: "AI 建議：後端工程師已預先選取，仍可手動改選",
      chosenRole: "後端工程師",
      action: "開新 Codex Session（後端工程師）",
    })
    assert.equal(personaSuggestionRequests, 1)
    assert.match(personaSuggestionKey, /^web-[0-9a-f]{32}$/)
    await tab.run(`document.querySelector(".work-created-modal .work-new-persona .chip").click()`)
    assert.deepEqual(await tab.run(`(() => {
      const card = document.querySelector(".work-created-modal .work-v2-card")
      return {
        chosenRole: card.querySelector('.work-new-persona [aria-checked="true"] span')?.textContent,
        aiStatus: card.querySelector(".work-persona-ai-result[role=status]")?.textContent,
      }
    })()`), { chosenRole: "不指定", aiStatus: "AI 建議：後端工程師目前已改選其他角色" })

    // When this context has no eligible Session, the menu explains why and
    // still offers no confirmation action.
    await tab.run(`document.querySelector('.work-created-modal .work-session-trigger').click()`)
    await tab.run(`new Promise((resolve, reject) => {
      const deadline = Date.now() + 5000
      const read = () => {
        if (document.querySelector('.work-created-modal .work-session-menu')) return resolve(true)
        if (Date.now() >= deadline) return reject(new Error('existing Session menu did not open'))
        setTimeout(read, 25)
      }
      read()
    })`)
    assert.deepEqual(await tab.run(`(() => {
      const card = document.querySelector('.work-created-modal .work-v2-card')
      return {
        empty: card.querySelector('.work-session-menu')?.textContent,
        action: card.querySelector('[aria-labelledby^="work-assign-existing-"] .work-assignment-action')?.textContent ?? null,
      }
    })()`), { empty: "這個 Project 目前沒有可用的 Session。", action: null })
  }))
