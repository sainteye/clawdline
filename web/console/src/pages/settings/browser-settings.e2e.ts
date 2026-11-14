// Build the console first, then run with `node --test` against a stand-in
// daemon: the Browser block on Settings — connection, the two switches, the
// emergency stop and the audit list with its filters — at desktop and phone
// widths, in Traditional Chinese and English. BROWSER_SETTINGS_SHOTS=<dir>
// keeps screenshots.
import { test, before, after } from "node:test"
import assert from "node:assert/strict"
import { spawn, type ChildProcess } from "node:child_process"
import { createServer, type Server, type ServerResponse } from "node:http"
import { existsSync, mkdtempSync, readFileSync, rmSync, statSync, writeFileSync } from "node:fs"
import { tmpdir } from "node:os"
import { dirname, extname, join, normalize, resolve } from "node:path"
import { fileURLToPath } from "node:url"

const dist = resolve(dirname(fileURLToPath(import.meta.url)), "../../../dist")
const chrome = process.env.CHROME || "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
const shots = process.env.BROWSER_SETTINGS_SHOTS || ""
const SESSION = "fixture-browser-settings"
const NAMED = "10000000-0000-4000-8000-000000000051"
const UNNAMED = "abcdef12-0000-4000-8000-000000000099"
const TYPES: Record<string, string> = { ".js": "text/javascript", ".css": "text/css", ".json": "application/json", ".svg": "image/svg+xml", ".png": "image/png", ".woff2": "font/woff2", ".webmanifest": "application/manifest+json" }

const now = () => Math.floor(Date.now() / 1000)
const longText = "長".repeat(2000)
function allRows() {
  const t = now()
  return [
    { at: t - 60, conversation: NAMED, op: "type", tab: 9, url: "https://accounts.example.com/sign-in", site: "example.com", target: "Password", text_length: 8, password: true, ok: true },
    { at: t - 120, conversation: NAMED, op: "type", tab: 9, url: "https://accounts.example.com/sign-in", site: "example.com", target: "Name", text: longText, text_length: 2400, text_cut: true, ok: true },
    { at: t - 180, conversation: NAMED, op: "click", tab: 9, url: "https://accounts.example.com/sign-in?next=%2Fhome", site: "example.com", target: "Sign in with a very long button label that should wrap on a phone", ok: true },
    { at: t - 240, conversation: UNNAMED, op: "open", tab: 11, url: "https://github.com/example/project/pulls", site: "github.com", ok: true },
    { at: t - 300, conversation: NAMED, op: "screenshot", tab: 9, ok: false, error: "browser_stopped" },
    { at: t - 10 * 86400, conversation: UNNAMED, op: "takeover", tab: 11, url: "https://github.com/", site: "github.com", ok: true },
  ]
}

const initialSettings = () => ({ browser_mode:"native", browser_enabled: false, browser_notify_new_site: false, browser_policy: { order:["native","isolated","user_chrome"], automatic:{native:true,isolated:false,user_chrome:false} } })
let settings = initialSettings()
let usualReady = false
let launches = 0
let stopped = false
let saveFailed = false
let stateError = false
let empty = false
let queries: Record<string, string>[] = []

const row = {
  id: SESSION, label: "Browser Session", backend: "owned", state: "working", work_state: "working",
  evidence: "process", assistant: "codex", sessionId: NAMED, cwd: "/tmp/fixture",
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
    if (path === "/__fixture/reset") { settings = initialSettings(); usualReady=false; launches=0; stopped = false; saveFailed = false; stateError = false; empty = false; queries = []; return json(res, 200, { ok: true }) }
    if (path === "/__fixture/ready") {usualReady=true;return json(res,200,{ok:true})}
    if (path === "/__fixture/fail-stop") {saveFailed = true; return json(res,200,{ok:true})}
    if (path === "/__fixture/recover-stop") {saveFailed = false; return json(res,200,{ok:true})}
    if (path === "/__fixture/empty") { empty = true; return json(res, 200, { ok: true }) }
    if (path === "/__fixture/log") return json(res, 200, { settings, stopped, queries, launches })
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
    if (path === "/v1/settings/integrations") return json(res,200,{codegraph_enabled:false,workflow_skills_enabled:false,workflow_profile:"clawdline",space_saving_images:false})
    if (path === "/v1/browser/status") return json(res, 200, { ok: true, enabled: settings.browser_enabled, notify_new_site: settings.browser_notify_new_site,
      profiles: { isolated:{ready:true},user_chrome:{ready:usualReady} }, status: { connected: true, stopped, state_error: stateError, host: { version: "0.1.0", browser: "Mozilla/5.0 (Macintosh) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.6778.85 Safari/537.36", last_poll: now() }, person_tabs: [], pending: 0, tabs: [], muted: [] } })
    if (path === "/v1/browser/prepare") return json(res,200,{ok:true,directory:"/test/Clawdline's data/chrome-extension-revision",platform:"darwin",revision:"revision",open_command:"open '/test/Clawdline'\\''s data/chrome-extension-revision'",connect_command:"clawdline browser install-host --mode user_chrome --extension-id " + "a".repeat(32)})
    if (path === "/v1/browser/setup") return json(res,200,{mode:url.searchParams.get("browser_mode") || settings.browser_mode,chromium_found:true,connected:true})
    if (path === "/v1/browser/launch" && req.method==="POST") {launches++;return json(res,200,{ok:true})}
    if (path === "/v1/browser/settings") {
      if (req.method === "POST") {
        const b = await body(req)
        for (const k of Object.keys(b)) if (k !== "browser_policy" && k !== "browser_notify_new_site") return json(res, 400, { error: "bad_request", detail: "Not a browser setting key: " + k })
        settings = { ...settings, ...b }
      }
      return json(res, 200, settings)
    }
    if (req.method === "POST" && path === "/v1/browser/stop") {
      const b = await body(req); stopped = !!b.stopped; stateError = saveFailed
      if (saveFailed) return json(res,503,{error:"browser_state_write_failed",detail:"The browser control state could not be saved. Keep Clawdline running and retry before restarting. Browser use remains paused."})
      return json(res,200,{ok:true,stopped})
    }
    if (path === "/v1/browser/audit") {
      const q = Object.fromEntries(url.searchParams)
      queries.push(q)
      const since = Number(q.since || 0)
      const rows = empty ? [] : allRows().filter((r) => (!q.conversation || r.conversation === q.conversation) && (!q.site || r.site === q.site) && r.at >= since)
      return json(res, 200, { ok: true, rows, pruned: empty ? 0 : 3, journal_error: empty ? "" : "The audit could not be written to disk; recent rows are kept only until the daemon restarts." })
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
  profile = mkdtempSync(join(tmpdir(), "clawdline-browser-settings-"))
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

const WORDS = {
  "zh-Hant": { connected: "Chrome 擴充功能已連線", host: "Chrome 131 · 擴充功能 0.1.0", password: "私密輸入 · 8 個字", cut: "（已截斷）",
    pruned: "較舊的 3 筆紀錄已刪除", journal: "紀錄無法寫入磁碟", empty: "還沒有任何瀏覽器操作紀錄", stop: "停止 Clawdline 瀏覽器操作",
    resume: "恢復", click: "點擊", refused: "未完成：瀏覽器操作已停止" },
  en: { connected: "Chrome extension connected", host: "Chrome 131 · extension 0.1.0", password: "Private input · 8 characters", cut: "(truncated)",
    pruned: "3 older records were deleted", journal: "could not be written to disk", empty: "No browser activity yet", stop: "Stop Clawdline browser actions",
    resume: "Resume", click: "Click", refused: "Not done: browser use was stopped" },
} as const

for (const [name, width, height, ui] of [["desktop", 1280, 900, "zh-Hant"], ["phone", 390, 844, "zh-Hant"], ["desktop", 1280, 900, "en"], ["phone", 390, 844, "en"]] as const) {
  test(`${name} ${ui}: the Browser block shows the connection, switches, stop and audit`, async () => {
    const w = WORDS[ui]
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
        if (Date.now() > deadline) assert.fail("did not reach " + expression + ": " + await run(`document.getElementById("settings-browser")?.innerText ?? "(no block)"`))
        await new Promise((r) => setTimeout(r, 50))
      }
    }
    const text = () => run(`document.getElementById("settings-browser").innerText`) as Promise<string>
    // The page scrolls inside its own container under a sticky tab bar, so a
    // shot scrolls its element to the top and then back past that bar.
    const shot = async (label: string, selector = "#settings-browser") => {
      if (!shots) return
      await run(`(() => { const el = document.querySelector(${JSON.stringify(selector)}); el.scrollIntoView({ block: "start" });
        let p = el.parentElement; while (p && !(p.scrollHeight > p.clientHeight && /auto|scroll/.test(getComputedStyle(p).overflowY))) p = p.parentElement;
        (p ?? document.scrollingElement).scrollBy(0, -150) })()`)
      await new Promise((r) => setTimeout(r, 100))
      const { data } = await browser.send("Page.captureScreenshot", { format: "png" }, id)
      writeFileSync(join(shots, `browser-settings-${label}-${ui}-${name}.png`), Buffer.from(data, "base64"))
    }
    const select = (sel: string, value: string) => run(`(() => { const s = document.querySelector(${JSON.stringify(sel)}); s.value = ${JSON.stringify(value)}; s.dispatchEvent(new Event("change", { bubbles: true })) })()`)
    const rowCount = () => run(`document.querySelectorAll("#settings-browser .browser-audit-row").length`) as Promise<number>
    try {
      await fetch(origin + "/__fixture/reset")
      await browser.send("Page.enable", {}, id); await browser.send("Runtime.enable", {}, id)
      await browser.send("Emulation.setDeviceMetricsOverride", { width, height, deviceScaleFactor: 2, mobile: width < 900 }, id)
      const mark = browser.loadCount; await browser.send("Page.navigate", { url: `${origin}/?ui=${ui}#page=settings` }, id); await browser.loaded(id, mark)
      await until(`!!document.getElementById("settings-tab-0")`)
      await run(`document.getElementById("settings-tab-0").click()`)
      await until(`!!document.querySelector("#settings-pane-0:not([hidden]) #settings-browser")`)
      await until(`document.querySelectorAll('.browser-policy-list > li').length === 3 && !document.getElementById('browser-policy-isolated').disabled`)
      await new Promise(resolve => setTimeout(resolve, 200))
      assert.equal((await (await fetch(origin + "/__fixture/log")).json()).queries.length,0,"Settings never reads activity before opening it")
      assert.equal(await run(`document.querySelector('.browser-audit-dialog') === null`),true)
      const order = () => run(`[...document.querySelectorAll('.browser-policy-list > li')].map(e => e.dataset.browser)`)
      assert.deepEqual(await order(),["native","isolated","user_chrome"])
      assert.equal(await run(`document.getElementById('browser-policy-user_chrome').disabled`),true,"unconfigured Chrome cannot be automatically enabled")
      assert.equal(await run(`document.querySelectorAll('.browser-policy-list button.chip, .browser-policy-setup').length`),0,"no arrow buttons or setup disclosures")
      assert.equal(await run(`!!document.querySelector('[data-browser="isolated"] .browser-policy-setup-link')`),false,"a connected browser offers no setup")
      await run(`document.querySelector('[data-browser="user_chrome"] .browser-policy-setup-link').click()`)
      await until(`document.querySelector('.browser-setup')?.innerText.includes('chrome://extensions')`)
      assert.ok(await run(`document.querySelector('.browser-setup input[readonly]').value.includes('prepare --mode user_chrome')`))
      await run(`document.getElementById('browser-prepare-extension').click()`)
      await until(`!!document.querySelector('.browser-prepared input')`)
      assert.equal(await run(`document.querySelector('.browser-prepared input').value`),"/test/Clawdline's data/chrome-extension-revision")
      assert.ok(await run(`[...document.querySelectorAll('.browser-setup input')].some(e => e.value.startsWith("open "))`))
      assert.ok(await run(`[...document.querySelectorAll('.browser-setup input')].some(e => e.value.includes("install-host --mode user_chrome --extension-id"))`))
      assert.ok(await run(`document.querySelector('.browser-setup').scrollWidth <= document.querySelector('.browser-setup').clientWidth`),"guide fits phone width")
      await shot("prepare-guide", ".browser-setup")
      await run(`document.querySelector('.browser-setup .setup-x').click()`)
      await until(`!document.querySelector('.browser-setup')`)
      let log = await (await fetch(origin + "/__fixture/log")).json()
      assert.equal(log.settings.browser_policy.automatic.user_chrome,false,"preparing files never enables Chrome")
      // Drag isolated's handle above native, as a pointer would.
      const handle = JSON.parse(await run(`JSON.stringify((r => ({ x: r.left + r.width / 2, y: r.top + r.height / 2 }))(document.querySelector('[data-browser="isolated"] .browser-policy-handle').getBoundingClientRect()))`))
      const above = JSON.parse(await run(`JSON.stringify((r => ({ y: r.top + 4 }))(document.querySelector('[data-browser="native"]').getBoundingClientRect()))`))
      await browser.send("Input.dispatchMouseEvent", { type: "mousePressed", x: handle.x, y: handle.y, button: "left", buttons: 1, clickCount: 1 }, id)
      for (const step of [0.5, 1]) await browser.send("Input.dispatchMouseEvent", { type: "mouseMoved", x: handle.x, y: handle.y + (above.y - handle.y) * step, button: "left", buttons: 1 }, id)
      assert.deepEqual(await order(),["isolated","native","user_chrome"],"rows reorder while dragging")
      assert.equal((await (await fetch(origin + "/__fixture/log")).json()).settings.browser_policy.order[0],"native","nothing is saved before release")
      await browser.send("Input.dispatchMouseEvent", { type: "mouseReleased", x: handle.x, y: above.y, button: "left", buttons: 0, clickCount: 1 }, id)
      await until(`document.querySelector('.browser-policy-list > li').dataset.browser === 'isolated' && !document.getElementById('browser-policy-isolated').disabled`)
      assert.deepEqual(await order(),["isolated","native","user_chrome"])
      await run(`document.getElementById('browser-policy-isolated').click()`)
      await until(`document.getElementById('browser-policy-isolated').getAttribute('aria-checked') === 'true' && !document.getElementById('browser-policy-native').disabled`)
      await run(`document.getElementById('browser-policy-native').click()`)
      await until(`document.getElementById('browser-policy-native').getAttribute('aria-checked') === 'false' && !document.getElementById('browser-policy-native').disabled`)
      log = await (await fetch(origin + "/__fixture/log")).json()
      assert.deepEqual(log.settings.browser_policy,{order:["isolated","native","user_chrome"],automatic:{native:false,isolated:true,user_chrome:false}})
      assert.equal(log.launches,0,"sorting and switches never launch or install Chrome")
      await fetch(origin + "/__fixture/ready")
      await until(`!document.getElementById('browser-policy-user_chrome').disabled`)
      await run(`document.getElementById('browser-policy-user_chrome').click()`)
      await until(`document.getElementById('browser-policy-user_chrome').getAttribute('aria-checked') === 'true' && !document.getElementById('browser-policy-isolated').disabled`)
      log = await (await fetch(origin + "/__fixture/log")).json()
      assert.deepEqual(log.settings.browser_policy,{order:["isolated","native","user_chrome"],automatic:{native:false,isolated:true,user_chrome:true}})
      await shot("policy")
      assert.equal(await run(`document.getElementById("browser-setting-browser_notify_new_site").getAttribute("aria-checked")`), "false")

      assert.equal((await (await fetch(origin + "/__fixture/log")).json()).queries.length,0,"mode/status reads never fetch activity")
      await run(`document.getElementById("browser-audit-open").focus(); document.getElementById("browser-audit-open").click()`)
      await until(`document.querySelectorAll("#settings-browser .browser-audit-row").length === 5`)
      assert.equal(await run(`document.querySelector('.browser-audit-dialog').matches(':modal')`),true)
      const all = await text()
      for (const want of [w.password, w.cut, w.pruned, w.journal, w.click, w.refused, "Browser Session", "abcdef12", "example.com", "/sign-in?next=/home", "github.com"]) {
        assert.ok(all.includes(want), `missing ${want}: ${all}`)
      }
      assert.ok(!all.includes(UNNAMED), "an unnamed Session shows only its first characters")
      assert.equal(await run(`[...document.querySelectorAll("#browser-audit-session option")].map((o) => o.textContent).join("|")`),
        (ui === "en" ? "All Sessions" : "所有 Session") + "|Browser Session|abcdef12")
      assert.ok(await run(`document.documentElement.scrollWidth <= innerWidth`), "no horizontal page scroll")
      // Every control in the block is at least 44 px tall.
      const small = await run(`[...document.querySelectorAll("#settings-browser button, #settings-browser select")].filter((e) => e.getClientRects().length && e.getBoundingClientRect().height < 44).map((e) => e.id || e.className)`)
      assert.deepEqual(small, [], "touch targets under 44 px")
      await shot("activity-modal", ".browser-audit-dialog")
      await shot("list", "#settings-browser .browser-audit-list")
      await shot("list-end", "#settings-browser .browser-audit-row:last-child")

      // Filters reach the route and narrow the list.
      await select("#browser-audit-session", NAMED)
      await until(`document.querySelectorAll("#settings-browser .browser-audit-row").length === 4`)
      await select("#browser-audit-session", "")
      await select("#browser-audit-site", "github.com")
      await until(`document.querySelectorAll("#settings-browser .browser-audit-row").length === 1`)
      await select("#browser-audit-range", "all")
      await until(`document.querySelectorAll("#settings-browser .browser-audit-row").length === 2`)
      await select("#browser-audit-site", "")
      await select("#browser-audit-range", "today")
      await until(`document.querySelectorAll("#settings-browser .browser-audit-row").length === 5`)
      log = await (await fetch(origin + "/__fixture/log")).json()
      const seen = log.queries as Record<string, string>[]
      assert.ok(seen.some((q) => q.conversation === NAMED), JSON.stringify(seen))
      assert.ok(seen.some((q) => q.site === "github.com" && !q.since), JSON.stringify(seen))
      assert.ok(seen.every((q) => q.limit === "200"), JSON.stringify(seen))
      assert.ok(await rowCount() === 5)

      await run(`document.getElementById("browser-audit-close").click()`)
      await until(`!document.querySelector('.browser-audit-dialog')`)
      assert.equal(await run(`document.activeElement.id`),"browser-audit-open")
      const countAfterClose=(await (await fetch(origin + "/__fixture/log")).json()).queries.length
      if (name==="phone" && ui==="en") {
        await new Promise(resolve=>setTimeout(resolve,15_200))
        assert.equal((await (await fetch(origin + "/__fixture/log")).json()).queries.length,countAfterClose,"closing stops audit polling")
      }

      // The emergency stop, its banner, and resuming.
      assert.ok((await text()).includes(w.stop))
      await run(`document.getElementById("browser-stop").click()`)
      await until(`!!document.querySelector("#settings-browser .browser-stopped")`)
      assert.equal((await (await fetch(origin + "/__fixture/log")).json()).stopped, true)
      assert.ok(await run(`document.documentElement.scrollWidth <= innerWidth`), "no horizontal page scroll while stopped")
      assert.ok(await run(`document.getElementById("browser-resume").getBoundingClientRect().height >= 44`))
      await shot("stopped")
      await run(`document.getElementById("browser-resume").click()`)
      await until(`!!document.getElementById("browser-stop") && !document.querySelector("#settings-browser .browser-stopped")`)
      assert.equal((await (await fetch(origin + "/__fixture/log")).json()).stopped, false)

      // A failed save is visibly distinct from a durable stop. Retrying the
      // same stop after storage recovers clears the warning before resume.
      await fetch(origin + "/__fixture/fail-stop")
      await run(`document.getElementById("browser-stop").click()`)
      await until(`!!document.getElementById("browser-stop-retry")`)
      assert.ok(await run(`document.getElementById("browser-stop-retry").getBoundingClientRect().height >= 44`))
      await shot("unsaved-stop")
      await fetch(origin + "/__fixture/recover-stop")
      await run(`document.getElementById("browser-stop-retry").click()`)
      await until(`!document.getElementById("browser-stop-retry") && !!document.getElementById("browser-resume")`)
      await run(`document.getElementById("browser-resume").click()`)
      await until(`!!document.getElementById("browser-stop")`)

      // An audit with nothing in it.
      await fetch(origin + "/__fixture/empty")
      const again = browser.loadCount; await browser.send("Page.reload", {}, id); await browser.loaded(id, again)
      await until(`!!document.getElementById("settings-tab-0")`)
      await run(`document.getElementById("settings-tab-0").click()`)
      await until(`!!document.getElementById("browser-audit-open")`)
      assert.equal(await run(`!!document.querySelector('.browser-audit-empty')`),false)
      await run(`document.getElementById("browser-audit-open").click()`)
      await until(`document.getElementById("settings-browser")?.innerText.includes(${JSON.stringify(w.empty)})`)
      await shot("empty")
    } finally {
      await browser.send("Target.closeTarget", { targetId }).catch(() => {})
    }
  })
}
