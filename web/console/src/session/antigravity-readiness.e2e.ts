// Built-console synthetic fixtures only; no daemon, vendor CLI, account or Cloud service.
// (cd web && npm run build)
// CLAWDLINE_SHOTS=<task artifacts> node --test web/console/src/session/antigravity-readiness.e2e.ts
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
let mode = "ready", language = "en", fixtureSequence = 0
let sends: string[] = [], inventoryReads: string[] = []
let nativeEffortOverride = false
let selectedModel = "synthetic-flash", selectedEffort = "high", modelWrites: string[] = [], fullInfoReads = 0
const streams = new Set<ServerResponse>()
function snapshot() {
  return { at: Date.now(), scan: { complete: true, completed: { complete: true, sequence: ++fixtureSequence }, emptyAuthoritative: true,
    epoch: 1, generation: fixtureSequence, source: { freshness: "current", observed_at: now(), provenance: "fixture" } },
    sessions: ["alpha", "beta"].map((machine, index) => ({ id: "fixture-" + machine, label: "Antigravity synthetic " + machine,
      machine, backend: mode.startsWith("owned-protocol") ? "owned" : "tmux", assistant: "antigravity", isClaude: true, identity: mode.startsWith("owned-protocol") ? "protocol" : "launch_hook", line: "synthetic menu revision", cwd: "/work/fixture", sessionId: "conversation-" + machine,
      execution_generation: (index ? "b" : "a").repeat(32), state: mode.includes("menu") ? "waiting" : "idle", work_state: "ready", evidence: "process",
      source: { freshness: (mode === "stale-state" || mode === "async-unknown") ? "stale" : "current", observed_at: now(), provenance: "fixture" },
      activity: { known: true, at: now() },
      ...(mode.includes("menu") ? { menu: { question: "Allow synthetic command?", options: [{ n: 1, label: "Allow once", can: true }],
        ...(mode === "unverified-menu" ? { source: "transcript" } : {}) } } : {}) })) }
}
function inventory(machine: string) {
  const facts = { id: "antigravity", label: "Gemini", installed: mode !== "missing", availability: "unknown",
    integrationState: "active", authState: mode === "required" || mode === "bootstrap" ? "required" : "ready",
    capabilities: { start: mode !== "required", resume: true, transcript: true, send: mode !== "no-send" && machine !== "beta", menus: true } }
  return { at: mode === "stale" ? 0 : now(), places: [{ id: "fixture-place", label: "Fixture", path: "/work/fixture", at: now() }],
    assistants: mode === "old" ? [{ id: "claude", label: "Claude" }, { id: "codex", label: "Codex" }] : [facts] }
}
function workItem() { return { id: "fixture-work", project: { id: "fixture-place", label: "Fixture", path: "/work/fixture", available: true },
  kind: "feature", title: "Synthetic Antigravity task", description: "Verify provider selection.", acceptance_criteria: "", acceptance_version: 1,
  acceptance_digest: "", gate_snapshot_cycle: 0, gate_snapshot_at: null, planning_gate: false, verify_gate: false, phase: "created", condition: null,
  user_action: "", area: "unassigned", deployment_policy: "agent_decides", review_required: false, owner_session: null, created_at: now(), updated_at: now(),
  closed_at: null, cycle: 1, version: 1, documents: [], images: [], steps: [] } }
function json(res: ServerResponse, status: number, body: unknown) { res.writeHead(status, { "content-type": "application/json" }); res.end(JSON.stringify(body)) }
const TYPES: Record<string, string> = { ".js": "text/javascript", ".css": "text/css", ".json": "application/json", ".png": "image/png", ".svg": "image/svg+xml", ".ico": "image/x-icon", ".webmanifest": "application/manifest+json" }
function document(file = "index.html"): string {
  const html = readFileSync(join(dist, file), "utf8")
  const words = language === "en" ? {} : JSON.parse(readFileSync(join(dist, "strings", language + ".json"), "utf8")); words.lang = language; words.dir = "ltr"
  return html.replace("<!-- clawdline:strings -->", "<script>localStorage.setItem('ui_language'," + JSON.stringify(language) + ");window.__strings=" + JSON.stringify(words).replaceAll("</", "<\\/") + "</script>").replace("<!-- clawdline:cloud -->", "")
}
function daemon(): Server {
 return createServer((req, res) => {
  const url = new URL(req.url ?? "/", "http://fixture"), path = url.pathname
  if (path === "/fixture") { mode = url.searchParams.get("mode") || "ready"; for (const stream of streams) stream.write("event: sessions\ndata: " + JSON.stringify(snapshot()) + "\n\n"); return json(res, 200, {}) }
  if (path === "/v1/sessions") return json(res, 200, snapshot())
  if (path === "/v1/usage/items/fixture-work" || path === "/v1/usage/sessions/conversation-alpha" || path === "/v1/usage/sessions/conversation-beta") return json(res, 503, { error: { code: "fixture_unavailable", message: "Synthetic usage is unavailable" } })
  if (path === "/v1/work/v2/items") return json(res, 200, { ok: true, rows: [workItem()], counts: { open: 1 }, next_cursor: null, page_size: 1, truncated: false })
  if (path === "/v1/work/v2/items/fixture-work") return json(res, 200, { ok: true, item: workItem() })
  if (path === "/v1/places") { const machine = url.searchParams.get("machine") || "alpha"; inventoryReads.push(machine)
    if (mode === "owned-protocol-refresh") { setTimeout(() => json(res,200,inventory(machine)),1800); return }
    return json(res, mode === "error" ? 503 : 200, inventory(machine)) }
  if (path === "/v1/strings") return json(res, 200, JSON.parse(readFileSync(join(dist, "catalogs", language + ".json"), "utf8")))
  if (path === "/v1/personas") return json(res, 200, { personas: [] })
  if (path === "/v1/orchestrator/tasks") return json(res, 200, { tasks: [], at: now() })
  if (path === "/v1/sessions/restorable") return json(res, 200, { available: false, reason: "boot_unknown", sessions: [], at: now() })
  if (path === "/v1/events") { res.writeHead(200, { "content-type": "text/event-stream", "cache-control": "no-cache" }); streams.add(res)
    const refresh = mode === "owned-protocol-refresh" ? setInterval(() => res.write("event: sessions\ndata: " + JSON.stringify(snapshot()) + "\n\n"),250) : undefined
    req.on("close", () => { streams.delete(res); clearInterval(refresh) }); res.write("event: sessions\ndata: " + JSON.stringify(snapshot()) + "\n\n"); return }
  if (req.method === "POST" && /\/(send|key)$/.test(path)) { sends.push(path)
    if (mode === "owned-protocol-models") {
      let body=""; req.on("data",chunk=>body+=String(chunk)); req.on("end",()=>{
        const text=JSON.parse(body).text
        modelWrites.push(text)
        if (text.startsWith("/model ")) { selectedModel=text.slice(7); selectedEffort="high" }
        if (text.startsWith("/effort ")) selectedEffort=nativeEffortOverride?"high":text.slice(8)
        json(res,200,{ok:true})
      }); return
    }
    return json(res, 200, { ok: true }) }
  if (path.includes("transcript") && mode.startsWith("async-")) return json(res, 200, { id: path.split("/")[3], entries: [{ role: "tool", tool: "request_user_input_async", text: "\u0001ask\u0001" + JSON.stringify([{ h: "Synthetic choice", q: "Choose the synthetic direction", o: [{ l: "Continue fixture", d: "Only a synthetic reply" }] }]), at: now() }], more: false })
  if (path.endsWith("/info") && mode === "owned-protocol-models") {
    if (url.searchParams.get("parts") !== "summary") fullInfoReads++
    return json(res,200,{info:{session:{id:path.split("/")[3],assistant:"antigravity",model:selectedModel,effort:selectedEffort,sessionId:"conversation-alpha"},
      models:[{id:"synthetic-flash",command:"synthetic-flash",name:"Fixture Flash"},{id:"synthetic-pro",command:"synthetic-pro",name:"Fixture Pro"}],
      efforts:selectedModel==="synthetic-flash"?["low","medium","high"]:["low","high"],
      limits:{windows:[{name:"7d",usedPercent:38,hit:false,resetsAt:now()+3600}],at:now(),ageSeconds:0,freshForSeconds:null,readAtMs:Date.now()},
      context:{usedPercent:12,usedTokens:12,windowTokens:100}}})
  }
  if (path.endsWith("/info") && mode.startsWith("owned-protocol")) return json(res,200,{info:{session:{id:path.split("/")[3],assistant:"antigravity",model:"synthetic-flash-high",sessionId:"conversation-alpha"},models:[],context:{usedPercent:12,usedTokens:12,windowTokens:100}}})
  if (path.includes("transcript") && mode.startsWith("owned-protocol")) return json(res, 200, { id: path.split("/")[3], evidence: "structured", signature: "synthetic-owned:1", entries: [
    {role:"user",text:"Synthetic input",at:now()},
    {role:"tool",tool:"read",text:"Read synthetic file",at:now(),activity:{kind:"called",title:"Read synthetic file",status:"completed",durationMs:-1,actions:[{kind:"read",path:"/work/fixture/sample.txt"}]}},
    {role:"assistant",text:"Synthetic owned protocol reply.",at:now()}] })
  if (path.includes("transcript")) return json(res, 200, { id: path.split("/")[3], entries: [{ role: "assistant", text: "Synthetic Antigravity reply.", at: now() }], more: false })
  if (path.startsWith("/v1/")) return json(res, 200, { rows: [], direct_todos: [], assigned_items: [], recent_items: [], open_decisions: [], entries: [], tasks: [], todos: [], skills: [], items: [], sessions: [], at: now(), counts: {} })
  if (path === "/" || path === "/bar.html") { res.writeHead(200, { "content-type": "text/html; charset=utf-8" }); return res.end(document(path === "/" ? "index.html" : "bar.html")) }
  const file = normalize(join(dist, path)); if (!file.startsWith(dist + "/") || !existsSync(file) || !statSync(file).isFile()) return json(res, 404, {})
  res.writeHead(200, { "content-type": TYPES[extname(file)] ?? "application/octet-stream" }); res.end(readFileSync(file))
 })
}
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
      if (msg.method === "Runtime.exceptionThrown") console.error("BROWSER", JSON.stringify(msg.params))
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
      const timer = setTimeout(() => (this.pending.delete(id), reject(new Error(method + " had no answer"))), 30_000)
      this.pending.set(id, {
        resolve: (v) => (clearTimeout(timer), resolve(v)),
        reject: (e) => (clearTimeout(timer), reject(e)),
      })
    })
  }

  async loaded(sessionId: string, mark: number): Promise<void> {
    const deadline = Date.now() + 20_000
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

interface Seen { send: boolean; message: boolean; text: string; wide: number; logos: number; options: number }
const PROBE = `(() => ({ send: !!document.querySelector('#send') && !document.querySelector('#send').disabled,
  editor: document.querySelector('#msg')?.outerHTML, why: document.querySelector('#why')?.textContent, message: !!document.querySelector('#msg'), text: document.body.innerText, wide: document.documentElement.scrollWidth,
  logos: document.querySelectorAll('.assistant-logo').length, options: document.querySelectorAll('#waiting .opt:not(:disabled)').length }))()`

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

  async draft(text: string): Promise<void> {
    await this.until("editor accepts input", s => String((s as any).editor).includes('contenteditable="plaintext-only"') || String((s as any).editor).includes('contenteditable="true"'))
    // A fresh isolated profile opens the real first-run wizard. Dismiss it
    // through its public close control before typing into the page behind it.
    await this.run(`document.querySelector('.setup-dialog[open] .setup-x')?.click()`)
    await this.run(`document.querySelector('#msg').focus()`)
    await this.b.send("Input.insertText", { text }, this.session)
  }
  async type(text: string): Promise<void> { await this.b.send("Input.insertText", { text }, this.session) }
  async enter(): Promise<void> {
    await this.b.send("Input.dispatchKeyEvent", { type: "keyDown", key: "Enter", code: "Enter", windowsVirtualKeyCode: 13 }, this.session)
    await this.b.send("Input.dispatchKeyEvent", { type: "keyUp", key: "Enter", code: "Enter", windowsVirtualKeyCode: 13 }, this.session)
  }
  async theme(value: string): Promise<void> {
    await this.b.send("Emulation.setEmulatedMedia", { features: [{ name: "prefers-color-scheme", value }] }, this.session)
  }
  async tabKey(): Promise<void> {
    await this.b.send("Input.dispatchKeyEvent", { type: "keyDown", key: "Tab", code: "Tab", windowsVirtualKeyCode: 9 }, this.session)
    await this.b.send("Input.dispatchKeyEvent", { type: "keyUp", key: "Tab", code: "Tab", windowsVirtualKeyCode: 9 }, this.session)
  }
  async go(address: string): Promise<void> {
    for (const stream of streams) stream.end()
    streams.clear()
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

  async until(what: string, ok: (s: Seen) => boolean, ms = 12_000): Promise<Seen> {
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
    return this.run(`document.querySelector(${JSON.stringify(selector)}).click()`)
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
  profile = mkdtempSync(join(tmpdir(), "clawdline-antigravity-readiness-"))
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
  server?.closeAllConnections()
  await new Promise<void>((ok) => (server ? server.close(() => ok()) : ok()))
  if (profile) rmSync(profile, { recursive: true, force: true, maxRetries: 5 })
})


for (const [label, width, height, mobile] of [["phone", 390, 844, true], ["desktop", 1280, 800, false]] as const) {
 for (const theme of ["light", "dark"]) for (const lang of ["en", "zh-Hant"]) {
  test(`${label} ${theme} ${lang}: readiness, brand, keyboard and overflow`, { timeout: 90_000 }, async () => {
   mode = "ready"; language = lang; sends = []; inventoryReads = []
   const tab = await Tab.open(browser, width, height, mobile)
   try {
    await tab.theme(theme); await tab.go("/#session=fixture-alpha")
    await tab.until("composer is present", s => s.message && !s.text.includes("Checking Gemini"))
    await tab.draft("Synthetic draft")
    let seen = await tab.until("ready Antigravity can send", s => s.send)
    assert.ok(seen.logos > 0); assert.ok(seen.wide <= width); assert.ok(inventoryReads.includes("alpha"))
    await tab.run(`document.querySelector('#detail-info').focus()`); await tab.tabKey(); assert.ok(await tab.run(`document.activeElement !== document.body`))
    await tab.shot(`antigravity-${label}-${theme}-${lang}-ready`)
    for (const blocked of ["missing", "required", "no-send", "old", "stale", "error", "stale-state", "unverified-menu"]) {
     mode = blocked
     // A new selection forces an independently owned inventory read even within the same second.
     await tab.go("/?case=" + blocked + "#session=fixture-alpha")
     await tab.until(blocked + " keeps send disabled", s => s.message && !s.send)
     await tab.run(`document.querySelector('#msg').textContent='Synthetic draft';document.querySelector('#msg').dispatchEvent(new InputEvent('input',{bubbles:true}))`)
     seen = await tab.until(blocked + " keeps the draft blocked", s => !s.send)
     assert.ok(seen.wide <= width); if (blocked.includes("menu")) assert.equal(seen.options, 0)
     if (["required", "error", "unverified-menu"].includes(blocked)) await tab.shot(`antigravity-${label}-${theme}-${lang}-${blocked}`)
    }
    assert.equal(sends.length, 0)
   } catch (error) { console.error(error); throw error } finally { await tab.close() }
  })
 }
}

test("Bar checks exact selected machine capabilities and keeps slash commands as drafts", { timeout: 60_000 }, async () => {
 mode = "ready"; language = "en"; sends = []; inventoryReads = []
 const tab = await Tab.open(browser, 720, 600, false)
 try {
  await tab.go("/bar.html")
  await tab.run(`(async()=>{while(!document.querySelector('.bar-text'))await new Promise(r=>setTimeout(r,20))})()`)
  await tab.run(`(async()=>{while(!document.querySelector('.assistant-logo'))await new Promise(r=>setTimeout(r,20))})()`)
  await tab.shot("antigravity-bar-brand")
  assert.ok(await tab.run(`document.querySelector('.bar-text').placeholder.includes('Gemini')`))
  assert.ok(await tab.run(`document.querySelectorAll('.assistant-logo').length > 0`))
  await tab.run(`document.querySelector('.bar-text').focus()`); await tab.type("/model"); await tab.enter()
  assert.equal(sends.length, 0)
  await tab.run(`document.querySelector('.bar-text').focus();document.querySelector('.bar-text').select()`)
  await tab.type("Synthetic bar message")
  await tab.run(`new Promise(r=>setTimeout(r,100))`); await tab.enter()
  await tab.run(`new Promise(r=>setTimeout(r,100))`)
  assert.deepEqual(sends, ["/v1/sessions/fixture-alpha/send"])
  await tab.go("/bar.html?beta=1")
  await tab.run(`(async()=>{while(document.querySelectorAll('.bar-row').length < 2)await new Promise(r=>setTimeout(r,20))})()`)
  await tab.run(`document.querySelectorAll('.bar-row')[1].click()`)
  await tab.run(`new Promise(r=>setTimeout(r,100))`)
  assert.ok(inventoryReads.includes("beta"))
  await tab.run(`document.querySelector('.bar-text').focus()`); await tab.type("Synthetic beta message"); await tab.enter()
  assert.equal(sends.length, 1)
 } catch (error) { console.error(error); throw error } finally { await tab.close() }
})

for (const width of [390, 1280]) test(`Work picker ${width}px: preview stays hidden without administrator access`, { timeout: 60_000 }, async () => {
 language = "zh-Hant"
 const tab = await Tab.open(browser, width, 900, width === 390)
 try {
  for (const state of ["required", "bootstrap", "old"]) {
   mode = state
   await tab.go("/?workcase=" + state + "#page=work")
   await tab.until("work card is visible", s => s.text.includes("Synthetic Antigravity task"))
   await tab.run(`document.querySelector('.setup-dialog[open] .setup-x')?.click()`)
   await tab.click(".work-card-summary")
   await tab.run(`(async()=>{while(!document.querySelector('.work-new-session [role="radio"]'))await new Promise(r=>setTimeout(r,20))})()`)
   assert.equal(await tab.run(`document.querySelector('.work-new-session [aria-label="Gemini"]')`), null)
   assert.ok(await tab.run(`document.querySelectorAll('.work-new-session [role="radio"]').length >= 2`))
   await tab.shot(`antigravity-work-${width}-${state}`)
   assert.ok((await tab.run(PROBE)).wide <= width)
  }
 } catch(error) { console.error(error); throw error } finally { await tab.close() }
})

test("Detail switches machines, invalidates on disconnect, and accepts only the ready synthetic send", { timeout: 60_000 }, async () => {
 mode = "ready"; language = "en"; sends = []; inventoryReads = []
 const tab = await Tab.open(browser, 390, 844, true)
 try {
  await tab.go("/?switch=alpha#session=fixture-alpha"); await tab.draft("Synthetic message")
  await tab.until("alpha can send", s => s.send)
  await tab.run(`window.dispatchEvent(new Event('offline'))`)
  await tab.until("disconnect disables the send", s => !s.send)
  await tab.shot("antigravity-disconnected")
  await tab.run(`window.dispatchEvent(new Event('online'))`)
  await tab.until("reconnect rechecks the machine", s => s.send)
  await tab.click("#send")
  await tab.run(`new Promise(r=>setTimeout(r,100))`)
  assert.deepEqual(sends, ["/v1/sessions/fixture-alpha/send"])
  await tab.go("/?switch=beta#session=fixture-beta")
  await tab.until("beta remains unavailable", s => s.message && !s.send && inventoryReads.includes("beta"))
  assert.ok(inventoryReads.includes("beta")); assert.equal(sends.length, 1)
  await tab.shot("antigravity-machine-beta-disabled")
 } catch(error) { console.error(error); throw error } finally { await tab.close() }
})

test("Async questions require the exact current idle execution; verified menus use keys", { timeout: 60_000 }, async () => {
 language = "en"; sends = []
 const tab = await Tab.open(browser, 390, 844, true)
 try {
  for (const state of ["async-ready", "async-menu", "async-unknown"]) {
   mode = state; await tab.go("/?async=" + state + "#session=fixture-alpha")
   await tab.run(`(async()=>{while(!document.querySelector('.async-question-option'))await new Promise(r=>setTimeout(r,20))})()`)
   await tab.run(`new Promise(r=>setTimeout(r,100))`)
   assert.equal(await tab.run(`document.querySelector('.async-question-option').disabled`), state !== "async-ready")
   if (state === "async-ready") { await tab.click(".async-question-option"); await tab.run(`new Promise(r=>setTimeout(r,100))`) }
   await tab.shot("antigravity-" + state)
  }
  assert.deepEqual(sends, ["/v1/sessions/fixture-alpha/send"])
  mode = "menu"; await tab.go("/?verifiedmenu=1#session=fixture-alpha")
  await tab.until("verified menu offers keys", s => s.options > 0)
  await tab.shot("antigravity-verified-menu")
  await tab.click("#waiting .opt")
  await tab.run(`new Promise(r=>setTimeout(r,100))`)
  assert.deepEqual(sends, ["/v1/sessions/fixture-alpha/send", "/v1/sessions/fixture-alpha/key"])
 } catch(error) { console.error(error); throw error } finally { await tab.close() }
})


test("Owned protocol phone: fresh identity sends once and renders the normalized reply", { timeout: 60_000 }, async () => {
 mode="owned-protocol";language="zh-Hant";sends=[];inventoryReads=[]
 const tab=await Tab.open(browser,390,844,true)
 try {
  await tab.go("/?owned=1#session=fixture-alpha")
  await tab.draft("Synthetic phone input")
  const seen=await tab.until("owned protocol composer can send",s=>s.send && s.text.includes("Synthetic owned protocol reply.") && s.text.includes("synthetic-flash-high"))
  assert.ok(seen.wide<=390)
  assert.ok(seen.logos>0)
  assert.ok(seen.text.includes("Read synthetic file"))
  assert.equal(await tab.run(`document.querySelector('#tx-focus').hidden`),true)
  assert.ok(await tab.run(`document.querySelector('#status-line').innerText.includes('12%')`))
  await tab.shot("antigravity-owned-phone-zh-Hant")
  await tab.click("#send")
  await tab.run(`new Promise(r=>setTimeout(r,100))`)
  assert.deepEqual(sends,["/v1/sessions/fixture-alpha/send"])
 } catch(error) {await tab.shot("antigravity-owned-phone-failure");throw error} finally {await tab.close()}
})


test("Owned observations do not cancel a slower readiness read", {timeout:30000}, async()=>{
 mode="owned-protocol-refresh";language="en";inventoryReads=[];sends=[]
 const tab=await Tab.open(browser,390,844,true)
 try {
  await tab.go("/?observation-refresh=1#session=fixture-alpha")
  await tab.until("fresh owned execution becomes editable while observations continue", s=>s.message && String((s as any).editor).includes('contenteditable="plaintext-only"') && s.text.includes("synthetic-flash-high"),7000)
  await tab.draft("One observed execution")
  await tab.until("slower readiness response enables exactly this execution",s=>s.send,7000)
  await tab.click("#send")
  await tab.run(`new Promise(r=>setTimeout(r,100))`)
  assert.deepEqual(sends,["/v1/sessions/fixture-alpha/send"])
  await tab.shot("gemini-owned-continuing-observation")
 }finally{await tab.close()}
})


for (const width of [390,1280]) for (const lang of ["en","zh-Hant"]) {
 test(`Gemini Info ${width}px ${lang}: base model and native effort switch separately`,{timeout:30000},async()=>{
  mode="owned-protocol-models";nativeEffortOverride=false;language=lang;selectedModel="synthetic-flash";selectedEffort="high";modelWrites=[];fullInfoReads=0
  const tab=await Tab.open(browser,width,900,width===390)
  try {
   await tab.go("/?native-models=1#session=fixture-alpha")
   await tab.until("native conversation is visible",s=>s.text.includes("Fixture Flash"))
   await tab.run(`document.querySelector('.setup-dialog[open] .setup-x')?.click()`)
   await tab.click("#detail-info")
   await tab.until("base model selector is present",s=>s.text.includes("Fixture Pro"))
   assert.equal(await tab.run(`document.querySelectorAll('#info [data-model]').length`),2)
   assert.equal(await tab.run(`document.querySelector('#info [data-model="synthetic-flash"]').getAttribute('aria-current')`),"true")
   assert.deepEqual(await tab.run(`Array.from(document.querySelectorAll('#info [data-effort]'),e=>e.dataset.effort)`),["low","medium","high"])
   assert.equal(await tab.run(`document.querySelector('#info [data-effort="high"]').getAttribute('aria-current')`),"true")
   assert.equal(await tab.run(`document.querySelector('#info .effort-scale').getAttribute('aria-valuemax')`),"3")
   assert.equal(await tab.run(`document.querySelector('#info .other')`),null)
   assert.equal(await tab.run(`document.querySelector('#info .assistant-name').textContent`),"Gemini")
   const quota=await tab.run(`document.querySelector('#info-body').innerText`)
   assert.ok(quota.includes(lang === "en" ? "38% used" : "已用 38%") && quota.includes("12%"))
   const reads=fullInfoReads
   await tab.click('#info [data-effort="low"]')
   await tab.run(`(async()=>{const end=Date.now()+5000;while(document.querySelector('#info [data-effort="low"]')?.getAttribute('aria-current')!=='true'){if(Date.now()>end)throw Error('effort selection did not settle');await new Promise(r=>setTimeout(r,20))}})()`)
   assert.deepEqual(modelWrites,["/effort low"])
   assert.equal(selectedModel,"synthetic-flash","effort does not become a different base model")
   assert.equal(await tab.run(`document.querySelector('#info [data-model="synthetic-flash"]').getAttribute('aria-current')`),"true")
   assert.equal(fullInfoReads,reads,"effort acknowledgement must not repeat quota reads")
   await tab.click('#info [data-model="synthetic-pro"]')
   await tab.run(`(async()=>{const end=Date.now()+5000;while(document.querySelector('#info [data-model="synthetic-pro"]')?.getAttribute('aria-current')!=='true'){if(Date.now()>end)throw Error('model selection did not settle');await new Promise(r=>setTimeout(r,20))}})()`)
   assert.deepEqual(modelWrites,["/effort low","/model synthetic-pro"])
   assert.deepEqual(await tab.run(`Array.from(document.querySelectorAll('#info [data-effort]'),e=>e.dataset.effort)`),["low","high"])
   assert.equal(await tab.run(`document.querySelector('#info [data-effort="high"]').getAttribute('aria-current')`),"true")
   assert.equal(fullInfoReads,reads,"model acknowledgement must not repeat quota reads")
   assert.ok((await tab.run(PROBE)).wide<=width)
   await tab.shot(`gemini-model-effort-${width}-${lang}`)
  }finally{await tab.close()}
 })
}


test("Gemini effort confirmation follows native state rather than the requested value",{timeout:30000},async()=>{
 mode="owned-protocol-models";nativeEffortOverride=true;language="en";selectedModel="synthetic-flash";selectedEffort="high";modelWrites=[]
 const tab=await Tab.open(browser,390,900,true)
 try {
  await tab.go("/?native-effort-override=1#session=fixture-alpha")
  await tab.until("native model is visible",s=>s.text.includes("Fixture Flash"))
  await tab.run(`document.querySelector('.setup-dialog[open] .setup-x')?.click()`)
  await tab.click("#detail-info")
  await tab.until("native effort is visible",s=>s.text.includes("Fixture Pro"))
  await tab.click('#info [data-effort="low"]')
  await tab.until("native effort confirmation is visible",s=>s.text.includes("Next turn reasoning effort: High"))
  assert.deepEqual(modelWrites,["/effort low"])
  assert.equal(await tab.run(`document.querySelector('#info [data-effort="high"]').getAttribute('aria-current')`),"true")
  assert.equal(await tab.run(`document.querySelector('#info-said').textContent`),"Next turn reasoning effort: High")
 }finally{nativeEffortOverride=false;await tab.close()}
})
