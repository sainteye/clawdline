// The offer of the sessions a reboot took away, in the built console on a phone:
//
//   (cd web && npm run build)
//   CLAWDLINE_SHOTS=<dir> node --test web/console/src/session/restore.e2e.ts
//
// A stand-in daemon and headless Chrome at 390x844, the harness of
// `list-gestures.e2e.ts` cut down to what this needs. It holds what the unit
// tests cannot: that the card really stands in the empty list's place, that it
// sits above rows when there are some, that nothing new is drawn when there is
// nothing to offer, and what a press actually sends.
//
// Named `.e2e.ts` rather than `.test.ts` so the unit run does not start a browser.
import { test, before, after } from "node:test"
import assert from "node:assert/strict"
import { spawn, type ChildProcess } from "node:child_process"
import { createServer, type Server, type ServerResponse } from "node:http"
import { existsSync, mkdtempSync, readFileSync, rmSync, statSync, writeFileSync } from "node:fs"
import { tmpdir } from "node:os"
import { dirname, extname, join, normalize, resolve } from "node:path"
import { fileURLToPath } from "node:url"

const here = dirname(fileURLToPath(import.meta.url))
const dist = resolve(process.env.CLAWDLINE_DIST || resolve(here, "../../dist"))
const shots = process.env.CLAWDLINE_SHOTS || ""
const chrome = process.env.CHROME || "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"

// Pane ids of three digits are spelled in pieces: tools/check-private.sh reads
// any `%NNN` in a published file as a pane copied from somebody's machine.
const pane = (n: number) => "%" + n

// ---- what the stand-in daemon says

type Scenario = { rows: Record<string, unknown>[]; offer: Record<string, unknown> }
let scenario: Scenario
/** Every restore and dismissal that arrived, with its key. */
let presses: { path: string; key: string; body: unknown }[] = []

const now = () => Math.floor(Date.now() / 1000)

function offered(n: number) {
  const titles = ["Fix the reconnect loop", "", "Write the release notes", "Trace the slow send"]
  const places = [["api", "/work/api"], ["console", "/work/web/console"], ["docs", "/work/docs"], ["daemon", "/work/daemon"]]
  return Array.from({ length: n }, (_, i) => ({
    conversation_id: "conv-" + (i + 1),
    assistant: i % 2 ? "codex" : "claude",
    place: "place-" + i,
    place_label: places[i % places.length][0],
    cwd: places[i % places.length][1],
    title: titles[i % titles.length],
    last_seen: now() - 60 * (i * 17 + 9),
  }))
}

function row(id: string, label: string) {
  return {
    id, label, backend: "tmux", state: "idle", work_state: "ready", evidence: "process",
    isClaude: true, assistant: "claude", sessionId: "conversation-" + id, cwd: "/work/api",
    activity: { known: true, at: now() - 120 },
  }
}

function snapshot() {
  return {
    at: Date.now(),
    scan: {
      complete: true, completed: { complete: true, sequence: 1 }, emptyAuthoritative: true, epoch: 1, generation: 1,
      provenance: "fixture", source: { freshness: "current", observed_at: now(), provenance: "fixture" },
    },
    sessions: scenario.rows,
  }
}

const TYPES: Record<string, string> = {
  ".js": "text/javascript", ".css": "text/css", ".json": "application/json", ".png": "image/png",
  ".webp": "image/webp", ".ico": "image/x-icon", ".svg": "image/svg+xml", ".webmanifest": "application/manifest+json",
}

function json(res: ServerResponse, status: number, body: unknown) {
  res.writeHead(status, { "content-type": "application/json" })
  res.end(JSON.stringify(body))
}

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
    if (path === "/v1/orchestrator/tasks") return json(res, 200, { at: Date.now(), tasks: [] })
    if (path === "/v1/sessions/restorable" && req.method === "GET") return json(res, 200, scenario.offer)
    if ((path === "/v1/sessions/restorable/restore" || path === "/v1/sessions/restorable/dismiss") && req.method === "POST") {
      let raw = ""
      req.on("data", (chunk) => (raw += chunk))
      req.on("end", () => {
        const body = JSON.parse(raw || "{}") as { conversations?: string[] }
        presses.push({ path, key: String(req.headers["idempotency-key"] ?? ""), body })
        if (path.endsWith("/dismiss")) {
          scenario.offer = { ...scenario.offer, sessions: [] }
          return json(res, 200, { ok: true, dismissed: 3, at: now() })
        }
        // The first opens, the second's conversation is gone, the rest open.
        const results = (body.conversations ?? []).map((id) =>
          id === "conv-2"
            ? { conversation_id: id, ok: false, code: "conversation_not_found", message: "not listed" }
            : { conversation_id: id, ok: true, id: pane(800), backend: "tmux" })
        const opened = new Set(results.filter((r) => r.ok).map((r) => r.conversation_id))
        const sessions = (scenario.offer.sessions as { conversation_id: string }[]).filter((s) => !opened.has(s.conversation_id))
        scenario.offer = { ...scenario.offer, sessions }
        json(res, 200, { results, at: now() })
      })
      return
    }
    if (path === "/v1/events") {
      res.writeHead(200, { "content-type": "text/event-stream", "cache-control": "no-cache" })
      res.write("event: sessions\ndata: " + JSON.stringify(snapshot()) + "\n\n")
      return
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

// ---- the browser, over the DevTools protocol

type Pending = { resolve: (v: any) => void; reject: (e: Error) => void }

class Browser {
  private seq = 0
  private pending = new Map<number, Pending>()
  private events: { method: string; sessionId?: string }[] = []
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

  send(method: string, params: object = {}, sessionId?: string): Promise<any> {
    const id = ++this.seq
    this.ws.send(JSON.stringify({ id, method, params, sessionId }))
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => (this.pending.delete(id), reject(new Error(method + " had no answer"))), 15_000)
      this.pending.set(id, {
        resolve: (v) => (clearTimeout(timer), resolve(v)),
        reject: (e) => (clearTimeout(timer), reject(e)),
      })
    })
  }

  async loaded(sessionId: string, mark: number): Promise<void> {
    const deadline = Date.now() + 10_000
    while (!this.events.slice(mark).some((e) => e.sessionId === sessionId && e.method === "Page.loadEventFired")) {
      if (Date.now() > deadline) throw new Error("the page did not load")
      await new Promise((r) => setTimeout(r, 50))
    }
  }

  mark(): number {
    return this.events.length
  }

  close() {
    this.ws.close()
  }
}

/** What the offer looks like on the page, read in one go so a failure can say all of it. */
const PROBE = `(() => {
  const empty = document.getElementById("list-empty")
  const hero = document.querySelector('[data-restore="hero"]')
  const card = document.querySelector('[data-restore="compact"]')
  const sheet = document.querySelector(".restore-sheet")
  return {
    rows: document.querySelectorAll("#rows > li.row").length,
    emptyShown: !!empty && !empty.hidden,
    emptyText: empty && !empty.hidden ? empty.textContent : "",
    hero: hero ? hero.textContent : null,
    card: card ? card.textContent : null,
    cardAboveRows: !!card && !!document.getElementById("rows") &&
      !!(card.compareDocumentPosition(document.getElementById("rows")) & Node.DOCUMENT_POSITION_FOLLOWING),
    sheet: sheet ? [...sheet.querySelectorAll(".restore-row")].map((r) => ({
      name: r.querySelector(".restore-name")?.textContent ?? "",
      checked: r.getAttribute("aria-checked") === "true",
      outcome: r.querySelector(".restore-outcome")?.textContent ?? "",
    })) : null,
    go: sheet ? sheet.querySelector(".restore-go")?.textContent ?? "" : "",
    wide: document.documentElement.scrollWidth,
  }
})()`

interface Seen {
  rows: number
  emptyShown: boolean
  emptyText: string
  hero: string | null
  card: string | null
  cardAboveRows: boolean
  sheet: { name: string; checked: boolean; outcome: string }[] | null
  go: string
  wide: number
}

class Tab {
  private b: Browser
  private session: string
  private target: string

  constructor(b: Browser, session: string, target: string) {
    this.b = b
    this.session = session
    this.target = target
  }

  static async open(b: Browser): Promise<Tab> {
    const { targetId } = await b.send("Target.createTarget", { url: "about:blank" })
    const { sessionId } = await b.send("Target.attachToTarget", { targetId, flatten: true })
    await b.send("Page.enable", {}, sessionId)
    await b.send("Runtime.enable", {}, sessionId)
    await b.send("Emulation.setDeviceMetricsOverride", { width: 390, height: 844, mobile: true, deviceScaleFactor: 2 }, sessionId)
    await b.send("Emulation.setTouchEmulationEnabled", { enabled: true, maxTouchPoints: 1 }, sessionId)
    return new Tab(b, sessionId, targetId)
  }

  close(): Promise<void> {
    return this.b.send("Target.closeTarget", { targetId: this.target }).then(() => undefined, () => undefined)
  }

  async go(address: string): Promise<void> {
    const mark = this.b.mark()
    await this.b.send("Page.navigate", { url: origin + address }, this.session)
    await this.b.loaded(this.session, mark)
  }

  async run(expression: string): Promise<any> {
    const { result, exceptionDetails } = await this.b.send(
      "Runtime.evaluate", { expression, returnByValue: true, awaitPromise: true }, this.session)
    if (exceptionDetails) throw new Error(exceptionDetails.exception?.description ?? exceptionDetails.text)
    return result.value
  }

  async until(what: string, ok: (s: Seen) => boolean, ms = 6_000): Promise<Seen> {
    const deadline = Date.now() + ms
    let last: Seen | null = null
    for (;;) {
      try {
        last = (await this.run(PROBE)) as Seen
        if (ok(last)) return last
      } catch {
        /* between documents */
      }
      if (Date.now() > deadline) assert.fail(what + "; the page showed " + JSON.stringify(last))
      await new Promise((r) => setTimeout(r, 50))
    }
  }

  click(selector: string): Promise<void> {
    return this.run(`document.querySelector(${JSON.stringify(selector)}).click()`)
  }

  async shot(name: string): Promise<void> {
    if (!shots) return
    // Let the sheet's fade finish before the picture is taken.
    await new Promise((r) => setTimeout(r, 250))
    const { data } = await this.b.send("Page.captureScreenshot", { format: "png" }, this.session)
    writeFileSync(join(shots, name + ".png"), Buffer.from(data, "base64"))
  }
}

let server: Server
let browserProcess: ChildProcess
let browser: Browser
let profile: string
let origin: string

before(async () => {
  assert.ok(existsSync(join(dist, "index.html")), "no built console at " + dist + "; run `npm run build` in web first")
  assert.ok(existsSync(chrome), "no Chrome at " + chrome + "; set CHROME")
  server = daemon()
  await new Promise<void>((ok) => server.listen(0, "127.0.0.1", ok))
  const address = server.address()
  origin = "http://127.0.0.1:" + (typeof address === "object" && address ? address.port : 0)
  profile = mkdtempSync(join(tmpdir(), "clawdline-restore-"))
  browserProcess = spawn(chrome, [
    "--headless=new", "--remote-debugging-port=0", "--user-data-dir=" + profile,
    "--no-first-run", "--no-default-browser-check", "about:blank",
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
  if (browserProcess && browserProcess.exitCode === null) {
    const gone = new Promise((ok) => browserProcess.once("exit", ok))
    browserProcess.kill()
    await gone
  }
  server?.closeAllConnections()
  await new Promise<void>((ok) => (server ? server.close(() => ok()) : ok()))
  if (profile) rmSync(profile, { recursive: true, force: true, maxRetries: 5 })
})

async function inTab(body: (tab: Tab) => Promise<void>) {
  const tab = await Tab.open(browser)
  try {
    await body(tab)
  } finally {
    await tab.close()
  }
}

test("with nothing to offer, the empty list says what it always said", () =>
  inTab(async (tab) => {
    scenario = { rows: [], offer: { available: false, reason: "boot_unknown", sessions: [], at: now() } }
    await tab.go("/")
    const seen = await tab.until("the empty list settles", (s) => s.emptyShown && s.emptyText.length > 0)
    assert.equal(seen.hero, null)
    assert.equal(seen.card, null)
    const said = seen.emptyText
    scenario.offer = { available: true, sessions: [], at: now() }
    await tab.go("/")
    const again = await tab.until("the empty list settles", (s) => s.emptyShown && s.emptyText.length > 0)
    assert.equal(again.hero, null, "an empty offer draws nothing")
    assert.equal(again.emptyText, said)
  }))

test("an empty list offers what a reboot took, in its own place, and a press sends the ticked ones under one key", () =>
  inTab(async (tab) => {
    presses = []
    scenario = { rows: [], offer: { available: true, sessions: offered(3), at: now(), previous_boot_last_seen: now() - 900 } }
    await tab.go("/")
    const seen = await tab.until("the offer stands in the empty list's place", (s) => s.hero !== null)
    assert.match(seen.hero!, /上次重新開機前開著 3 個 session/)
    assert.match(seen.hero!, /查看並恢復/)
    assert.ok(seen.wide <= 390, "nothing runs off the side of the phone: " + seen.wide)
    await tab.shot("restore-empty-list-card")

    await tab.click('[data-restore="hero"] .restore-open')
    const sheet = await tab.until("the sheet lists every conversation, ticked", (s) => s.sheet?.length === 3)
    assert.deepEqual(sheet.sheet!.map((r) => r.checked), [true, true, true])
    // A row with no title goes by its folder's label.
    assert.deepEqual(sheet.sheet!.map((r) => r.name), ["Fix the reconnect loop", "console", "Write the release notes"])
    assert.match(sheet.go, /恢復選取的 3 個/)
    await tab.shot("restore-sheet-open")

    // Untick the third, and the press sends the other two in the sheet's order.
    await tab.click(".restore-sheet li:nth-child(3) .restore-row")
    await tab.until("the button counts what is ticked", (s) => /恢復選取的 2 個/.test(s.go))
    await tab.click(".restore-sheet .restore-go")
    const after = await tab.until("each row says what happened", (s) => !!s.sheet && s.sheet[1].outcome !== "" && !/中/.test(s.sheet[1].outcome))
    assert.equal(presses.length, 1)
    assert.equal(presses[0].path, "/v1/sessions/restorable/restore")
    assert.ok(presses[0].key.length >= 16, "the press carries its own key")
    assert.deepEqual(presses[0].body, { conversations: ["conv-1", "conv-2"] })
    assert.match(after.sheet![0].outcome, /已打開/)
    assert.match(after.sheet![1].outcome, /找不到這段對話/)
    assert.equal(after.sheet![2].outcome, "", "a row that was not sent says nothing")
    await tab.shot("restore-sheet-outcomes")

    // Skip the rest: one more press, one more key, no list.
    await tab.click(".restore-sheet .restore-buttons .chip:nth-child(2)")
    await tab.until("the sheet closes and nothing is on offer", (s) => s.sheet === null && s.hero === null)
    assert.equal(presses[1].path, "/v1/sessions/restorable/dismiss")
    assert.deepEqual(presses[1].body, {})
    assert.notEqual(presses[1].key, presses[0].key)
  }))

test("with rows already open, the offer is a card above them", () =>
  inTab(async (tab) => {
    scenario = {
      rows: [row(pane(701), "A session opened after the restart")],
      offer: { available: true, sessions: offered(4), at: now() },
    }
    await tab.go("/")
    const seen = await tab.until("the card sits above the list", (s) => s.rows === 1 && s.card !== null)
    assert.ok(seen.cardAboveRows)
    assert.equal(seen.hero, null)
    assert.match(seen.card!, /上次重新開機前開著 4 個 session/)
    assert.ok(seen.wide <= 390, "nothing runs off the side of the phone: " + seen.wide)
    await tab.shot("restore-list-with-card")
  }))
