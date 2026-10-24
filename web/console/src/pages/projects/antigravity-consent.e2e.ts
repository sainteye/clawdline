// Manual browser acceptance: node --experimental-strip-types --test this file.
// Uses a task-owned Chrome profile and synthetic HTTP responses, never a
// provider, personal credential or the running daemon. This is component
// acceptance, not deployed Cloud or full Project-settings acceptance.
import { test } from "node:test"
import assert from "node:assert/strict"
import { spawn } from "node:child_process"
import { createServer } from "node:http"
import { mkdtempSync, readFileSync, rmSync, writeFileSync, mkdirSync } from "node:fs"
import { tmpdir } from "node:os"
import { dirname, join, resolve } from "node:path"
import { fileURLToPath } from "node:url"
import { build } from "esbuild"
const here = dirname(fileURLToPath(import.meta.url))
const src = resolve(here, "../..")
const chrome = process.env.CHROME || "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
const shots = process.env.CLAWDLINE_SHOTS || ""
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

test("Antigravity consent has an explicit scope and recovers a lost answer on desktop and phone", { timeout: 90000 }, async () => {
 const built = await build({ stdin: { contents: `
 import React from "react"; import { createRoot } from "react-dom/client";
 import { AntigravityConsent } from "./AntigravityConsent.tsx";
 import { activateCatalog } from "../../catalog.ts";
 import en from "../../../public/catalogs/en.json"; import zh from "../../../public/catalogs/zh-Hant.json";
 const lang = new URL(location.href).searchParams.get("lang") === "zh-Hant" ? "zh-Hant" : "en";
 activateCatalog(lang === "en" ? en : zh, lang);
 createRoot(document.getElementById("root")).render(<AntigravityConsent place="synthetic-project" onBusy={busy => { window.fixtureBusy = busy }} />);
 `, resolveDir: here, sourcefile: "consent-fixture.tsx", loader: "tsx" }, bundle: true, write: false, format: "esm", jsx: "automatic", loader: { ".css": "empty" }, logLevel: "silent" })
 const script = built.outputFiles[0].text
 const css = ["legacy/tokens.css", "legacy/base.css", "pages/projects/antigravity-consent.css"].map(p => readFileSync(join(src, p), "utf8")).join("\n")
 const preview = { before_digest: "absent", helper_digest: "a".repeat(64), managed_digest: "b".repeat(64) }
 const scope = "project-and-linked-worktrees"
 let state: any, lose = false, unreadable = false, submissions: { input: any; key: string }[] = [], receipts = new Set<string>()
 const server = createServer(async (req, res) => {
  const url = new URL(req.url || "/", "http://127.0.0.1")
  if (url.pathname === "/script.js") { res.writeHead(200, { "content-type": "text/javascript" }); res.end(script); return }
  if (url.pathname === "/v1/projects/synthetic-project/antigravity-consent") {
   if (req.method === "POST") {
    let body = ""; for await (const part of req) body += part
    const input = JSON.parse(body), key = String(req.headers["idempotency-key"])
    submissions.push({ input, key })
    if (!receipts.has(key)) { receipts.add(key); state = { phase: input.action === "enable" ? "active" : "disabled", revision: state.revision + (input.action === "enable" ? 2 : 1), scope, hooks: input.action === "enable" ? "installed" : "removed", preview } }
    if (lose) { lose = false; res.writeHead(503, { "content-type": "application/json" }); res.end(JSON.stringify({error:"acknowledgment_unknown"})); return }
   }
   res.writeHead(unreadable ? 503 : 200, { "content-type": "application/json" }); res.end(JSON.stringify(unreadable ? { error: "unavailable" } : state)); return
  }
  res.writeHead(200, { "content-type": "text/html" }); res.end(`<html><head><meta name="viewport" content="width=device-width,initial-scale=1"><style>${css}body{overflow:auto}#root{padding:16px;max-width:740px;margin:auto}</style></head><body><div id="root"></div><script type="module" src="/script.js"></script></body></html>`)
 })
 await new Promise<void>(ok => server.listen(0, "127.0.0.1", ok))
 const address = server.address(); assert.ok(address && typeof address !== "string")
 const origin = `http://127.0.0.1:${address.port}`
 const profile = mkdtempSync(join(tmpdir(), "clawdline-consent-browser-"))
 const process = spawn(chrome, ["--headless=new", "--remote-debugging-port=0", "--user-data-dir="+profile, "--no-first-run", "--no-default-browser-check", "about:blank"])
 let browser: Browser | undefined
 try {
  const endpoint = await new Promise<string>((ok, fail) => { const timer = setTimeout(() => fail(new Error("Chrome startup timed out")), 15000); let said = ""; process.stderr?.on("data", chunk => { said += String(chunk); const m = /DevTools listening on (ws:\/\/\S+)/.exec(said); if (m) { clearTimeout(timer); ok(m[1]) } }); process.once("error", e => { clearTimeout(timer); fail(e) }) })
  browser = await Browser.connect(endpoint)
  for (const width of [390, 1280]) for (const theme of ["light", "dark"]) for (const lang of ["en", "zh-Hant"]) {
   state = { phase: "absent", revision: 0, scope, hooks: "removed", preview }; lose = false; unreadable = false; submissions = []; receipts.clear()
   const { targetId } = await browser.send("Target.createTarget", { url: "about:blank" })
   const { sessionId }: { sessionId: string } = await browser.send("Target.attachToTarget", { targetId, flatten: true })
   const send = (method: string, params: object = {}): Promise<any> => browser!.send(method, params, sessionId)
   const run = async (expression: string): Promise<any> => { const out = await send("Runtime.evaluate", { expression, returnByValue: true, awaitPromise: true }); if (out.exceptionDetails) throw new Error(out.exceptionDetails.text); return out.result.value }
   const until = async (expression: string) => { const deadline = Date.now()+8000; while (!(await run(expression))) { if (Date.now()>deadline) throw new Error("UI did not settle: "+expression+"; submissions="+JSON.stringify(submissions)+"; screen="+await run("document.body.innerText")); await new Promise(ok=>setTimeout(ok,25)) } }
   try {
    await send("Page.enable"); await send("Runtime.enable")
    await send("Emulation.setDeviceMetricsOverride", { width, height: 844, mobile: width===390, deviceScaleFactor: 1 })
    await send("Emulation.setEmulatedMedia", { features: [{ name: "prefers-color-scheme", value: theme }] })
    await send("Page.navigate", { url: origin+"/?lang="+lang })
    await until(`!!document.querySelector('input[type=checkbox]')`)
    assert.equal(await run(`document.documentElement.lang`),lang)
    assert.equal(submissions.length,0)
    assert.ok(await run(`document.querySelector('.antigravity-consent-actions button').disabled`))
    assert.ok(await run(`document.documentElement.scrollWidth <= innerWidth`))
    assert.ok(await run(`Array.from(document.querySelectorAll('button')).every(b=>b.getBoundingClientRect().height>=44)`))
    assert.ok(await run(`document.body.innerText.includes(${JSON.stringify(lang==='en'?'verified linked worktrees':'已驗證')})`))
    if (shots) { mkdirSync(shots,{recursive:true}); const shot = await send("Page.captureScreenshot",{format:"png",captureBeyondViewport:true});writeFileSync(join(shots,`consent-${width}-${theme}-${lang}.png`),Buffer.from(shot.data,"base64")) }
    await run(`document.querySelector('input').click()`)
    await until(`!document.querySelector('.antigravity-consent-actions button').disabled`)
    lose=true
    await run(`document.querySelector('.antigravity-consent-actions button').click()`)
    await until(`!!document.querySelector('[role=alert]') && !window.fixtureBusy`)
    assert.equal(submissions.length,1); assert.equal(state.phase,"active")
    assert.equal(await run(`document.querySelector('input').checked`),false)
    await run(`document.querySelector('[role=alert] button').click()`)
    await until(`!document.querySelector('[role=alert]') && !window.fixtureBusy`)
    assert.equal(submissions.length,2);assert.deepEqual(submissions[0],submissions[1]);assert.equal(state.revision,2)
    await run(`document.querySelector('.antigravity-consent-actions button:last-child').click()`)
    await until(`!window.fixtureBusy && document.querySelector('.antigravity-consent-files')?.textContent.includes(${JSON.stringify(lang==='en'?'No Clawdline':'已沒有')})`)
    assert.equal(state.phase,"disabled"); assert.equal(submissions.length,3)
    unreadable=true
    await run(`document.querySelector('.antigravity-consent-head button').click()`)
    await until(`!document.querySelector('input')`)
    assert.equal(submissions.length,3)
   } finally { await browser.send("Target.closeTarget",{targetId}) }
  }
 } finally {
  browser?.close()
  if (process.exitCode === null) { const gone=new Promise(ok=>process.once("exit",ok));process.kill();await gone }
  server.closeAllConnections();await new Promise<void>(ok=>server.close(()=>ok()))
  rmSync(profile,{recursive:true,force:true,maxRetries:5})
 }
})
