// Role chips on the way back into a past conversation, in the built console:
//
//   (cd web && npm run build)
//   CLAWDLINE_SHOTS=<dir> node --test web/console/src/session/resume-role.e2e.ts
//
// A stand-in daemon and headless Chrome, the harness of `restore.e2e.ts`. It
// holds what `place-routes.test.ts` cannot: that the resume step really draws
// the role row with "No role" chosen, that a picked role reaches the route the
// press sends, and that the restore sheet names a row's recorded role.
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

const now = () => Math.floor(Date.now() / 1000)

// ---- what the stand-in daemon says

function bot(accent: string) {
  const rows = ["...a....", ".aaaaaa.", ".awaawa.", ".aaaaaa.", "..aaaa..", ".aaaaaa.", ".a....a."]
  return { accent, cells: rows.map((r) => [...r].map((c) => (c === "a" ? accent : c === "w" ? "#ffffff" : null))) }
}

const personas = {
  license: "MIT",
  personas: [
    ["architect", "Architect", "架構師", "#6f8cff"],
    ["frontend", "Frontend Engineer", "前端工程師", "#3fb68b"],
    ["code-reviewer", "Code Reviewer", "程式碼審查", "#e0a23b"],
    ["technical-writer", "Technical Writer", "技術寫作", "#c86bd8"],
  ].map(([id, en, zh, accent]) => ({
    id, name: { en, "zh-Hant": zh }, summary: { en: en + ".", "zh-Hant": zh + "。" }, suggested_kinds: [], icon: bot(accent),
  })),
}

let offer: Record<string, unknown> = { available: false, reason: "boot_unknown", sessions: [], at: 0 }
/** Every resume that arrived, by its path. */
let resumes: string[] = []

function snapshot() {
  return {
    at: Date.now(),
    scan: {
      complete: true, completed: { complete: true, sequence: 1 }, emptyAuthoritative: true, epoch: 1, generation: 1,
      provenance: "fixture", source: { freshness: "current", observed_at: now(), provenance: "fixture" },
    },
    sessions: [],
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
    if (path === "/v1/personas") return json(res, 200, personas)
    if (path === "/v1/sessions/restorable") return json(res, 200, offer)
    if (path === "/v1/places") {
      return json(res, 200, {
        places: [{ id: "place-api", label: "api", path: "/work/api", at: now() - 300 },
          { id: "place-console", label: "console", path: "/work/web/console", at: now() - 900 }],
        assistants: [{ id: "claude", label: "Claude" }, { id: "codex", label: "Codex" }],
      })
    }
    if (/^\/v1\/places\/[^/]+\/sessions/.test(path)) {
      return json(res, 200, {
        at: now(), place: "place-api", assistant: "claude", more: false,
        sessions: [
          { id: "past-1", title: "Tighten the reconnect loop", at: now() - 3600, live: false },
          { id: "past-2", title: "Write the release notes for the resume sheet", at: now() - 86400 * 2, live: false },
        ],
      })
    }
    if (req.method === "POST" && /^\/v1\/places\/[^/]+\/resume\//.test(path)) {
      resumes.push(path)
      return json(res, 200, { ok: true, id: "new-1", backend: "tmux", assistant: "claude", place: "place-api", cwd: "/work/api", session: "past-1", attach: "", at: now() })
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

/** The start sheet's role row and past list, read in one go. */
const PROBE = `(() => {
  const row = document.getElementById("start-persona")
  const chips = row ? [...row.querySelectorAll(".chip")] : []
  const restore = document.querySelector(".restore-sheet")
  return {
    roleShown: !!row && !row.hidden,
    roles: chips.map((c) => c.textContent),
    chosen: chips.filter((c) => c.getAttribute("aria-pressed") === "true").map((c) => c.textContent),
    pasts: [...document.querySelectorAll("#start-list [data-session]")].map((b) => b.dataset.session),
    places: [...document.querySelectorAll("#start-list .place[data-id]")].length,
    restoreRoles: restore ? [...restore.querySelectorAll(".restore-row")].map((r) => r.querySelector(".persona-tag-name")?.textContent ?? "") : null,
    wide: document.documentElement.scrollWidth,
  }
})()`

interface Seen {
  roleShown: boolean
  roles: string[]
  chosen: string[]
  pasts: string[]
  places: number
  restoreRoles: string[] | null
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

  static async open(b: Browser, width: number, height: number, mobile: boolean): Promise<Tab> {
    const { targetId } = await b.send("Target.createTarget", { url: "about:blank" })
    const { sessionId } = await b.send("Target.attachToTarget", { targetId, flatten: true })
    await b.send("Page.enable", {}, sessionId)
    await b.send("Runtime.enable", {}, sessionId)
    await b.send("Emulation.setDeviceMetricsOverride", { width, height, mobile, deviceScaleFactor: 2 }, sessionId)
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

  /** The chip in a row whose text is exactly these words. */
  pressChip(row: string, words: string): Promise<void> {
    return this.run(`[...document.querySelectorAll(${JSON.stringify(row + " .chip")})].find((c) => c.textContent === ${JSON.stringify(words)}).click()`)
  }

  async shot(name: string): Promise<void> {
    if (!shots) return
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
  profile = mkdtempSync(join(tmpdir(), "clawdline-resume-role-"))
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

/** Open the start sheet, tick "resume", and enter the api place. */
async function toPastList(tab: Tab): Promise<Seen> {
  await tab.go("/")
  await tab.run(`localStorage.setItem("clawdline.start.persona", "frontend")`)
  await tab.click("#start-go")
  await tab.until("the places are listed", (s) => s.places > 0)
  await tab.click("#start-resume .chip.check")
  const listing = await tab.until("resuming hides the role row while places are listed", (s) => !s.roleShown)
  assert.equal(listing.roleShown, false)
  await tab.click('#start-list .place[data-id="place-api"]')
  return tab.until("the past conversations and the role row are drawn", (s) => s.pasts.length === 2 && s.roleShown)
}

for (const [label, width, height, mobile] of [["phone", 390, 844, true], ["desktop", 1280, 800, false]] as const) {
  test(`${label}: the resume step offers the roles with none chosen, and a pick reaches the route`, async () => {
    const tab = await Tab.open(browser, width, height, mobile)
    try {
      resumes = []
      const seen = await toPastList(tab)
      assert.deepEqual(seen.chosen, ["不指定"], "the start's remembered role is not carried into a resume")
      assert.deepEqual(seen.roles, ["不指定", "架構師", "前端工程師", "程式碼審查", "技術寫作"])
      assert.ok(seen.wide <= width, "nothing runs off the side: " + seen.wide)
      await tab.shot(`resume-roles-${label}`)
      await tab.pressChip("#start-persona", "架構師")
      await tab.until("the pick is shown", (s) => s.chosen[0] === "架構師")
      await tab.shot(`resume-roles-${label}-picked`)
      await tab.click('#start-list [data-session="past-1"]')
      const deadline = Date.now() + 5000
      while (!resumes.length && Date.now() < deadline) await new Promise((r) => setTimeout(r, 50))
      assert.deepEqual(resumes, ["/v1/places/place-api/resume/claude/past-1/as/architect"])
    } finally {
      await tab.close()
    }
  })
}

test("a resume with no role chosen sends the route it always sent", async () => {
  const tab = await Tab.open(browser, 390, 844, true)
  try {
    resumes = []
    await toPastList(tab)
    await tab.click('#start-list [data-session="past-2"]')
    const deadline = Date.now() + 5000
    while (!resumes.length && Date.now() < deadline) await new Promise((r) => setTimeout(r, 50))
    assert.deepEqual(resumes, ["/v1/places/place-api/resume/claude/past-2"])
  } finally {
    await tab.close()
  }
})

test("the restore sheet names the role each conversation will come back with", async () => {
  const tab = await Tab.open(browser, 390, 844, true)
  try {
    offer = {
      available: true, at: now(), previous_boot_last_seen: now() - 900,
      sessions: [
        { conversation_id: "c1", assistant: "claude", place: "p0", place_label: "api", cwd: "/work/api", title: "Fix the reconnect loop", last_seen: now() - 600, persona: "code-reviewer" },
        { conversation_id: "c2", assistant: "codex", place: "p1", place_label: "console", cwd: "/work/web/console", title: "", last_seen: now() - 1800 },
      ],
    }
    await tab.go("/")
    await tab.until("the offer is drawn", () => true)
    await tab.run(`(async () => { for (let i = 0; i < 100 && !document.querySelector(".restore-open"); i++) await new Promise((r) => setTimeout(r, 50)); document.querySelector(".restore-open").click() })()`)
    const seen = await tab.until("the sheet lists both rows", (s) => s.restoreRoles?.length === 2)
    assert.deepEqual(seen.restoreRoles, ["程式碼審查", ""])
    assert.ok(seen.wide <= 390, "nothing runs off the side: " + seen.wide)
    await tab.shot("restore-sheet-role-phone")
  } finally {
    offer = { available: false, reason: "boot_unknown", sessions: [], at: 0 }
    await tab.close()
  }
})
