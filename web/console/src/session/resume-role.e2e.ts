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
    id, name: { en, "zh-Hant": zh }, summary: { en: en + ".", "zh-Hant": zh + "。" }, suggested_kinds: [], icon: bot(accent), teams: ["engineering"],
  })),
}

let offer: Record<string, unknown> = { available: false, reason: "boot_unknown", sessions: [], at: 0 }
/** Every resume that arrived, by its path. */
let resumes: string[] = []
let placeReads = 0
let commandAssistant = ""
let commandCWD = "/work/api"
let commandPastFailure = false
let commandLive = false
let commandSends: string[] = []
let commandPastReads: string[] = []
const safeCommandSession = () => ({ activity_generation: 1, attestation_id: null, mover: null,
  obligation_generation: 1, observed_at: now(), provenance: [], reasons: [], session_generation: 1,
  source: { freshness: "current", observed_at: now(), provenance: "fixture" }, state: "safe", version: "fixture-safe" })

function snapshot() {
  return {
    at: Date.now(),
    scan: {
      complete: true, completed: { complete: true, sequence: 1 }, emptyAuthoritative: true, epoch: 1, generation: 1,
      provenance: "fixture", source: { freshness: "current", observed_at: now(), provenance: "fixture" },
    },
    sessions: commandAssistant ? [{ id: "fixture-current", backend: "tmux", assistant: commandAssistant,
      label: "Current fixture", sessionId: "fixture-conversation", cwd: commandCWD, state: "idle", evidence: "process",
      isClaude: commandAssistant === "claude", work_state: "ready",
      activity: { known: true, at: now() }, closeability: safeCommandSession() },
      ...(commandLive ? ["fixture-wrong", "fixture-live"].map(id => ({ id, backend: "tmux",
        assistant: id === "fixture-wrong" ? "claude" : "codex", label: id, sessionId: "past-1", cwd: "/work/api",
        state: "idle", work_state: "ready", evidence: "process", activity: { known: true, at: now() }, closeability: safeCommandSession() })) : [])] : [],
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
  const slot = "<script>localStorage.setItem('ui_language','zh-Hant');window.__strings=" + JSON.stringify(words).replaceAll("</", "<\\/") + "</script>"
  return html.replace("<!-- clawdline:strings -->", slot).replace("<!-- clawdline:cloud -->", "")
}

function daemon(): Server {
  return createServer((req, res) => {
    const url = new URL(req.url ?? "/", "http://fixture")
    const path = url.pathname
    if (path === "/v1/sessions") return json(res, 200, snapshot())
    if (path === "/v1/health") return json(res, 200, { ok: true })
    if (path === "/v1/strings" && url.searchParams.get("lang") === "zh-Hant") {
      return json(res, 200, JSON.parse(readFileSync(join(dist, "catalogs", "zh-Hant.json"), "utf8")))
    }
    if (path === "/v1/orchestrator/tasks") return json(res, 200, { at: Date.now(), tasks: [] })
    if (path === "/v1/personas") return json(res, 200, personas)
    if (path === "/v1/sessions/restorable") return json(res, 200, offer)
    if (path === "/v1/places") {
      placeReads++
      return json(res, 200, {
        places: [{ id: "place-api", label: "api " + placeReads, path: "/work/api", at: now() - 300 },
          { id: "place-console", label: "console", path: "/work/web/console", at: now() - 900 }],
        assistants: [{ id: "claude", label: "Claude" }, { id: "codex", label: "Codex" }],
      })
    }
    if (/^\/v1\/places\/[^/]+\/sessions/.test(path)) {
      commandPastReads.push(path)
      if (commandPastFailure) return json(res, 503, { error: { code: "busy", message: "Synthetic history read failure" } })
      return json(res, 200, {
        at: now(), place: "place-api", assistant: "claude", more: false,
        sessions: [
          { id: "past-1", title: "Tighten the reconnect loop", at: now() - 3600, live: commandLive },
          { id: "past-2", title: "Write the release notes for the resume sheet", at: now() - 86400 * 2, live: false },
        ],
      })
    }
    if (path === "/v1/sessions/fixture-current/skills") return json(res, 200, {
      skills: [{ name: "resume", description: "A synthetic colliding skill", source: "project" }],
    })
    if (path === "/v1/transcript") return json(res, 200, { id: "fixture-current", entries: [], evidence: "transcript", signature: "fixture-empty" })
    if (path.startsWith("/v1/work/v2/session-todos/")) return json(res, 200, {ok:true,done:0,active:0,waiting:0,assigned_items:[],recent_items:[],direct_todos:[],open_decisions:[],truncated:false})
    if (path.startsWith("/v1/work/v2/human-interventions/")) return json(res, 200, {ok:true,rows:[],pruned_resolved:0})
    if (path === "/v1/sessions/fixture-current/info") return json(res, 200, { info: { session: { assistant: commandAssistant }, models: [] } })
    if (req.method === "POST" && path === "/v1/sessions/fixture-current/send") {
      let body = ""; req.on("data", chunk => body += String(chunk))
      req.on("end", () => { commandSends.push(JSON.parse(body).text); json(res, 200, { ok: true }) })
      return
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
      if (msg.method === "Runtime.exceptionThrown") console.error("Browser exception:", msg.params?.exceptionDetails?.exception?.description ?? msg.params?.exceptionDetails?.text)
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
    return this.run(`(async () => {
      const selector = ${JSON.stringify(selector)}
      for (let i = 0; i < 100 && !document.querySelector(selector); i++) await new Promise((r) => setTimeout(r, 50))
      const button = document.querySelector(selector)
      if (!button) throw new Error('missing control: ' + selector)
      button.click()
    })()`)
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
  console.error("fixture cleanup: browser")
  browser?.close()
  if (browserProcess && browserProcess.exitCode === null && browserProcess.signalCode === null) {
    const gone = new Promise((ok) => browserProcess.once("exit", ok))
    browserProcess.kill()
    await gone
  }
  console.error("fixture cleanup: server")
  server?.closeAllConnections()
  await new Promise<void>((ok) => (server ? server.close(() => ok()) : ok()))
  console.error("fixture cleanup: profile")
  if (profile) rmSync(profile, { recursive: true, force: true, maxRetries: 5 })
  console.error("fixture cleanup: done")
})

/** Open the start sheet, tick "resume", and enter the api place. */
async function toPastList(tab: Tab): Promise<Seen> {
  await tab.go("/")
  await tab.click("#start-go")
  await tab.until("the places are listed", (s) => s.places > 0)
  await tab.click("#start-resume .chip.check")
  const listing = await tab.until("resuming hides the role row while places are listed", (s) => !s.roleShown)
  assert.equal(listing.roleShown, false)
  await tab.click('#start-list .place[data-id="place-api"]')
  return tab.until("the past conversations and the role row are drawn", (s) => s.pasts.length === 2 && s.roleShown)
}

test("each new start begins with no role, including after an earlier choice", async () => {
  const tab = await Tab.open(browser, 390, 844, true)
  try {
    await tab.go("/")
    await tab.run(`localStorage.setItem("clawdline.start.persona", "frontend")`)
    await tab.click("#start-go")
    assert.deepEqual((await tab.until("the new start shows no role", (s) => s.roleShown)).chosen, ["不指定"])
    await tab.pressChip("#start-persona", "架構師")
    await tab.until("the chosen role is shown", (s) => s.chosen[0] === "架構師")
    await tab.click("#start-close")
    await tab.click("#start-go")
    assert.deepEqual((await tab.until("the next start shows no role", (s) => s.roleShown)).chosen, ["不指定"])
  } finally {
    await tab.close()
  }
})

for (const [label, width, height, mobile] of [["phone", 390, 844, true], ["desktop", 1280, 800, false]] as const) {
  test(`${label}: project refreshes preserve the focused native team picker and its options`, async () => {
    personas.personas[0].teams = ["engineering", "marketing"]
    const tab = await Tab.open(browser, width, height, mobile)
    try {
      await tab.go("/")
      await tab.click("#start-go")
      await tab.until("the team picker is drawn", (s) => s.roleShown && s.places === 2)
      await tab.run(`(() => {
        const picker = document.querySelector('#start-persona select')
        if (!picker) throw new Error('missing team picker')
        window.savedPicker = picker
        window.savedOptions = [...picker.options]
        picker.focus()
      })()`)
      // Wait for two real three-second background refreshes, with changing
      // Project labels, rather than calling the renderer directly.
      const initial = placeReads
      const deadline = Date.now() + 10_000
      while (placeReads < initial + 2 && Date.now() < deadline) await new Promise((r) => setTimeout(r, 50))
      assert.ok(placeReads >= initial + 2, "the background Project reads ran")
      await tab.run(`(async () => {
        for (let i = 0; i < 100 && !document.querySelector('#start-list').textContent.includes(${JSON.stringify("api ")} + ${placeReads}); i++)
          await new Promise((r) => setTimeout(r, 50))
      })()`)
      assert.deepEqual(await tab.run(`(() => {
        const picker = document.querySelector('#start-persona select')
        return {
          connected: window.savedPicker.isConnected,
          same: picker === window.savedPicker,
          focused: document.activeElement === window.savedPicker,
          options: [...picker.options].every((option, i) => option === window.savedOptions[i]),
          refreshed: document.querySelector('#start-list').textContent.includes('api ' + ${placeReads}),
        }
      })()`), { connected: true, same: true, focused: true, options: true, refreshed: true })
      await tab.run(`(() => {
        window.savedPicker.value = 'marketing'
        window.savedPicker.dispatchEvent(new Event('change', { bubbles: true }))
      })()`)
      assert.deepEqual((await tab.until("the refreshed picker still switches teams", (s) => s.roles.length === 2)).roles, ["不指定", "架構師"])
      await tab.pressChip("#start-persona", "架構師")
      assert.deepEqual((await tab.until("the new team's role can be chosen", (s) => s.chosen[0] === "架構師")).chosen, ["架構師"])
    } finally {
      personas.personas[0].teams = ["engineering"]
      await tab.close()
    }
  })

  test(`${label}: the resume step offers the roles with none chosen, and a pick reaches the route`, async () => {
    const tab = await Tab.open(browser, width, height, mobile)
    try {
      resumes = []
      const seen = await toPastList(tab)
      assert.deepEqual(seen.chosen, ["不指定"], "a resume starts without a role")
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

async function compose(tab: Tab, text: string): Promise<void> {
  await tab.run(`(async () => {
    const end = Date.now() + 5000;
    while (!document.querySelector('#msg[contenteditable="true"], #msg[contenteditable="plaintext-only"]')) {
      if (Date.now() > end) throw Error('composer not ready: '+document.querySelector('#msg')?.outerHTML+'; '+document.body.innerText.slice(-1800));
      await new Promise(resolve => setTimeout(resolve, 20));
    }
    const box = document.querySelector('#msg'); box.textContent = ${JSON.stringify(text)};
    box.dispatchEvent(new InputEvent('input', { bubbles: true, inputType: 'insertText' }));
  })()`)
}

for (const assistant of ["claude", "codex"]) {
  test(`${assistant}: /resume opens the current Project picker without a terminal send`, async () => {
    commandAssistant = assistant; commandSends = []; commandPastReads = []; resumes = []
    const tab = await Tab.open(browser, 390, 844, true)
    try {
      await tab.go("/#session=fixture-current")
      await compose(tab, "/resume")
      // The colliding Skill must not consume this exact command on Enter.
      await tab.run(`document.querySelector('#msg').dispatchEvent(new KeyboardEvent('keydown', {key:'Enter',bubbles:true,cancelable:true}))`)
      const seen = await tab.until("the resume command opens the native Clawdline picker", state => state.pasts.length === 2)
      assert.ok(seen.wide <= 390)
      assert.equal(await tab.run(`document.querySelector('#start').hidden`), false)
      assert.equal(await tab.run(`document.querySelector('#msg').textContent`), "")
      assert.deepEqual(commandSends, [])
      assert.deepEqual(resumes, [], "opening the modal does not restore anything")
      assert.deepEqual(commandPastReads, [`/v1/places/place-api/sessions/${assistant}`])
      await tab.shot(`resume-command-${assistant}`)
      await tab.click('#start-list [data-session="past-1"]')
      const end = Date.now() + 3000
      while (!resumes.length && Date.now() < end) await new Promise(resolve => setTimeout(resolve, 20))
      assert.deepEqual(resumes, [`/v1/places/place-api/resume/${assistant}/past-1`])
      assert.deepEqual(commandSends, [])
    } finally { commandAssistant = ""; await tab.close() }
  })
}

test("resume searches, cancellation, history failures and unmatched Projects stay local", async () => {
  commandAssistant = "codex"; commandSends = []; resumes = []; commandCWD = "/unregistered/project"
  const tab = await Tab.open(browser, 1100, 800, false)
  try {
    await tab.go("/#session=fixture-current")
    await compose(tab, "/resume release notes")
    await tab.click("#send")
    await tab.until("an unregistered cwd offers Projects instead of guessing one", state => state.places === 2)
    await tab.click('#start-list .place[data-id="place-api"]')
    await tab.until("inline resume arguments filter the retained conversations", state => state.pasts.length === 1 && state.pasts[0] === "past-2")
    await tab.click("#start-close")
    assert.deepEqual(resumes, [])
    assert.deepEqual(commandSends, [])

    commandCWD = "/work/api"; commandPastFailure = true
    // A new document makes the new Project visible; a same-URL navigation would not reload.
    await tab.go("/?retry-fixture=1#session=fixture-current")
    await compose(tab, "/resume")
    await tab.click("#send")
    await tab.until("an unreadable history remains an open picker", state => state.places === 0 && state.pasts.length === 0)
    await tab.run(`(async()=>{ const end=Date.now()+5000; while(!document.querySelector('.start-past-more')) {if(Date.now()>end)throw Error('missing retry');await new Promise(r=>setTimeout(r,20))}})()`)
    assert.deepEqual(commandSends, [])
    commandPastFailure = false
    await tab.click(".start-past-more")
    await tab.until("retry reads the same provider and Project", state => state.pasts.length === 2)
    await tab.click("#start-close")
    assert.deepEqual(resumes, [])

    await compose(tab, "explain /resume")
    await tab.click("#send")
    await tab.run(`new Promise(resolve=>setTimeout(resolve,300))`)
    assert.deepEqual(commandSends, ["explain /resume"], "mentioning a command is an ordinary prompt")
  } finally { commandAssistant = ""; commandCWD = "/work/api"; commandPastFailure = false; await tab.close() }
})

test("resume opens an already running conversation of the same assistant without launching another", async () => {
  commandAssistant = "codex"; commandLive = true; resumes = []; commandSends = []
  const tab = await Tab.open(browser, 390, 844, true)
  try {
    await tab.go("/#session=fixture-current")
    await compose(tab, "/resume")
    await tab.click("#send")
    await tab.until("the running conversation is offered", state => state.pasts.length === 2)
    assert.equal(await tab.run(`document.querySelector('#start-list [data-session="past-1"]').disabled`), false)
    await tab.click('#start-list [data-session="past-1"]')
    await tab.run(`(async()=>{ const end=Date.now()+5000; while(!location.hash.includes('fixture-live')) {if(Date.now()>end)throw Error('wrong live Session: '+location.hash);await new Promise(r=>setTimeout(r,20))}})()`)
    assert.deepEqual(resumes, [])
    assert.deepEqual(commandSends, [])
  } finally { commandAssistant = ""; commandLive = false; await tab.close() }
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

for (const assistant of ["claude", "codex"]) {
 test(`${assistant}: every catalog command can be selected and native lines use the ordinary send`, async () => {
  commandAssistant = assistant; commandSends = []
  const tab = await Tab.open(browser, 390, 844, true)
  try {
   await tab.go('/')
   await tab.run('localStorage.clear()')
   await tab.go('/?catalog=' + assistant + '#session=fixture-current')
   await compose(tab, '/')
   await tab.run(`(async () => {
    const end = Date.now()+5000;
    while(document.querySelectorAll('#skill-menu .skill-option').length < 10) {
     if(Date.now()>end) throw Error('catalog did not appear');
     await new Promise(r=>setTimeout(r,20));
    }
   })()`)
   const commands = await tab.run(`Array.from(document.querySelectorAll('#skill-menu .command'), node=>node.textContent)`)
   assert.ok(commands.length > 9)
   assert.ok(commands.includes('/model'))
   assert.ok(commands.includes('/resume'))
   assert.equal(await tab.run(`Array.from(document.querySelectorAll('#skill-menu .skill-option')).find(node=>node.querySelector('.command').textContent==='/model').querySelector('.description').textContent`), assistant === 'claude' ? '選擇模型' : '選擇模型與推理強度')
   assert.ok(commands.includes(assistant === 'claude' ? '/effort' : '/permissions'))
   // Exercise every rendered selection, including those outside the viewport.
   for (const command of commands) {
    await compose(tab, '/')
    await tab.run(`Array.from(document.querySelectorAll('#skill-menu .skill-option')).find(node=>node.querySelector('.command').textContent===${JSON.stringify(command)}).click()`)
    assert.equal(await tab.run(`document.querySelector('#msg').textContent`), command + ' ')
   }
   assert.deepEqual(commandSends, [], 'choosing metadata never executes a command')
   await compose(tab, '/')
   await tab.shot(`native-commands-${assistant}`)
   await compose(tab, assistant === 'claude' ? '/effort high' : '/plan inspect the API')
   await tab.click('.composer button[type=submit]')
   const end=Date.now()+3000
   while(!commandSends.length && Date.now()<end) await new Promise(r=>setTimeout(r,20))
   assert.deepEqual(commandSends, [assistant === 'claude' ? '/effort high' : '/plan inspect the API'])
   await compose(tab, '/future-provider-command value')
   await tab.click('.composer button[type=submit]')
   const next=Date.now()+3000
   while(commandSends.length<2 && Date.now()<next) await new Promise(r=>setTimeout(r,20))
   assert.equal(commandSends[1],'/future-provider-command value', 'execution has no builtin allowlist')
  } finally { commandAssistant=''; await tab.close() }
 })
}
