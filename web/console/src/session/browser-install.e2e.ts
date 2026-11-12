// Build the console first (`cd web/console && npx vite build`), then run with
// `node --test` against a stand-in daemon: the browser setup guide opened from
// the Session's Chrome tab panel, at desktop and phone widths, in Traditional
// Chinese and English. BROWSER_INSTALL_SHOTS=<dir> keeps screenshots.
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
const shots = process.env.BROWSER_INSTALL_SHOTS || ""
const SESSION = "fixture-browser-install"
const CONVERSATION = "10000000-0000-4000-8000-000000000052"
const TYPES: Record<string, string> = { ".js": "text/javascript", ".css": "text/css", ".json": "application/json", ".svg": "image/svg+xml", ".png": "image/png", ".woff2": "font/woff2", ".webmanifest": "application/manifest+json" }

// The stand-in daemon's state. A launch connects the extension a moment later
// unless the fixture says the window never connects.
type Fixture = { chromium: boolean; connects: boolean; connected: boolean; enabled: boolean; notify: boolean; launches: number; settings: unknown[] }
let state: Fixture
function reset(over: Partial<Fixture> = {}) {
  state = { chromium: true, connects: true, connected: false, enabled: false, notify: false, launches: 0, settings: [], ...over }
}
reset()

const row = {
  id: SESSION, label: "Browser Session", backend: "owned", state: "working", work_state: "working",
  evidence: "process", assistant: "claude", sessionId: CONVERSATION, cwd: "/tmp/fixture",
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
    if (path === "/__fixture/reset") { reset(JSON.parse(url.searchParams.get("state") || "{}")); return json(res, 200, { ok: true }) }
    if (path === "/__fixture/log") return json(res, 200, state)
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
    if (path === "/v1/browser/status") return json(res, 200, { ok: true, enabled: state.enabled, notify_new_site: state.notify,
      status: { connected: state.connected, stopped: false, person_tabs: [], pending: 0, tabs: [] } })
    if (path === "/v1/browser/setup") return json(res, 200, { ok: true, chromium_found: state.chromium,
      chromium: state.chromium ? "/fixture/Chromium" : "", profile: "/fixture/chrome-profile", running: state.launches > 0,
      connected: state.connected, enabled: state.enabled, notify_new_site: state.notify, install_command: "npx playwright install chromium" })
    if (req.method === "POST" && path === "/v1/browser/launch") {
      await body(req)
      if (!state.chromium) return json(res, 409, { error: "browser_chromium_missing", detail: "No Chromium was found on this machine. Install one with: npx playwright install chromium" })
      state.launches++
      const mine = state
      if (state.connects) setTimeout(() => { mine.connected = true }, 2500)
      return json(res, 200, { ok: true, pid: 4242, browser: "/fixture/Chromium", profile: "/fixture/chrome-profile", extension_id: "a".repeat(32) })
    }
    if (req.method === "POST" && path === "/v1/browser/settings") {
      const b = await body(req)
      state.settings.push(b)
      if (typeof b.browser_enabled === "boolean") state.enabled = b.browser_enabled
      if (typeof b.browser_notify_new_site === "boolean") state.notify = b.browser_notify_new_site
      return json(res, 200, { browser_enabled: state.enabled, browser_notify_new_site: state.notify })
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
  profile = mkdtempSync(join(tmpdir(), "clawdline-browser-install-"))
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

type Page = { run: (expression: string) => Promise<any>; until: (expression: string, ms?: number) => Promise<void>; shot: (label: string) => Promise<void>; close: () => Promise<void> }

async function open(name: string, width: number, height: number, ui: string, over: Partial<Fixture>): Promise<Page> {
  await fetch(origin + "/__fixture/reset?state=" + encodeURIComponent(JSON.stringify(over)))
  const { targetId } = await browser.send("Target.createTarget", { url: "about:blank" })
  const { sessionId: id } = await browser.send("Target.attachToTarget", { targetId, flatten: true })
  const run = async (expression: string) => {
    const { result, exceptionDetails } = await browser.send("Runtime.evaluate", { expression, returnByValue: true, awaitPromise: true }, id)
    if (exceptionDetails) throw new Error(exceptionDetails.exception?.description ?? exceptionDetails.text)
    return result.value
  }
  const until = async (expression: string, ms = 8_000) => {
    const deadline = Date.now() + ms
    while (!(await run(expression))) {
      if (Date.now() > deadline) assert.fail("did not reach " + expression + ": " + await run(`(document.querySelector("dialog.browser-setup") ?? document.getElementById("browser-panel"))?.innerText ?? "(nothing)"`))
      await new Promise((r) => setTimeout(r, 50))
    }
  }
  const shot = async (label: string) => {
    if (!shots) return
    const { data } = await browser.send("Page.captureScreenshot", { format: "png" }, id)
    writeFileSync(join(shots, `browser-install-${label}-${ui}-${name}.png`), Buffer.from(data, "base64"))
  }
  await browser.send("Page.enable", {}, id); await browser.send("Runtime.enable", {}, id)
  await browser.send("Emulation.setDeviceMetricsOverride", { width, height, deviceScaleFactor: 2, mobile: width < 900 }, id)
  const mark = browser.loadCount; await browser.send("Page.navigate", { url: `${origin}/?ui=${ui}#session=${SESSION}` }, id); await browser.loaded(id, mark)
  await until(`!!document.getElementById("detail-actions-trigger") && !document.getElementById("detail-actions-trigger").disabled`)
  await run(`document.getElementById("detail-actions-trigger").click()`)
  await until(`!document.getElementById("session-actions").hidden && !document.getElementById("session-browser").hidden`)
  await run(`document.getElementById("session-browser").click()`)
  await until(`!!document.querySelector("#browser-panel:not([hidden]) .browser-setup-ask button")`)
  return { run, until, shot, close: () => browser.send("Target.closeTarget", { targetId }).catch(() => {}) }
}

const SETUP = `document.querySelector("dialog.browser-setup")`
const heading = `${SETUP}?.querySelector("h2")?.textContent`
const press = (text: string) => `[...${SETUP}.querySelectorAll("button")].find((b) => b.textContent === ${JSON.stringify(text)}).click()`

const WORDS = {
  "zh-Hant": { ask: "設定瀏覽器", notConnected: "Clawdline Chrome 擴充功能尚未連線", off: "瀏覽器使用目前是關閉的", next: "下一步", launch: "打開 Clawdline 專用 Chrome", waiting: "等待 Chrome 連線…",
    title: "讓 Agent 用瀏覽器做事，你隨時看得到", confirm: "在這個 Chrome 裡登入一次", start: "開始使用", finish: "完成", done: "瀏覽器設定好了", close: "關閉", later: "稍後再說",
    noChromium: "找不到 Chromium", closed: "Chrome 視窗被關掉了", still: "仍在等待連線，偵測到會自動繼續" },
  en: { ask: "Set up the browser", notConnected: "The Clawdline Chrome extension is not connected", off: "Browser use is turned off", next: "Next", launch: "Open Clawdline's Chrome", waiting: "Waiting for Chrome to connect…",
    title: "Let agents work in a browser, and see it whenever you like", confirm: "Sign in once, in this Chrome", start: "Start using it", finish: "Finish", done: "The browser is set up", close: "Close", later: "Not now",
    noChromium: "No Chromium was found", closed: "The Chrome window was closed", still: "Still waiting for a connection" },
} as const

/** The guide fits the screen: 600 px on a desktop, the whole width on a phone, nothing scrolls sideways, and every control is 44 px tall on a phone. */
async function fits(page: Page, width: number) {
  const box = await page.run(`(() => { const d = ${SETUP}; const r = d.getBoundingClientRect(); return { w: r.width, sw: d.scrollWidth, cw: d.clientWidth, page: document.documentElement.scrollWidth } })()`)
  if (width < 600) assert.equal(Math.round(box.w), width, "a full-width sheet on a phone")
  else assert.equal(Math.round(box.w), 600, "a 600 px dialog")
  assert.ok(box.sw <= box.cw, "nothing in the guide scrolls sideways: " + JSON.stringify(box))
  assert.ok(box.page <= width, "no horizontal page scroll")
  if (width < 600) {
    const small = await page.run(`[...${SETUP}.querySelectorAll("button, label.browser-setup-switch, input:not([type=checkbox])")].filter((b) => b.getClientRects().length).map((b) => [b.textContent || b.getAttribute("aria-label"), Math.round(b.getBoundingClientRect().height)]).filter(([, h]) => h < 44)`)
    assert.deepEqual(small, [], "every target is at least 44 px tall")
  }
}

for (const [name, width, height, ui] of [["desktop", 1280, 800, "zh-Hant"], ["phone", 390, 844, "zh-Hant"], ["desktop", 1280, 800, "en"], ["phone", 390, 844, "en"]] as const) {
  test(`${name} ${ui}: the guide opens Clawdline's Chrome, waits for it and saves the two choices`, async () => {
    const w = WORDS[ui]
    const page = await open(name, width, height, ui, {})
    try {
      assert.ok(await page.run(`document.querySelector("#browser-panel .browser-setup-ask")?.textContent?.includes(${JSON.stringify(w.notConnected)})`))
      await page.shot("panel")
      await page.run(`document.querySelector("#browser-panel .browser-setup-ask button").click()`)
      await page.until(`${SETUP}?.open && ${heading} === ${JSON.stringify(w.title)}`)
      assert.equal(await page.run(`${SETUP}.querySelectorAll(".browser-setup-features li").length`), 4)
      await fits(page, width)
      await page.shot("1-benefits")

      await page.run(press(w.next))
      await page.until(`!!${SETUP}.querySelector(".setup-primary") && ${SETUP}.querySelector(".setup-primary").textContent === ${JSON.stringify(w.launch)}`)
      await fits(page, width)
      await page.shot("2-prepare")
      await page.run(press(w.launch))
      await page.until(`${SETUP}.querySelector(".browser-setup-wait")?.textContent === ${JSON.stringify(w.waiting)}`)
      await page.shot("2-waiting")
      // The stand-in connects 2.5 s after the launch; the guide moves on by itself.
      await page.until(`${heading} === ${JSON.stringify(w.confirm)}`, 10_000)
      assert.equal((await (await fetch(origin + "/__fixture/log")).json()).launches, 1)
      await fits(page, width)
      await page.shot("3-confirm")

      await page.run(press(w.next))
      await page.until(`${heading} === ${JSON.stringify(w.start)}`)
      assert.deepEqual(await page.run(`[...${SETUP}.querySelectorAll("input[role=switch]")].map((i) => i.checked)`), [true, true], "both switches start on")
      assert.deepEqual((await (await fetch(origin + "/__fixture/log")).json()).settings, [], "nothing is written before Finish")
      await fits(page, width)
      await page.shot("4-start")

      await page.run(press(w.finish))
      await page.until(`${heading} === ${JSON.stringify(w.done)}`)
      assert.deepEqual((await (await fetch(origin + "/__fixture/log")).json()).settings, [{ browser_enabled: true, browser_notify_new_site: true }])
      assert.equal(await page.run(`${SETUP}.querySelectorAll(".setup-summary li[data-done]").length`), 3)
      await fits(page, width)
      await page.shot("5-done")
      await page.run(press(w.close))
      await page.until(`!${SETUP}`)
    } finally {
      await page.close()
    }
  })
}

for (const [name, width, height, ui] of [["phone", 390, 844, "zh-Hant"], ["desktop", 1280, 800, "en"]] as const) {
  test(`${name} ${ui}: without a Chromium the guide shows the command that installs one`, async () => {
    const w = WORDS[ui]
    const page = await open(name, width, height, ui, { chromium: false })
    try {
      await page.run(`document.querySelector("#browser-panel .browser-setup-ask button").click()`)
      await page.until(`${SETUP}?.open`)
      await page.run(press(w.next))
      await page.until(`!!${SETUP}.querySelector(".setup-primary") && ${SETUP}.querySelector(".setup-primary").textContent === ${JSON.stringify(w.launch)}`)
      await page.run(press(w.launch))
      await page.until(`!!${SETUP}.querySelector("[data-cause=chromium]")`)
      assert.ok(await page.run(`${SETUP}.querySelector("[data-cause=chromium]").textContent.includes(${JSON.stringify(w.noChromium)})`))
      assert.equal(await page.run(`${SETUP}.querySelector(".browser-setup-copy input").value`), "npx playwright install chromium")
      assert.equal((await (await fetch(origin + "/__fixture/log")).json()).launches, 0)
      await fits(page, width)
      await page.shot("2-missing")
    } finally {
      await page.close()
    }
  })
}

test("desktop zh-Hant: after 30 seconds without a connection the guide lists what may have happened", async () => {
  const w = WORDS["zh-Hant"]
  const page = await open("desktop", 1280, 800, "zh-Hant", { connects: false })
  try {
    await page.run(`document.querySelector("#browser-panel .browser-setup-ask button").click()`)
    await page.until(`${SETUP}?.open`)
    await page.run(press(w.next))
    await page.until(`!!${SETUP}.querySelector(".setup-primary") && ${SETUP}.querySelector(".setup-primary").textContent === ${JSON.stringify(w.launch)}`)
    await page.run(press(w.launch))
    await page.until(`!!${SETUP}.querySelector(".browser-setup-wait")`)
    assert.equal(await page.run(`!!${SETUP}.querySelector(".browser-setup-causes")`), false, "no causes before 30 s")
    await page.until(`!!${SETUP}.querySelector("[data-cause=closed]")`, 40_000)
    assert.ok(await page.run(`${SETUP}.querySelector("[data-cause=closed]").textContent.includes(${JSON.stringify(w.closed)})`))
    assert.ok(await page.run(`${SETUP}.querySelector("[data-cause=waiting]").textContent.includes(${JSON.stringify(w.still)})`))
    assert.equal(await page.run(`!!${SETUP}.querySelector("[data-cause=chromium]")`), false, "a Chromium was found")
    await page.shot("2-slow")
    await page.run(`${SETUP}.querySelector("[data-cause=closed] button").click()`)
    await page.until(`fetch("/__fixture/log").then((r) => r.json()).then((s) => s.launches === 2)`)
  } finally {
    await page.close()
  }
})

test("phone en: with browser use turned off the panel says so and offers the guide", async () => {
  const w = WORDS.en
  const page = await open("phone", 390, 844, "en", { connected: true, enabled: false })
  try {
    assert.ok(await page.run(`document.querySelector("#browser-panel .browser-setup-ask")?.textContent?.includes(${JSON.stringify(w.off)})`))
    assert.ok(await page.run(`document.documentElement.scrollWidth <= innerWidth`), "no horizontal page scroll")
    await page.shot("panel-off")
    await page.run(`document.querySelector("#browser-panel .browser-setup-ask button").click()`)
    await page.until(`${SETUP}?.open`)
    await page.run(press(w.later))
    await page.until(`!${SETUP}`)
  } finally {
    await page.close()
  }
})
