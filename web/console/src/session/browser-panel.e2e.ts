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

let headerCount: number | null = null
let channelMode = "isolated"
let shotModes: string[] = []
let person = false
let clicks: { x: number; y: number }[] = []
let typed: string[] = []
let keys: string[] = []
let muted = false
let layout = "desktop"
let frames = 0
let heldControl: (() => void) | null = null
let holdControl = false
let holdType = false
let heldType: (() => void) | null = null
let failNextType = false
let holdNextStatus = false
let heldStatus: (() => void) | null = null
let desktopFields: {x:number;y:number;width:number;height:number;kind:string}[] = []
let mobileFields: typeof desktopFields = []

const row = {
  id: SESSION, label: "Browser Session", backend: "owned", state: "working", work_state: "working",
  evidence: "process", assistant: "codex", sessionId: CONVERSATION, cwd: "/tmp/fixture",
}
const activityAt = Math.floor(Date.now() / 1000)
let sequence = 0
const snapshot = () => ({ at: Date.now(), scan: { complete: true, completed: { complete: true, sequence: ++sequence }, emptyAuthoritative: true, epoch: 1, generation: sequence, provenance: "fixture" }, sessions: [{ ...row, ...(headerCount !== null ? { identity: "protocol" } : {}), ...(headerCount === 3 ? { persona: "reviewer" } : {}) }] })

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
    if (path === "/__fixture/header") { headerCount = Number(url.searchParams.get("count")); return json(res, 200, {}) }
    if (path === "/v1/personas") return json(res, 200, { personas: [{ id: "reviewer", name: { en: "Reviewer", "zh-Hant": "審查員" }, summary: { en: "Reviews changes", "zh-Hant": "審查變更" }, source: "builtin", suggested_kinds: [], teams: [], icon: { accent: "#4ccb7f", cells: [["#4ccb7f"]] } }] })
    if (path === "/v1/squad/session-snapshots/" + CONVERSATION) return json(res, 200, { snapshot_id: "header-snapshot", snapshot: { definition_id: "reviewer", scope_id: "header-fixture", scope_kind: "repo", skills: [{ id: "review", version: "1", enabled: true, name: { en: "Review", "zh-Hant": "審查" }, purpose: { en: "Review changes", "zh-Hant": "審查變更" }, content: "Review the changes." }] } })
    if (path === "/v1/squad/events/head") return json(res, 200, { seq: 0 })
    if (path === "/v1/squad/events") return json(res, 200, { events: [], next_after: 0, has_more: false })
    if (path === "/v1/squad/settings") return json(res, 200, { motion: { value: false } })
    if (path === "/v1/sessions/" + SESSION + "/outputs") return json(res, 200, { files: headerCount !== null && headerCount >= 2 ? [{ id: "a".repeat(64), name: "result.txt", path: "result.txt", absolute_path: "/synthetic/result.txt", kind: "created", first_at: 1, updated_at: 1, updates: 1, bytes: 10, state: "available", media_type: "text/plain", source: "fixture", anchor: 0 }] : [], more: false, partial: false, coverage: "complete" })
    if (path === "/__fixture/page") {
      res.writeHead(200, { "content-type": "text/html; charset=utf-8" })
      return res.end(`<!doctype html><html><meta name=viewport content="width=device-width,initial-scale=1"><style>body{margin:0;font:18px system-ui;color:#1e293b;background:#f2f5f8}header{padding:22px 28px;background:#17344d;color:white}main{max-width:850px;margin:36px auto;padding:0 24px}article{background:white;border-radius:12px;padding:28px;border:1px solid #d7e0e9}h1{font-size:28px;margin-top:0}label{display:block;margin-top:20px}input{display:block;box-sizing:border-box;width:100%;padding:14px;border:1px solid #a7b5c5;border-radius:6px;font:inherit;margin:8px 0 20px}button{background:#167349;color:white;border:0;border-radius:6px;padding:14px 24px;font:inherit}@media(min-width:700px){main{display:grid;grid-template-columns:220px 1fr;gap:24px}}small{color:#596b7e}</style><header>Example · Browser trial</header><main><aside><h2>Welcome</h2><p>Use the form to try the page.</p><small>This is a local test page.</small></aside><article><h1>Try the browser</h1><p>Zoom in, move the picture, then tap a field.</p><label>Your name<input placeholder="Enter a name"></label><label>Other text<input placeholder="Another field"></label><button>Continue</button></article></main></html>`)
    }
    if (path === "/__fixture/reset") { headerCount = null; person = false; channelMode="isolated";shotModes=[]; muted = false; clicks = []; typed = []; keys = []; layout = "desktop"; frames = 0; return json(res, 200, { ok: true }) }
    if (path === "/__fixture/channel-mode") { const b=await body(req);channelMode=b.mode;person=true;shotModes=[];return json(res,200,{ok:true}) }
    if (path === "/__fixture/hold-control") { holdControl = true; return json(res, 200, {}) }
    if (path === "/__fixture/admit") { holdControl = false; heldControl?.(); heldControl = null; return json(res, 200, {}) }
    if (path === "/__fixture/hold-type") { holdType = true; return json(res, 200, {}) }
    if (path === "/__fixture/release-type") { holdType = false; heldType?.(); heldType = null; return json(res, 200, {}) }
    if (path === "/__fixture/fail-next-type") { failNextType = true; return json(res, 200, {}) }
    if (path === "/__fixture/hold-status") { holdNextStatus = true; return json(res, 200, {}) }
    if (path === "/__fixture/release-status") { heldStatus?.(); heldStatus = null; return json(res, 200, {}) }
    if (path === "/__fixture/log") return json(res, 200, { person, muted, clicks, typed, keys, layout, frames, channelMode, shotModes, statusHeld: !!heldStatus })
    if (path === "/v1/browser/activity") return json(res, 200, { ok: true, rows: [
      { at: activityAt, op: "click", target: "Continue", site: "example.com", url: "https://www.console.example.com/sign-in", ok: true, thumb: page.toString("base64") },
      { at: activityAt - 5, op: "open", site: "example.com", url: "https://www.console.example.com/sign-in", ok: true, thumb: page.toString("base64") },
      ...["release", "release", "close", "read", "screenshot", "takeover", "handback"].map((op, i) => ({ id: "quiet-"+i, at: activityAt-i-10, op, site:"example.com", ok:true, thumb:"" })),
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
    if (path === "/v1/browser/status") {
      const value = { ok: true, mode:channelMode, enabled: true, notify_new_site: true,
        status: { connected: true, stopped: false, person_tabs: person ? [TAB] : [], pending: 0, muted: muted ? [CONVERSATION] : [], tabs: headerCount === 0 ? [] : [{ tab: TAB, conversation: CONVERSATION, person, current: true }] } }
      if (holdNextStatus) { holdNextStatus = false; await new Promise<void>(resolve => { heldStatus = resolve }) }
      return json(res, 200, value)
    }
    if (req.method === "POST" && path === "/v1/browser/control") { const b = await body(req); if (b.browser_mode!==channelMode) return json(res,409,{error:"browser_wrong_mode",detail:"stale profile viewer"}); if (holdControl && b.person) await new Promise<void>(resolve => { heldControl = resolve }); person = !!b.person; return json(res, 200, { ok: true, tab: b.tab, person }) }
    if (path === "/v1/browser/screenshot") { if (url.searchParams.get("browser_mode")!==channelMode) return json(res,409,{error:"browser_wrong_mode",detail:"stale profile viewer"}); shotModes.push(channelMode);frames++; return json(res, 200, { ok: true, value: { tab: TAB, url: "https://www.console.example.com/sign-in", title: "Sign in", width: layout === "mobile" ? 390 : 1280, height: 720, layout, editables: layout === "mobile" ? mobileFields : desktopFields, data: (layout === "mobile" ? mobilePage : page).toString("base64") } }) }
    if (req.method === "POST" && path === "/v1/browser/person") {
      const b = await body(req)
      if (b.browser_mode!==channelMode) return json(res,409,{error:"browser_wrong_mode",detail:"stale profile viewer"})
      if (b.op === "screenshot") return json(res, 200, { ok: true, value: { tab: TAB, url: "https://www.console.example.com/sign-in", title: "Sign in", width: 1280, height: 720, data: page.toString("base64") } })
      if (!person) return json(res, 409, { error: "browser_take_over_first", detail: "Take over the tab before typing or clicking in it." })
      if (b.op === "click") clicks.push({ x: b.args.x, y: b.args.y })
      if (b.op === "type") {
        typed.push(b.args.text)
        if (holdType) await new Promise<void>(resolve => { heldType = resolve })
        if (failNextType) { failNextType = false; return json(res, 503, {error:"browser_timeout",detail:"Synthetic input acknowledgement was lost."}) }
      }
      if (b.op === "key") keys.push(b.args.key)
      if (b.op === "viewport") layout = b.args.mode
      const field = b.op === "click" ? (layout === "mobile" ? mobileFields : desktopFields).find(f=>b.args.x>=f.x && b.args.x<=f.x+f.width && b.args.y>=f.y && b.args.y<=f.y+f.height) : null
      return json(res, 200, { ok: true, value: b.op === "click" ? {editable:field?.kind ?? null} : b.op === "key" ? {editable:"text"} : {} })
    }
    if (path.startsWith("/v1/work/v2/human-interventions/")) return json(res, 200, { ok: true, rows: [{
      id: "10000000-0000-4000-8000-000000000059", source_conversation: CONVERSATION, source_label: "Codex", target_conversation: CONVERSATION,
      target_session: SESSION, kind: "browser", title: "Sign in", summary: "The page needs sign-in.", action: "Take over the browser.", reason: "Only the person signs in.",
      options: [], created_at: activityAt, read_at: null, resolved_at: null, version: 1,
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
    const fields = await browser.send("Runtime.evaluate", { expression: `Array.from(document.querySelectorAll('input')).map(el=>{const r=el.getBoundingClientRect();return {x:r.x,y:r.y,width:r.width,height:r.height,kind:'text'}})`, returnByValue: true }, sessionId)
    if (width === 1280) { page = Buffer.from(data, "base64"); desktopFields = fields.result.value }
    else { mobilePage = Buffer.from(data, "base64"); mobileFields = fields.result.value }
  }
  await browser.send("Target.closeTarget", { targetId })
})
after(async () => {
  if (browserProcess && browserProcess.exitCode === null) browserProcess.kill()
  browser?.close()
  server?.closeAllConnections(); await new Promise<void>((ok) => server ? server.close(() => ok()) : ok())
  if (profile) rmSync(profile, { recursive: true, force: true, maxRetries: 5 })
})

for (const [name, width, height, ui] of [["desktop",1280,800,"zh-Hant"],["phone",390,844,"zh-Hant"],["desktop",1280,800,"en"],["phone",390,844,"en"],["phone",360,640,"zh-Hant"]] as const) {
  test(`${name} ${ui} ${width}: direct takeover, native input, safe controls and zoom`, async () => {
    const { targetId } = await browser.send("Target.createTarget", { url:"about:blank" })
    const { sessionId:id } = await browser.send("Target.attachToTarget", { targetId,flatten:true })
    const run = async (expression:string) => {
      const {result,exceptionDetails}=await browser.send("Runtime.evaluate",{expression,returnByValue:true,awaitPromise:true},id)
      if(exceptionDetails)throw new Error(exceptionDetails.exception?.description ?? exceptionDetails.text)
      return result.value
    }
    const until = async (expression:string) => {
      const deadline=Date.now()+8000
      while(!await run(expression)){if(Date.now()>deadline)assert.fail("did not reach "+expression+": "+await run(`document.getElementById('browser-panel')?.innerText`));await new Promise(r=>setTimeout(r,30))}
    }
    const shot = async (label:string) => {if(!shots)return;const {data}=await browser.send("Page.captureScreenshot",{format:"png"},id);writeFileSync(join(shots,`browser-panel-${label}-${ui}-${name}-${width}.png`),Buffer.from(data,"base64"))}
    const tap = async (x:number,y:number, resize=false) => {
      if(name==="phone"){
        await browser.send("Input.dispatchTouchEvent",{type:"touchStart",touchPoints:[{x,y,id:1}]},id)
        if(resize){
          await run(`document.documentElement.style.setProperty('--vvh','540px')`)
          await run(`new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r)))`)
        }
        await browser.send("Input.dispatchTouchEvent",{type:"touchEnd",touchPoints:[]},id)
      }else{
        await browser.send("Input.dispatchMouseEvent",{type:"mousePressed",x,y,button:"left",clickCount:1},id)
        await browser.send("Input.dispatchMouseEvent",{type:"mouseReleased",x,y,button:"left",clickCount:1},id)
      }
    }
    const pointerCentre = async (selector:string) => run(`(()=>{const r=document.querySelector(${JSON.stringify(selector)}).getBoundingClientRect();return {x:r.x+r.width/2,y:r.y+r.height/2}})()`)
    try{
      await fetch(origin+"/__fixture/reset")
      await browser.send("Page.enable",{},id)
      await browser.send("Emulation.setDeviceMetricsOverride",{width,height,deviceScaleFactor:2,mobile:name==="phone"},id)
      await browser.send("Emulation.setTouchEmulationEnabled",{enabled:name==="phone",maxTouchPoints:5},id)
      const mark=browser.loadCount;await browser.send("Page.navigate",{url:`${origin}/?ui=${ui}#session=${SESSION}`},id);await browser.loaded(id,mark)
      await until(`!!document.querySelector('.browser-chip')`)
      await until(`document.querySelectorAll('.browser-action').length===2`)
      assert.ok(await run(`Array.from(document.querySelectorAll('.browser-action span')).some(el=>el.textContent==='www.console.example.com')`), "the transcript preserves the subdomain instead of showing the notification grouping domain")
      const activityURL = await run(`location.href`)
      const activityPicture = await run(`document.querySelector('.browser-action img').src`)
      await run(`document.querySelector('.browser-action img').click()`)
      await until(`document.getElementById('image-lightbox')?.hidden===false`)
      assert.equal(await run(`document.getElementById('image-lightbox-image').src`),activityPicture,"preview shows the captured action picture")
      await run(`document.getElementById('image-lightbox-close').click()`)
      await until(`document.getElementById('image-lightbox').hidden`)
      assert.equal(await run(`location.href`),activityURL,"closing the picture preserves the Session")
      assert.ok(await run(`!!document.activeElement?.closest('.browser-action')`),"focus returns to the action picture")
      await run(`document.getElementById('detail-actions-trigger').click()`)
      await until(`!document.getElementById('session-browser').hidden`)
      await run(`document.getElementById('session-browser').click()`)
      await until(`document.querySelector('.browser-shot')?.getBoundingClientRect().width>0`)
      assert.equal(await run(`document.querySelector('.browser-host').textContent`), "www.console.example.com")
      assert.ok(await run(`document.querySelector('.browser-composer-input').getBoundingClientRect().height>=44`),"a visible native draft stays in the footer")
      assert.equal(await run(`document.getElementById('browser-tools').hidden`),true)
      if(name==="phone"){
        assert.ok(await run(`document.querySelector('.browser-shot').getBoundingClientRect().width>=1279`),"readable original-size page")
        assert.ok(await run(`parseFloat(getComputedStyle(document.querySelector('.browser-controls')).paddingBottom)>=42`),"home indicator has its full safe touch margin even when env() is zero")
        assert.ok(await run(`document.querySelector('.browser-viewport').getBoundingClientRect().height>=innerHeight*.5`),"one compact footer leaves the page most of the screen")
      }
      await shot("watching-redesign")
      await run(`[...document.querySelectorAll('.browser-zoom button')].find(b=>b.textContent===${JSON.stringify(ui==="en"?"Fit":"符合畫面")}).click()`)
      await fetch(origin+"/__fixture/hold-control")
      await fetch(origin+"/__fixture/hold-status")
      const f=desktopFields[0]
      const hit=await run(`(()=>{const r=document.querySelector('.browser-shot').getBoundingClientRect();return {x:r.left+${f.x+f.width/2}/1280*r.width,y:r.top+${f.y+f.height/2}/720*r.height}})()`)
      assert.equal(await run(`!!document.querySelector('.browser-native-field')`),false,"screenshot geometry does not replace the visible footer editor")
      await run(`window.__touchedField=document.getElementById('browser-native-keyboard');true`)
      await tap(hit.x,hit.y,name==="phone"&&ui==="zh-Hant"&&width===390)
      assert.equal(await run(`document.activeElement === window.__touchedField`),true,"the visible footer editor is focused before Cloud approves takeover")
      // iOS follows a tap with compatibility mouse events and a click that can
      // focus the tabbable picture; the person saw the footer flash and lose focus.
      await run(`document.querySelector('.browser-viewport').focus();true`)
      await run(`new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r)))`)
      assert.equal(await run(`document.activeElement === window.__touchedField`),true,"a focus the tap hands to the picture returns to the footer editor")
      await browser.send("Input.insertText",{text:"中文試用"},id)
      assert.deepEqual((await(await fetch(origin+"/__fixture/log")).json()).typed,[],"typing cannot pass an unconfirmed takeover")
      const other=desktopFields[1]
      const otherHit=await run(`(()=>{const r=document.querySelector('.browser-shot').getBoundingClientRect();return {x:r.left+${other.x+other.width/2}/1280*r.width,y:r.top+${other.y+other.height/2}/720*r.height}})()`)
      await tap(otherHit.x,otherHit.y)
      assert.equal(await run(`document.activeElement===window.__touchedField`),true,"a blocked second field cannot steal an unconfirmed input queue")
      await until(`fetch('/__fixture/log').then(r=>r.json()).then(log=>log.statusHeld)` )
      await fetch(origin+"/__fixture/admit")
      assert.deepEqual((await(await fetch(origin+"/__fixture/log")).json()).typed,[],"takeover alone never sends a local draft")
      assert.equal((await(await fetch(origin+"/__fixture/log")).json()).person,true,"tapping the page takes over and clicks once")
      await until(`!document.querySelector('.browser-input-pending')`)
      await until(`!!document.querySelector('.browser-who[data-person="true"]')`)
      const oldFrames = (await(await fetch(origin+"/__fixture/log")).json()).frames
      await fetch(origin+"/__fixture/release-status")
      await until(`fetch('/__fixture/log').then(r=>r.json()).then(log=>log.frames>=${oldFrames + 2})`)
      assert.equal(await run(`document.activeElement === window.__touchedField`), true, "a delayed pre-takeover status and idle screenshot refresh must not dismiss the native keyboard")
      assert.equal(await run(`document.getElementById('browser-native-keyboard').value`),"中文試用","the draft remains visible across refreshes")
      await fetch(origin+"/__fixture/hold-type")
      const sendPoint = await pointerCentre('.browser-composer button')
      await tap(sendPoint.x,sendPoint.y)
      await until(`fetch('/__fixture/log').then(r=>r.json()).then(log=>log.typed[0]==='中文試用')`)
      await browser.send("Input.insertText",{text:"下一段"},id)
      const pendingFrames = (await(await fetch(origin+"/__fixture/log")).json()).frames
      await until(`fetch('/__fixture/log').then(r=>r.json()).then(log=>log.frames>=${pendingFrames + 2})`)
      assert.equal(await run(`document.activeElement === window.__touchedField`),true,"a pending send and screenshot refresh never disable or blur the editor")
      assert.equal(await run(`document.getElementById('browser-native-keyboard').value`),"下一段")
      // The person could not tell that text was on its way; the button says so.
      const sending=ui==="en"?"Sending…":"送出中…", sent=ui==="en"?"Sent":"已送出"
      assert.equal(await run(`document.querySelector('.browser-composer button').getAttribute('aria-busy')`),"true")
      assert.ok((await run(`document.querySelector('.browser-composer button').textContent`)).includes(sending),"the Send button shows that text is being sent")
      await fetch(origin+"/__fixture/release-type")
      await until(`!document.querySelector('.browser-input-pending')`)
      await until(`document.querySelector('.browser-composer button').textContent.includes(${JSON.stringify(sent)})`)
      assert.equal(await run(`document.getElementById('browser-native-keyboard').value`),"下一段","acknowledgement preserves the next draft")
      await run(`document.getElementById('browser-native-keyboard').select()`)
      await browser.send("Input.dispatchKeyEvent",{type:"keyDown",key:"Backspace",code:"Backspace",windowsVirtualKeyCode:8},id)
      await browser.send("Input.dispatchKeyEvent",{type:"keyUp",key:"Backspace",code:"Backspace",windowsVirtualKeyCode:8},id)
      assert.equal(await run(`document.activeElement === window.__touchedField`),true,"sending text keeps the same native editor focused")
      await run(`document.documentElement.style.removeProperty('--vvh')`)
      await browser.send("Emulation.setDeviceMetricsOverride",{width,height,deviceScaleFactor:2,mobile:name==="phone"},id)
      await browser.send("Input.imeSetComposition",{text:"測",selectionStart:1,selectionEnd:1},id)
      await browser.send("Input.insertText",{text:"測試"},id)
      assert.equal(await run(`document.getElementById('browser-native-keyboard').value`),"測試","IME remains a visible local draft until sent")
      await until(`!document.querySelector('.browser-composer button').disabled`)
      await run(`document.querySelector('.browser-composer').requestSubmit()`)
      await until(`fetch('/__fixture/log').then(r=>r.json()).then(log=>log.typed.length===2)`)
      assert.deepEqual((await(await fetch(origin+"/__fixture/log")).json()).typed,["中文試用","測試"],"IME sends the committed Chinese once")
      await until(`!document.querySelector('.browser-input-pending')`)
      await browser.send("Input.insertText",{text:"123"},id)
      await browser.send("Input.dispatchKeyEvent",{type:"keyDown",key:"Backspace",code:"Backspace",windowsVirtualKeyCode:8},id)
      await browser.send("Input.dispatchKeyEvent",{type:"keyUp",key:"Backspace",code:"Backspace",windowsVirtualKeyCode:8},id)
      assert.equal(await run(`document.getElementById('browser-native-keyboard').value`),"12","Backspace edits the local draft without a remote keystroke")
      assert.deepEqual((await(await fetch(origin+"/__fixture/log")).json()).keys,[])
      // Delete beside Send text removes a character from the page field and,
      // like Send text, never takes focus: the phone keyboard stays up.
      const del=await run(`(()=>{const r=document.querySelector('.browser-composer-delete').getBoundingClientRect();return {x:r.left+r.width/2,y:r.top+r.height/2}})()`)
      await tap(del.x,del.y)
      await until(`fetch('/__fixture/log').then(r=>r.json()).then(log=>log.keys.length===1)`)
      assert.deepEqual((await(await fetch(origin+"/__fixture/log")).json()).keys,["Backspace"],"Delete sends one Backspace to the page")
      assert.equal(await run(`document.activeElement === window.__touchedField`),true,"Delete keeps the footer editor focused")
      assert.equal(await run(`document.getElementById('browser-native-keyboard').value`),"12","Delete leaves the local draft alone")
      // The keyboard's own Backspace edits the draft while it has text and,
      // once it is empty, reaches the page field like Delete.
      for (let i=0;i<3;i++){
        await browser.send("Input.dispatchKeyEvent",{type:"keyDown",key:"Backspace",code:"Backspace",windowsVirtualKeyCode:8},id)
        await browser.send("Input.dispatchKeyEvent",{type:"keyUp",key:"Backspace",code:"Backspace",windowsVirtualKeyCode:8},id)
      }
      assert.equal(await run(`document.getElementById('browser-native-keyboard').value`),"","two Backspaces empty the draft")
      await until(`fetch('/__fixture/log').then(r=>r.json()).then(log=>log.keys.length===2)`)
      assert.deepEqual((await(await fetch(origin+"/__fixture/log")).json()).keys,["Backspace","Backspace"],"a Backspace on an empty draft reaches the page")
      assert.equal(await run(`document.activeElement === window.__touchedField`),true,"forwarding Backspace keeps the footer editor focused")
      if(name==="phone"){
        await run(`Object.defineProperty(window.visualViewport,'height',{configurable:true,value:Math.round(innerHeight*.58)});window.visualViewport.dispatchEvent(new Event('resize'))`)
        await until(`document.getElementById('browser-panel').getBoundingClientRect().height<innerHeight*.7`)
        assert.ok(await run(`document.querySelector('.browser-controls').getBoundingClientRect().bottom<=visualViewport.height+1`),"toolbar stays above a raised keyboard")
        await shot("keyboard-redesign")
        await run(`delete window.visualViewport.height;window.visualViewport.dispatchEvent(new Event('resize'))`)
      }
      await run(`document.querySelector('.browser-tools-toggle').click()`)
      await until(`!document.getElementById('browser-tools').hidden`)
      await run(`document.querySelector('.browser-notify').click()`)
      await until(`document.querySelector('.browser-notify').getAttribute('aria-pressed')==='false'`)
      await shot("tools-redesign")
      await run(`document.querySelector('.browser-tools-toggle').click()`)
      await run(`document.querySelector('.browser-take').click()`)
      await until(`fetch('/__fixture/log').then(r=>r.json()).then(log=>!log.person)`)
      assert.equal(await run(`document.activeElement?.matches('.browser-composer-input')`),false,"handback closes native input")
      const beforeClicks=(await(await fetch(origin+"/__fixture/log")).json()).clicks.length
      await run(`document.querySelector('.browser-zoom button[aria-label=${JSON.stringify(ui==="en"?"Zoom in":"放大")} ]').click()`)
      const c=await pointerCentre('.browser-viewport')
      if(name==="phone"){
        const points=(d:number)=>[{x:c.x-d,y:c.y,id:1},{x:c.x+d,y:c.y,id:2}]
        const before=await run(`document.querySelector('.browser-shot').getBoundingClientRect().width`)
        await browser.send("Input.dispatchTouchEvent",{type:"touchStart",touchPoints:points(25)},id)
        await browser.send("Input.dispatchTouchEvent",{type:"touchMove",touchPoints:points(50)},id)
        await browser.send("Input.dispatchTouchEvent",{type:"touchEnd",touchPoints:[]},id)
        assert.ok(await run(`document.querySelector('.browser-shot').getBoundingClientRect().width>${before+20}`))
        await browser.send("Input.dispatchTouchEvent",{type:"touchStart",touchPoints:[{x:c.x,y:c.y,id:1}]},id)
        await browser.send("Input.dispatchTouchEvent",{type:"touchMove",touchPoints:[{x:c.x-70,y:c.y,id:1}]},id)
        await browser.send("Input.dispatchTouchEvent",{type:"touchCancel",touchPoints:[]},id)
      }
      const afterGestures=await(await fetch(origin+"/__fixture/log")).json()
      assert.equal(afterGestures.clicks.length,beforeClicks);assert.equal(afterGestures.person,false,"gestures never take over or click")
      await run(`(()=>{const s=document.querySelector('.browser-mode select');s.value='mobile';s.dispatchEvent(new Event('change',{bubbles:true}))})()`)
      await until(`fetch('/__fixture/log').then(r=>r.json()).then(log=>log.layout==='mobile'&&log.person)`)
      await until(`document.querySelector('.browser-mode select')?.value==='mobile'`)
      await shot("mobile-site-redesign")
      assert.ok(await run(`document.documentElement.scrollWidth<=innerWidth`),"zoom stays inside the picture")
      if (name==="phone" && width===390 && ui==="zh-Hant") {
        await run(`document.querySelector('.browser-tools-toggle').click()`)
        await run(`[...document.querySelectorAll('#browser-tools button')].find(b=>b.textContent==='鍵盤').click()`)
        assert.equal(await run(`document.activeElement?.id`),"browser-native-keyboard")
        await fetch(origin+"/__fixture/channel-mode",{method:"POST",body:JSON.stringify({mode:"user_chrome"})})
        await until(`fetch('/__fixture/log').then(r=>r.json()).then(log=>log.shotModes.includes('user_chrome'))`)
        await until(`!!document.querySelector('.browser-shot')`)
        assert.notEqual(await run(`document.activeElement?.id`),"browser-native-keyboard","a new profile with the same tab ID closes the old field's keyboard")
        const field=mobileFields[0]
        const nextHit=await run(`(()=>{const r=document.querySelector('.browser-shot').getBoundingClientRect();return {x:r.left+${field.x+field.width/2}/390*r.width,y:r.top+${field.y+field.height/2}/720*r.height}})()`)
        await tap(nextHit.x,nextHit.y)
        await browser.send("Input.insertText",{text:"新模式輸入"},id)
        await until(`!document.querySelector('.browser-composer button').disabled`)
        await run(`document.querySelector('.browser-composer').requestSubmit()`)
        await until(`fetch('/__fixture/log').then(r=>r.json()).then(log=>log.typed.includes('新模式輸入'))`)
        await until(`!document.querySelector('.browser-input-pending')`)
        await browser.send("Input.insertText",{text:"未確認文字"},id)
        await fetch(origin+"/__fixture/fail-next-type")
        await until(`!document.querySelector('.browser-composer button').disabled`)
        await run(`document.querySelector('.browser-composer').requestSubmit()`)
        await until(`!!document.querySelector('.browser-input-error')`)
        assert.equal(await run(`document.getElementById('browser-native-keyboard').value`),"未確認文字","an uncertain result preserves the draft for inspection")
        const failedFrames = (await(await fetch(origin+"/__fixture/log")).json()).frames
        await until(`fetch('/__fixture/log').then(r=>r.json()).then(log=>log.frames>=${failedFrames + 2})`)
        assert.equal((await(await fetch(origin+"/__fixture/log")).json()).typed.filter((v:string)=>v==='未確認文字').length,1,"an uncertain input is never automatically retried")
      }

      if(name==="phone"){
        await browser.send("Emulation.setDeviceMetricsOverride",{width:844,height:390,deviceScaleFactor:2,mobile:true},id)
        await until(`document.querySelector('.browser-viewport').getBoundingClientRect().height>100`)
        await shot("landscape-redesign")
      }
    }finally{
      await fetch(origin+"/__fixture/admit").catch(()=>{})
      await fetch(origin+"/__fixture/release-status").catch(()=>{})
      await fetch(origin+"/__fixture/release-type").catch(()=>{})
      await browser.send("Target.closeTarget",{targetId}).catch(()=>{})
    }
  })
}


for (const [width, ui] of [[320, "zh-Hant"], [390, "zh-Hant"], [390, "en"], [1280, "en"]] as const) {
  test(`header options ${width} ${ui}: latest entrance, disclosure and available controls`, async () => {
    const { targetId } = await browser.send("Target.createTarget", { url: "about:blank" })
    const { sessionId: id } = await browser.send("Target.attachToTarget", { targetId, flatten: true })
    const run = async (expression: string) => {
      const { result, exceptionDetails } = await browser.send("Runtime.evaluate", { expression, returnByValue: true, awaitPromise: true }, id)
      if (exceptionDetails) throw new Error(exceptionDetails.exception?.description ?? exceptionDetails.text)
      return result.value
    }
    const until = async (expression: string) => {
      const deadline = Date.now() + 8000
      while (!await run(expression)) {
        if (Date.now() > deadline) assert.fail("did not reach " + expression)
        await new Promise(resolve => setTimeout(resolve, 30))
      }
    }
    const shown = (selector: string) => `!!document.querySelector(${JSON.stringify(selector)})?.getClientRects().length`
    const shot = async (label: string) => {
      if (!shots) return
      const { data } = await browser.send("Page.captureScreenshot", { format: "png" }, id)
      writeFileSync(join(shots, `header-${label}-${ui}-${width}.png`), Buffer.from(data, "base64"))
    }
    try {
      await browser.send("Page.enable", {}, id)
      await browser.send("Page.bringToFront", {}, id)
      await browser.send("Emulation.setDeviceMetricsOverride", { width, height: 844, deviceScaleFactor: 2, mobile: width < 520 }, id)
      for (const count of [3, 2, 1, 0]) {
        await fetch(origin + "/__fixture/header?count=" + count)
        await browser.send("Page.navigate", { url: `${origin}/?ui=${ui}&header=${count}#session=${SESSION}` }, id)
        await until(`document.querySelector('#detail-name')?.textContent === 'Browser Session'`)
        if (count) await until(`!!document.querySelector('.browser-chip')`)
        if (count >= 2) await until(`!!document.querySelector('.output-entry')`)
        if (count === 3) await until(`!!document.querySelector('.skill-bot-toggle')`)
        if (count <= 1) {
          assert.equal(await run(shown(".header-options-toggle")), false)
          if (count === 0) assert.equal(await run(shown(".browser-chip")), false)
          continue
        }
        const phone = width <= 520
        assert.equal(await run(shown(".header-options-toggle")), phone)
        assert.equal(await run(shown(".output-entry")), !phone)
        assert.equal(await run(`document.querySelector('#detail-head').scrollWidth <= document.querySelector('#detail-head').clientWidth`), true)
        if (!phone) continue
        await shot("collapsed-" + count)
        const nameWidth = await run(`document.querySelector('#detail-name').clientWidth`)
        assert.ok(nameWidth > 0, "the Session name remains visible")
        assert.equal(await run(`(()=>{
          const button=document.querySelector('.header-options-toggle');
          const r=button.getBoundingClientRect(), arrow=document.querySelector('.header-options-arrow').getBoundingClientRect();
          return parseFloat(getComputedStyle(button).borderTopWidth)>0 &&
            arrow.left>=r.left && arrow.right<=r.right && arrow.top>=r.top && arrow.bottom<=r.bottom;
        })()`), true, "the disclosure is inside the visible button outline")
        assert.equal(await run(`(()=>{
          const chip=document.querySelector('.header-options-label'), range=document.createRange();
          range.selectNodeContents(chip.lastChild);
          const text=range.getBoundingClientRect(), arrow=document.querySelector('.header-options-arrow').getBoundingClientRect();
          const target=document.querySelector('.header-options-toggle').getBoundingClientRect();
          return arrow.left-text.right>=0 && arrow.left-text.right<=3 && target.height>=44;
        })()`), true, "the arrow sits close to the label with a full-height touch target")
        const labelPoint = await run(`(()=>{const r=document.querySelector('.header-options-label').getBoundingClientRect();return {x:r.left+2,y:r.top+r.height/2}})()`)
        await browser.send("Input.dispatchMouseEvent", { type: "mousePressed", ...labelPoint, button: "left", clickCount: 1 }, id)
        await browser.send("Input.dispatchMouseEvent", { type: "mouseReleased", ...labelPoint, button: "left", clickCount: 1 }, id)
        await until(shown(".output-entry"))
        assert.equal(await run(shown("#browser-panel")), false, "pressing the left side only opens choices")
        assert.equal(await run(shown(".output-entry")), true)
        assert.equal(await run(shown(".browser-chip")), true)
        if (count === 3) assert.equal(await run(shown(".skill-bot-toggle")), true)
        assert.equal(await run(`(()=>{
          const rows=[...document.querySelector('.header-options-extra').children].map(el=>el.getBoundingClientRect());
          return rows.length===${count} && rows.every((row,i)=>i===0 || (row.top>=rows[i-1].bottom && Math.abs(row.left-rows[i-1].left)<1 && Math.abs(row.width-rows[i-1].width)<1 && Math.abs(row.height-rows[i-1].height)<1));
        })()`), true, "all options, including the previewed one, form vertical rows")
        const labels = ui === "en" ? ["Skills for this Session", "Output files", "Chrome tab"] : ["這個 Session 的 Skill", "產出檔案", "Chrome 分頁"]
        assert.deepEqual(await run(`Array.from(document.querySelector('.header-options-extra').children, el=>el.innerText.trim())`), labels.slice(3 - count), "each row has a visible description")
        const bounds = await run(`(()=>{const r=document.querySelector('.header-options-extra').getBoundingClientRect();return {left:r.left,right:r.right}})()`)
        assert.ok(bounds.left >= 0 && bounds.right <= width, "expanded choices fit the viewport")
        assert.equal(await run(`document.querySelector('#detail-name').clientWidth`), nameWidth, "expanding choices does not squeeze the name")
        await shot("expanded-" + count)
        await browser.send("Input.dispatchKeyEvent", { type: "keyDown", key: "Escape", code: "Escape", windowsVirtualKeyCode: 27 }, id)
        assert.equal(await run(shown(".output-entry")), false)
        assert.equal(await run(`document.activeElement?.className`), "header-options-toggle")
        await run(`document.querySelector('.header-options-toggle').click()`)
        await until(shown(".output-entry"))
        await run(`document.querySelector('#detail-name').dispatchEvent(new PointerEvent('pointerdown', { bubbles: true }))`)
        assert.equal(await run(shown(".output-entry")), false)
        await run(`document.querySelector('.header-options-toggle').click()`)
        await until(shown(".output-entry"))
        await run(`document.querySelector('.browser-chip').click()`)
        await until(shown("#browser-panel"))
        assert.equal(await run(shown(".header-options-extra")), false)
        await run(`document.querySelector('.browser-close').click()`)
        await run(`document.querySelector('.header-options-toggle').click()`)
        await until(shown(".output-entry"))
        await run(`document.querySelector('.output-entry').click()`)
        await until(`document.querySelector('.output-entry')?.getAttribute('aria-expanded') === 'true'`)
        assert.equal(await run(shown(".header-options-extra")), false)
      }
    } finally {
      headerCount = null
      await browser.send("Target.closeTarget", { targetId })
    }
  })
}
