// Build the console first, then run with `node --test` to exercise a real
// desktop and phone viewport against a stand-in daemon.
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
const shots = process.env.HUMAN_INTERVENTION_SHOTS || ""
const SESSION = "fixture-note-pane"
const CONVERSATION = "10000000-0000-4000-8000-000000000031"
const NOTE = "10000000-0000-4000-8000-000000000032"
const now = Math.floor(Date.now() / 1000)
let sendRequests = 0
let failReads = false
let failActions = false
let readDelay = 0
let noteCount = 1
const row = {
  id: SESSION, label: "Delivery Session", backend: "owned", state: "idle", work_state: "ready",
  evidence: "process", assistant: "codex", sessionId: CONVERSATION, cwd: "/tmp/fixture",
  agents_reading: { state: "complete" },
  agents: [{ id: "fixture-agent", at: now, depth: 1, type: "Explore", what: "Inspect fixture", state: "done" }],
  closeability: { activity_generation: 1, attestation_id: null, mover: null, obligation_generation: 1,
    observed_at: 1, provenance: [], reasons: [], session_generation: 1, source: "broker", state: "safe", version: "fixture" },
}
const snapshot = () => ({ at: Date.now(), scan: { complete: true, completed: { complete: true, sequence: 1 },
  emptyAuthoritative: true, epoch: 1, generation: 1, provenance: "fixture" }, sessions: [row] })
const TYPES: Record<string, string> = { ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".json": "application/json", ".webp": "image/webp" }
function json(res: ServerResponse, status: number, value: unknown) { res.writeHead(status, { "content-type": "application/json" }); res.end(JSON.stringify(value)) }
function document(): string {
  const html = readFileSync(join(dist, "index.html"), "utf8")
  const words = JSON.parse(readFileSync(join(dist, "strings", "zh-Hant.json"), "utf8"))
  words.lang = "zh-Hant"; words.dir = "ltr"
  return html.replace("<!-- clawdline:strings -->", "<script>window.__strings=" + JSON.stringify(words).replaceAll("</", "<\\/") + "</script>")
    .replace("<!-- clawdline:cloud -->", "")
}
function fixture(): Server {
  let readAt: number | null = null
  let resolvedAt: number | null = null
  let version = 1
  return createServer((req, res) => {
    const path = new URL(req.url ?? "/", "http://fixture").pathname
    if (path === "/__fixture/reset") { readAt = null; resolvedAt = null; version = 1; failReads = false; failActions = false; readDelay = 0; noteCount = 1; sendRequests = 0; return json(res, 200, { ok: true }) }
    if (path === "/__fixture/count") { noteCount = Number(new URL(req.url ?? "/", "http://fixture").searchParams.get("value")) || 1; return json(res, 200, { ok: true }) }
    if (path === "/__fixture/fail-reads") { failReads = new URL(req.url ?? "/", "http://fixture").searchParams.get("on") === "1"; return json(res, 200, { ok: true }) }
    if (path === "/__fixture/fail-actions") { failActions = new URL(req.url ?? "/", "http://fixture").searchParams.get("on") === "1"; return json(res, 200, { ok: true }) }
    if (path === "/__fixture/read-delay") { readDelay = Number(new URL(req.url ?? "/", "http://fixture").searchParams.get("ms")) || 0; return json(res, 200, { ok: true }) }
    if (path === "/v1/sessions") return json(res, 200, snapshot())
    if (path === "/v1/events") {
      res.writeHead(200, { "content-type": "text/event-stream", "cache-control": "no-cache" })
      res.write("event: sessions\ndata: " + JSON.stringify(snapshot()) + "\n\n")
      return
    }
    if (path === "/v1/transcript") return json(res, 200, { entries: [], evidence: "process", id: SESSION, signature: "fixture" })
    if (path.startsWith("/v1/work/v2/session-todos/")) return json(res, 200, { ok: true, direct_todos: [], assigned_items: [], recent_items: [], truncated: false })
    if (path.startsWith("/v1/work/v2/human-interventions/")) {
      const note = { id: NOTE, source_conversation: "10000000-0000-4000-8000-000000000001", source_label: "Manager Session",
        target_conversation: CONVERSATION, target_session: SESSION, kind: "answer", title: "Choose a release day",
        summary: "The release is waiting for a date.", action: "Choose a date.", reason: "Only the person knows the preferred date.",
        detail: ("The report is ready. Please review the proposed date and the linked evidence.\n").repeat(36), options: [
          { label: "Tuesday", draft: "Tuesday works for me." }, { label: "Wednesday", draft: "Wednesday works for me." }],
        created_at: now, read_at: readAt, resolved_at: resolvedAt, version }
      if (req.method === "GET") {
        const answer = () => failReads ? json(res, 503, { error: "read_failed" }) : json(res, 200, { ok: true, rows: Array.from({ length: noteCount }, (_, index) => ({ ...note, id: `10000000-0000-4000-8000-${String(32 + index).padStart(12, "0")}` })), pruned_resolved: 0 })
        if (readDelay) setTimeout(answer, readDelay); else answer()
        return
      }
      if (req.method === "POST") {
        const chunks: Buffer[] = []
        req.on("data", (c: Buffer) => chunks.push(c))
        req.on("end", () => {
          if (failActions) { setTimeout(() => json(res, 503, { error: "action_failed" }), 200); return }
          const body = JSON.parse(Buffer.concat(chunks).toString("utf8"))
          if (body.expected_version !== version) return json(res, 409, { error: "version_conflict" })
          if (path.endsWith("/read")) readAt = now
          else if (path.endsWith("/resolve")) { readAt = now; resolvedAt = now }
          else if (path.endsWith("/reopen")) resolvedAt = null
          else return json(res, 404, { error: "not_found" })
          version++
          return json(res, 200, { ok: true, note: { ...note, read_at: readAt, resolved_at: resolvedAt, version } })
        })
        return
      }
    }
    if (path.startsWith("/v1/")) {
      if (req.method === "POST" && (path.endsWith("/send") || path === "/v1/messages")) sendRequests++
      return json(res, 404, { error: "not_found", detail: path })
    }
    if (path === "/") { res.writeHead(200, { "content-type": "text/html; charset=utf-8" }); return res.end(document()) }
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
      this.pending.set(id, { ok: (v) => { clearTimeout(timer); ok(v) }, fail: (e) => { clearTimeout(timer); fail(e) } })
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
  profile = mkdtempSync(join(tmpdir(), "clawdline-human-interventions-"))
  browserProcess = spawn(chrome, ["--headless=new", "--remote-debugging-port=0", "--user-data-dir=" + profile, "--no-first-run", "--no-default-browser-check", "about:blank"])
  const endpoint = await new Promise<string>((ok, fail) => {
    let said = ""; const timer = setTimeout(() => { browserProcess.kill(); fail(new Error("Chrome did not start: " + said)) }, 20_000)
    browserProcess.stderr?.on("data", (chunk) => { said += String(chunk); const match = /DevTools listening on (ws:\/\/\S+)/.exec(said); if (match) { clearTimeout(timer); ok(match[1]) } })
  })
  browser = await Browser.connect(endpoint)
})
after(async () => {
  browser?.close()
  if (browserProcess && browserProcess.exitCode === null) {
    const gone = new Promise((ok) => browserProcess.once("exit", ok))
    browserProcess.kill()
    await Promise.race([gone, new Promise((ok) => setTimeout(ok, 5000))])
  }
  server?.closeAllConnections(); await new Promise<void>((ok) => server ? server.close(() => ok()) : ok())
  if (profile) rmSync(profile, { recursive: true, force: true, maxRetries: 5 })
})
for (const [name, width, height] of [["desktop", 1280, 800], ["phone", 390, 844]] as const) {
  test(name + ": the person reads and composes without accidental sending", async () => {
    const { targetId } = await browser.send("Target.createTarget", { url: "about:blank" })
    const { sessionId: id } = await browser.send("Target.attachToTarget", { targetId, flatten: true })
    const run = async (expression: string) => {
      const { result, exceptionDetails } = await browser.send("Runtime.evaluate", { expression, returnByValue: true, awaitPromise: true }, id)
      if (exceptionDetails) throw new Error(exceptionDetails.exception?.description ?? exceptionDetails.text)
      return result.value
    }
    const until = async (expression: string) => {
      const deadline = Date.now() + 8_000
      while (!(await run(expression))) { if (Date.now() > deadline) assert.fail("UI did not reach " + expression); await new Promise((r) => setTimeout(r, 50)) }
    }
    try {
      await fetch(origin + "/__fixture/reset")
      await fetch(origin + "/__fixture/read-delay?ms=1200")
      await browser.send("Page.enable", {}, id); await browser.send("Runtime.enable", {}, id)
      await browser.send("Emulation.setDeviceMetricsOverride", { width, height, deviceScaleFactor: 2, mobile: name === "phone" }, id)
      const mark = browser.loadCount; await browser.send("Page.navigate", { url: origin + "/#session=" + SESSION }, id); await browser.loaded(id, mark)
      await until(`!!document.querySelector(".human-interventions-head")`)
      assert.match(await run(`document.querySelector(".human-interventions-head").getAttribute("aria-label")`), /載入中/)
      assert.doesNotMatch(await run(`document.querySelector(".human-interventions-head").getAttribute("aria-label")`), /0 筆/)
      await until(`!!document.querySelector(".human-interventions-dot") && !!document.querySelector(".session-todos-empty")`)
      await fetch(origin + "/__fixture/read-delay?ms=0")
      assert.match(await run(`document.querySelector(".human-interventions-live").textContent`), /1 筆未處理/)
      assert.equal(await run(`document.querySelector(".session-todos").open`), false)
      assert.equal(await run(`document.querySelector(".human-interventions-head").getAttribute("aria-expanded")`), "false")
      assert.equal(await run(`!!document.querySelector(".human-intervention-card")`), false)
      assert.equal(await run(`document.querySelector(".session-todos > summary").contains(document.querySelector(".human-interventions-head"))`), true)
      if (shots) { const { data } = await browser.send("Page.captureScreenshot", { format: "png" }, id); writeFileSync(join(shots, name + "-collapsed.png"), Buffer.from(data, "base64")) }
      await browser.send("Target.activateTarget", { targetId })
      await run(`document.querySelector(".human-interventions-head").focus()`)
      assert.equal(await run(`document.activeElement?.classList.contains("human-interventions-head")`), true)
      await browser.send("Input.dispatchKeyEvent", { type: "keyDown", key: "Enter", code: "Enter", text: "\r", windowsVirtualKeyCode: 13, nativeVirtualKeyCode: 13 }, id)
      await browser.send("Input.dispatchKeyEvent", { type: "keyUp", key: "Enter", code: "Enter", windowsVirtualKeyCode: 13 }, id)
      await until(`!!document.querySelector(".human-intervention-card")`)
      assert.equal(await run(`document.querySelector(".session-todos").open`), false)
      assert.match(await run(`document.querySelector(".human-intervention-card").innerText`), /Manager Session/)
      assert.match(await run(`document.querySelector(".human-intervention-options .human-intervention-explain").textContent`), /按「送出」才會傳送/)
      assert.equal(await run(`document.querySelector(".human-intervention-options .human-intervention-explain").getBoundingClientRect().top < document.querySelector(".human-intervention-option button").getBoundingClientRect().top`), true)
      assert.equal(await run(`document.querySelector(".human-intervention-actions").previousElementSibling.textContent.includes("不等於回覆或核准")`), true)
      const accessibility = await browser.send("Accessibility.getFullAXTree", {}, id)
      assert.ok(accessibility.nodes.some((node: any) => node.role?.value === "heading" && node.name?.value === "Choose a release day"))
      assert.ok(accessibility.nodes.some((node: any) => node.role?.value === "button" && node.name?.value === "需要你關注，1 筆未處理便條"))
      assert.equal(await run(`document.documentElement.scrollWidth <= ${width}`), true)
      if (shots) { const { data } = await browser.send("Page.captureScreenshot", { format: "png" }, id); writeFileSync(join(shots, name + "-note.png"), Buffer.from(data, "base64")) }
      await run(`document.querySelector(".human-intervention-card details summary").click()`)
      assert.equal(await run(`document.documentElement.scrollWidth <= ${width}`), true)
      await run(`document.getElementById("msg").textContent = "Existing draft."`)
      await run(`document.querySelector(".human-intervention-option button").click()`)
      await until(`document.getElementById("msg").textContent.includes("Tuesday works for me.")`)
      assert.match(await run(`document.getElementById("msg").textContent`), /Existing draft/)
      await run(`document.dispatchEvent(new CustomEvent("clawdline:intervention-compose", { detail: { target: { machine: "another-machine", session: ${JSON.stringify(SESSION)}, conversation: ${JSON.stringify(CONVERSATION)} }, text: "WRONG MACHINE" } }))`)
      assert.doesNotMatch(await run(`document.getElementById("msg").textContent`), /WRONG MACHINE/)
      await run(`document.querySelector(".human-intervention-actions button:nth-child(1)").click()`)
      await until(`document.querySelector(".human-intervention-card").innerText.includes("已閱讀")`)
      assert.equal(await run(`!!document.querySelector(".human-interventions-dot")`), true, "reading alone does not clear attention")
      await until(`document.activeElement?.classList.contains("human-intervention-card")`)
      assert.match(await run(`document.querySelector(".human-intervention-card").innerText`), /標記已處理/)
      await run(`fetch("/__fixture/fail-actions?on=1")`)
      await run(`document.querySelector(".human-intervention-actions button:last-child").click(); document.querySelector(".human-interventions-head").click()`)
      await until(`document.querySelector(".human-interventions-head").innerText.includes("操作失敗")`)
      assert.equal(await run(`document.querySelector(".human-interventions-head").getAttribute("aria-expanded")`), "false")
      assert.match(await run(`document.querySelector(".human-interventions-head").getAttribute("aria-label")`), /操作失敗/)
      assert.equal(await run(`!!document.querySelector(".human-interventions-live[role=alert]")`), true)
      await run(`fetch("/__fixture/fail-actions?on=0"); document.querySelector(".human-interventions-head").click()`)
      await until(`!!document.querySelector(".human-intervention-card")`)
      await run(`document.getElementById("bg-strip-line").click()`)
      await until(`!!document.querySelector("#bg-sheet .agents .one.child")`)
      await run(`document.querySelector("#bg-sheet .agents .one.child").click()`)
      await until(`!document.getElementById("msg")`)
      await run(`document.querySelector(".human-intervention-option button").click()`)
      await until(`!!document.getElementById("msg") && document.getElementById("msg").textContent.includes("Tuesday works for me.")`)
      assert.equal(await run(`document.activeElement?.id`), "msg")
      assert.match(await run(`document.getElementById("msg").textContent`), /Existing draft/)
      await run(`fetch("/__fixture/fail-reads?on=1")`)
      await run(`document.querySelector(".human-intervention-actions button").click()`)
      await until(`!!document.querySelector(".human-interventions-error") && document.querySelector(".human-intervention-option button").disabled`)
      assert.equal(await run(`document.querySelector(".human-intervention-actions button").disabled`), true)
      await run(`fetch("/__fixture/fail-reads?on=0"); document.querySelector(".human-interventions-error button").click()`)
      await until(`!document.querySelector(".human-interventions-error") && !document.querySelector(".human-interventions-dot")`)
      assert.equal(await run(`!!document.querySelector(".human-interventions-recent")`), true)
      assert.equal(await run(`document.querySelector(".human-interventions-head").getAttribute("aria-expanded")`), "true")
      assert.equal(await run(`document.querySelectorAll("#tx .entry").length`), 0)
      assert.equal(sendRequests, 0, "choosing a suggested reply must not send it")
      await fetch(origin + "/__fixture/reset")
      await fetch(origin + "/__fixture/count?value=8")
      const nextLoad = browser.loadCount
      await browser.send("Page.reload", {}, id); await browser.loaded(id, nextLoad)
      await until(`!!document.querySelector(".human-interventions-dot")`)
      assert.equal(await run(`!!document.querySelector(".human-intervention-card")`), false)
      await run(`document.querySelector(".human-interventions-head").click()`)
      await until(`document.querySelectorAll(".human-intervention-card").length === 8`)
      assert.equal(await run(`document.querySelector(".human-interventions-body").scrollHeight > document.querySelector(".human-interventions-body").clientHeight`), true)
      assert.equal(await run(`document.documentElement.scrollWidth <= ${width}`), true)
    } finally { await browser.send("Target.closeTarget", { targetId }) }
  })
}
