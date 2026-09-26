// The background strip above the composer, through a real browser at 1280px
// and at 390px:
//
//   (cd web && npm run build)
//   node --test --experimental-strip-types web/console/src/session/background-strip.e2e.ts
//
// The built console is served by a stand-in daemon that answers one Claude
// session with two background shells and one subagent, the Shell panel's read
// (`GET /v1/sessions/{id}/shells/{shell}`, whose output grows by a line on
// every read until it ends) and the subagent's transcript. Headless Chrome
// (CHROME, else the usual macOS install) checks that the strip shows, the
// sheet opens inside the screen, a shell's output grows in its panel, and an
// agent row opens that agent's transcript. BACKGROUND_SHOTS=<dir> saves a
// picture of each step; BACKGROUND_REPORT=<file> writes the measurements.
//
// Named `.e2e.ts` so the unit run over `*.test.ts` does not start a browser.
import { test, before, after } from "node:test"
import assert from "node:assert/strict"
import { spawn, type ChildProcess } from "node:child_process"
import { createServer, type Server, type ServerResponse } from "node:http"
import { existsSync, mkdtempSync, readFileSync, rmSync, statSync, writeFileSync } from "node:fs"
import { tmpdir } from "node:os"
import { dirname, extname, join, normalize, resolve } from "node:path"
import { fileURLToPath } from "node:url"

const dist = resolve(dirname(fileURLToPath(import.meta.url)), "../../dist")
const chrome = process.env.CHROME || "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
const shots = process.env.BACKGROUND_SHOTS || ""
const reportTo = process.env.BACKGROUND_REPORT || ""
const DESK = { width: 1280, height: 800, mobile: false }
const PHONE = { width: 390, height: 844, mobile: true }

const ID = "ttys031"
const NOW = Math.floor(Date.now() / 1000)
const SHELL = "b0aau3e6s"
const AGENT = "agent-fixture-1"

const ROW = {
  id: ID,
  label: "A session with work in the background",
  backend: "owned",
  state: "idle",
  work_state: "ready",
  evidence: "process",
  isClaude: true,
  assistant: "claude",
  sessionId: "10000000-0000-4000-8000-000000000031",
  cwd: "/tmp/fixture",
  shells: [
    { id: SHELL, at: NOW - 30, command: "npm run build -- --watch", what: "Build the console", doing: "line 1" },
    { id: "b1oas8ao7", at: NOW - 600, command: "go test ./...", what: "Run the Go tests", doing: "ok  internal/app" },
  ],
  agents_reading: { state: "complete" },
  agents: [
    { id: AGENT, at: NOW - 90, depth: 1, type: "Explore", what: "Inspect the fixture", state: "running", doing: "Grep" },
  ],
  closeability: {
    activity_generation: 1, attestation_id: null, mover: null, obligation_generation: 1, observed_at: 1,
    provenance: [], reasons: [], session_generation: 1, source: "broker", state: "safe", version: "fixture",
  },
}

function snapshot() {
  return {
    at: Date.now(),
    scan: { complete: true, completed: { complete: true, sequence: 1 }, emptyAuthoritative: true, epoch: 1,
      generation: 1, provenance: "fixture" },
    sessions: [ROW],
  }
}

// The shell's output file, one line longer on every read, as a build prints.
let shellReads = 0
function shellAnswer() {
  shellReads++
  const lines = Array.from({ length: shellReads }, (_, i) => "line " + (i + 1))
  return {
    shell: { id: SHELL, at: NOW, command: "npm run build -- --watch", what: "Build the console", doing: lines.at(-1) },
    text: lines.join("\n") + "\n",
    ended: false,
    signature: "sig-" + shellReads,
  }
}

const TYPES: Record<string, string> = { ".js": "text/javascript", ".css": "text/css", ".json": "application/json", ".svg": "image/svg+xml" }

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
    const path = new URL(req.url ?? "/", "http://fixture").pathname
    if (path === "/v1/sessions") return json(res, 200, snapshot())
    if (path === "/v1/events") {
      res.writeHead(200, { "content-type": "text/event-stream", "cache-control": "no-cache" })
      res.write("event: sessions\ndata: " + JSON.stringify(snapshot()) + "\n\n")
      return
    }
    if (path === `/v1/sessions/${ID}/shells/${SHELL}`) return json(res, 200, shellAnswer())
    if (path === `/v1/sessions/${ID}/agents/${AGENT}`) {
      return json(res, 200, { id: AGENT, evidence: "transcript", signature: "agent-1",
        entries: [{ role: "assistant", text: "The fixture agent looked at three files." }] })
    }
    if (path === "/v1/transcript") return json(res, 200, { entries: [], evidence: "process", id: ID, signature: "fixture" })
    if (path.startsWith("/v1/")) return json(res, 404, { error: "not_found", detail: path })
    if (path === "/") {
      res.writeHead(200, { "content-type": "text/html; charset=utf-8" })
      return res.end(document())
    }
    const file = normalize(join(dist, path))
    if (!file.startsWith(dist + "/") || !existsSync(file) || !statSync(file).isFile()) return json(res, 404, {})
    res.writeHead(200, { "content-type": TYPES[extname(file)] ?? "application/octet-stream" })
    res.end(readFileSync(file))
  })
}

// ---- a browser, over the DevTools protocol (as pages/verify/verify.e2e.ts)

class Browser {
  private seq = 0
  private pending = new Map<number, { ok: (v: any) => void; fail: (e: Error) => void }>()
  private loads: string[] = []
  private ws: WebSocket
  private constructor(ws: WebSocket) {
    this.ws = ws
    ws.addEventListener("message", (ev) => {
      const msg = JSON.parse(String(ev.data))
      if (msg.id === undefined) {
        if (msg.method === "Page.loadEventFired") this.loads.push(msg.sessionId)
        return
      }
      const p = this.pending.get(msg.id)
      this.pending.delete(msg.id)
      if (msg.error) p?.fail(new Error(msg.error.message))
      else p?.ok(msg.result)
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
    return new Promise((ok, fail) => {
      const timer = setTimeout(() => fail(new Error(method + " had no answer")), 15_000)
      this.pending.set(id, { ok: (v) => (clearTimeout(timer), ok(v)), fail: (e) => (clearTimeout(timer), fail(e)) })
    })
  }
  async loaded(sessionId: string, before: number): Promise<void> {
    const deadline = Date.now() + 10_000
    while (!this.loads.slice(before).includes(sessionId)) {
      if (Date.now() > deadline) throw new Error("the page did not load")
      await new Promise((r) => setTimeout(r, 50))
    }
  }
  get loadCount() {
    return this.loads.length
  }
  close() {
    this.ws.close()
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
  profile = mkdtempSync(join(tmpdir(), "clawdline-background-"))
  browserProcess = spawn(chrome, ["--headless=new", "--remote-debugging-port=0", "--user-data-dir=" + profile,
    "--no-first-run", "--no-default-browser-check", "about:blank"])
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


const report: Record<string, unknown> = {}
after(() => {
  if (reportTo) writeFileSync(reportTo, JSON.stringify(report, null, 2) + "\n")
})

const BOX = `((sel) => { const n = document.querySelector(sel); if (!n || !n.getClientRects().length) return null;
  const r = n.getBoundingClientRect(); return { top: Math.round(r.top), bottom: Math.round(r.bottom), left: Math.round(r.left), right: Math.round(r.right), height: Math.round(r.height) } })`

for (const [name, size] of [["desk-1280", DESK], ["phone-390", PHONE]] as const) {
  test(name + ": the strip opens the background shells and subagents", async () => {
    const { targetId } = await browser.send("Target.createTarget", { url: "about:blank" })
    const { sessionId: s } = await browser.send("Target.attachToTarget", { targetId, flatten: true })
    const run = async (expression: string) => {
      const { result, exceptionDetails } = await browser.send("Runtime.evaluate",
        { expression, returnByValue: true, awaitPromise: true }, s)
      if (exceptionDetails) throw new Error(exceptionDetails.exception?.description ?? exceptionDetails.text)
      return result.value
    }
    const until = async (what: string, expression: string, ms = 8_000) => {
      const deadline = Date.now() + ms
      for (;;) {
        try { if (await run(expression)) return } catch { /* between documents */ }
        if (Date.now() > deadline) assert.fail(what)
        await new Promise((r) => setTimeout(r, 50))
      }
    }
    const shot = async (step: string) => {
      if (!shots) return
      const { data } = await browser.send("Page.captureScreenshot", { format: "png" }, s)
      writeFileSync(join(shots, name + "-" + step + ".png"), Buffer.from(data, "base64"))
    }
    const click = (sel: string) => run(`document.querySelector(${JSON.stringify(sel)}).click()`)
    const seen: Record<string, unknown> = {}
    try {
      await browser.send("Page.enable", {}, s)
      await browser.send("Runtime.enable", {}, s)
      await browser.send("Emulation.setDeviceMetricsOverride", { ...size, deviceScaleFactor: 2 }, s)
      const mark = browser.loadCount
      await browser.send("Page.navigate", { url: origin + "/#session=" + ID }, s)
      await browser.loaded(s, mark)

      // The strip: one line, right above the composer, saying both.
      await until("the strip is drawn", `!!${BOX}(".bg-strip-line")`)
      const strip = await run(`${BOX}(".bg-strip-line")`)
      const composer = await run(`${BOX}("#pane-detail > .composer")`)
      const line = await run(`document.querySelector(".bg-strip-line .said").textContent`)
      seen.strip = strip
      seen.composerTop = composer?.top
      seen.line = line
      assert.match(line, /2 個背景 shell/)
      assert.match(line, /1 個 subagent 在跑/)
      assert.ok(composer && strip.bottom <= composer.top + 1, "the strip is not above the composer: " + JSON.stringify(seen))
      // And nothing about agents in the to-do fold any more.
      assert.equal(await run(`!!document.querySelector("#session-todos .agents, .session-todos-agent-count")`), false)
      await shot("strip")

      // The sheet: both sections, inside the screen.
      await click(".bg-strip-line")
      await until("the sheet opens", `!!${BOX}("#bg-sheet")`)
      // Measured where it comes to rest, not mid-way through its entrance.
      await until("the sheet settles", `document.getElementById("bg-sheet").getAnimations().every((a) => a.playState === "finished")`)
      const sheet = await run(`${BOX}("#bg-sheet")`)
      seen.sheet = sheet
      seen.sheetShells = await run(`document.querySelectorAll("#bg-sheet .bg-shell").length`)
      seen.sheetAgents = await run(`document.querySelectorAll("#bg-sheet .agents .one.child").length`)
      seen.firstShellRow = await run(`document.querySelector("#bg-sheet .bg-shell").innerText`)
      assert.equal(seen.sheetShells, 2)
      assert.equal(seen.sheetAgents, 1)
      assert.ok(sheet.left >= 0 && sheet.right <= size.width + 0.5 && sheet.top >= 0 && sheet.bottom <= size.height + 0.5,
        "the sheet leaves the screen: " + JSON.stringify(sheet))
      assert.equal(await run(`document.documentElement.scrollWidth <= ${size.width}`), true, "the page scrolls sideways")
      await shot("sheet")

      // A shell: its panel takes the transcript's space and its output grows.
      const readsBefore = shellReads
      await click(`#bg-sheet .bg-shell[data-shell="${SHELL}"]`)
      await until("the shell panel opens", `!!${BOX}("#shell-panel") && document.getElementById("pane-detail").dataset.panel === "shell"`)
      await until("the output arrives", `/line 1/.test(document.querySelector("#shell-body .shell-out")?.textContent ?? "")`)
      const first = await run(`document.querySelector("#shell-body .shell-out").textContent.trim().split("\\n").length`)
      await until("the output grows", `document.querySelector("#shell-body .shell-out").textContent.trim().split("\\n").length >= ${first + 2}`, 6_000)
      const later = await run(`document.querySelector("#shell-body .shell-out").textContent.trim().split("\\n").length`)
      seen.shellLinesFirst = first
      seen.shellLinesLater = later
      seen.shellReads = shellReads - readsBefore
      seen.shellCommand = await run(`document.querySelector("#shell-body .shell-cmd").textContent`)
      assert.equal(seen.shellCommand, "npm run build -- --watch")
      assert.equal(await run(`!!${BOX}(".bg-strip-line")`), false, "the strip stays under an open panel")
      await shot("shell")
      await run(`document.querySelector("#shell-panel").dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true }))`)
      await until("Escape closes the panel", `!document.getElementById("pane-detail").dataset.panel`)
      const settled = shellReads
      await new Promise((r) => setTimeout(r, 2_000))
      seen.readsAfterClose = shellReads - settled
      assert.equal(seen.readsAfterClose, 0, "a closed panel still reads")

      // An agent: opens its transcript in the pane.
      await click(".bg-strip-line")
      await until("the sheet opens again", `!!${BOX}("#bg-sheet")`)
      await click("#bg-sheet .agents .one.child")
      await until("the agent's transcript opens", `!!document.querySelector(".agent-head") && /three files/.test(document.getElementById("tx").textContent)`)
      seen.agentHead = await run(`document.querySelector(".agent-head .name").textContent`)
      await shot("agent")
    } finally {
      report[name] = seen
      await browser.send("Target.closeTarget", { targetId }).catch(() => undefined)
    }
  })
}
