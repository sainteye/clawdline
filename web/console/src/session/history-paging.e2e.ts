// Browser regression for paged Codex history and retrying failed reads.
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
const shots = process.env.STATUS_SHOTS || ""

const ID = "ttys019"
const NOW = Math.floor(Date.now() / 1000)
// The label the screenshot that asked for this carried: long enough, with the
// branch and two plan windows beside it, to run the chip over the windows.
const LABEL = "deploy·marketing"
let remoteControl = false
let remoteWrites = 0
let remoteText = ""

let firstAttempts = 0
let olderAttempts = 0
const firstID = "10000000-0000-4000-8000-000000000020"
const olderID = "10000000-0000-4000-8000-000000000021"

const ROW = {
  id: ID,
  label: "A session with a deploy in flight",
  backend: "owned",
  state: "idle",
  work_state: "ready",
  evidence: "process",
  isClaude: true,
  sessionId: "10000000-0000-4000-8000-000000000009",
  cwd: "/tmp/fixture",
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
    sessions: [{ ...ROW, ...(remoteControl ? { backend: "claude_remote", label: "Remote conversation" } : {}) }],
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
    if (remoteControl && req.method === "POST" && path.startsWith("/v1/sessions/")) remoteWrites++
    if (path === "/v1/places") return json(res, 200, { places: [{ id: "project", label: "Fixture project", path: "/tmp/fixture" }], assistants: [{ id: "codex", label: "Codex" }] })
    if (path === "/v1/places/project/sessions/codex") {
      const cursor = new URL(req.url ?? "/", "http://fixture").searchParams.get("cursor")
      const attempt = cursor ? ++olderAttempts : ++firstAttempts
      if (attempt === 1) return json(res, 503, { error: { code: "codex_unavailable", message: "Codex history could not be read. Retry the request." } })
      return json(res, 200, { sessions: [{ id: cursor ? olderID : firstID, title: cursor ? "Older conversation" : "Recent conversation", at: NOW, live: false }], more: !cursor, next_cursor: cursor ? "" : "older/+?= page" })
    }
    if (path === "/v1/sessions") return json(res, 200, snapshot())
    if (path === "/v1/events") {
      res.writeHead(200, { "content-type": "text/event-stream", "cache-control": "no-cache" })
      res.write("event: sessions\ndata: " + JSON.stringify(snapshot()) + "\n\n")
      return
    }
    if (remoteControl && req.method === "POST" && path === "/v1/sessions/"+ID+"/send") {
      let body="";req.on("data", chunk=>body+=chunk);req.on("end",()=>{remoteText=JSON.parse(body).text;json(res,200,{ok:true,id:ID,action:"accepted"})});return
    }
    if (path === "/v1/transcript") return json(res, 200, { entries: remoteControl ? [{ role: "user", text: "Remote history fixture", timestamp: NOW }, ...(remoteText ? [{role:"user",text:remoteText,timestamp:NOW},{role:"error",text:"Weekly quota exhausted",timestamp:NOW}] : [])] : [], evidence: "transcript", id: ID, signature: "fixture:"+remoteText })
    if (path.startsWith("/v1/")) return json(res, 404, { error: { code: "not_found", message: path } })
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
  profile = mkdtempSync(join(tmpdir(), "clawdline-history-"))
  browserProcess = spawn(chrome, ["--headless=new", "--remote-debugging-port=0", "--user-data-dir=" + profile,
    "--no-first-run", "--no-default-browser-check", "--disable-extensions", "about:blank"], { detached: true })
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
  if (browser) await browser.send("Browser.close").catch(() => undefined)
  browser?.close()
  // This browser was spawned in a private process group. Renderers can outlive
  // its parent and hold stderr open, so stop only this test-owned group.
  if (browserProcess?.pid) { try { process.kill(-browserProcess.pid, "SIGTERM") } catch { /* already gone */ } }
  if (browserProcess && browserProcess.exitCode === null) {
    const gone = new Promise((ok) => browserProcess.once("exit", ok))
    browserProcess.kill()
    await gone
  }
  server?.closeAllConnections()
  await new Promise<void>((ok) => (server ? server.close(() => ok()) : ok()))
  if (profile) rmSync(profile, { recursive: true, force: true, maxRetries: 5 })
})


test("history retries preserve the current page and append older sessions once", async () => {
 const { targetId } = await browser.send("Target.createTarget",{url:"about:blank"})
 const { sessionId:s } = await browser.send("Target.attachToTarget",{targetId,flatten:true})
 const run=async(expression:string)=>{
  const {result,exceptionDetails}=await browser.send("Runtime.evaluate",{expression,returnByValue:true,awaitPromise:true},s)
  if(exceptionDetails)throw new Error(exceptionDetails.exception?.description??exceptionDetails.text)
  return result.value
 }
 const until=async(expression:string)=>{
  const deadline=Date.now()+8000
  while(Date.now()<deadline){if(await run(expression))return;await new Promise(r=>setTimeout(r,50))}
  throw new Error("not ready: "+expression+"; "+await run(`document.body.innerText`))
 }
 const click=async(selector:string)=>run(`document.querySelector(${JSON.stringify(selector)}).click()`)
 try{
  await browser.send("Page.enable",{},s)
  await browser.send("Runtime.enable",{},s)
  const mark=browser.loadCount
  await browser.send("Page.navigate",{url:origin+"/#page=sessions"},s)
  await browser.loaded(s,mark)
  await until(`!!document.querySelector("#start-go")`)
  await click("#start-go")
  await until(`!!document.querySelector("#start-resume button:not(:disabled)")`)
  await click("#start-resume button:not(:disabled)")
  await until(`document.querySelector("#start-resume button")?.getAttribute("aria-pressed") === "true"`)
  await until(`!!document.querySelector('#start-list button[data-id="project"]')`)
  await click('#start-list button[data-id="project"]')
  await until(`document.querySelector("#start-list")?.innerText.includes("重試") || document.querySelector("#start-list")?.innerText.includes("Retry")`)
  assert.equal(firstAttempts,1,"a failed read must not automatically retry")
  assert.equal(await run(`document.querySelectorAll(".start-past-skeleton").length`),0)
  await click("#start-list button")
  await until(`!!document.querySelector('#start-list button[data-session="${firstID}"]')`)
  await click("#start-list button:not([data-session])")
  await until(`document.querySelector("#start-list")?.innerText.includes("重試") || document.querySelector("#start-list")?.innerText.includes("Retry")`)
  assert.equal(olderAttempts,1)
  assert.equal(await run(`document.querySelectorAll("#start-list button[data-session]").length`),1,"failure must retain the already loaded session")
  await click("#start-list button:not([data-session])")
  await until(`!!document.querySelector('#start-list button[data-session="${olderID}"]')`)
  assert.equal(await run(`document.querySelectorAll("#start-list button[data-session]").length`),2)
  assert.equal(olderAttempts,2,"retry should repeat only the failed page")
  if(shots){const {data}=await browser.send("Page.captureScreenshot",{format:"png"},s);writeFileSync(join(shots,"history-paging.png"),Buffer.from(data,"base64"))}
 }finally{await browser.send("Target.closeTarget",{targetId}).catch(()=>undefined)}
})

test("Remote Control sends text and shows quota failure with terminal controls hidden on desktop and phone", async () => {
 remoteControl=true
 try {
  for(const width of [1200,390]){
   const {targetId}=await browser.send("Target.createTarget",{url:"about:blank"})
   const {sessionId:s}=await browser.send("Target.attachToTarget",{targetId,flatten:true})
   const run=async(expression:string)=>{
    const {result,exceptionDetails}=await browser.send("Runtime.evaluate",{expression,returnByValue:true,awaitPromise:true},s)
    if(exceptionDetails)throw new Error(exceptionDetails.exception?.description??exceptionDetails.text)
    return result.value
   }
   try{
    await browser.send("Page.enable",{},s)
    await browser.send("Runtime.enable",{},s)
    await browser.send("Emulation.setDeviceMetricsOverride",{width,height:850,deviceScaleFactor:1,mobile:width<600},s)
    const mark=browser.loadCount
    await browser.send("Page.navigate",{url:origin+"/#session="+ID},s)
    await browser.loaded(s,mark)
    const deadline=Date.now()+10000
    while(Date.now()<deadline && !await run(`document.querySelector("#why")?.textContent.includes("Remote Control") && document.body.innerText.includes("Remote history fixture")`))await new Promise(r=>setTimeout(r,50))
    assert.match(await run(`document.querySelector("#why")?.textContent??document.body.innerText`),/Remote Control/)
    assert.match(await run(`document.body.innerText`),/Remote history fixture/)
    assert.equal(await run(`document.querySelector("#msg")?.isContentEditable`),true)
    assert.equal(await run(`document.querySelector("#attach")?.disabled`),true)
    assert.equal(await run(`document.querySelector("#send")?.disabled`),true)
    await run(`document.querySelector("#detail-actions-trigger").click()`)
    for(const id of ["session-focus","session-interrupt","session-screen"]){
     assert.equal(await run(`document.getElementById(${JSON.stringify(id)})?.hidden`),true)
     assert.equal(await run(`document.getElementById(${JSON.stringify(id)})?.getClientRects().length`),0,"unsupported control must actually be hidden")
    }
    assert.equal(await run(`document.querySelector("#session-end")?.disabled`),true)
    assert.equal(await run(`document.documentElement.scrollWidth <= innerWidth`),true)
    await run(`document.querySelector("#detail-actions-trigger").click()`)
    const prior=remoteWrites
    const text="RC UI fixture "+width
    await run(`(() => {const box=document.querySelector("#msg");box.textContent=${JSON.stringify(text)};box.dispatchEvent(new Event("input",{bubbles:true}));})()`)
    const sendReady=Date.now()+3000
    while(Date.now()<sendReady && await run(`document.querySelector("#send").disabled`))await new Promise(r=>setTimeout(r,30))
    assert.equal(await run(`document.querySelector("#send").disabled`),false)
    await run(`document.querySelector("#composer").requestSubmit()`)
    const delivered=Date.now()+5000
    while(Date.now()<delivered && (remoteWrites!==prior+1 || remoteText!==text || !await run(`document.body.innerText.includes("Weekly quota exhausted") && document.querySelectorAll(".entry.pending").length===0`)))await new Promise(r=>setTimeout(r,40))
    assert.equal(remoteWrites,prior+1,"one explicit submit must make exactly one write")
    assert.equal(remoteText,text)
    assert.equal(await run(`document.body.innerText.includes("Weekly quota exhausted") && document.querySelectorAll(".entry.pending").length===0`),true)
    if(shots){const {data}=await browser.send("Page.captureScreenshot",{format:"png"},s);writeFileSync(join(shots,"remote-control-"+width+".png"),Buffer.from(data,"base64"))}
   }finally{await browser.send("Target.closeTarget",{targetId}).catch(()=>undefined)}
  }
  assert.equal(remoteWrites,2,"desktop and phone each submitted one text event")
 }finally{remoteControl=false}
})
