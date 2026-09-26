// The Status Line on a phone while a deploy is running, through a real browser at 390px:
//
//   (cd web && npm run build)
//   node --test --experimental-strip-types web/console/src/session/status-line-deploy.e2e.ts
//
// The built console is served by a stand-in daemon that answers one session,
// its `/info` and its `/git`, and headless Chrome (CHROME, else the usual
// macOS install) loads it at 390×844. With a deploy running, the chip takes
// the space the context reading and the working tree held; with none, both
// are back. STATUS_SHOTS=<dir> also saves a picture of each footer.
//
// Named `.e2e.ts` so the unit run over `*.test.ts` does not start a browser.
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
const PHONE = { width: 390, height: 844, mobile: true }

const ID = "ttys019"
const NOW = Math.floor(Date.now() / 1000)
// The label the screenshot that asked for this carried: long enough, with the
// branch and two plan windows beside it, to run the chip over the windows.
const LABEL = "deploy·marketing"

let deploying = true

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
    sessions: [ROW],
  }
}

function info() {
  const links = deploying
    ? [{ kind: "deploy", label: LABEL, state: "running", startedAt: NOW - 60, typicalSeconds: 300,
        url: "https://example.com/run" }]
    : []
  return {
    info: {
      session: { id: ID, title: ROW.label, assistant: "claude", namingAssistant: "claude", sessionId: ROW.sessionId,
        cwd: ROW.cwd, model: "claude-opus-5-5" },
      models: [{ id: "claude-opus-5-5", name: "Opus 5.5" }],
      context: { usedPercent: 14 },
      usage: { costUsd: 2.59 },
      limits: { ageSeconds: 5, at: NOW, readAtMs: Date.now(), windows: [
        { name: "5h", usedPercent: 42, hit: false }, { name: "7d", usedPercent: 41, hit: false },
      ] },
      deploy: links,
      links,
    },
  }
}

const GIT = {
  git: {
    ahead: 0, behind: 0, branch: "feature/a-branch-long-enough-to-matter", clean: false, head: "0123456789abcdef",
    files: [{ path: "a.txt", kind: "untracked", staged: false, unstaged: true }],
  },
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
    if (path === "/v1/sessions") return json(res, 200, snapshot())
    if (path === "/v1/events") {
      res.writeHead(200, { "content-type": "text/event-stream", "cache-control": "no-cache" })
      res.write("event: sessions\ndata: " + JSON.stringify(snapshot()) + "\n\n")
      return
    }
    if (path === `/v1/sessions/${ID}/info`) return json(res, 200, info())
    if (path === `/v1/sessions/${ID}/git`) return json(res, 200, GIT)
    if (path === "/v1/transcript") return json(res, 200, { entries: [], evidence: "process", id: ID, signature: "fixture" })
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
  profile = mkdtempSync(join(tmpdir(), "clawdline-status-"))
  browserProcess = spawn(chrome, ["--headless=new", "--remote-debugging-port=0", "--user-data-dir=" + profile,
    "--no-first-run", "--no-default-browser-check", "about:blank"])
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

/** What the footer shows: each cell drawn or not, and where the chip and the windows sit. */
const FOOTER = `(() => {
  const foot = document.getElementById("status-line")
  const shown = (sel) => { const n = foot.querySelector(sel); return !!n && n.getClientRects().length > 0 }
  const box = (sel) => { const n = foot.querySelector(sel); if (!n) return null; const r = n.getBoundingClientRect(); return { left: r.left, right: r.right } }
  const label = foot.querySelector(".deploy .label")
  return {
    deploy: shown(".deploy"), context: shown(".context"), files: shown(".files"),
    model: shown(".model"), cost: shown(".cost"), limits: foot.querySelector(".limits").textContent.trim(),
    chip: box(".deploy"), windows: box(".limits"), labelEllipsised: !!label && label.scrollWidth > label.clientWidth,
  }
})()`

test("phone: a running deploy hides the context and the tree, and neither comes back until it ends", async () => {
  const { targetId } = await browser.send("Target.createTarget", { url: "about:blank" })
  const { sessionId: s } = await browser.send("Target.attachToTarget", { targetId, flatten: true })
  const run = async (expression: string) => {
    const { result, exceptionDetails } = await browser.send("Runtime.evaluate",
      { expression, returnByValue: true, awaitPromise: true }, s)
    if (exceptionDetails) throw new Error(exceptionDetails.exception?.description ?? exceptionDetails.text)
    return result.value
  }
  const until = async (what: string, expression: string) => {
    const deadline = Date.now() + 8_000
    for (;;) {
      try { if (await run(expression)) return } catch { /* between documents */ }
      if (Date.now() > deadline) assert.fail(what + ": " + JSON.stringify(await run(FOOTER).catch((e) => String(e))))
      await new Promise((r) => setTimeout(r, 50))
    }
  }
  const shot = async (name: string) => {
    if (!shots) return
    const { data } = await browser.send("Page.captureScreenshot", { format: "png" }, s)
    writeFileSync(join(shots, name + ".png"), Buffer.from(data, "base64"))
  }
  try {
    await browser.send("Page.enable", {}, s)
    await browser.send("Runtime.enable", {}, s)
    await browser.send("Emulation.setDeviceMetricsOverride", { ...PHONE, deviceScaleFactor: 2 }, s)

    deploying = true
    let mark = browser.loadCount
    await browser.send("Page.navigate", { url: origin + "/#session=" + ID }, s)
    await browser.loaded(s, mark)
    await until("the chip is drawn", `(() => { const n = document.querySelector("#status-line .deploy"); return !!n && n.getClientRects().length > 0 })()`)
    await until("the windows are drawn", `/7d/.test(document.querySelector("#status-line .limits").textContent)`)
    // The tree is \`hidden\` until \`/git\` answers; wait for its branch to be in the
    // markup, so what hides it below is the stylesheet and not the wait.
    await until("the tree has answered", `/a-branch/.test(document.querySelector("#status-line .files .branch")?.textContent ?? "")`)
    let seen = await run(FOOTER)
    await shot("status-deploy-390")
    assert.equal(seen.context, false, "ctx is still drawn beside a running deploy: " + JSON.stringify(seen))
    assert.equal(seen.files, false, "the tree is still drawn beside a running deploy: " + JSON.stringify(seen))
    assert.equal(seen.cost, true, "the cost stays: " + JSON.stringify(seen))
    assert.equal(seen.labelEllipsised, true, "the long label is cut inside the chip: " + JSON.stringify(seen))
    assert.match(seen.limits, /5h\s*42%/)
    assert.match(seen.limits, /7d\s*41%/)
    assert.ok(seen.chip.right <= seen.windows.left + 0.5, "the chip runs over the plan windows: " + JSON.stringify(seen))
    assert.ok(seen.windows.right <= 390.5, "the windows run off the screen: " + JSON.stringify(seen))

    // Nothing running: the same session, read again, has both cells back.
    deploying = false
    mark = browser.loadCount
    await browser.send("Page.reload", {}, s)
    await browser.loaded(s, mark)
    await until("the context is back", `(() => { const n = document.querySelector("#status-line .context"); return !!n && n.getClientRects().length > 0 })()`)
    await until("the tree is back", `(() => { const n = document.querySelector("#status-line .files"); return !!n && n.getClientRects().length > 0 })()`)
    seen = await run(FOOTER)
    await shot("status-idle-390")
    assert.equal(seen.deploy, false)
    assert.equal(await run(`document.getElementById("status-line").hasAttribute("data-deploy")`), false)
  } finally {
    await browser.send("Target.closeTarget", { targetId }).catch(() => undefined)
  }
})
