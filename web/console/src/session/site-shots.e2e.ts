// The website's feature pictures, photographed from the built console against
// a stand-in daemon that serves one of two small stories (`site-shots.ts`).
// `tools/site-shots.sh` builds the console and runs this once per language;
// without SITE_SHOTS every test skips. docs/site-shots.md is the whole of it.
import { test, before, after } from "node:test"
import assert from "node:assert/strict"
import { spawn, type ChildProcess } from "node:child_process"
import { createServer, type Server, type ServerResponse } from "node:http"
import { existsSync, mkdtempSync, readFileSync, rmSync, statSync, writeFileSync } from "node:fs"
import { tmpdir } from "node:os"
import { dirname, extname, join, normalize, resolve } from "node:path"
import { fileURLToPath } from "node:url"
// @ts-expect-error -- Node's strip-types test runner needs the source extension.
import { SITE_CALLOUTS, SITE_LANG, SITE_SHOTS, STORY, calloutScript, inventedScreenshotScript, siteSuffix, type Callout } from "./site-shots.ts"

const dist = resolve(dirname(fileURLToPath(import.meta.url)), "../../dist")
const chrome = process.env.CHROME || "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
const now = Math.floor(Date.now() / 1000)
const PROJECT_PATH = "~/code/shop"
const TRIP_PATH = "~/code/trips"
const TRIP = STORY.trip
const SQUAD = STORY.squad
const SQUAD_PATH = "~/code/budget"

const CHECKOUT = "site-checkout"
const SIGN_IN = "site-sign-in"
const SEARCH = "site-search"
const conversation = (id: string) => "conversation-" + id

const REFACTOR = "site-refactor"
const SYNC = "site-sync-tests"
const CALENDAR = "site-calendar"

// The Session asking the question is waiting on the person; the picture of the
// queued to-do shows it working again after the answer.
let checkoutWaiting = true
// Which story the daemon tells: the shop's, or the blog's trip planner, whose
// refactoring Session asks the person through its terminal's own menu.
let story: "shop" | "trip" | "squad" = "shop"
let refactorAsking = false
// The squad's code review has finished and reported back to its coordinator.
let reviewFinished = false

const COORDINATOR = "site-coordinator"
const CHILDREN = { backend: "site-backend", checker: "site-checker", review: "site-review" } as const
// The child's built-in role, by the id the daemon's catalog uses.
const ROLES = { backend: "backend", checker: "reality-checker", review: "code-reviewer" } as const

/**
 * The four built-in roles the squad uses, as `GET /v1/personas` serves them:
 * names and summaries read from the catalog itself, icons copied from
 * `internal/domain/persona/icons.go`.
 */
const ICONS: Record<string, { accent: string; palette: Record<string, string>; rows: string[] }> = {
  architect: { accent: "B", palette: { B: "#6c7bd9", o: "#141416", Y: "#f2c14e", y: "#c9962a" },
    rows: ["..YYYY..", ".YYYYYY.", "yyyyyyyy", ".BoBBoB.", ".BBBBBB.", ".BBBBBB.", "..B..B.."] },
  backend: { accent: "B", palette: { B: "#3f8f86", o: "#141416", A: "#9be7c4", g: "#56e39a", d: "#24524c" },
    rows: ["...AA...", "...dd...", ".BBBBBB.", ".BoBBoB.", ".dddddd.", ".dgdgdg.", "..B..B.."] },
  "reality-checker": { accent: "B", palette: { B: "#e0664f", o: "#141416", c: "#2f9e5b", W: "#f4efe3", A: "#ffd166" },
    rows: ["...AA...", "...BB...", ".BBBBBB.", ".BoBBoB.", ".BWWWcB.", ".BcWcWB.", ".BWcWWB."] },
  "code-reviewer": { accent: "B", palette: { B: "#a371f7", o: "#141416", G: "#f0f0f0", A: "#f0f0f0" },
    rows: ["...AA...", "...BB...", ".BBBBBB.", "GGGGGGGG", ".GoBBoG.", ".BBBBBB.", "..B..B.."] },
}
const catalog = resolve(dirname(fileURLToPath(import.meta.url)), "../../../../internal/domain/persona/catalog")
const personas = () => ({
  license: "MIT",
  personas: Object.entries(ICONS).map(([id, art]) => {
    const head = readFileSync(join(catalog, id + ".md"), "utf8").split("\n---")[0]
    const field = (key: string) => (new RegExp("^" + key + ": (.+)$", "m").exec(head)?.[1] ?? "").trim()
    return {
      id, name: { en: field("name_en"), "zh-Hant": field("name_zh") }, summary: { en: field("summary_en"), "zh-Hant": field("summary_zh") },
      suggested_kinds: [], icon: { accent: art.palette[art.accent], cells: art.rows.map((row) => [...row].map((c) => c === "." ? null : art.palette[c])) },
    }
  }),
})

const squadSessions = () => [
  session(COORDINATOR, SQUAD.coordinator, 1, { cwd: SQUAD_PATH, persona: "architect", state: "working", work_state: "working", line: "12m" }),
  session(CHILDREN.backend, SQUAD.children.backend, 1, { cwd: SQUAD_PATH, persona: ROLES.backend, state: "working", work_state: "working", line: "8m" }),
  session(CHILDREN.checker, SQUAD.children.checker, 3, { cwd: SQUAD_PATH, persona: ROLES.checker, assistant: "codex", isClaude: false, state: "working", work_state: "working", line: "5m" }),
  session(CHILDREN.review, SQUAD.children.review, reviewFinished ? 1 : 2, { cwd: SQUAD_PATH, persona: ROLES.review,
    ...(reviewFinished ? {} : { state: "working", work_state: "working", line: "3m" }) }),
]

// The coordinator's dispatched tasks, which is what nests each child under it.
const squadTasks = () => (Object.keys(CHILDREN) as (keyof typeof CHILDREN)[]).map((key, index) => ({
  id: "site-task-" + key, kind: "custom", title: SQUAD.children[key], created: now - 900 + index * 60,
  ...(key === "review" && reviewFinished ? { state: "success", finishedAt: now - 120 } : { state: "briefed" }),
  root: { terminalId: COORDINATOR, sessionId: conversation(COORDINATOR) }, child: { terminalId: CHILDREN[key] },
}))

function session(id: string, label: string, minutesAgo: number, extra: Record<string, unknown> = {}) {
  return {
    id, label, backend: "tmux", state: "idle", work_state: "ready", evidence: "process",
    isClaude: true, assistant: "claude", sessionId: conversation(id), cwd: PROJECT_PATH,
    activity: { known: true, at: now - minutesAgo * 60 },
    closeability: { activity_generation: 1, attestation_id: null, mover: null, obligation_generation: 1, observed_at: 1,
      provenance: [], reasons: [], session_generation: 1, source: "broker", state: "safe", version: "site" },
    ...extra,
  }
}

const menu = () => ({
  question: TRIP.question,
  // The caret on none of the rows, so no "highlighted" word sits in the picture.
  options: TRIP.options.map(([label, detail], index) => ({ n: index + 1, label, detail, can: true, selected: false })),
})

const tripSessions = () => [
  session(REFACTOR, TRIP.refactor, 1, { cwd: TRIP_PATH, ...(refactorAsking
    ? { state: "waiting", work_state: "waiting_you", menu: menu() }
    : { state: "working", work_state: "working", line: "4m" }) }),
  session(SYNC, TRIP.sync, 2, { cwd: TRIP_PATH, assistant: "codex", isClaude: false, state: "working", work_state: "working", line: "9m" }),
  session(CALENDAR, TRIP.calendar, 55, { cwd: TRIP_PATH }),
]

const snapshot = () => ({
  at: Date.now(),
  scan: { complete: true, completed: { complete: true, sequence: 1 }, emptyAuthoritative: true, epoch: 1, generation: 1, provenance: "fixture" },
  sessions: story === "squad" ? squadSessions() : story === "trip" ? tripSessions() : [
    session(CHECKOUT, STORY.checkout, 1, checkoutWaiting
      ? { attention_count: 1, state: "waiting", work_state: "waiting_you" }
      : { state: "working", work_state: "working", line: "2m" }),
    session(SIGN_IN, STORY.signIn, 6, { assistant: "codex", isClaude: false, state: "working", work_state: "working", line: "6m" }),
    session(SEARCH, STORY.search, 40),
  ],
})

const project = { id: "site-shop", label: STORY.project, path: PROJECT_PATH, icon: null, available: true }

function item(id: string, kind: string, [title, description]: readonly [string, string], phase: string, owner: string | null, minutesAgo: number) {
  return {
    id, project, kind, title, description, phase, condition: null, area: phase === "created" ? "unassigned" : phase,
    deployment_policy: "agent_decides", review_required: false, owner_session: owner ? conversation(owner) : null,
    created_at: now - 86400, updated_at: now - minutesAgo * 60, phase_entered_at: now - minutesAgo * 60,
    closed_at: null, cycle: 1, version: 1,
  }
}

const items = () => [
  item("site-checkout-item", "feature", STORY.items.checkout, "implementing", CHECKOUT, 25),
  item("site-sign-in-item", "feature", STORY.items.signIn, "verifying", SIGN_IN, 12),
  item("site-search-item", "feature", STORY.items.search, "created", null, 90),
  item("site-reviews-item", "feature", STORY.items.reviews, "created", null, 300),
]

const note = () => ({
  id: "10000000-0000-4000-8000-000000000101", kind: "answer",
  source_conversation: conversation(CHECKOUT), source_label: STORY.checkout,
  target_conversation: conversation(CHECKOUT), target_session: CHECKOUT,
  ...STORY.note, options: [...STORY.note.options], document_url: "", detail: "",
  created_at: now - 60, read_at: null, resolved_at: null, version: 1,
})

const todo = () => ({
  id: "site-todo", text: STORY.todo, created_at: now - 120, sent_at: now - 110, read_at: now - 100, completed_at: null, version: 3,
  images: [{ id: "site-todo-image", title: STORY.screenshot, media_type: "image/png", byte_count: 68, width: 1, height: 1,
    position: 0, created_by: "site", created_at: now - 120 }],
})

const transcript = (id: string) => id === CHECKOUT ? [
  { role: "user", text: STORY.asked, at: now - 1800, kind: "message" },
  { role: "assistant", text: STORY.answered, at: now - 90, kind: "message" },
] : id === COORDINATOR ? [
  { role: "user", text: SQUAD.asked, at: now - 1500, kind: "message" },
  { role: "assistant", text: SQUAD.planned, at: now - 900, kind: "message" },
  ...(reviewFinished ? [
    // The completion notice as the daemon's transcript reader hands it on
    // (`clawdline.notice` kind `task_finished`, for the root that asked).
    { role: "notice", text: "", at: now - 120, notice: { kind: "task_finished", state: "success", audience: "parent", outstanding: 2,
      task: { id: "site-task-review", title: SQUAD.children.review } } },
    { role: "assistant", text: SQUAD.verdict, at: now - 60, kind: "message" },
  ] : []),
] : id === REFACTOR ? [
  { role: "user", text: TRIP.asked, at: now - 3600, kind: "message" },
  { role: "assistant", text: TRIP.answered, at: now - 600, kind: "message" },
  // A session message as the daemon's transcript reader hands it on
  // (`internal/adapters/transcript` KindMessage): the sender's label and assistant.
  { role: "message", text: TRIP.message, at: now - 240, source: TRIP.sync, sourceAssistant: "codex", sourceMode: "clawdline" },
  ...(refactorAsking ? [{ role: "assistant", text: TRIP.considered, at: now - 60, kind: "message" }] : []),
] : []

const TYPES: Record<string, string> = { ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".json": "application/json", ".png": "image/png", ".webp": "image/webp", ".ico": "image/x-icon" }
function json(res: ServerResponse, status: number, value: unknown) { res.writeHead(status, { "content-type": "application/json" }); res.end(JSON.stringify(value)) }
function page(): string {
  const html = readFileSync(join(dist, "index.html"), "utf8")
  const words = JSON.parse(readFileSync(join(dist, "strings", "zh-Hant.json"), "utf8"))
  words.lang = "zh-Hant"; words.dir = "ltr"
  return html.replace("<!-- clawdline:strings -->", "<script>localStorage.setItem('ui_language'," + JSON.stringify(SITE_LANG) + ");window.__strings=" + JSON.stringify(words).replaceAll("</", "<\\/") + "</script>")
    .replace("<!-- clawdline:cloud -->", "")
}

function daemon(): Server {
  return createServer((req, res) => {
    const url = new URL(req.url ?? "/", "http://fixture")
    const path = url.pathname
    if (path === "/v1/strings") {
      const file = join(dist, "catalogs", (url.searchParams.get("lang") || SITE_LANG) + ".json")
      return existsSync(file) ? json(res, 200, JSON.parse(readFileSync(file, "utf8"))) : json(res, 404, { error: "not_found" })
    }
    if (path === "/v1/health") return json(res, 200, { ok: true })
    if (path === "/v1/sessions") return json(res, 200, snapshot())
    if (path === "/v1/events") {
      res.writeHead(200, { "content-type": "text/event-stream", "cache-control": "no-cache" })
      res.write("event: sessions\ndata: " + JSON.stringify(snapshot()) + "\n\n")
      return // held open, as the daemon's stream is
    }
    if (path === "/v1/transcript") {
      const id = url.searchParams.get("session") ?? ""
      return json(res, 200, { entries: transcript(id), evidence: "process", id, signature: "site" })
    }
    const about = /^\/v1\/sessions\/([^/]+)\/(info|git)$/.exec(path)
    if (about) {
      const row = snapshot().sessions.find((s) => s.id === decodeURIComponent(about[1]))
      if (!row) return json(res, 404, { error: "not_found" })
      if (about[2] === "git") return json(res, 200, { git: { ahead: 0, behind: 0, branch: story === "squad" ? "split-sync-engine" : story === "trip" ? "storage-refactor" : "checkout-redesign", clean: true, head: "0123456789abcdef", files: [] } })
      return json(res, 200, { info: {
        session: { id: row.id, title: row.label, assistant: row.assistant, namingAssistant: row.assistant, sessionId: row.sessionId, cwd: row.cwd, model: "claude-opus-5-5" },
        models: [{ id: "claude-opus-5-5", name: "Opus 5.5" }], context: { usedPercent: 18 }, deploy: [], links: [],
      } })
    }
    if (path === "/v1/personas") return json(res, 200, personas())
    if (path === "/v1/places") return json(res, 200, { places: [story === "squad"
      ? { id: "site-budget", label: SQUAD.project, path: SQUAD_PATH, icon: { accent: "#8fd06b", cells: [["#8fd06b", "#8fd06b"], ["#141416", "#8fd06b"]] } }
      : story === "trip"
      ? { id: "site-trips", label: TRIP.project, path: TRIP_PATH, icon: { accent: "#5fa8e0", cells: [["#5fa8e0", "#141416"], ["#5fa8e0", "#5fa8e0"]] } }
      : { id: project.id, label: project.label, path: PROJECT_PATH, icon: { accent: "#e07a5f", cells: [["#e07a5f", "#e07a5f"], ["#e07a5f", "#141416"]] } }],
    assistants: [{ id: "claude", label: "Claude" }] })
    if (path.startsWith("/v1/work/v2/human-interventions/")) {
      const mine = story === "shop" && decodeURIComponent(path).includes(CHECKOUT) && checkoutWaiting
      return json(res, 200, { ok: true, rows: mine ? [note()] : [], pruned_resolved: 0 })
    }
    const todos = /^\/v1\/work\/v2\/session-todos\/(.+)$/.exec(path)
    if (todos && req.method === "GET") {
      const mine = story === "shop" && decodeURIComponent(todos[1]).includes(CHECKOUT) && !checkoutWaiting
      return json(res, 200, { ok: true, assigned_items: [], recent_items: [], direct_todos: mine ? [todo()] : [],
        open_decisions: [], decisions_error: null, truncated: false })
    }
    if (path === "/v1/work/v2/items" && req.method === "GET") {
      const rows = items()
      const counts: Record<string, number> = {}
      for (const row of rows) counts[row.area] = (counts[row.area] ?? 0) + 1
      return json(res, 200, { ok: true, rows, counts, truncated: false })
    }
    if (path === "/v1/work/v2/proposals") return json(res, 200, { rows: [], truncated: false })
    if (path === "/v1/work/decisions") return json(res, 200, { counts: {}, rows: [], next_cursor: null })
    if (path === "/v1/orchestrator/tasks") return json(res, 200, { at: Date.now(), tasks: story === "squad" ? squadTasks() : [] })
    if (path === "/v1/orchestrator/schedules") return json(res, 200, { at: Date.now(), schedules: [] })
    if (path === "/v1/work/v2/images/site-todo-image") {
      res.writeHead(200, { "content-type": "image/png" })
      return res.end(Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=", "base64"))
    }
    if (path.startsWith("/v1/")) return json(res, 404, { error: "not_found", detail: path })
    if (path === "/") { res.writeHead(200, { "content-type": "text/html; charset=utf-8" }); return res.end(page()) }
    const file = normalize(join(dist, path))
    if (!file.startsWith(dist + "/") || !existsSync(file) || !statSync(file).isFile()) return json(res, 404, {})
    res.writeHead(200, { "content-type": TYPES[extname(file)] ?? "application/octet-stream" }); res.end(readFileSync(file))
  })
}

class Browser {
  private seq = 0
  private pending = new Map<number, { ok: (v: any) => void; fail: (e: Error) => void }>()
  private loads: string[] = []
  private ws: WebSocket
  private constructor(ws: WebSocket) {
    this.ws = ws
    ws.addEventListener("message", (ev) => {
      const msg = JSON.parse(String(ev.data))
      if (msg.id === undefined) { if (msg.method === "Page.loadEventFired") this.loads.push(msg.sessionId); return }
      const p = this.pending.get(msg.id); this.pending.delete(msg.id)
      if (msg.error) p?.fail(new Error(msg.error.message)); else p?.ok(msg.result)
    })
  }
  static async connect(url: string): Promise<Browser> {
    const ws = new WebSocket(url)
    await new Promise<void>((ok, fail) => { ws.addEventListener("open", () => ok()); ws.addEventListener("error", () => fail(new Error("Chrome unreachable"))) })
    return new Browser(ws)
  }
  send(method: string, params: object = {}, sessionId?: string): Promise<any> {
    const id = ++this.seq; this.ws.send(JSON.stringify({ id, method, params, sessionId }))
    return new Promise((ok, fail) => {
      const timer = setTimeout(() => fail(new Error(method + " timed out")), 15_000)
      this.pending.set(id, { ok: (v) => { clearTimeout(timer); ok(v) }, fail: (e) => { clearTimeout(timer); fail(e) } })
    })
  }
  async loaded(sessionId: string, before: number): Promise<void> {
    const deadline = Date.now() + 10_000
    while (!this.loads.slice(before).includes(sessionId)) { if (Date.now() > deadline) throw new Error("page did not load"); await new Promise((r) => setTimeout(r, 50)) }
  }
  get loadCount() { return this.loads.length }
  close() { this.ws.close() }
}

let server: Server, browserProcess: ChildProcess, browser: Browser, profile: string, origin: string
before(async () => {
  if (!SITE_SHOTS) return
  assert.ok(existsSync(join(dist, "index.html")), "build the console first")
  assert.ok(existsSync(chrome), "Chrome is required")
  server = daemon(); await new Promise<void>((ok) => server.listen(0, "127.0.0.1", ok))
  const address = server.address(); origin = "http://127.0.0.1:" + (typeof address === "object" && address ? address.port : 0)
  profile = mkdtempSync(join(tmpdir(), "clawdline-site-shots-"))
  browserProcess = spawn(chrome, ["--headless=new", "--remote-debugging-port=0", "--user-data-dir=" + profile, "--no-first-run", "--no-default-browser-check", "about:blank"])
  const endpoint = await new Promise<string>((ok, fail) => {
    let said = ""; const timer = setTimeout(() => { browserProcess.kill(); fail(new Error("Chrome did not start: " + said)) }, 20_000)
    browserProcess.stderr?.on("data", (chunk) => { said += String(chunk); const match = /DevTools listening on (ws:\/\/\S+)/.exec(said); if (match) { clearTimeout(timer); ok(match[1]) } })
  })
  browser = await Browser.connect(endpoint)
})
after(async () => {
  browser?.close()
  if (browserProcess && browserProcess.exitCode === null) {
    const gone = new Promise((ok) => browserProcess.once("exit", ok))
    browserProcess.kill()
    await Promise.race([gone, new Promise((ok) => setTimeout(ok, 5000))])
  }
  server?.closeAllConnections(); await new Promise<void>((ok) => server ? server.close(() => ok()) : ok())
  if (profile) rmSync(profile, { recursive: true, force: true, maxRetries: 5 })
})

/** Opens the console at `address` on a phone, runs `arrange`, marks `marks` and saves `<scene>-<lang>.png`. */
async function shoot(scene: keyof typeof SITE_CALLOUTS, address: string, arrange: (run: (expression: string) => Promise<any>, until: (expression: string) => Promise<void>) => Promise<void>, marks: Omit<Callout, "n" | "label">[]) {
  const { targetId } = await browser.send("Target.createTarget", { url: "about:blank" })
  const { sessionId: id } = await browser.send("Target.attachToTarget", { targetId, flatten: true })
  const run = async (expression: string) => {
    const { result, exceptionDetails } = await browser.send("Runtime.evaluate", { expression, returnByValue: true, awaitPromise: true }, id)
    if (exceptionDetails) throw new Error(exceptionDetails.exception?.description ?? exceptionDetails.text)
    return result.value
  }
  const until = async (expression: string) => {
    const deadline = Date.now() + 8000
    while (!(await run(expression))) { if (Date.now() > deadline) assert.fail("the page never reached " + expression); await new Promise((r) => setTimeout(r, 50)) }
  }
  try {
    await browser.send("Page.enable", {}, id); await browser.send("Runtime.enable", {}, id)
    await browser.send("Emulation.setDeviceMetricsOverride", { width: 390, height: 844, deviceScaleFactor: 2, mobile: true }, id)
    await browser.send("Emulation.setEmulatedMedia", { features: [{ name: "prefers-color-scheme", value: "dark" }] }, id)
    const mark = browser.loadCount; await browser.send("Page.navigate", { url: origin + address }, id); await browser.loaded(id, mark)
    await arrange(run, until)
    // Folds open with an animation; a picture taken during one clips the row.
    await new Promise((r) => setTimeout(r, 900))
    assert.equal(await run(`document.documentElement.lang.startsWith(${JSON.stringify(SITE_LANG === "en" ? "en" : "zh")})`), true, "the console is in the picture's language")
    const words = SITE_CALLOUTS[scene]
    await run(calloutScript(marks.map((m, index) => ({ ...m, n: index + 1, label: words[index] }))))
    await new Promise((r) => setTimeout(r, 200))
    const { data } = await browser.send("Page.captureScreenshot", { format: "png" }, id)
    writeFileSync(join(SITE_SHOTS, `${scene}-${siteSuffix}.png`), Buffer.from(data, "base64"))
  } finally { await browser.send("Target.closeTarget", { targetId }) }
}

test("site shot: the Agent asks, and a tap answers", { skip: !SITE_SHOTS }, async () => {
  checkoutWaiting = true
  await shoot("attention-note", "/#session=" + CHECKOUT, async (run, until) => {
    await until(`!!document.querySelector(".human-interventions-head")`)
    await run(`document.querySelector(".human-interventions-head").click()`)
    await until(`!!document.querySelector(".human-intervention-option")`)
  }, [
    { sel: ".human-intervention-card h3, .human-intervention-card > p:not(.human-intervention-source)" },
    { sel: ".human-intervention-option" },
  ])
})

test("site shot: the next request, queued with a screenshot", { skip: !SITE_SHOTS }, async () => {
  checkoutWaiting = false
  try {
    await shoot("session-todo", "/#session=" + CHECKOUT, async (run, until) => {
      await until(`(() => {
        const fold = document.querySelector('.session-todos')
        if (fold && !fold.open) fold.querySelector('summary')?.click()
        return !!document.querySelector('button.session-todo-image-link:not(:disabled)')
      })()`)
    }, [
      { sel: ".session-direct-todo", pad: 2 },
      { sel: ".session-todo-image-link", pad: 3 },
    ])
  } finally { checkoutWaiting = true }
})

test("site shot: a Board item shows its stage and its Session", { skip: !SITE_SHOTS }, async () => {
  await shoot("board-item", "/#page=work", async (run, until) => {
    await until(`!!document.querySelector('#work [data-work-id="site-checkout-item"] .work-card-summary')`)
    await run(`document.querySelector('#work [data-work-id="site-checkout-item"] .work-card-summary').click()`)
    await until(`!!document.querySelector('.work-item-detail-modal .work-session-link')`)
  }, [
    { sel: ".work-item-detail-modal .work-milestones" },
    { sel: ".work-item-detail-modal .work-session-link", pad: 4 },
  ])
})

test("site shot: a bug reported from the phone, with the problem circled", { skip: !SITE_SHOTS }, async () => {
  story = "trip"
  try {
    await shoot("todo-screenshot", "/#session=" + REFACTOR, async (run, until) => {
      await until(`(() => {
        const fold = document.querySelector('.session-todos')
        if (fold && !fold.open) fold.querySelector('summary')?.click()
        return !!document.querySelector('.session-todos-add')
      })()`)
      await run(`document.querySelector('.session-todos-add').click()`)
      await until(`!!document.querySelector('.session-todo-modal textarea')`)
      // Typed and attached as a person would: React reads the native setter's input event and the picker's change event.
      await run(`(() => {
        const box = document.querySelector('.session-todo-modal textarea')
        Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value').set.call(box, ${JSON.stringify(TRIP.todo)})
        box.dispatchEvent(new Event('input', { bubbles: true }))
      })()`)
      await run(`(async () => {
        const file = await ${inventedScreenshotScript(TRIP.screenshot, TRIP.picture)}
        const picker = document.querySelector('.session-todo-modal input[type=file]')
        const files = new DataTransfer(); files.items.add(file); picker.files = files.files
        picker.dispatchEvent(new Event('change', { bubbles: true }))
        return true
      })()`)
      await until(`!!document.querySelector('.session-todo-modal .work-pending-picture img')?.complete`)
    }, [
      { sel: ".session-todo-modal textarea", pad: 3 },
      { sel: ".session-todo-modal .work-pending-picture", pad: 5 },
    ])
  } finally { story = "shop" }
})

test("site shot: another Session asks this one to take a Board item", { skip: !SITE_SHOTS }, async () => {
  story = "trip"
  try {
    await shoot("session-message", "/#session=" + REFACTOR, async (run, until) => {
      await until(`!!document.querySelector('.entry[data-role="message"] .message-card')`)
      await run(`document.querySelector('.entry[data-role="message"]').scrollIntoView({ block: "center" })`)
    }, [
      // The sender's name alone, padded clear so the number does not sit on its first letter.
      { sel: '.entry[data-role="message"] .message-source > span:first-child', pad: 7 },
      { sel: '.entry[data-role="message"] .message-card > div:not(.message-source)', pad: 4 },
    ])
  } finally { story = "shop" }
})

test("site shot: the Session asks the person before it takes the work", { skip: !SITE_SHOTS }, async () => {
  story = "trip"; refactorAsking = true
  try {
    await shoot("ask-person", "/#session=" + REFACTOR, async (run, until) => {
      await until(`!!document.querySelector('#waiting:not([hidden]) .menu .opt')`)
    }, [
      { sel: "#waiting .question" },
      { sel: "#waiting .menu", pad: 4 },
    ])
  } finally { story = "shop"; refactorAsking = false }
})

test("site shot: a coordinating Session with its squad nested under it", { skip: !SITE_SHOTS }, async () => {
  story = "squad"
  try {
    await shoot("squad-list", "/", async (run, until) => {
      await until(`document.querySelectorAll('li.row[data-depth] .persona-state-name').length === 3
        && !!document.querySelector('li.row[data-id="${COORDINATOR}"] .persona-state-name')`)
    }, [
      { sel: `li.row[data-id="${COORDINATOR}"]`, pad: 2 },
      { sel: "li.row[data-depth]", pad: 2 },
    ])
  } finally { story = "shop" }
})

test("site shot: the code review reports back with its verdict", { skip: !SITE_SHOTS }, async () => {
  story = "squad"; reviewFinished = true
  try {
    await shoot("review-notice", "/#session=" + COORDINATOR, async (run, until) => {
      await until(`!!document.querySelector('.entry[data-role="notice"] .notice-card')`)
      await run(`document.querySelector('.entry[data-role="notice"]').scrollIntoView({ block: "center" })`)
    }, [
      { sel: '.entry[data-role="notice"] .notice-card', pad: 4 },
      { sel: '.entry[data-role="notice"] ~ .entry[data-role="assistant"] .body', pad: 4 },
    ])
  } finally { story = "shop"; reviewFinished = false }
})
