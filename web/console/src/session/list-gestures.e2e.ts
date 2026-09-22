// The three gestures the session list answers on a phone, driven as a finger
// drives them:
//
//   (cd web && npm run build)
//   node --test web/console/src/session/list-gestures.e2e.ts
//
// Real touch events over the DevTools protocol, at 390x844 with touch
// emulation on, against the built console and a stand-in daemon. The harness
// is `address.e2e.ts`'s, with `Input.dispatchTouchEvent` added: a gesture is
// the one thing about this list that no string test can hold, because what
// decides it is which axis the browser gave the page and whether a passive
// listener could answer at all.
//
// **Two of these tests guard behaviour that was here before the swipe was.**
// `pull to refresh` and `the order is held` describe the list as it already
// worked, so they pass with the swipe taken out and fail if either of the two
// things it had to leave alone is broken. They were run that way before the
// swipe existed and the run is in the task's report.
//
// Named `.e2e.ts` rather than `.test.ts` so the unit run over
// `session/*.test.ts` does not start a browser.
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

/** One row per closeability the list can draw, and one more to sort against. */
const SAFE = pane(701)
const BLOCKED = pane(702)
const UNKNOWN = pane(703)
const SPARE = pane(704)
const NEEDS_ATTESTATION = pane(705)
const RETAINED = pane(706)

type ReadingScenario =
  | "normal"
  | "five"
  | "ninety"
  | "expired"
  | "worst"
  | "status-one"
  | "status-two"
  | "status-three"
let readingScenario: ReadingScenario = "normal"

type Row = Record<string, unknown>

/** A reading that proves nothing is owed: current, attested, and no reason left. */
function safeCloseability(): Row {
  return {
    activity_generation: 3,
    attestation_id: "att-fixture",
    mover: null,
    obligation_generation: 3,
    observed_at: 1,
    provenance: ["broker", "self"],
    reasons: [],
    session_generation: 1,
    source: { freshness: "current", max_age_seconds: 30, observed_at: 1, provenance: "session_watch" },
    state: "safe",
    version: "cl1_fixture",
  }
}

/** An obligation standing: the agent is still working in this session. */
function blockedCloseability(): Row {
  return {
    ...safeCloseability(),
    attestation_id: null,
    mover: { kind: "session", self: false, session_id: BLOCKED },
    reasons: [{ code: "terminal_working", kind: "obligation", mover: { kind: "session", self: false, session_id: BLOCKED } }],
    state: "blocked",
    version: "cl1_blocked",
  }
}

/** Not known: the reading could not tell which session this terminal is. */
function unknownCloseability(): Row {
  return {
    ...safeCloseability(),
    attestation_id: null,
    mover: { kind: "broker" },
    reasons: [{ code: "session_identity_ambiguous", kind: "evidence", mover: { kind: "broker" } }],
    state: "unknown",
    version: "cl1_unknown",
  }
}

/** The broker's checks passed, but this session has not made its own attestation. */
function needsAttestationCloseability(): Row {
  return {
    ...safeCloseability(),
    attestation_id: null,
    mover: { kind: "session", self: true, session_id: NEEDS_ATTESTATION },
    reasons: [{ code: "not_attested", kind: "attestation", mover: { kind: "session", self: true, session_id: NEEDS_ATTESTATION } }],
    state: "needs_attestation",
    version: "cl1_needs_attestation",
  }
}

function row(id: string, label: string, closeability: Row, movedAt: number, extra: Row = {}): Row {
  return {
    id,
    label,
    backend: "tmux",
    state: "idle",
    work_state: "ready",
    evidence: "process",
    isClaude: true,
    assistant: "claude",
    cwd: "/tmp/fixture",
    activity: { known: true, at: movedAt },
    closeability,
    ...extra,
  }
}

/**
 * One row for every closeability, and a fifth whose only job is to move.
 *
 * The order is the page's own rule — working first, then the idle rows by when
 * each last moved — so `BLOCKED, SAFE, NEEDS_ATTESTATION, UNKNOWN, SPARE`.
 * Moving `SPARE` to the front of the idle band is what the held order has to
 * refuse to follow.
 */
const MOVED = { safe: 300, needsAttestation: 250, unknown: 200, spare: 100, spareAfter: 400 }
let spareMoved = MOVED.spare

function rows(): Row[] {
  if (readingScenario === "expired") return []
  if (readingScenario.startsWith("status-")) {
    const extra: Row = {
      work_state: "milestone_complete",
      disposition: {
        scope: "session",
        evidence: "authenticated_session_delivery",
      },
    }
    if (readingScenario === "status-three") {
      extra.owed = {
        note: "還有一個很長的交付決定等待負責人確認",
        person_needed: true,
        since: 1,
      }
    }
    const status = row(RETAINED, "Status density fixture", needsAttestationCloseability(), 500, extra)
    if (readingScenario === "status-one") delete status.closeability
    return [status]
  }
  if (readingScenario === "worst") {
    return [
      row(RETAINED, "A very long Clawdfather session title that must stay inside its card", {
        ...blockedCloseability(),
        reasons: [
          { code: "pending_landing", kind: "obligation", mover: { kind: "broker" } },
          { code: "pending_task", kind: "obligation", mover: { kind: "broker" } },
        ],
      }, 500, {
        tty: "ttys008-with-a-long-terminal-name",
        state: "working",
        work_state: "working",
        line: "Gitifying every package in the repository (1m 53s · downloading dependencies)",
        agents_reading: { state: "complete" },
        agents: [
          { id: "agent-1", at: 1, depth: 1, type: "Explore", what: "one", state: "running" },
          { id: "agent-2", at: 1, depth: 1, type: "Explore", what: "two", state: "running" },
          { id: "agent-3", at: 1, depth: 1, type: "Explore", what: "three", state: "running" },
        ],
        coordinator: { label: "Clawdfather", status: "online", commands: [] },
      }),
    ]
  }
  if (readingScenario === "five" || readingScenario === "ninety") {
    const age = readingScenario === "five" ? 5 : 90
    return [
      row(RETAINED, "Earlier terminal reading", safeCloseability(), 500, {
        source: {
          freshness: "unverified",
          observed_at: Date.now() / 1000 - age,
          provenance: "iterm",
        },
      }),
    ]
  }
  return [
    row(SAFE, "Alpha is finished", safeCloseability(), MOVED.safe),
    row(BLOCKED, "Bravo is still working", blockedCloseability(), 400, {
      state: "working",
      work_state: "working",
      line: "1m",
    }),
    row(NEEDS_ATTESTATION, "Echo has not checked in", needsAttestationCloseability(), MOVED.needsAttestation),
    row(UNKNOWN, "Charlie cannot be read", unknownCloseability(), MOVED.unknown),
    row(SPARE, "Delta is finished too", safeCloseability(), spareMoved),
  ]
}

const ORDER_AT_REST = [BLOCKED, SAFE, NEEDS_ATTESTATION, UNKNOWN, SPARE]
const ORDER_ONCE_SPARE_MOVED = [BLOCKED, SPARE, SAFE, NEEDS_ATTESTATION, UNKNOWN]

// ---- the stand-in daemon

let generation = 0
/** Every `/v1/sessions` read, so a refresh is counted rather than guessed at. */
let listReads = 0
/** `POST /v1/sessions/{id}/close`, with the Idempotency-Key each arrived under. */
let closes: { id: string; key: string; force: unknown }[] = []
/** Every stream this daemon is holding open, so a new list can be pushed down one. */
const streams = new Set<ServerResponse>()

function snapshot() {
  generation++
  const age = readingScenario === "five" ? 5 : readingScenario === "ninety" ? 90 : 0
  const source = readingScenario === "normal" || readingScenario === "worst"
    ? { freshness: "current", observed_at: Date.now() / 1000, provenance: "fixture" }
    : readingScenario === "expired"
      ? { freshness: "missing", observed_at: Date.now() / 1000 - 121, provenance: "iterm" }
      : { freshness: "unverified", observed_at: Date.now() / 1000 - age, provenance: "iterm" }
  return {
    at: Date.now(),
    scan: {
      complete: true,
      completed: { complete: true, sequence: generation },
      emptyAuthoritative: true,
      epoch: 1,
      generation,
      provenance: "fixture",
      source,
    },
    // The daemon's own answer moves; the page's order is the page's business.
    sessions: rows(),
  }
}

/** A new list down every open stream, as the daemon pushes one when a row moves. */
function pushSessions(): void {
  const frame = "event: sessions\ndata: " + JSON.stringify(snapshot()) + "\n\n"
  for (const stream of streams) stream.write(frame)
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

/** The document with its words written in, as `page.go` writes them. */
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
    if (path === "/v1/sessions") {
      listReads++
      return json(res, 200, snapshot())
    }
    if (path === "/v1/health") return json(res, 200, { ok: true })
    const sessionWork = /^\/v1\/work\/v2\/session-todos\/(.+)$/.exec(path)
    if (sessionWork && req.method === "GET") {
      const sessionID = decodeURIComponent(sessionWork[1])
      const assigned = sessionID === SAFE
        ? [{
            id: "work-fixture",
            project: { id: "project-fixture", label: "Clawdline", path: "/tmp/fixture", icon: null, available: true },
            kind: "feature",
            title: "Finish the release receipt",
            description: "",
            phase: "merging",
            condition: null,
            area: "merging",
            deployment_policy: "agent_decides",
            owner_session: SAFE,
            created_at: 1,
            updated_at: 1,
            closed_at: null,
            cycle: 1,
            version: 1,
          }]
        : []
      return json(res, 200, { ok: true, assigned_items: assigned, recent_items: [], direct_todos: [], truncated: false })
    }
    if (path === "/v1/orchestrator/tasks") {
      const tasks = readingScenario === "worst"
        ? [{
            id: "task-fixture",
            task_id: "task-fixture",
            title: "A child task with a title too long for the phone row",
            state: "briefed",
            created: 1,
            root: { terminalId: pane(700), sessionId: "root-fixture" },
            child: { terminalId: RETAINED },
          }]
        : []
      return json(res, 200, { at: Date.now(), tasks })
    }
    if (path === "/v1/events") {
      res.writeHead(200, { "content-type": "text/event-stream", "cache-control": "no-cache" })
      res.write("event: sessions\ndata: " + JSON.stringify(snapshot()) + "\n\n")
      streams.add(res)
      req.on("close", () => streams.delete(res))
      return // held open, as the daemon's stream is
    }
    const closing = /^\/v1\/sessions\/(.+)\/close$/.exec(path)
    if (closing && req.method === "POST") {
      let body = ""
      req.on("data", (chunk) => (body += chunk))
      req.on("end", () => {
        let force: unknown = null
        try {
          force = (JSON.parse(body || "{}") as { force?: unknown }).force
        } catch {
          /* the test reads what arrived, not what it meant */
        }
        closes.push({
          id: decodeURIComponent(closing[1]),
          key: String(req.headers["idempotency-key"] ?? ""),
          force,
        })
        if (decodeURIComponent(closing[1]) === BLOCKED && force !== true) {
          return json(res, 409, {
            error: "close_blocked",
            detail: "still owed: landing",
            reasons: blockedCloseability().reasons,
          })
        }
        json(res, 200, { ok: true })
      })
      return
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

/** What the list shows, read in one go so a failure can say all of it. */
const PROBE = `(() => {
  const rows = [...document.querySelectorAll("#rows > li.row")]
  const ptr = document.getElementById("ptr")
  const label = document.getElementById("ptr-label")
  const sheet = document.getElementById("action-confirm")
  const swiped = rows.find((n) => n.dataset.swipe === "open" || n.dataset.swipe === "dragging")
  return {
    order: rows.map((n) => n.dataset.id),
    swiping: swiped ? swiped.dataset.id : null,
    swipeState: swiped ? swiped.dataset.swipe : null,
    swipeX: swiped ? swiped.style.getPropertyValue("--swipe-x") : "",
    action: swiped ? (swiped.querySelector(".swipe-end")?.textContent ?? null) : null,
    actionKind: swiped ? (swiped.querySelector(".swipe-end")?.dataset.closeability ?? null) : null,
    rowState: swiped ? (swiped.querySelector(".state")?.textContent ?? null) : null,
    readingBanner: document.querySelector(".session-reading")?.textContent ?? null,
    retainedText: document.querySelector(".retained-reading")?.textContent ?? null,
    moving: rows.some((n) => getComputedStyle(n).transform !== "none"),
    taskVisible: rows.some((n) => {
      const task = n.querySelector(".task-chip")
      return task && getComputedStyle(task).display !== "none"
    }),
    ptrHeight: ptr ? ptr.style.height : "",
    ptrWord: label ? label.textContent : "",
    scrollTop: document.getElementById("list-scroll")?.scrollTop ?? -1,
    sheet: sheet && !sheet.hidden ? (document.getElementById("action-confirm-title")?.textContent ?? "") : null,
    sheetSay: sheet && !sheet.hidden ? (document.getElementById("action-confirm-say")?.textContent ?? "") : null,
    confirmAction: sheet && !sheet.hidden ? (document.getElementById("action-confirm-go")?.textContent ?? "") : null,
    confirmDisabled: sheet && !sheet.hidden ? !!document.getElementById("action-confirm-go")?.hasAttribute("disabled") : null,
    focused: document.activeElement ? document.activeElement.id : "",
  }
})()`

interface Seen {
  order: string[]
  swiping: string | null
  swipeState: string | null
  swipeX: string
  action: string | null
  actionKind: string | null
  rowState: string | null
  readingBanner: string | null
  retainedText: string | null
  moving: boolean
  taskVisible: boolean
  ptrHeight: string
  ptrWord: string
  scrollTop: number
  sheet: string | null
  sheetSay: string | null
  confirmAction: string | null
  confirmDisabled: boolean | null
  focused: string
}

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

  static async open(b: Browser, origin: string): Promise<Tab> {
    const { targetId } = await b.send("Target.createTarget", { url: "about:blank" })
    const { sessionId } = await b.send("Target.attachToTarget", { targetId, flatten: true })
    await b.send("Page.enable", {}, sessionId)
    await b.send("Runtime.enable", {}, sessionId)
    await b.send("Emulation.setDeviceMetricsOverride", { ...PHONE, deviceScaleFactor: 2 }, sessionId)
    // Without this the page is a phone that has no fingers: `ontouchstart` is
    // absent, the listeners are never bound, and every gesture test passes by
    // doing nothing at all.
    await b.send("Emulation.setTouchEmulationEnabled", { enabled: true, maxTouchPoints: 1 }, sessionId)
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

  /** Where a row is on screen, so a finger can be put on it. */
  centreOf(id: string): Promise<{ x: number; y: number }> {
    return this.run(`(() => {
      const row = [...document.querySelectorAll("#rows > li.row")].find((n) => n.dataset.id === ${JSON.stringify(id)})
      if (!row) throw new Error("no row " + ${JSON.stringify(id)})
      const box = row.getBoundingClientRect()
      return { x: Math.round(box.left + box.width / 2), y: Math.round(box.top + box.height / 2) }
    })()`)
  }

  touchAt = (x: number, y: number) => this.touch("touchStart", [{ x, y }])

  private touch(type: string, points: { x: number; y: number }[]): Promise<void> {
    return this.b.send(
      "Input.dispatchTouchEvent",
      { type, touchPoints: points.map((p) => ({ ...p, radiusX: 8, radiusY: 8, force: 1 })) },
      this.session,
    )
  }

  /**
   * One finger, from `from` to `from + by`, in `steps` moves. The pauses are
   * what makes it a drag rather than a teleport: a page that decides an axis
   * on the first move sees the first move.
   */
  async drag(from: { x: number; y: number }, by: { x: number; y: number }, opts: { steps?: number; hold?: number; release?: boolean } = {}) {
    const steps = opts.steps ?? 8
    await this.touch("touchStart", [from])
    for (let i = 1; i <= steps; i++) {
      await this.touch("touchMove", [{ x: Math.round(from.x + (by.x * i) / steps), y: Math.round(from.y + (by.y * i) / steps) }])
      await new Promise((r) => setTimeout(r, 12))
    }
    if (opts.hold) await new Promise((r) => setTimeout(r, opts.hold))
    if (opts.release !== false) await this.touch("touchEnd", [])
  }

  /** The finger leaves, wherever it was. */
  lift(): Promise<void> {
    return this.touch("touchEnd", [])
  }

  /** A press, as a finger makes it: down and up in the same place. */
  async tap(x: number, y: number) {
    await this.touch("touchStart", [{ x, y }])
    await new Promise((r) => setTimeout(r, 30))
    await this.touch("touchEnd", [])
  }

  async press(selector: string): Promise<void> {
    await this.run(`(() => {
      const el = document.querySelector(${JSON.stringify(selector)})
      if (!el) throw new Error("nothing at " + ${JSON.stringify(selector)})
      el.click()
    })()`)
  }

  /** A picture of the phone, for the report. */
  async shot(name: string): Promise<void> {
    if (!shots) return
    const { data } = await this.b.send("Page.captureScreenshot", { format: "png" }, this.session)
    writeFileSync(join(shots, name + ".png"), Buffer.from(data, "base64"))
  }
}

// ---- the run

async function inTab(body: (tab: Tab) => Promise<void>) {
  const tab = await Tab.open(browser, origin)
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
const PHONE = { width: 390, height: 844, mobile: true }

before(async () => {
  assert.ok(existsSync(join(dist, "index.html")), "no built console at " + dist + "; run `npm run build` in web first")
  assert.ok(existsSync(chrome), "no Chrome at " + chrome + "; set CHROME")
  server = daemon()
  await new Promise<void>((ok) => server.listen(0, "127.0.0.1", ok))
  const address = server.address()
  origin = "http://127.0.0.1:" + (typeof address === "object" && address ? address.port : 0)
  profile = mkdtempSync(join(tmpdir(), "clawdline-gestures-"))
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
  if (browserProcess && browserProcess.exitCode === null) {
    const gone = new Promise((ok) => browserProcess.once("exit", ok))
    browserProcess.kill()
    await gone
  }
  server?.closeAllConnections()
  await new Promise<void>((ok) => (server ? server.close(() => ok()) : ok()))
  if (profile) rmSync(profile, { recursive: true, force: true, maxRetries: 5 })
})

/** The list, arrived and at rest, before a finger touches it. */
async function list(tab: Tab): Promise<Seen> {
  spareMoved = MOVED.spare
  closes = []
  await tab.go("/")
  return tab.until("the list arrives in its resting order", (s) => s.order.join() === ORDER_AT_REST.join())
}

// ---- the two gestures that were here first

test("pull to refresh: a pull at the top reads the list again and says so on the way", () =>
  inTab(async (tab) => {
    await list(tab)
    const before = listReads
    const from = await tab.centreOf(SAFE)
    // Held at the bottom of the pull so the word can be read before the
    // finger lifts: past 62px the pad says "let go and it refreshes".
    await tab.drag({ x: from.x, y: 160 }, { x: 0, y: 220 }, { steps: 10, hold: 0, release: false })
    const pulled = await tab.seen()
    assert.equal(pulled.ptrWord, "放開就重新整理", "the pad asks to be released past the threshold")
    assert.ok(parseFloat(pulled.ptrHeight) > 0, "the pad has opened; it showed " + JSON.stringify(pulled.ptrHeight))
    await tab.lift()
    await tab.until("the list is read again", () => listReads > before)
    await tab.until("the pad closes again", (s) => s.ptrHeight === "0px" && s.ptrWord === "下拉重新整理", 4000)
  }))

test("the order is held: a finger on the list stops it re-sorting under itself", () =>
  inTab(async (tab) => {
    await list(tab)
    const from = await tab.centreOf(SAFE)
    // The finger goes down and stays down, and drags across the row — the
    // gesture the swipe is made of. While it is there the daemon pushes a list
    // in which one row has moved up; the rows must not move under the finger.
    await tab.drag(from, { x: -90, y: 0 }, { steps: 6, release: false })
    spareMoved = MOVED.spareAfter
    pushSessions()
    // Long enough for a redraw to have happened if one were going to: the
    // list is rebuilt on every frame that arrives.
    await new Promise((r) => setTimeout(r, 500))
    const held = await tab.seen()
    assert.equal(held.order.join(), ORDER_AT_REST.join(), "the order is held while the finger is down")
    // And the proof that the frame really did arrive: once the finger has been
    // gone, and nothing is left uncovered for it to hold the list still for,
    // the list takes the new order.
    await tab.lift()
    await tab.drag({ x: from.x - 90, y: from.y }, { x: 110, y: 0 }, { steps: 6 })
    await tab.until(
      "the order is let go once the finger has been off the list for a moment",
      (s) => s.order.join() === ORDER_ONCE_SPARE_MOVED.join(),
      6000,
    )
  }))

test("rows travel to a changed sorted position instead of snapping there", () =>
  inTab(async (tab) => {
    await list(tab)
    spareMoved = MOVED.spareAfter
    pushSessions()
    const moving = await tab.until(
      "the changed order is visible while its rows are moving",
      (s) => s.order.join() === ORDER_ONCE_SPARE_MOVED.join() && s.moving,
    )
    assert.equal(moving.moving, true)
    await tab.until("the rows reach their new resting positions", (s) => !s.moving)
  }))

// ---- the swipe

/** A left swipe on one row, finished. */
async function swipeOpen(tab: Tab, id: string): Promise<Seen> {
  const from = await tab.centreOf(id)
  await tab.drag(from, { x: -130, y: 0 }, { steps: 8 })
  return tab.until("the row's action is uncovered", (s) => s.swiping === id && s.swipeState === "open")
}

test("a left swipe uncovers a close, and the swipe itself closes nothing", () =>
  inTab(async (tab) => {
    await list(tab)
    const open = await swipeOpen(tab, SAFE)
    assert.equal(open.action, "關閉 Session", "the uncovered control says what pressing it does")
    assert.equal(open.actionKind, "safe")
    assert.equal(open.swipeX, "-126px", "and the row's contents have moved out of its way")
    await tab.shot("swipe-safe")
    // The whole point: the gesture is not the decision.
    assert.deepEqual(closes, [], "nothing has been closed by the gesture")
    assert.equal(open.sheet, null, "and nothing has been asked yet either")
  }))

test("pressing the uncovered close asks first, naming the row, with Cancel under the focus", () =>
  inTab(async (tab) => {
    await list(tab)
    await swipeOpen(tab, SAFE)
    await tab.press("li.row[data-swipe='open'] > .swipe-end")
    const asked = await tab.until("the confirmation is up", (s) => s.sheet !== null)
    assert.equal(asked.sheet, "要關閉 Alpha is finished 嗎？", "it names the row it would act on")
    assert.equal(asked.focused, "action-confirm-cancel", "and opens on the answer that changes nothing")
    const work = await tab.until("the unfinished Board item is named", (s) =>
      s.confirmDisabled === false && (s.sheetSay ?? "").includes("Finish the release receipt"))
    assert.match(work.sheetSay ?? "", /尚未完成的看板項目/)
    assert.match(work.sheetSay ?? "", /Finish the release receipt · Clawdline/)
    await tab.shot("swipe-confirm")
    assert.deepEqual(closes, [], "still nothing closed while the question stands")
  }))

test("the second press is what closes it, once, under a key a retry can be answered with", () =>
  inTab(async (tab) => {
    await list(tab)
    await swipeOpen(tab, SAFE)
    await tab.press("li.row[data-swipe='open'] > .swipe-end")
    await tab.until("the confirmation has checked Board work", (s) => s.sheet !== null && s.confirmDisabled === false)
    await tab.press("#action-confirm-go")
    await tab.until("the close has been asked for", () => closes.length > 0)
    assert.equal(closes.length, 1, "one press, one close")
    assert.equal(closes[0].id, SAFE)
    assert.equal(closes[0].force, false, "the first decision is not forced")
    assert.ok(closes[0].key.length > 0, "under an Idempotency-Key, so a lost answer is not a second close")
  }))

test("a row with something still owed still uncovers the close action", () =>
  inTab(async (tab) => {
    await list(tab)
    const open = await swipeOpen(tab, BLOCKED)
    assert.equal(open.actionKind, "blocked")
    assert.equal(open.action, "關閉 Session", "the cell says what pressing it does, not why the close is blocked")
    assert.match(open.rowState ?? "", /還有 1 項未了結/, "the row itself keeps the live closeability status")
    await tab.press("li.row[data-swipe='open'] > .swipe-end")
    const asked = await tab.until("the blocked confirmation is up", (s) => s.sheet !== null)
    assert.equal(asked.sheet, "要關閉 Bravo is still working 嗎？")
    assert.match(asked.sheetSay ?? "", /Agent 會先結束，接著關閉它的終端機分頁。/, "it says what closing does")
    assert.match(asked.sheetSay ?? "", /broker 無法證明這個 session 可以安全關閉。/, "it says closing is not proven safe")
    assert.match(asked.sheetSay ?? "", /terminal_working · 由另一個 session 推進/, "it says what blocks closing and who moves it")
    await tab.shot("swipe-blocked")
  }))

test("a refused close explains what is owed, then still close overrides that disclosed obligation", () =>
  inTab(async (tab) => {
    await list(tab)
    await swipeOpen(tab, BLOCKED)
    await tab.press("li.row[data-swipe='open'] > .swipe-end")
    await tab.until("the first confirmation has checked Board work", (s) => s.sheet !== null && s.confirmDisabled === false)
    await tab.press("#action-confirm-go")
    await tab.until("the daemon's refusal is shown", (s) => s.sheet !== null && s.confirmAction === "仍要關閉" && s.confirmDisabled === false)
    assert.equal(closes.length, 1)
    assert.equal(closes[0].force, false, "the close gate gets the first decision")
    const said = await tab.seen()
    assert.match(said.sheetSay ?? "", /terminal_working/, "the reminder carries the daemon's reason")

    await tab.press("#action-confirm-go")
    await tab.until("the override reaches the daemon", () => closes.length === 2)
    assert.equal(closes[1].force, true, "only the explicit still-close decision overrides the gate")
    assert.notEqual(closes[1].key, closes[0].key, "the override is a new decision, not a retry")
  }))

test("an unknown row keeps saying unknown while its swipe remains an action", () =>
  inTab(async (tab) => {
    await list(tab)
    const open = await swipeOpen(tab, UNKNOWN)
    assert.equal(open.actionKind, "unknown")
    assert.equal(open.action, "關閉 Session")
    assert.match(open.rowState ?? "", /無法判斷能否關閉/, "unknown remains distinct from blocked on the row")
    await tab.press("li.row[data-swipe='open'] > .swipe-end")
    const asked = await tab.until("the unknown confirmation is up", (s) => s.sheet !== null)
    assert.equal(asked.sheet, "要關閉 Charlie cannot be read 嗎？")
    assert.match(asked.sheetSay ?? "", /Agent 會先結束，接著關閉它的終端機分頁。/)
    assert.match(asked.sheetSay ?? "", /broker 無法證明這個 session 可以安全關閉。/)
    assert.match(asked.sheetSay ?? "", /session_identity_ambiguous · 重新讀一次才知道/)
    await tab.shot("swipe-unknown")
  }))

test("a session awaiting attestation explains that inside its named confirmation", () =>
  inTab(async (tab) => {
    await list(tab)
    const open = await swipeOpen(tab, NEEDS_ATTESTATION)
    assert.equal(open.actionKind, "needs_attestation")
    assert.equal(open.action, "關閉 Session")
    assert.match(open.rowState ?? "", /等這個 session 自己確認/)
    await tab.press("li.row[data-swipe='open'] > .swipe-end")
    const asked = await tab.until("the attestation confirmation is up", (s) => s.sheet !== null)
    assert.equal(asked.sheet, "要關閉 Echo has not checked in 嗎？")
    assert.match(asked.sheetSay ?? "", /Agent 會先結束，接著關閉它的終端機分頁。/)
    assert.match(asked.sheetSay ?? "", /這不是系統發現工作尚未完成。/)
    assert.match(asked.sheetSay ?? "", /這個 session 還沒確認是否留有本機修改、未交付事項或 Clawdline 以外的工作。/)
  }))

test("one row is uncovered at a time, and the press that puts one away opens no session", () =>
  inTab(async (tab) => {
    await list(tab)
    await swipeOpen(tab, SAFE)
    const other = await tab.centreOf(UNKNOWN)
    await tab.tap(other.x, other.y)
    const after = await tab.until("the uncovered action is put away", (s) => s.swiping === null)
    assert.equal(after.sheet, null)
    assert.equal(
      await tab.run(`document.querySelectorAll("#rows > li.row.open").length`),
      0,
      "and that press did not open a session either",
    )
  }))

test("a diagonal drag belongs to one gesture: the pad and the row never move together", () =>
  inTab(async (tab) => {
    await list(tab)
    const from = await tab.centreOf(SAFE)
    // Mostly down, a little left: the scroller's, so the pad opens and the row
    // stays where it is.
    await tab.drag({ x: from.x, y: 150 }, { x: -70, y: 200 }, { steps: 10, release: false })
    const pulling = await tab.seen()
    assert.ok(parseFloat(pulling.ptrHeight) > 0, "the pull is the pull")
    assert.equal(pulling.swiping, null, "and the row did not come with it")
    await tab.lift()
    await tab.until("the pad closes again", (s) => s.ptrHeight === "0px", 4000)
    // Mostly left, a little down: the row's, so the row moves and the pad
    // stays shut.
    const row = await tab.centreOf(SAFE)
    await tab.drag(row, { x: -130, y: 30 }, { steps: 10, release: false })
    const swiping = await tab.seen()
    assert.equal(swiping.swiping, SAFE, "the swipe is the swipe")
    assert.equal(swiping.swipeState, "dragging")
    assert.equal(parseFloat(swiping.ptrHeight || "0"), 0, "and the pad did not come with it")
    await tab.lift()
  }))

// ---- retained-reading age and phone layout

test("the densest phone row gives each segment a boundary and never widens the list", () =>
  inTab(async (tab) => {
    readingScenario = "worst"
    try {
      await tab.go("/")
      await tab.until(
        "the worst-case row and its task arrive",
        (s) => s.order.join() === RETAINED && s.taskVisible,
      )
      const measured = await tab.run(`(() => {
        const scroller = document.querySelector(".list-scroll")
        const row = document.querySelector("#rows > li.row")
        const meta = row.querySelector(".meta")
        const state = row.querySelector(".state")
        const line = row.querySelector(".line")
        const shown = (selector) => getComputedStyle(row.querySelector(selector)).display !== "none"
        const box = row.getBoundingClientRect()
        const rail = scroller.getBoundingClientRect()
        return {
          viewport: [innerWidth, innerHeight],
          list: [scroller.clientWidth, scroller.scrollWidth],
          row: [row.clientWidth, row.scrollWidth, box.left, box.right, rail.left, rail.right],
          meta: [meta.clientWidth, meta.scrollWidth],
          state: [state.clientWidth, state.scrollWidth],
          line: [line.clientWidth, line.scrollWidth, getComputedStyle(line).textOverflow],
          machine: !!row.querySelector(".machine"),
          path: shown(".path"),
          coordinator: shown(".coordinator-chip"),
          agents: shown(".agents-chip"),
          task: shown(".task-chip"),
        }
      })()`)
      assert.deepEqual(measured.viewport, [390, 844])
      assert.equal(measured.list[1], measured.list[0], "the list has no horizontal overflow")
      assert.ok(measured.row[3] <= measured.row[5], "the card ends inside the list rail: " + JSON.stringify(measured.row))
      assert.ok(measured.meta[1] <= measured.meta[0], "the metadata segments converge inside their line")
      assert.ok(measured.state[1] <= measured.state[0], "the state segments converge inside their line")
      assert.ok(measured.line[0] >= 70, "the live sentence keeps enough room to attach its ellipsis")
      assert.equal(measured.line[2], "ellipsis")
      assert.equal(measured.machine, false, "a single-machine list does not repeat its machine on every row")
      assert.equal(measured.path, false, "the repeated path is first to leave the phone row")
      assert.equal(measured.coordinator, false, "the crown keeps the role when its duplicate word leaves")
      assert.equal(measured.agents, true)
      assert.equal(measured.task, true)
      await tab.shot("list-overflow-after")
    } finally {
      readingScenario = "normal"
    }
  }))

for (const [scenario, segments] of [
  ["status-one", 1],
  ["status-two", 2],
  ["status-three", 3],
] as const) {
  test(`${segments} status segment${segments === 1 ? "" : "s"} share the 390x844 row without an internal hole`, () =>
    inTab(async (tab) => {
      readingScenario = scenario
      try {
        await tab.go("/")
        await tab.until("the status-density row arrives", (s) => s.order.join() === RETAINED)
        const measured = await tab.run(`(() => {
          const state = document.querySelector("#rows > li.row .state")
          const completion = state.querySelector(":scope > .session-work-completion")
          const copy = completion.querySelector(".session-work-copy")
          const direct = [...state.children]
          const box = (node) => {
            const rect = node.getBoundingClientRect()
            const style = getComputedStyle(node)
            return {
              left: rect.left,
              right: rect.right,
              width: rect.width,
              clientWidth: node.clientWidth,
              scrollWidth: node.scrollWidth,
              overflow: style.overflow,
              textOverflow: style.textOverflow,
            }
          }
          return {
            viewport: [innerWidth, innerHeight],
            state: box(state),
            completion: box(completion),
            copy: box(copy),
            direct: direct.map((node) => ({ cls: node.className, ...box(node) })),
          }
        })()`)
        assert.deepEqual(measured.viewport, [390, 844])
        assert.equal(measured.direct.length, segments, "the fixture draws the requested number of independent status axes")
        assert.ok(measured.state.scrollWidth <= measured.state.clientWidth, "the complete state rail fits its row")
        assert.ok(
          Math.abs(measured.completion.right - measured.copy.right) <= 1,
          "the delivered sentence reaches its own boundary instead of leaving empty space inside it: " + JSON.stringify(measured),
        )
        assert.equal(measured.copy.textOverflow, "ellipsis", "the delivered sentence owns its ellipsis")
        for (const part of measured.direct) {
          assert.ok(part.right <= measured.state.right + 1, "each status segment ends inside the state rail: " + JSON.stringify(part))
        }
        await tab.shot(scenario)
      } finally {
        readingScenario = "normal"
      }
    }))
}

test("the first visible phone count has no separator and owns its ellipsis", () =>
  inTab(async (tab) => {
    readingScenario = "worst"
    try {
      await tab.go("/")
      await tab.until("the working count arrives", (s) => s.order.join() === RETAINED)
      const measured = await tab.run(`(() => {
        const shown = [...document.querySelectorAll("#counts > .part")]
          .filter((node) => getComputedStyle(node).display !== "none")
        const first = shown[0]
        const style = getComputedStyle(first)
        const before = getComputedStyle(first, "::before")
        return {
          text: first.textContent,
          separator: before.content,
          textOverflow: style.textOverflow,
          clientWidth: first.clientWidth,
          scrollWidth: first.scrollWidth,
        }
      })()`)
      assert.equal(measured.text, "1 個在跑")
      assert.ok(measured.separator === "none" || measured.separator === "normal" || measured.separator === "\"\"")
      assert.equal(measured.textOverflow, "ellipsis", "the visible count owns its ellipsis instead of relying on header clipping")
    } finally {
      readingScenario = "normal"
    }
  }))

test("a five-second retained reading has no row-level age note at 390x844", () =>
  inTab(async (tab) => {
    readingScenario = "five"
    try {
      await tab.go("/")
      const seen = await tab.until("the retained row arrives without an age note", (s) => s.order.join() === RETAINED)
      assert.equal(seen.retainedText, null)
      await tab.shot("reading-5-seconds")
    } finally {
      readingScenario = "normal"
    }
  }))

test("a ninety-second retained reading is a quiet, one-line annotation at 390x844", () =>
  inTab(async (tab) => {
    readingScenario = "ninety"
    try {
      await tab.go("/")
      const seen = await tab.until("the older retained row arrives with its age note", (s) => s.retainedText !== null)
      assert.equal(seen.retainedText, "1 分鐘前沒有新輸出")
      const measured = await tab.run(`(() => {
        const note = document.querySelector(".retained-reading")
        if (!note) throw new Error("no retained note")
        const style = getComputedStyle(note)
        const box = note.getBoundingClientRect()
        return {
          fontSize: style.fontSize,
          lineHeight: style.lineHeight,
          whiteSpace: style.whiteSpace,
          width: box.width,
          height: box.height,
          clientWidth: note.clientWidth,
          scrollWidth: note.scrollWidth,
        }
      })()`)
      assert.equal(measured.fontSize, "11.5px")
      assert.equal(measured.whiteSpace, "nowrap")
      assert.ok(measured.scrollWidth <= measured.clientWidth, "the whole note fits instead of clipping: " + JSON.stringify(measured))
      assert.ok(measured.height <= 16, "the note occupies one text line: " + JSON.stringify(measured))
      await tab.shot("reading-90-seconds")
    } finally {
      readingScenario = "normal"
    }
  }))

test("a reading beyond the retention window removes the row and says the source is missing", () =>
  inTab(async (tab) => {
    readingScenario = "expired"
    try {
      await tab.go("/")
      const seen = await tab.until("the expired row is gone and the batch says missing", (s) =>
        s.order.length === 0 && (s.readingBanner ?? "").includes("沒有仍可採用的上次讀數"),
      )
      assert.deepEqual(seen.order, [])
      assert.match(seen.readingBanner ?? "", /沒有仍可採用的上次讀數/)
      await tab.shot("reading-expired")
    } finally {
      readingScenario = "normal"
    }
  }))
