// The transcript's way back to its newest end, through a real browser at
// 1280px and at 390px:
//
//   (cd web && npm run build)
//   node --test --experimental-strip-types web/console/src/session/jump-to-latest.e2e.ts
//
// The built console is served by a stand-in daemon that answers one Claude
// session whose transcript is many screens long and gains a turn when the test
// asks for one. Headless Chrome (CHROME, else the usual macOS install) checks
// that the button is not there at the end or half a screen up, is there a
// screen up, says so when a turn lands below the reader, and takes them to the
// end. JUMP_SHOTS=<dir> saves a picture of each step; JUMP_REPORT=<file>
// writes the measurements.
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
const shots = process.env.JUMP_SHOTS || ""
const reportTo = process.env.JUMP_REPORT || ""
const DESK = { width: 1280, height: 800, mobile: false }
const PHONE = { width: 390, height: 844, mobile: true }

const ID = "ttys032"

const ROW = {
  id: ID,
  label: "A long conversation",
  backend: "owned",
  state: "idle",
  work_state: "ready",
  evidence: "process",
  isClaude: true,
  assistant: "claude",
  sessionId: "10000000-0000-4000-8000-000000000032",
  cwd: "/tmp/fixture",
  closeability: {
    activity_generation: 1, attestation_id: null, mover: null, obligation_generation: 1, observed_at: 1,
    provenance: [], reasons: [], session_generation: 1, source: "broker", state: "safe", version: "fixture",
  },
}

// Sixty turns, each a paragraph: many screens at either width. `turns` grows
// when the test says a new one has landed.
let turns = 60
function transcript() {
  const entries = Array.from({ length: turns }, (_, i) => ({
    role: i % 2 ? "assistant" : "user",
    text: "Turn " + (i + 1) + ". " + "This is a line of the conversation that wraps across the reading column. ".repeat(3),
  }))
  return { entries, evidence: "transcript", id: ID, signature: "turns-" + turns }
}

function snapshot() {
  return {
    at: Date.now(),
    scan: { complete: true, completed: { complete: true, sequence: 1 }, emptyAuthoritative: true, epoch: 1,
      generation: 1, provenance: "fixture" },
    sessions: [ROW],
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
    if (path === "/v1/transcript") return json(res, 200, transcript())
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
  profile = mkdtempSync(join(tmpdir(), "clawdline-jump-"))
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
const FROM_END = `(() => { const el = document.getElementById("tx-scroll"); return Math.round(el.scrollHeight - el.clientHeight - el.scrollTop) })()`
const SHOWN = `document.querySelector(".tx-jump")?.dataset.shown === "on"`
const HIDDEN = `document.querySelector(".tx-jump")?.dataset.shown === "off"`

for (const [name, size] of [["desk-1280", DESK], ["phone-390", PHONE]] as const) {
  test(name + ": the transcript offers the way back to its newest end", async () => {
    turns = 60
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
    const scrollTo = (expr: string) => run(`(() => { const el = document.getElementById("tx-scroll"); el.scrollTop = ${expr}; return el.scrollTop })()`)
    const seen: Record<string, unknown> = {}
    try {
      await browser.send("Page.enable", {}, s)
      await browser.send("Runtime.enable", {}, s)
      await browser.send("Emulation.setDeviceMetricsOverride", { ...size, deviceScaleFactor: 2 }, s)
      const mark = browser.loadCount
      await browser.send("Page.navigate", { url: origin + "/#session=" + ID }, s)
      await browser.loaded(s, mark)

      // Opened at the end: nothing offered.
      await until("the transcript is drawn", `/Turn 60\\./.test(document.getElementById("tx").textContent)`)
      await until("it opens at the end", `${FROM_END} <= 40`)
      seen.screens = await run(`(() => { const el = document.getElementById("tx-scroll"); return +(el.scrollHeight / el.clientHeight).toFixed(1) })()`)
      assert.ok((seen.screens as number) > 4, "the fixture is not long enough: " + seen.screens)
      assert.equal(await run(HIDDEN), true, "offered at the end")
      assert.equal(await run(`document.querySelector(".tx-jump-go").tabIndex`), -1)

      // Half a screen up is reading: still nothing.
      await scrollTo(`el.scrollHeight - el.clientHeight * 1.5`)
      await new Promise((r) => setTimeout(r, 300))
      seen.halfScreenFromEnd = await run(FROM_END)
      assert.equal(await run(HIDDEN), true, "offered half a screen up")

      // Far up: offered, inside the reading pane and above the composer.
      await scrollTo(`0`)
      await until("offered far up", SHOWN)
      await until("it has come to rest", `document.querySelector(".tx-jump-go").getAnimations().every((a) => a.playState === "finished")`)
      const button = await run(`${BOX}(".tx-jump-go")`)
      const pane = await run(`${BOX}("#tx-scroll")`)
      const composer = await run(`${BOX}("#pane-detail > .composer")`)
      seen.button = button
      seen.pane = pane
      seen.composerTop = composer?.top
      seen.label = await run(`document.querySelector(".tx-jump-go").textContent`)
      assert.equal(seen.label, "跳到最新")
      assert.ok(button.top >= pane.top && button.bottom <= pane.bottom, "the button is outside the pane: " + JSON.stringify(seen))
      assert.ok(pane.bottom - button.bottom <= 40, "the button is not at the pane's bottom edge: " + JSON.stringify(seen))
      assert.ok(button.height >= 32, "too small to tap: " + button.height)
      assert.equal(await run(`document.documentElement.scrollWidth <= ${size.width}`), true, "the page scrolls sideways")
      assert.equal(await run(`document.elementFromPoint(${Math.round((button.left + button.right) / 2)}, ${Math.round((button.top + button.bottom) / 2)})?.closest(".tx-jump-go") !== null`), true,
        "something covers the button")
      await shot("far-up")

      // A turn lands below the reader: it says so.
      turns = 61
      await until("the new turn is read", `/Turn 61\\./.test(document.getElementById("tx").textContent)`, 10_000)
      await until("it says something is new", `document.querySelector(".tx-jump")?.dataset.fresh === "on"`)
      seen.freshLabel = await run(`document.querySelector(".tx-jump-go").textContent`)
      seen.stayedPut = await run(`document.getElementById("tx-scroll").scrollTop`)
      assert.equal(seen.freshLabel, "有新訊息")
      assert.equal(seen.stayedPut, 0, "the reader was moved by the new turn")
      await shot("fresh")

      // Pressed: to the end, and it goes away.
      const started = Date.now()
      await run(`document.querySelector(".tx-jump-go").click()`)
      await until("it reaches the end", `${FROM_END} <= 1`, 5_000)
      seen.jumpMs = Date.now() - started
      await until("it goes away", `${HIDDEN} && document.querySelector(".tx-jump")?.dataset.fresh === "off"`)
      seen.lastTurnVisible = await run(`(() => { const t = [...document.querySelectorAll("#tx .entry")].at(-1).getBoundingClientRect(); const p = document.getElementById("tx-scroll").getBoundingClientRect(); return { last: Math.round(t.bottom), pane: Math.round(p.bottom), top: Math.round(p.top), text: [...document.querySelectorAll("#tx .entry")].at(-1).textContent.slice(0, 40) } })()`)
      const last = seen.lastTurnVisible as { last: number; pane: number; top: number }
      assert.ok(last.last <= last.pane + 1 && last.last > last.top, "the newest turn is not on screen: " + JSON.stringify(last))
      await shot("at-end")

      // Past the end by a little with something new: offered at once.
      await scrollTo(`el.scrollHeight - el.clientHeight - 200`)
      turns = 62
      await until("the next turn is read", `/Turn 62\\./.test(document.getElementById("tx").textContent)`, 10_000)
      await until("offered a little way up once something is new", SHOWN)
      assert.equal(await run(`document.querySelector(".tx-jump")?.dataset.fresh`), "on")
    } finally {
      report[name] = seen
      await browser.send("Target.closeTarget", { targetId }).catch(() => undefined)
    }
  })
}
