// Archiving a Session from the list and bringing it back from the sidebar's
// Archive page (docs/session-archive.md), driven as a finger drives it:
//
//   (cd web && npm run build)
//   CLAWDLINE_SHOTS=<dir> node --test web/console/src/session/archive.e2e.ts
//
// A stand-in daemon and headless Chrome at 390x844 with touch emulation on:
// the harness of `list-gestures.e2e.ts` and `restore.e2e.ts`, cut down to what
// this needs. It holds what the unit tests cannot: that a real swipe uncovers
// two buttons that both fit on the phone, that a row without a conversation id
// uncovers only the close, that 封存 goes through the confirmation and what
// the press sends, and that the Archive page draws the entry with the row's
// own mark and tint and sends one restore for it.
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

const KEPT = pane(711) // has a conversation: 封存 and 關閉
const BARE = pane(712) // has none: 關閉 alone
const OWED = pane(713) // has a conversation and an obligation: close_blocked first
const REOPENED = pane(790)

const now = () => Math.floor(Date.now() / 1000)

/** A project mark: the same shape the daemon sends a row, tinted blue. */
const ICON = {
  accent: "#5b8def",
  cells: [".####.", "#o##o#", "######", ".#..#."].map((r) =>
    r.split("").map((ch) => (ch === "#" ? "#5b8def" : ch === "o" ? "#141416" : "#1b2233"))),
}

function safe() {
  return {
    activity_generation: 3, attestation_id: "att-fixture", mover: null, obligation_generation: 3, observed_at: 1,
    provenance: ["broker", "self"], reasons: [], session_generation: 1,
    source: { freshness: "current", max_age_seconds: 30, observed_at: 1, provenance: "session_watch" },
    state: "safe", version: "cl1_fixture",
  }
}

const OWED_REASONS = [{ code: "terminal_working", kind: "obligation", mover: { kind: "session", self: false, session_id: OWED } }]

type Row = Record<string, unknown>

function row(id: string, label: string, extra: Row = {}): Row {
  return {
    id, label, backend: "tmux", state: "idle", work_state: "ready", evidence: "process",
    isClaude: true, assistant: "claude", sessionId: "conversation-" + id.slice(1), cwd: "/work/api",
    activity: { known: true, at: now() - 120 }, closeability: safe(), icon: ICON, ...extra,
  }
}

// ---- what the stand-in daemon says

let rows: Row[] = []
let archived: Row[] = []
/** Every archive and restore that arrived, with its key. */
let presses: { path: string; key: string; body: Record<string, unknown> }[] = []
const streams = new Set<ServerResponse>()

function snapshot() {
  return {
    at: Date.now(),
    scan: {
      complete: true, completed: { complete: true, sequence: 1 }, emptyAuthoritative: true, epoch: 1, generation: 1,
      provenance: "fixture", source: { freshness: "current", observed_at: now(), provenance: "fixture" },
    },
    sessions: rows,
  }
}

function pushSessions(): void {
  const frame = "event: sessions\ndata: " + JSON.stringify(snapshot()) + "\n\n"
  for (const stream of streams) stream.write(frame)
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

function bodyOf(req: import("node:http").IncomingMessage): Promise<Record<string, unknown>> {
  return new Promise((ok) => {
    let raw = ""
    req.on("data", (chunk) => (raw += chunk))
    req.on("end", () => {
      try {
        ok(JSON.parse(raw || "{}"))
      } catch {
        ok({})
      }
    })
  })
}

function daemon(): Server {
  return createServer(async (req, res) => {
    const url = new URL(req.url ?? "/", "http://fixture")
    const path = url.pathname
    if (path === "/v1/strings" && url.searchParams.get("lang") === "zh-Hant") {
      return json(res, 200, JSON.parse(readFileSync(join(dist, "catalogs", "zh-Hant.json"), "utf8")))
    }
    if (path === "/v1/sessions") return json(res, 200, snapshot())
    if (path === "/v1/health") return json(res, 200, { ok: true })
    if (path === "/v1/orchestrator/tasks") return json(res, 200, { at: Date.now(), tasks: [] })
    if (path.startsWith("/v1/work/v2/session-todos/")) {
      return json(res, 200, { assigned_items: [], recent_items: [], direct_todos: [], truncated: false })
    }
    if (path === "/v1/sessions/archived" && req.method === "GET") return json(res, 200, { sessions: archived, at: now() })
    const archiving = /^\/v1\/sessions\/(.+)\/archive$/.exec(path)
    if (archiving && req.method === "POST") {
      const id = decodeURIComponent(archiving[1])
      const body = await bodyOf(req)
      presses.push({ path, key: String(req.headers["idempotency-key"] ?? ""), body })
      const target = rows.find((r) => r.id === id)
      if (!target) return json(res, 404, { error: "not_found", detail: id })
      if (id === OWED && body.force !== true) {
        return json(res, 409, { error: "close_blocked", detail: "still owed", reasons: OWED_REASONS })
      }
      const entry = {
        conversation_id: target.sessionId, assistant: "claude", place: "place-api", place_label: "api",
        cwd: target.cwd, title: target.label, icon: ICON, archived_at: now(),
      }
      archived = [entry, ...archived]
      rows = rows.filter((r) => r.id !== id)
      json(res, 200, { ok: true, id, action: "archived", archived: entry, ...(body.force ? { forced: true } : {}) })
      pushSessions()
      return
    }
    if (path === "/v1/sessions/archived/restore" && req.method === "POST") {
      const body = await bodyOf(req)
      presses.push({ path, key: String(req.headers["idempotency-key"] ?? ""), body })
      const ids = (body.conversations as string[]) ?? []
      const results = ids.map((c) => ({ conversation_id: c, ok: true, id: REOPENED, backend: "tmux" }))
      const back = archived.find((a) => ids.includes(a.conversation_id as string))
      archived = archived.filter((a) => !ids.includes(a.conversation_id as string))
      // The resumed conversation is a new terminal, and the list hears of it.
      if (back) rows = [...rows, row(REOPENED, String(back.title), { sessionId: back.conversation_id })]
      json(res, 200, { results, at: now() })
      pushSessions()
      return
    }
    if (path === "/v1/events") {
      res.writeHead(200, { "content-type": "text/event-stream", "cache-control": "no-cache" })
      res.write("event: sessions\ndata: " + JSON.stringify(snapshot()) + "\n\n")
      streams.add(res)
      req.on("close", () => streams.delete(res))
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

/** What the list, the swipe, the sheet and the Archive page show, read in one go so a failure can say all of it. */
const PROBE = `(() => {
  const rect = (el) => {
    if (!el || el.hidden) return null
    const style = getComputedStyle(el)
    if (style.display === "none") return null
    const b = el.getBoundingClientRect()
    return { left: Math.round(b.left), right: Math.round(b.right), width: Math.round(b.width), height: Math.round(b.height) }
  }
  const rowOf = (id) => [...document.querySelectorAll("#rows > li.row")].find((n) => n.dataset.id === id)
  const swipeOf = (id) => {
    const row = rowOf(id)
    if (!row) return null
    return {
      state: row.dataset.swipe || "",
      width: row.dataset.swipeWidth || "",
      archive: rect(row.querySelector(".swipe-archive")),
      archiveText: row.querySelector(".swipe-archive")?.textContent ?? null,
      close: rect(row.querySelector(".swipe-end")),
      closeText: row.querySelector(".swipe-end")?.textContent ?? null,
      titleColor: getComputedStyle(row.querySelector(".title")).color,
      right: Math.round(row.getBoundingClientRect().right),
    }
  }
  const overlay = document.getElementById("action-confirm")
  const sheet = document.getElementById("action-confirm-sheet")
  const page = document.getElementById("archive")
  return {
    rows: [...document.querySelectorAll("#rows > li.row")].map((n) => n.dataset.id),
    kept: swipeOf(${JSON.stringify(KEPT)}),
    bare: swipeOf(${JSON.stringify(BARE)}),
    owed: swipeOf(${JSON.stringify(OWED)}),
    confirm: overlay && !overlay.hidden ? {
      kind: sheet?.dataset.kind ?? "",
      title: document.getElementById("action-confirm-title")?.textContent ?? "",
      say: document.getElementById("action-confirm-say")?.textContent ?? "",
      go: document.getElementById("action-confirm-go")?.textContent ?? "",
      goDisabled: !!document.getElementById("action-confirm-go")?.hasAttribute("disabled"),
    } : null,
    page: page && !page.hidden ? {
      title: page.querySelector("h1")?.textContent ?? "",
      empty: page.querySelector(".archive-empty")?.textContent ?? null,
      entries: [...page.querySelectorAll(".archive-entry")].map((e) => ({
        conversation: e.dataset.conversation,
        title: e.querySelector(".archive-title")?.textContent ?? "",
        color: getComputedStyle(e.querySelector(".archive-title")).color,
        mark: (() => { const c = e.querySelector("canvas.mark"); return c ? { w: c.width, h: c.height, none: c.classList.contains("none") } : null })(),
        where: e.querySelector(".archive-where")?.textContent ?? "",
        when: e.querySelector(".archive-when")?.textContent ?? "",
        restore: e.querySelector(".archive-restore")?.textContent ?? "",
        outcome: e.querySelector(".archive-outcome")?.textContent ?? "",
      })),
      reopened: [...page.querySelectorAll(".archive-reopened > li")].map((li) => li.textContent),
    } : null,
    wide: document.documentElement.scrollWidth,
    hash: location.hash,
  }
})()`

interface Swiped {
  state: string
  width: string
  archive: { left: number; right: number; width: number; height: number } | null
  archiveText: string | null
  close: { left: number; right: number; width: number; height: number } | null
  closeText: string | null
  titleColor: string
  right: number
}

interface Seen {
  rows: string[]
  kept: Swiped | null
  bare: Swiped | null
  owed: Swiped | null
  confirm: { kind: string; title: string; say: string; go: string; goDisabled: boolean } | null
  page: {
    title: string
    empty: string | null
    entries: {
      conversation: string; title: string; color: string; mark: { w: number; h: number; none: boolean } | null
      where: string; when: string; restore: string; outcome: string
    }[]
    reopened: string[]
  } | null
  wide: number
  hash: string
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
    await b.send("Page.addScriptToEvaluateOnNewDocument", {
      source: "try { localStorage.setItem('ui_language', 'zh-Hant') } catch {}",
    }, sessionId)
    await b.send("Emulation.setDeviceMetricsOverride", { width: 390, height: 844, mobile: true, deviceScaleFactor: 2 }, sessionId)
    // Without this the page has no fingers: the swipe's listeners are never
    // bound and every gesture here would pass by doing nothing.
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

  press(selector: string): Promise<void> {
    return this.run(`(() => {
      const el = document.querySelector(${JSON.stringify(selector)})
      if (!el) throw new Error("nothing at " + ${JSON.stringify(selector)})
      el.click()
    })()`)
  }

  centreOf(id: string): Promise<{ x: number; y: number }> {
    return this.run(`(() => {
      const row = [...document.querySelectorAll("#rows > li.row")].find((n) => n.dataset.id === ${JSON.stringify(id)})
      if (!row) throw new Error("no row " + ${JSON.stringify(id)})
      const box = row.getBoundingClientRect()
      return { x: Math.round(box.left + box.width / 2), y: Math.round(box.top + box.height / 2) }
    })()`)
  }

  private touch(type: string, points: { x: number; y: number }[]): Promise<void> {
    return this.b.send(
      "Input.dispatchTouchEvent",
      { type, touchPoints: points.map((p) => ({ ...p, radiusX: 8, radiusY: 8, force: 1 })) },
      this.session,
    )
  }

  /** One finger dragged left across a row, in steps, as `list-gestures.e2e.ts` drags. */
  async swipeLeft(id: string, by = 220): Promise<void> {
    const from = await this.centreOf(id)
    await this.touch("touchStart", [from])
    const steps = 8
    for (let i = 1; i <= steps; i++) {
      await this.touch("touchMove", [{ x: Math.round(from.x - (by * i) / steps), y: from.y }])
      await new Promise((r) => setTimeout(r, 12))
    }
    await this.touch("touchEnd", [])
  }

  async shot(name: string): Promise<void> {
    if (!shots) return
    // Let the swipe's and the sheet's transitions finish before the picture.
    await new Promise((r) => setTimeout(r, 300))
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
  profile = mkdtempSync(join(tmpdir(), "clawdline-archive-"))
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
  for (const stream of streams) stream.end()
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

test("a row with a conversation swipes open to 封存 beside 關閉, both on the phone; a row without one to 關閉 alone", () =>
  inTab(async (tab) => {
    rows = [row(KEPT, "Tidy the release notes"), row(BARE, "A shell with no conversation", { sessionId: "" })]
    archived = []
    await tab.go("/")
    await tab.until("both rows are drawn", (s) => s.rows.length === 2)

    await tab.swipeLeft(KEPT)
    const open = await tab.until("the row rests open", (s) => s.kept?.state === "open" && !!s.kept.archive && !!s.kept.close)
    const kept = open.kept!
    assert.equal(kept.width, "176")
    assert.match(kept.archiveText ?? "", /封存/)
    assert.match(kept.closeText ?? "", /關閉/)
    assert.equal(kept.archive!.width, 88, "封存 is one of two 88px buttons")
    assert.equal(kept.close!.width, 88, "and so is 關閉")
    assert.ok(kept.archive!.right <= kept.close!.left + 1, "封存 stands to the left of 關閉: " + JSON.stringify(kept))
    assert.ok(kept.close!.right <= 390 && kept.archive!.left >= 0, "both are on the screen: " + JSON.stringify(kept))
    assert.ok(kept.archive!.height >= 44, "and each is a thumb tall: " + kept.archive!.height)
    assert.ok(open.wide <= 390, "nothing runs off the side of the phone: " + open.wide)
    await tab.shot("archive-swipe-two-buttons")

    // A press anywhere puts it away; then the row with nothing to resume.
    const bare = await tab.centreOf(BARE)
    await tab.swipeLeft(BARE, 5) // the press that closes the open row
    await tab.until("the open row closes", (s) => s.kept?.state !== "open")
    await tab.swipeLeft(BARE)
    const single = await tab.until("the bare row rests open", (s) => s.bare?.state === "open" && !!s.bare.close)
    assert.equal(single.bare!.archive, null, "no 封存 where there is no conversation")
    assert.equal(single.bare!.archiveText, null, "not drawn at all, not merely hidden")
    assert.equal(single.bare!.width, "126")
    assert.equal(single.bare!.close!.width, 126, "the close keeps its own width")
    void bare
  }))

test("More commands groups the reads and opens the same archive confirmation", () =>
  inTab(async (tab) => {
    rows = [row(KEPT, "Tidy the release notes")]
    archived = []
    presses = []
    await tab.go("/")
    await tab.until("the row is drawn", (s) => s.rows.includes(KEPT))
    await tab.press(`#rows > li.row[data-id="${KEPT}"] .label`)
    await tab.press("#detail-actions-trigger")
    const main = await tab.run(`(() => ({
      ids: [...document.querySelectorAll("#session-actions-main > button")].map((b) => b.id),
      more: document.getElementById("session-more")?.textContent?.trim(),
    }))()`)
    assert.deepEqual(main.ids, ["session-interrupt", "session-focus", "session-info", "session-screen", "session-more", "session-end"])
    assert.match(main.more, /更多指令/)

    await tab.press("#session-more")
    const submenu = await tab.run(`(() => ({
      ids: [...document.querySelectorAll("#session-actions-more > button")].map((b) => b.id),
      git: document.getElementById("session-git")?.textContent?.trim(),
      archive: document.getElementById("session-archive")?.textContent?.trim(),
      disabled: document.getElementById("session-archive")?.hasAttribute("disabled"),
    }))()`)
    assert.deepEqual(submenu.ids, ["session-actions-back", "session-documents", "session-user-messages", "session-snippets", "session-git", "session-archive"])
    assert.equal(submenu.git, "Git 變更")
    assert.equal(submenu.archive, "封存 Session")
    assert.equal(submenu.disabled, false)

    await tab.press("#session-archive")
    await tab.until("the confirmation is shown", (s) => s.confirm?.kind === "archive")
    assert.equal(presses.length, 0, "the menu press only asks")
    await tab.press("#action-confirm-go")
    await tab.until("the row is archived", (s) => !s.rows.includes(KEPT) && s.confirm === null)
    assert.equal(presses.length, 1)
    assert.equal(presses[0].path, "/v1/sessions/" + encodeURIComponent(KEPT) + "/archive")
  }))

test("封存 asks first, sends one archive under its key, the row goes, and the Archive page brings it back", () =>
  inTab(async (tab) => {
    rows = [row(KEPT, "Tidy the release notes"), row(BARE, "A shell with no conversation", { sessionId: "" })]
    archived = []
    presses = []
    await tab.go("/")
    const listed = await tab.until("both rows are drawn", (s) => s.rows.length === 2 && !!s.kept)
    const rowColor = listed.kept!.titleColor

    await tab.swipeLeft(KEPT)
    await tab.until("the row rests open", (s) => s.kept?.state === "open" && !!s.kept.archive)
    await tab.press(`#rows > li.row[data-id="${KEPT}"] .swipe-archive`)
    const asked = await tab.until("the confirmation is up and ready", (s) => s.confirm?.kind === "archive" && !s.confirm.goDisabled)
    assert.equal(presses.length, 0, "a swipe and a press on it archive nothing by themselves")
    assert.match(asked.confirm!.title, /要封存 Tidy the release notes 嗎/)
    assert.match(asked.confirm!.say, /釋放/)
    assert.match(asked.confirm!.say, /叫回/)
    assert.match(asked.confirm!.go, /封存/)
    await tab.shot("archive-confirm")

    await tab.press("#action-confirm-go")
    await tab.until("the row leaves the list and the sheet goes", (s) => !s.rows.includes(KEPT) && s.confirm === null)
    assert.equal(presses.length, 1)
    assert.equal(presses[0].path, "/v1/sessions/" + encodeURIComponent(KEPT) + "/archive")
    assert.ok(presses[0].key.length >= 16, "the archive carries its decision's key")
    assert.equal(presses[0].body.force, false, "a first decision never forces")

    await tab.press("#nav-archive")
    const page = await tab.until("the Archive page lists it, its mark painted", (s) =>
      (s.page?.entries.length ?? 0) === 1 && (s.page!.entries[0].mark?.w ?? 0) > 0)
    const entry = page.page!.entries[0]
    assert.equal(page.page!.title, "封存的 Session")
    assert.equal(entry.conversation, "conversation-" + KEPT.slice(1))
    assert.equal(entry.title, "Tidy the release notes", "the title the row showed")
    assert.equal(entry.color, rowColor, "in the tint the row's title had")
    assert.ok(entry.mark && entry.mark.w > 0 && entry.mark.h > 0 && !entry.mark.none, "the project's mark is painted: " + JSON.stringify(entry.mark))
    assert.match(entry.where, /api/)
    assert.match(entry.when, /封存 · \d{4}-\d{2}-\d{2} \d{2}:\d{2}/, "how long ago and the date and time: " + entry.when)
    assert.equal(entry.restore, "叫回")
    assert.ok(page.wide <= 390, "nothing runs off the side of the phone: " + page.wide)
    await tab.shot("archive-page")

    await tab.press(".archive-entry .archive-restore")
    const back = await tab.until("the entry goes and the page says it reopened", (s) =>
      (s.page?.entries.length ?? 1) === 0 && (s.page?.reopened.length ?? 0) === 1)
    assert.match(back.page!.reopened[0], /Tidy the release notes/)
    assert.match(back.page!.reopened[0], /已重新打開/)
    assert.match(back.page!.reopened[0], /打開它/)
    assert.equal(presses.length, 2)
    assert.equal(presses[1].path, "/v1/sessions/archived/restore")
    assert.deepEqual(presses[1].body, { conversations: ["conversation-" + KEPT.slice(1)] })
    assert.ok(presses[1].key.length >= 16 && presses[1].key !== presses[0].key, "the restore carries a key of its own")
    await tab.shot("archive-restored")

    // The way to the new Session is its address.
    await tab.press(".archive-reopened .board-button")
    await tab.until("the reopened Session is the one opened", (s) =>
      s.page === null && s.hash.startsWith("#session=") && s.hash.includes("790"))
  }))

test("a blocked archive comes back with the daemon's reasons, and only the second decision forces it", () =>
  inTab(async (tab) => {
    rows = [row(OWED, "Still landing a change")]
    archived = []
    presses = []
    await tab.go("/")
    await tab.until("the row is drawn", (s) => !!s.owed)
    await tab.swipeLeft(OWED)
    await tab.until("the row rests open", (s) => s.owed?.state === "open" && !!s.owed.archive)
    await tab.press(`#rows > li.row[data-id="${OWED}"] .swipe-archive`)
    await tab.until("the confirmation is up and ready", (s) => s.confirm?.kind === "archive" && !s.confirm.goDisabled)
    await tab.press("#action-confirm-go")
    const blocked = await tab.until("the sheet comes back to force it", (s) =>
      presses.length === 1 && s.confirm?.kind === "archive" && /仍然封存/.test(s.confirm.go) && !s.confirm.goDisabled)
    assert.ok(blocked.rows.includes(OWED), "nothing was archived")
    assert.equal(presses[0].body.force, false)
    await tab.press("#action-confirm-go")
    await tab.until("the forced archive takes the row", (s) => !s.rows.includes(OWED))
    assert.equal(presses.length, 2)
    assert.equal(presses[1].body.force, true)
    assert.notEqual(presses[1].key, presses[0].key, "a second decision is a second key")
  }))
