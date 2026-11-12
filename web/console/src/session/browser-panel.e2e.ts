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
let page = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), "../../public/icon-192.png"))
let mobilePage = page

let person = false
let clicks: { x: number; y: number }[] = []
let typed: string[] = []
let keys: string[] = []
let muted = false
let layout = "desktop"
let frames = 0

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
    if (path === "/__fixture/page") {
      res.writeHead(200, { "content-type": "text/html; charset=utf-8" })
      return res.end(`<!doctype html><html><meta name=viewport content="width=device-width,initial-scale=1"><style>body{margin:0;font:18px system-ui;color:#1e293b;background:#f2f5f8}header{padding:22px 28px;background:#17344d;color:white}main{max-width:850px;margin:36px auto;padding:0 24px}article{background:white;border-radius:12px;padding:28px;border:1px solid #d7e0e9}h1{font-size:28px;margin-top:0}label{display:block;margin-top:20px}input{display:block;box-sizing:border-box;width:100%;padding:14px;border:1px solid #a7b5c5;border-radius:6px;font:inherit;margin:8px 0 20px}button{background:#167349;color:white;border:0;border-radius:6px;padding:14px 24px;font:inherit}@media(min-width:700px){main{display:grid;grid-template-columns:220px 1fr;gap:24px}}small{color:#596b7e}</style><header>Example · Browser trial</header><main><aside><h2>Welcome</h2><p>Use the form to try the page.</p><small>This is a local test page.</small></aside><article><h1>Try the browser</h1><p>Zoom in, move the picture, then tap a field.</p><label>Your name<input placeholder="Enter a name"></label><button>Continue</button></article></main></html>`)
    }
    if (path === "/__fixture/reset") { person = false; muted = false; clicks = []; typed = []; keys = []; layout = "desktop"; frames = 0; return json(res, 200, { ok: true }) }
    if (path === "/__fixture/log") return json(res, 200, { person, muted, clicks, typed, keys, layout, frames })
    if (path === "/v1/browser/activity") return json(res, 200, { ok: true, rows: [
      { at: Math.floor(Date.now() / 1000), op: "click", target: "Continue", site: "example.com", url: "https://example.com/sign-in", ok: true, thumb: page.toString("base64") },
      { at: Math.floor(Date.now() / 1000) - 5, op: "open", site: "example.com", url: "https://example.com/sign-in", ok: true, thumb: page.toString("base64") },
    ] })
    if (req.method === "POST" && path === "/v1/browser/mute") { const b = await body(req); muted = !!b.muted; return json(res, 200, { ok: true, conversation: b.conversation, muted }) }
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
      status: { connected: true, stopped: false, person_tabs: person ? [TAB] : [], pending: 0, muted: muted ? [CONVERSATION] : [], tabs: [{ tab: TAB, conversation: CONVERSATION, person, current: true }] } })
    if (req.method === "POST" && path === "/v1/browser/control") { const b = await body(req); person = !!b.person; return json(res, 200, { ok: true, tab: b.tab, person }) }
    if (path === "/v1/browser/screenshot") { frames++; return json(res, 200, { ok: true, value: { tab: TAB, url: "https://example.com/sign-in", title: "Sign in", width: layout === "mobile" ? 390 : 1280, height: 720, layout, data: (layout === "mobile" ? mobilePage : page).toString("base64") } }) }
    if (req.method === "POST" && path === "/v1/browser/person") {
      const b = await body(req)
      if (b.op === "screenshot") return json(res, 200, { ok: true, value: { tab: TAB, url: "https://example.com/sign-in", title: "Sign in", width: 1280, height: 720, data: page.toString("base64") } })
      if (!person) return json(res, 409, { error: "browser_take_over_first", detail: "Take over the tab before typing or clicking in it." })
      if (b.op === "click") clicks.push({ x: b.args.x, y: b.args.y })
      if (b.op === "type") typed.push(b.args.text)
      if (b.op === "key") keys.push(b.args.key)
      if (b.op === "viewport") layout = b.args.mode
      return json(res, 200, { ok: true, value: {} })
    }
    if (path.startsWith("/v1/work/v2/human-interventions/")) return json(res, 200, { ok: true, rows: [{
      id: "10000000-0000-4000-8000-000000000059", source_conversation: CONVERSATION, source_label: "Codex", target_conversation: CONVERSATION,
      target_session: SESSION, kind: "browser", title: "Sign in", summary: "The page needs sign-in.", action: "Take over the browser.", reason: "Only the person signs in.",
      options: [], created_at: Math.floor(Date.now() / 1000), read_at: null, resolved_at: null, version: 1,
    }], pruned_resolved: 0 })
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
  // Capture an actual responsive page for the fixtures, rather than stretch
  // a square icon into a desktop. The screenshots then show readable controls.
  const { targetId } = await browser.send("Target.createTarget", { url: "about:blank" })
  const { sessionId } = await browser.send("Target.attachToTarget", { targetId, flatten: true })
  await browser.send("Page.enable", {}, sessionId)
  for (const width of [1280, 390]) {
    await browser.send("Emulation.setDeviceMetricsOverride", { width, height: 720, deviceScaleFactor: 1, mobile: false }, sessionId)
    const mark = browser.loadCount
    await browser.send("Page.navigate", { url: origin + "/__fixture/page" }, sessionId)
    await browser.loaded(sessionId, mark)
    const { data } = await browser.send("Page.captureScreenshot", { format: "jpeg", quality: 80 }, sessionId)
    if (width === 1280) page = Buffer.from(data, "base64")
    else mobilePage = Buffer.from(data, "base64")
  }
  await browser.send("Target.closeTarget", { targetId })
})
after(async () => {
  if (browser) await browser.send("Browser.close").catch(() => {})
  if (browserProcess && browserProcess.exitCode === null) browserProcess.kill()
  browser?.close()
  server?.closeAllConnections(); await new Promise<void>((ok) => server ? server.close(() => ok()) : ok())
  if (profile) rmSync(profile, { recursive: true, force: true, maxRetries: 5 })
})

for (const [name, width, height, ui] of [["desktop", 1280, 800, "zh-Hant"], ["phone", 390, 844, "zh-Hant"], ["desktop", 1280, 800, "en"], ["phone", 390, 844, "en"], ["phone", 360, 640, "zh-Hant"]] as const) {
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
      writeFileSync(join(shots, `browser-panel-${label}-${ui}-${name}-${width}.png`), Buffer.from(data, "base64"))
    }
    try {
      await fetch(origin + "/__fixture/reset")
      await browser.send("Page.enable", {}, id); await browser.send("Runtime.enable", {}, id)
      await browser.send("Emulation.setDeviceMetricsOverride", { width, height, deviceScaleFactor: 2, mobile: width < 900 }, id)
      await browser.send("Emulation.setTouchEmulationEnabled", { enabled: name === "phone", maxTouchPoints: 5 }, id)
      const mark = browser.loadCount; await browser.send("Page.navigate", { url: `${origin}/?ui=${ui}#session=${SESSION}` }, id); await browser.loaded(id, mark)
      await until(`!!document.getElementById("detail-actions-trigger") && !document.getElementById("detail-actions-trigger").disabled`)
      await until(`document.querySelectorAll(".browser-action").length === 2`)
      assert.equal(await run(`document.querySelectorAll(".browser-action img").length`), 2, "actions appear in the transcript before the panel opens")
      await shot("transcript")
      await until(`!!document.querySelector(".browser-chip")`)
      await run(`document.getElementById("detail-actions-trigger").click()`)
      await until(`!document.getElementById("session-actions").hidden && !document.getElementById("session-browser").hidden`)
      await shot("menu")
      await run(`document.getElementById("session-browser").click()`)
      await until(`!!document.querySelector("#browser-panel:not([hidden]) .browser-shot")`)
      if (name === "phone") {
        assert.ok(await run(`document.querySelector("#browser-panel .browser-shot").getBoundingClientRect().width >= 1279`), "phone opens at a readable original size instead of shrinking the desktop to a thumbnail")
        assert.ok(await run(`document.querySelector("#browser-panel .browser-viewport")?.getBoundingClientRect().height >= innerHeight / 2`), "the picture receives at least half the phone before takeover")
        assert.ok(await run(`(() => { const r=document.getElementById('browser-panel').getBoundingClientRect();return r.x===0 && r.y===0 && r.width===innerWidth && r.height===innerHeight })()`), "mobile viewing fills the screen")
      }
      const agent = ui === "en" ? "Codex is using this tab" : "Codex 正在使用這個分頁"
      await until(`document.querySelector("#browser-panel .browser-who")?.textContent?.includes(${JSON.stringify(agent)})`)
      assert.equal(await run(`!!document.getElementById("browser-text")`), false, "no typing before a takeover")
      assert.equal(await run(`document.querySelector('.browser-layout button').disabled`), true, "watching does not grant device-emulation control")
      assert.equal(await run(`document.querySelector("#browser-panel .browser-host")?.textContent`), "example.com")
      assert.equal(await run(`document.querySelector("#browser-panel .browser-path")?.textContent`), "/sign-in")
      assert.equal(await run(`!!document.querySelector("#browser-panel .panel-view-head")`), false, "no title row above the address bar")
      assert.equal(await run(`(() => { const t = document.querySelector(".session-todos"); return !t || getComputedStyle(t).display === "none" })()`), true, "the to-do row is folded away")
      assert.ok(await run(`document.documentElement.scrollWidth <= innerWidth`), "no horizontal page scroll")
      await until(`document.querySelectorAll("#browser-panel .browser-activity li").length === 2`)
      const opWord = ui === "en" ? "Clicked" : "點擊"
      assert.ok(await run(`document.querySelector("#browser-panel .browser-activity li b")?.textContent === ${JSON.stringify(opWord)}`))
      await run(`document.querySelector("#browser-panel .browser-notify").click()`)
      await until(`document.querySelector("#browser-panel .browser-notify")?.getAttribute("aria-pressed") === "false"`)
      assert.equal((await (await fetch(origin + "/__fixture/log")).json()).muted, true)
      await shot("watching")

      await run(`document.querySelector("#browser-panel .browser-who .browser-take").click()`)
      await until(`!!document.getElementById("browser-text")`)
      const paused = ui === "en" ? "You are in control · Codex is paused" : "你正在操作 · Codex 已暫停"
      assert.ok(await run(`document.querySelector("#browser-panel .browser-who")?.textContent?.includes(${JSON.stringify(paused)})`))
      await run(`document.querySelector('.browser-layout button').click()`)
      await until(`document.querySelector('.browser-layout button').getAttribute('aria-pressed')==='true'`)
      assert.equal((await (await fetch(origin + "/__fixture/log")).json()).layout, "mobile")
      await shot("mobile-site")
      await run(`document.querySelectorAll('.browser-layout button')[1].click()`)
      await until(`document.querySelectorAll('.browser-layout button')[1].getAttribute('aria-pressed')==='true'`)
      const tap = async (x: number, y: number) => {
        if (name === "phone") {
          await browser.send("Input.dispatchTouchEvent", { type: "touchStart", touchPoints: [{ x, y, id: 1 }] }, id)
          await browser.send("Input.dispatchTouchEvent", { type: "touchEnd", touchPoints: [] }, id)
        } else {
          await browser.send("Input.dispatchMouseEvent", { type: "mousePressed", x, y, button: "left", clickCount: 1 }, id)
          await browser.send("Input.dispatchMouseEvent", { type: "mouseReleased", x, y, button: "left", clickCount: 1 }, id)
        }
      }
      await run(`[...document.querySelectorAll('.browser-zoom button')].find(b=>b.textContent===${JSON.stringify(ui === "en" ? "Fit" : "符合畫面")}).click()`)
      const centre = await run(`(() => { const r=document.querySelector('.browser-shot').getBoundingClientRect(); return {x:r.left+r.width/2,y:r.top+r.height/2} })()`)
      // Trusted pointer input, rather than a synthetic click that skips touch handlers.
      await tap(centre.x, centre.y)
      await until(`!document.querySelector('.browser-take').disabled`)
      assert.equal((await (await fetch(origin + "/__fixture/log")).json()).clicks.length, 1)

      await run(`document.querySelector('.browser-zoom button[aria-label=${JSON.stringify(ui === "en" ? "Zoom in" : "放大")} ]').click()`)
      const beforePan = await run(`document.querySelector('.browser-shot').getBoundingClientRect().left`)
      const bounds = await run(`(() => {const r=document.querySelector('.browser-viewport').getBoundingClientRect();return {x:r.left+r.width/2,y:r.top+r.height/2}})()`)
      const dragPoints = (dx: number) => [{ x: bounds.x + dx, y: bounds.y, id: 1 }]
      if (name === "phone") {
        await browser.send("Input.dispatchTouchEvent", { type: "touchStart", touchPoints: dragPoints(0) }, id)
        await browser.send("Input.dispatchTouchEvent", { type: "touchMove", touchPoints: dragPoints(-70) }, id)
        await browser.send("Input.dispatchTouchEvent", { type: "touchEnd", touchPoints: [] }, id)
      } else {
        await browser.send("Input.dispatchMouseEvent", { type: "mousePressed", x: bounds.x, y: bounds.y, button: "left", clickCount: 1 }, id)
        await browser.send("Input.dispatchMouseEvent", { type: "mouseMoved", x: bounds.x-70, y: bounds.y, button: "left", buttons: 1 }, id)
        await browser.send("Input.dispatchMouseEvent", { type: "mouseReleased", x: bounds.x-70, y: bounds.y, button: "left", clickCount: 1 }, id)
      }
      assert.equal((await (await fetch(origin + "/__fixture/log")).json()).clicks.length, 1, "dragging sends no page click")
      assert.ok(await run(`document.querySelector('.browser-shot').getBoundingClientRect().left <= ${beforePan}`), "the picture moves or stays at its edge")
      if (name === "phone") {
        const beforePinch = await run(`document.querySelector('.browser-shot').getBoundingClientRect().width`)
        const fingers = (span: number) => [{ x: bounds.x - span, y: bounds.y, id: 1 }, { x: bounds.x + span, y: bounds.y, id: 2 }]
        await browser.send("Input.dispatchTouchEvent", { type: "touchStart", touchPoints: fingers(30) }, id)
        await browser.send("Input.dispatchTouchEvent", { type: "touchMove", touchPoints: fingers(60) }, id)
        await browser.send("Input.dispatchTouchEvent", { type: "touchEnd", touchPoints: [] }, id)
        assert.ok(await run(`document.querySelector('.browser-shot').getBoundingClientRect().width > ${beforePinch + 30}`), "two fingers enlarge the picture")
        assert.equal((await (await fetch(origin + "/__fixture/log")).json()).clicks.length, 1, "pinching sends no page click")
      }
      // A point inside the clipped, zoomed picture maps back to its remote CSS pixels.
      const mapped = await run(`(() => {const r=document.querySelector('.browser-shot').getBoundingClientRect(), v=document.querySelector('.browser-viewport').getBoundingClientRect();const x=v.left+v.width/2+15,y=v.top+v.height/2+10;return {x,y,remoteX:Math.floor((x-r.left)/r.width*1280),remoteY:Math.floor((y-r.top)/r.height*720)}})()`)
      await tap(mapped.x, mapped.y)
      await until(`!document.querySelector('.browser-take').disabled`)
      const mappedLog = (await (await fetch(origin + "/__fixture/log")).json()).clicks
      assert.equal(mappedLog.length, 2)
      assert.ok(Math.abs(mappedLog[1].x-mapped.remoteX)<=1 && Math.abs(mappedLog[1].y-mapped.remoteY)<=1, "tap uses the zoomed and panned image bounds")

      await run(`[...document.querySelectorAll('.browser-zoom button')].find(b=>b.textContent==='100%').click()`)
      assert.equal(await run(`document.querySelector('.browser-zoom output').textContent`), "100%")
      if (name === "phone") {
        const frameCount = (await (await fetch(origin + "/__fixture/log")).json()).frames
        await until(`fetch('/__fixture/log').then(r=>r.json()).then(log=>log.frames>${frameCount})`)
        assert.equal(await run(`document.querySelector('.browser-zoom output').textContent`), "100%", "new screenshots keep the chosen zoom")
        await browser.send("Input.dispatchTouchEvent", { type: "touchStart", touchPoints: [{x:bounds.x,y:bounds.y,id:1}] }, id)
        await browser.send("Input.dispatchTouchEvent", { type: "touchCancel", touchPoints: [] }, id)
        assert.equal((await (await fetch(origin + "/__fixture/log")).json()).clicks.length, 2, "cancelled touches send no click")
        await browser.send("Emulation.setDeviceMetricsOverride", { width: 844, height: 390, deviceScaleFactor: 2, mobile: true }, id)
        await until(`document.querySelector('.browser-viewport').getBoundingClientRect().height > 100`)
        assert.ok(await run(`document.documentElement.scrollWidth<=innerWidth`), "landscape keeps panning inside the picture")
        await shot("landscape")
        await browser.send("Emulation.setDeviceMetricsOverride", { width, height, deviceScaleFactor: 2, mobile: true }, id)
      }
      await run(`(() => { const input = document.getElementById("browser-text"); const set = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value").set;
        set.call(input, "王小明"); input.dispatchEvent(new Event("input", { bubbles: true })) })()`)
      await run(`document.querySelector("#browser-panel .browser-type button[type=submit]").click()`)
      await until(`document.getElementById("browser-text").value === ""`)
      await run(`[...document.querySelectorAll("#browser-panel .browser-keys button")].find((b) => b.textContent === "Enter").click()`)
      await shot("in-control")
      const log = await (await fetch(origin + "/__fixture/log")).json()
      assert.equal(log.clicks.length, 2)
      assert.ok(Math.abs(log.clicks[0].x - 640) < 2 && Math.abs(log.clicks[0].y - 360) < 2, JSON.stringify(log.clicks))
      assert.deepEqual(log.typed, ["王小明"])
      assert.deepEqual(log.keys, ["Enter"])

      await run(`document.querySelector("#browser-panel .browser-who .browser-take").click()`)
      await until(`!document.getElementById("browser-text")`)
      assert.equal((await (await fetch(origin + "/__fixture/log")).json()).person, false)
      await run(`document.querySelector("#browser-panel .browser-close").click()`)
      await until(`!!document.querySelector(".human-interventions-head")`)
      await run(`(() => { const b = document.querySelector(".human-interventions-head"); if (b.getAttribute("aria-expanded") !== "true") b.click() })()`)
      const take = ui === "en" ? "Take over" : "接手操作"
      await until(`[...document.querySelectorAll(".human-intervention-card button")].some(b => b.textContent === ${JSON.stringify(take)})`)
      await run(`[...document.querySelectorAll(".human-intervention-card button")].find(b => b.textContent === ${JSON.stringify(take)}).click()`)
      await until(`!!document.querySelector("#browser-panel:not([hidden]) .browser-shot")`)
      assert.equal((await (await fetch(origin + "/__fixture/log")).json()).person, false, "opening a takeover Note sends no browser input")

    } finally {
      await browser.send("Target.closeTarget", { targetId }).catch(() => {})
    }
  })
}
