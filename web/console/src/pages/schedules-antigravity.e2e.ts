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
let mode = "ready", language = "en"
let sends: string[] = [], inventoryReads: string[] = []
let defaults: Record<string,unknown> = {}, writes: any[] = []
const scheduleID = "11111111-1111-4111-8111-111111111111"
const record = () => ({ id:scheduleID, file:scheduleID+".json",title:"Synthetic native schedule", enabled:true, close_tab:"on_success",catch_up_hours:6,notify_on_failure:true,runs:[],webhook_binding_availability:"unbound",when:{trigger_only:true},trigger_only:true,task:{assistant:"antigravity",project_dir:"/work/fixture",instructions:"Synthetic instructions",model:"native-synthetic",reasoning_effort:mode==="unknown-effort"?"future-native":"max"}})
const defaultSnapshot = () => ({codex_default_model:null,claude_default_model:null,codex_default_effort:null,antigravity_default_model:"native-synthetic",antigravity_default_effort:"max",models:{codex:[],claude:[],antigravity:[]},...defaults})
const streams = new Set<ServerResponse>()
function snapshot() {
  return { at: Date.now(), scan: { complete: true, completed: { complete: true, sequence: 1 }, emptyAuthoritative: true,
    epoch: 1, generation: 1, source: { freshness: "current", observed_at: now(), provenance: "fixture" } },
    sessions: ["alpha", "beta"].map((machine, index) => ({ id: "fixture-" + machine, label: "Antigravity synthetic " + machine,
      machine, backend: "tmux", assistant: "antigravity", isClaude: true, identity: "launch_hook", line: "synthetic menu revision", cwd: "/work/fixture", sessionId: "conversation-" + machine,
      execution_generation: (index ? "b" : "a").repeat(32), state: mode.includes("menu") ? "waiting" : "idle", work_state: "ready", evidence: "process",
      source: { freshness: (mode === "stale-state" || mode === "async-unknown") ? "stale" : "current", observed_at: now(), provenance: "fixture" },
      activity: { known: true, at: now() },
      ...(mode.includes("menu") ? { menu: { question: "Allow synthetic command?", options: [{ n: 1, label: "Allow once", can: true }],
        ...(mode === "unverified-menu" ? { source: "transcript" } : {}) } } : {}) })) }
}
function inventory(machine: string) {
  const facts = { id: "antigravity", label: "Antigravity", installed: mode !== "missing", availability: "unknown",
    integrationState: "active", authState: mode === "required" || mode === "bootstrap" ? "required" : "ready",
    capabilities: { start: mode !== "required", resume: true, transcript: true, send: mode !== "no-send" && machine !== "beta", menus: true } }
  return { at: mode === "stale" ? 0 : now(), places: [{ id: "fixture-place", label: "Fixture", path: "/work/fixture", at: now() }],
    assistants: mode === "old" ? [{ id: "claude", label: "Claude" }, { id: "codex", label: "Codex" }] : [facts, {id:"claude",label:"Claude"}, {id:"codex",label:"Codex"}] }
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
  if (path === "/v1/capacity") return json(res,503,{error:{code:"fixture_unavailable",message:"Synthetic capacity unavailable"}})
  if (path === "/v1/settings/default-models") {
   if(mode === "settings-error") return json(res,503,{error:{code:"fixture_unavailable",message:"Synthetic settings unavailable"}})
   if(req.method === "POST" && mode === "settings-write-error") return json(res,503,{error:"fixture_unavailable",detail:"Synthetic settings write unavailable"})
   if(req.method === "POST") {let body="";req.on("data",chunk=>body+=chunk);req.on("end",()=>{ const change=JSON.parse(body);writes.push(change);defaults={...defaults,...change};json(res,200,defaultSnapshot())});return}
   if(mode === "settings-loading") {setTimeout(()=>json(res,200,defaultSnapshot()),300);return}
   return json(res,200,defaultSnapshot())
  }
  if (path === "/v1/auth/status") return json(res,200,{authenticated:true,canSend:true,local:true,capabilities:["read","send","admin"]})
  if (path === "/v1/orchestrator/schedules") return json(res,200,{at:now(),schedules:[{...record(),assistant:"antigravity",project_dir:"/work/fixture",at:"",days:[]}]})
  if(path === "/v1/orchestrator/schedules/"+scheduleID){
    if(req.method === "PATCH"){let body="";req.on("data",chunk=>body+=chunk);req.on("end",()=>{writes.push(JSON.parse(body));json(res,200,{ok:true,schedule:{id:scheduleID,title:"Synthetic native schedule",enabled:true}})});return}
    return json(res,200,{schedule:record()})
  }
  if (path === "/v1/sessions") return json(res, 200, snapshot())
  if (path === "/v1/usage/items/fixture-work" || path === "/v1/usage/sessions/conversation-alpha" || path === "/v1/usage/sessions/conversation-beta") return json(res, 503, { error: { code: "fixture_unavailable", message: "Synthetic usage is unavailable" } })
  if (path === "/v1/work/v2/items") return json(res, 200, { ok: true, rows: [workItem()], counts: { open: 1 }, next_cursor: null, page_size: 1, truncated: false })
  if (path === "/v1/work/v2/items/fixture-work") return json(res, 200, { ok: true, item: workItem() })
  if (path === "/v1/places") { const machine = url.searchParams.get("machine") || "alpha"; inventoryReads.push(machine); return json(res, mode === "error" ? 503 : 200, inventory(machine)) }
  if (path === "/v1/strings") return json(res, 200, JSON.parse(readFileSync(join(dist, "catalogs", language + ".json"), "utf8")))
  if (path === "/v1/personas") return json(res, 200, { personas: [] })
  if (path === "/v1/orchestrator/tasks") return json(res, 200, { tasks: [], at: now() })
  if (path === "/v1/sessions/restorable") return json(res, 200, { available: false, reason: "boot_unknown", sessions: [], at: now() })
  if (path === "/v1/events") { res.writeHead(200, { "content-type": "text/event-stream", "cache-control": "no-cache" }); streams.add(res); req.on("close", () => streams.delete(res)); res.write("event: sessions\ndata: " + JSON.stringify(snapshot()) + "\n\n"); return }
  if (req.method === "POST" && /\/(send|key)$/.test(path)) { sends.push(path); return json(res, 200, { ok: true }) }
  if (path.includes("transcript") && mode.startsWith("async-")) return json(res, 200, { id: path.split("/")[3], entries: [{ role: "tool", tool: "request_user_input_async", text: "\u0001ask\u0001" + JSON.stringify([{ h: "Synthetic choice", q: "Choose the synthetic direction", o: [{ l: "Continue fixture", d: "Only a synthetic reply" }] }]), at: now() }], more: false })
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

type Seen = any
const PROBE = `(() => ({text:document.body.innerText,wide:document.documentElement.scrollWidth,
 settings:document.querySelector('#settings-antigravity-default-model')?.textContent,
 model:document.querySelector('#schedule-model')?.textContent,
 form:!!document.querySelector('#schedule-form:not([hidden])'),
 canSave:!!document.querySelector('#schedule-go')&&!document.querySelector('#schedule-go').disabled,
 native:document.querySelector('#schedule-model')?.dataset.kind,
 edit:!!document.querySelector('#schedule-history-edit')&&!document.querySelector('#schedule-history-edit').disabled,
 effort:document.querySelector('#settings-antigravity-default-effort')?.textContent,
 settingsSaid:document.querySelector('#settings-default-models .said')?.textContent,
 retryDisabled:document.querySelector('#settings-default-models button.chip')?.disabled,
 settingsDisabled:document.querySelector('#settings-antigravity-default-effort')?.disabled}))()`

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
  profile = mkdtempSync(join(process.env.CLAWDLINE_WORK || tmpdir(), "clawdline-native-settings-"))
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

for (const [label,width,height,mobile] of [["desktop",1280,800,false],["phone",390,844,true]] as const) for(const lang of ["en","zh-Hant"]){
 test(`${label} ${lang}: actual settings save/readback and schedule native preservation`,{timeout:90000},async()=>{
  mode="ready";language=lang;defaults={};writes=[]
  const tab=await Tab.open(browser,width,height,mobile)
  try{
   await tab.go("/#page=settings")
   await tab.until("settings tabs load",s=>s.text.length>0)
   await tab.run(`document.querySelector('#settings-tab-1').click()`)
   let seen=await tab.until("saved native model appears",s=>s.settings?.includes("native-synthetic")&&!s.settingsDisabled)
   assert.ok(seen.wide<=width)
   await tab.click("#settings-antigravity-default-effort")
   await tab.run(`[...document.querySelectorAll('#settings-default-models [role=option]')].find(x=>x.textContent==='low').click()`)
   await tab.until("saved native effort readback",s=>s.effort?.includes("low")&&!s.settingsDisabled)
   assert.deepEqual(writes.at(-1),{antigravity_default_effort:"low"})
   await tab.run(`document.querySelector('#settings-antigravity-default-effort').focus()`)
   await tab.tabKey();assert.ok(await tab.run(`document.activeElement!==document.body`))
   await tab.shot(`native-settings-${label}-${lang}`)
   mode="settings-write-error"
   await tab.click("#settings-antigravity-default-effort")
   await tab.run(`[...document.querySelectorAll('#settings-default-models [role=option]')].find(x=>x.textContent==='high').click()`)
   await tab.until("failed write retains saved effort",s=>s.effort?.includes("low")&&!s.settingsDisabled&&s.settingsSaid?.length>0)
   mode="ready"
   await tab.click("#settings-antigravity-default-model")
   assert.equal(await tab.run(`document.querySelectorAll('#settings-default-models [role=option]').length`),2)
   await tab.run(`document.querySelector('#settings-default-models [role=option]').click()`)
   await tab.until("explicit native default is saved",s=>s.settings&&!s.settings.includes("native-synthetic")&&!s.settingsDisabled)
   assert.deepEqual(writes.at(-1),{antigravity_default_model:""})
   await tab.go("/?saved=1#page=settings");await tab.run(`document.querySelector('#settings-tab-1').click()`)
   await tab.until("native setting persists on reopening",s=>s.effort?.includes("low"))
   for(const state of ["settings-loading","settings-error"]){
    mode=state;await tab.go("/?state="+state+"#page=settings");await tab.run(`document.querySelector('#settings-tab-1').click()`)
    if(state==="settings-loading") await tab.until("loading controls disabled",s=>s.settingsDisabled===true)
    else await tab.until("failed read disables settings",s=>s.settingsDisabled===true && s.retryDisabled===false && s.settingsSaid?.length>0)
   }
   mode="ready";await tab.go("/?schedule=1#page=sessions")
   await tab.until("schedule is listed",s=>s.text.includes("Synthetic native schedule"))
   await tab.click('.schedule-row[data-id="'+scheduleID+'"]')
   await tab.until("schedule edit is ready",s=>s.edit)
   await tab.click('#schedule-history-edit')
   seen=await tab.until("native model and effort are retained",s=>s.form&&s.native==="antigravity"&&s.model?.includes("native-synthetic")&&s.canSave)
   assert.ok(seen.model.includes("max"));assert.ok(seen.wide<=width)
   await tab.shot(`native-schedule-${label}-${lang}`)
   await tab.run(`document.querySelector('#schedule-model').scrollIntoView({block:"center"})`)
   await tab.shot(`native-schedule-options-${label}-${lang}`)
   await tab.pressChip("#schedule-with","Codex")
   assert.equal(await tab.run(`document.querySelector('#schedule-model').textContent.includes('native-synthetic')`),false)
   await tab.pressChip("#schedule-with","Antigravity")
   assert.equal(await tab.run(`document.querySelector('#schedule-model [data-field=model][data-value="native-synthetic"]').getAttribute('aria-pressed')`),"true")
   await tab.click('#schedule-model [data-field=effort][data-value=medium]')
   await tab.click('#schedule-go')
   await tab.until("save closes form",s=>!s.form)
   assert.equal(writes.at(-1).assistant,"antigravity");assert.equal(writes.at(-1).model,"native-synthetic");assert.equal(writes.at(-1).reasoning_effort,"medium")
  }finally{await tab.close()}
 })
}
test("unknown saved effort remains visible; missing readiness cannot save",{timeout:90000},async()=>{
 language="en";mode="unknown-effort";const tab=await Tab.open(browser,390,844,true)
 try{
  await tab.go("/?unknown=1#page=sessions");await tab.until("schedule list",s=>s.text.includes("Synthetic native schedule"))
  await tab.click('.schedule-row[data-id="'+scheduleID+'"]');await tab.until("edit ready",s=>s.edit);await tab.click('#schedule-history-edit')
  await tab.until("unknown effort is visible",s=>s.form&&s.model?.includes("future-native"))
  assert.equal(await tab.run(`document.querySelector('#schedule-model [data-field=effort][data-value="future-native"]').getAttribute('aria-pressed')`),"true")
  for(const blocked of ["missing","required","stale","old","error"]){
   mode=blocked;await tab.go("/?blocked="+blocked+"#page=sessions");await tab.until("schedule list",s=>s.text.includes("Synthetic native schedule"))
   await tab.click('.schedule-row[data-id="'+scheduleID+'"]');await tab.until("edit ready",s=>s.edit);await tab.click('#schedule-history-edit')
   await tab.until("readiness blocks save",s=>s.form&&s.native==="antigravity"&&!s.canSave)
  }
 }finally{await tab.close()}
})
