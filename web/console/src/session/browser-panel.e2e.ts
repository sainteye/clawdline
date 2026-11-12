// Build the console first, then run with `node --test` against a stand-in
// daemon: the Session's Chrome tab panel, at desktop and phone widths, in
// Traditional Chinese and English. BROWSER_PANEL_SHOTS=<dir> keeps screenshots.
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
const shots = process.env.BROWSER_PANEL_SHOTS || ""
const SESSION = "fixture-browser-pane"
const CONVERSATION = "10000000-0000-4000-8000-000000000051"
const TAB = 9
const TYPES: Record<string, string> = { ".js": "text/javascript", ".css": "text/css", ".json": "application/json", ".svg": "image/svg+xml", ".png": "image/png", ".woff2": "font/woff2", ".webmanifest": "application/manifest+json" }

// Any real image stands in for the page; the browser decodes it by its bytes.
const page = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), "../../public/icon-192.png"))

let person = false
let clicks: { x: number; y: number }[] = []
let typed: string[] = []
let keys: string[] = []

const row = {
  id: SESSION, label: "Browser Session", backend: "owned", state: "working", work_state: "working",
  evidence: "process", assistant: "codex", sessionId: CONVERSATION, cwd: "/tmp/fixture",
}
let sequence = 0
const snapshot = () => ({ at: Date.now(), scan: { complete: true, completed: { complete: true, sequence: ++sequence }, emptyAuthoritative: true, epoch: 1, generation: sequence, provenance: "fixture" }, sessions: [row] })

function json(res: ServerResponse, status: number, value: unknown) { res.writeHead(status, { "content-type": "application/json" }); res.end(JSON.stringify(value)) }
function document(ui: string): string {
  const html = readFileSync(join(dist, "index.html"), "utf8")
  return html.replace("<!-- clawdline:strings -->", "<script>localStorage.setItem('ui_language'," + JSON.stringify(ui) + ")</script>").replace("<!-- clawdline:cloud -->", "")
}
function body(req: import("node:http").IncomingMessage): Promise<any> {
  return new Promise((ok) => { const chunks: Buffer[] = []; req.on("data", (c: Buffer) => chunks.push(c)); req.on("end", () => ok(JSON.parse(Buffer.concat(chunks).toString("utf8") || "{}"))) })
}
function fixture(): Server {
  return createServer(async (req, res) => {
    const url = new URL(req.url ?? "/", "http://fixture")
    const path = url.pathname
    if (path === "/__fixture/reset") { person = false; clicks = []; typed = []; keys = []; return json(res, 200, { ok: true }) }
    if (path === "/__fixture/log") return json(res, 200, { person, clicks, typed, keys })
    if (path === "/v1/sessions") return json(res, 200, snapshot())
    if (path === "/v1/health") return json(res, 200, { authed: true, version: "fixture", api_level: 99 })
    if (path === "/v1/strings") {
      const file = join(dist, "catalogs", `${url.searchParams.get("lang") || "en"}.json`)
      return existsSync(file) ? json(res, 200, JSON.parse(readFileSync(file, "utf8"))) : json(res, 404, { error: "not_found" })
    }
    if (path === "/v1/events") {
      res.writeHead(200, { "content-type": "text/event-stream", "cache-control": "no-cache" })
      res.write("event: sessions\ndata: " + JSON.stringify(snapshot()) + "\n\n")
      return
    }
    if (path === "/v1/transcript") return json(res, 200, { entries: [], evidence: "transcript", id: SESSION, signature: "fixture" })
    if (path === "/v1/browser/status") return json(res, 200, { ok: true, enabled: true, notify_new_site: true,
      status: { connected: true, stopped: false, person_tabs: person ? [TAB] : [], pending: 0, tabs: [{ tab: TAB, conversation: CONVERSATION, person, current: true }] } })
    if (req.method === "POST" && path === "/v1/browser/control") { const b = await body(req); person = !!b.person; return json(res, 200, { ok: true, tab: b.tab, person }) }
    if (path === "/v1/browser/screenshot") return json(res, 200, { ok: true, value: { tab: TAB, url: "https://example.com/sign-in", title: "Sign in", width: 1280, height: 720, data: page.toString("base64") } })
    if (req.method === "POST" && path === "/v1/browser/person") {
      const b = await body(req)
      if (b.op === "screenshot") return json(res, 200, { ok: true, value: { tab: TAB, url: "https://example.com/sign-in", title: "Sign in", width: 1280, height: 720, data: page.toString("base64") } })
      if (!person) return json(res, 409, { error: "browser_take_over_first", detail: "Take over the tab before typing or clicking in it." })
      if (b.op === "click") clicks.push({ x: b.args.x, y: b.args.y })
      if (b.op === "type") typed.push(b.args.text)
      if (b.op === "key") keys.push(b.args.key)
      return json(res, 200, { ok: true, value: {} })
    }
    if (path.startsWith("/v1/")) return json(res, 404, { error: "not_found", detail: path })
    if (path === "/") { res.writeHead(200, { "content-type": "text/html; charset=utf-8" }); return res.end(document(url.searchParams.get("ui") || "zh-Hant")) }
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
      this.pending.set(id, { ok: (v) => { clearTimeout(timer); ok(v) }, fail: (e) => { clearTimeout(timer); fail(new Error(method + ": " + e.message)) } })
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
  assert.ok(existsSync(join(dist, "index.html")), "build the console first")
  assert.ok(existsSync(chrome), "Chrome is required")
  server = fixture(); await new Promise<void>((ok) => server.listen(0, "127.0.0.1", ok))
  const address = server.address(); origin = "http://127.0.0.1:" + (typeof address === "object" && address ? address.port : 0)
  profile = mkdtempSync(join(tmpdir(), "clawdline-browser-panel-"))
  browserProcess = spawn(chrome, ["--headless=new", "--remote-debugging-port=0", "--user-data-dir=" + profile, "--no-first-run", "--no-default-browser-check", "about:blank"])
  const endpoint = await new Promise<string>((ok, fail) => {
    let said = ""; const timer = setTimeout(() => { browserProcess.kill(); fail(new Error("Chrome did not start: " + said)) }, 20_000)
    browserProcess.stderr?.on("data", (chunk) => { said += String(chunk); const match = /DevTools listening on (ws:\/\/\S+)/.exec(said); if (match) { clearTimeout(timer); ok(match[1]) } })
  })
  browser = await Browser.connect(endpoint)
})
after(async () => {
  if (browser) await browser.send("Browser.close").catch(() => {})
  if (browserProcess && browserProcess.exitCode === null) browserProcess.kill()
  browser?.close()
  server?.closeAllConnections(); await new Promise<void>((ok) => server ? server.close(() => ok()) : ok())
  if (profile) rmSync(profile, { recursive: true, force: true, maxRetries: 5 })
})

for (const [name, width, height, ui] of [["desktop", 1280, 800, "zh-Hant"], ["phone", 390, 844, "zh-Hant"], ["desktop", 1280, 800, "en"]] as const) {
  test(`${name} ${ui}: the Chrome tab is watched, taken over, used and handed back`, async () => {
    const { targetId } = await browser.send("Target.createTarget", { url: "about:blank" })
    const { sessionId: id } = await browser.send("Target.attachToTarget", { targetId, flatten: true })
    const run = async (expression: string) => {
      const { result, exceptionDetails } = await browser.send("Runtime.evaluate", { expression, returnByValue: true, awaitPromise: true }, id)
      if (exceptionDetails) throw new Error(exceptionDetails.exception?.description ?? exceptionDetails.text)
      return result.value
    }
    const until = async (expression: string) => {
      const deadline = Date.now() + 8_000
      while (!(await run(expression))) {
        if (Date.now() > deadline) assert.fail("did not reach " + expression + ": " + await run(`document.getElementById("browser-panel")?.innerText ?? "(no panel)"`))
        await new Promise((r) => setTimeout(r, 50))
      }
    }
    const shot = async (label: string) => {
      if (!shots) return
      const { data } = await browser.send("Page.captureScreenshot", { format: "png" }, id)
      writeFileSync(join(shots, `browser-panel-${label}-${ui}-${name}.png`), Buffer.from(data, "base64"))
    }
    try {
      await fetch(origin + "/__fixture/reset")
      await browser.send("Page.enable", {}, id); await browser.send("Runtime.enable", {}, id)
      await browser.send("Emulation.setDeviceMetricsOverride", { width, height, deviceScaleFactor: 2, mobile: width < 900 }, id)
      const mark = browser.loadCount; await browser.send("Page.navigate", { url: `${origin}/?ui=${ui}#session=${SESSION}` }, id); await browser.loaded(id, mark)
      await until(`!!document.getElementById("detail-actions-trigger") && !document.getElementById("detail-actions-trigger").disabled`)
      await run(`document.getElementById("detail-actions-trigger").click()`)
      await until(`!document.getElementById("session-actions").hidden && !document.getElementById("session-browser").hidden`)
      await shot("menu")
      await run(`document.getElementById("session-browser").click()`)
      await until(`!!document.querySelector("#browser-panel:not([hidden]) .browser-shot")`)
      const agent = ui === "en" ? "Codex is using this tab" : "Codex 正在使用這個分頁"
      await until(`document.querySelector("#browser-panel .browser-who")?.textContent?.includes(${JSON.stringify(agent)})`)
      assert.equal(await run(`!!document.getElementById("browser-text")`), false, "no typing before a takeover")
      assert.equal(await run(`document.querySelector("#browser-panel .browser-host")?.textContent`), "example.com")
      assert.equal(await run(`document.querySelector("#browser-panel .browser-path")?.textContent`), "/sign-in")
      assert.equal(await run(`!!document.querySelector("#browser-panel .panel-view-head")`), false, "no title row above the address bar")
      assert.equal(await run(`(() => { const t = document.querySelector(".session-todos"); return !t || getComputedStyle(t).display === "none" })()`), true, "the to-do row is folded away")
      assert.ok(await run(`document.documentElement.scrollWidth <= innerWidth`), "no horizontal page scroll")
      await shot("watching")

      await run(`document.querySelector("#browser-panel .browser-who button").click()`)
      await until(`!!document.getElementById("browser-text")`)
      const paused = ui === "en" ? "You are in control · Codex is paused" : "你正在操作 · Codex 已暫停"
      assert.ok(await run(`document.querySelector("#browser-panel .browser-who")?.textContent?.includes(${JSON.stringify(paused)})`))
      // A tap at the picture's centre is a click at the page's centre.
      await run(`(() => { const img = document.querySelector("#browser-panel .browser-shot"); const r = img.getBoundingClientRect();
        img.dispatchEvent(new MouseEvent("click", { bubbles: true, clientX: r.left + r.width / 2, clientY: r.top + r.height / 2 })) })()`)
      await run(`(() => { const input = document.getElementById("browser-text"); const set = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value").set;
        set.call(input, "王小明"); input.dispatchEvent(new Event("input", { bubbles: true })) })()`)
      await run(`document.querySelector("#browser-panel .browser-type button[type=submit]").click()`)
      await until(`document.getElementById("browser-text").value === ""`)
      await run(`[...document.querySelectorAll("#browser-panel .browser-keys button")].find((b) => b.textContent === "Enter").click()`)
      await shot("in-control")
      const log = await (await fetch(origin + "/__fixture/log")).json()
      assert.equal(log.clicks.length, 1)
      assert.ok(Math.abs(log.clicks[0].x - 640) < 2 && Math.abs(log.clicks[0].y - 360) < 2, JSON.stringify(log.clicks))
      assert.deepEqual(log.typed, ["王小明"])
      assert.deepEqual(log.keys, ["Enter"])

      await run(`document.querySelector("#browser-panel .browser-who button").click()`)
      await until(`!document.getElementById("browser-text")`)
      assert.equal((await (await fetch(origin + "/__fixture/log")).json()).person, false)
    } finally {
      await browser.send("Target.closeTarget", { targetId }).catch(() => {})
    }
  })
}
