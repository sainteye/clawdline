// The 驗收 page on a phone, through a real browser at 390px:
//
//   (cd web && npm run build)
//   node --test --experimental-strip-types web/console/src/pages/verify/verify.e2e.ts
//
// The built console is served by a stand-in daemon that answers the session
// list and the verification routes, and headless Chrome (CHROME, else the
// usual macOS install) loads it at 390×844. What is measured is what the
// person would see: no part of the list or of a record is wider than the
// screen, the ten-column comparison scrolls inside its own box, and each
// press sends the one request it names. VERIFY_SHOTS=<dir> also saves a
// picture of each screen there.
//
// Named `.e2e.ts` so the unit run over `*.test.ts` does not start a browser.
import { test, before, after } from "node:test"
import assert from "node:assert/strict"
import { spawn, type ChildProcess } from "node:child_process"
import { createServer, type IncomingMessage, type Server, type ServerResponse } from "node:http"
import { existsSync, mkdtempSync, readFileSync, rmSync, statSync, writeFileSync } from "node:fs"
import { tmpdir } from "node:os"
import { dirname, extname, join, normalize, resolve } from "node:path"
import { fileURLToPath } from "node:url"

const dist = resolve(dirname(fileURLToPath(import.meta.url)), "../../../dist")
const chrome = process.env.CHROME || "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
const shots = process.env.VERIFY_SHOTS || ""
const PHONE = { width: 390, height: 844, mobile: true }

const ID = "7e000000-0000-4000-8000-000000000006"
const NOW = Math.floor(Date.now() / 1000)

// A record as long as a real one gets: a title and criteria with no spaces to
// break at in one language and long words in the other, and every column of
// the comparison filled.
function record() {
  return {
    id: ID,
    title: "壓縮門檻實驗（300000）——這一行故意寫得很長，看看手機上會不會把畫面撐寬",
    why: "The compaction window was lowered to 300000 tokens for child tasks; this checks it paid.",
    started_at: NOW - 86_400,
    due_at: NOW + 6 * 86_400 + 3_600,
    criteria: [
      { index: 0, text: "300000 組每個 task 成本明顯低於 before-setting", state: "unset", updated_at: 0 },
      { index: 1, text: "成功率沒有下降、stalled 與重派沒有增加", state: "passed", updated_at: NOW },
      { index: 2, text: "抽查 3–5 個壓縮過的 task：沒有遺失指示或重做", state: "unset", updated_at: 0 },
    ],
    notes: [{ id: 1, at: NOW - 600, author_kind: "session", author: "task:" + "a".repeat(36), text: "readout: " + "x".repeat(300) }],
    source: { kind: "compaction_compare", since: "14d" },
    schedule_id: "sched-fixture",
    seed: "",
    status: "open",
    close_reason: "",
    closed_at: 0,
    created_at: NOW - 86_400,
    updated_at: NOW,
  }
}

function group(name: string, window: number) {
  return {
    group: name, window, sessions: 12, tasks: 12, read_tasks: 11, running: 1, ended: 11, success: 10, failure: 1,
    cancelled: 0, timeout: 0, stalled: 0, lost: 0, respawns: 1, success_rate: 0.91, failure_rate: 0.09,
    timeout_rate: 0, stalled_rate: 0, too_few: false, cost_total: 123.45, cost_median_per_task: 9.87,
    cost_known: false, calls_per_task: 123.4, compactions_per_task: 1.25, above_200k_share: 0.42,
    peak_context_max: 400000, peak_context_median: 250000,
  }
}

let asked: string[] = []
let sent: Record<string, unknown>[] = []

function json(res: ServerResponse, status: number, body: unknown) {
  res.writeHead(status, { "content-type": "application/json" })
  res.end(JSON.stringify(body))
}

function body(req: IncomingMessage): Promise<Record<string, unknown>> {
  return new Promise((ok) => {
    let raw = ""
    req.setEncoding("utf8")
    req.on("data", (chunk) => { raw += chunk })
    req.on("end", () => { try { ok(raw ? JSON.parse(raw) : {}) } catch { ok({}) } })
  })
}

function document(): string {
  const html = readFileSync(join(dist, "index.html"), "utf8")
  const words = JSON.parse(readFileSync(join(dist, "strings", "zh-Hant.json"), "utf8"))
  words.lang = "zh-Hant"
  words.dir = "ltr"
  const slot = "<script>window.__strings=" + JSON.stringify(words).replaceAll("</", "<\\/") + "</script>"
  return html.replace("<!-- clawdline:strings -->", slot).replace("<!-- clawdline:cloud -->", "")
}

const TYPES: Record<string, string> = { ".js": "text/javascript", ".css": "text/css", ".json": "application/json", ".svg": "image/svg+xml" }

function daemon(): Server {
  return createServer((req, res) => {
    const url = new URL(req.url ?? "/", "http://fixture")
    const path = url.pathname
    if (path.startsWith("/v1/verifications")) asked.push(`${req.method} ${path}${url.search}`)
    if (path === "/v1/sessions") {
      return json(res, 200, { at: Date.now(), scan: { complete: true, completed: { complete: true, sequence: 1 },
        emptyAuthoritative: true, epoch: 1, generation: 1, provenance: "fixture" }, sessions: [] })
    }
    if (path === "/v1/events") {
      res.writeHead(200, { "content-type": "text/event-stream", "cache-control": "no-cache" })
      return
    }
    if (path === "/v1/verifications" && req.method === "GET") {
      const closed = { ...record(), id: "closed-1", title: "An earlier check", status: "accepted", close_reason: "held", closed_at: NOW - 60 }
      return json(res, 200, { at: NOW, verifications: [record(), closed] })
    }
    if (path === `/v1/verifications/${ID}` && req.method === "GET") {
      return json(res, 200, { at: NOW, verification: record(), data: { kind: "compaction_compare", compaction_compare: {
        since: NOW - 14 * 86_400, until: NOW, min_tasks: 5, truncated: false, excluded: 3, excluded_truncated: false,
        excluded_tasks: [], not_recorded: [], groups: [group("none", 0), group("300000", 300000)],
      } } })
    }
    if (path.startsWith(`/v1/verifications/${ID}`) && req.method === "POST") {
      void body(req).then((b) => {
        sent.push({ path, key: req.headers["idempotency-key"], ...b })
        json(res, 200, { at: NOW, verification: record() })
      })
      return
    }
    if (path === `/v1/verifications/${ID}` && req.method === "DELETE") {
      res.writeHead(204)
      return res.end()
    }
    if (path.startsWith("/v1/")) return json(res, 404, { error: "not_found", message: path })
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

// ---- a browser, over the DevTools protocol (as session/address.e2e.ts)

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
  profile = mkdtempSync(join(tmpdir(), "clawdline-verify-"))
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

/** How wide the page and everything visible on it is, and what pokes past the screen. */
const WIDTH = `(() => {
  const page = document.getElementById("verify")
  const over = [...page.querySelectorAll("*")]
    .filter((n) => n.getClientRects().length && !n.closest(".verify-table table"))
    .filter((n) => n.getBoundingClientRect().right > innerWidth + 0.5)
    .map((n) => n.tagName.toLowerCase() + "." + [...n.classList].join(".") + " " + Math.round(n.getBoundingClientRect().right))
  return { inner: innerWidth, scroll: document.documentElement.scrollWidth, body: document.body.scrollWidth, over }
})()`

test("phone: the list and a record fit 390px, the table scrolls inside itself, and each press is its one request", async () => {
  const { targetId } = await browser.send("Target.createTarget", { url: "about:blank" })
  const { sessionId: s } = await browser.send("Target.attachToTarget", { targetId, flatten: true })
  const run = async (expression: string) => {
    const { result, exceptionDetails } = await browser.send("Runtime.evaluate",
      { expression, returnByValue: true, awaitPromise: true }, s)
    if (exceptionDetails) throw new Error(exceptionDetails.exception?.description ?? exceptionDetails.text)
    return result.value
  }
  const until = async (what: string, expression: string) => {
    const deadline = Date.now() + 5_000
    for (;;) {
      try { if (await run(expression)) return } catch { /* between documents */ }
      if (Date.now() > deadline) assert.fail(what)
      await new Promise((r) => setTimeout(r, 50))
    }
  }
  const shot = async (name: string) => {
    if (!shots) return
    const { data } = await browser.send("Page.captureScreenshot", { format: "png", captureBeyondViewport: true }, s)
    writeFileSync(join(shots, name + ".png"), Buffer.from(data, "base64"))
  }
  try {
    await browser.send("Page.enable", {}, s)
    await browser.send("Runtime.enable", {}, s)
    await browser.send("Emulation.setDeviceMetricsOverride", { ...PHONE, deviceScaleFactor: 1 }, s)
    const mark = browser.loadCount
    await browser.send("Page.navigate", { url: origin + "/#page=verify" }, s)
    await browser.loaded(s, mark)

    // The drawer names the page 驗收, and the list shows the open record with its countdown.
    await until("the list arrives", `!!document.querySelector("#verify-open .verify-row")`)
    assert.equal(await run(`document.getElementById("nav-verify")?.textContent?.trim()`), "驗收")
    assert.match(await run(`document.querySelector("#verify-open .verify-due").textContent`), /^還有 6 天 1 小時到期$/)
    assert.equal(await run(`document.querySelectorAll("#verify-open > li").length`), 1, "only the open record is listed")
    let width = await run(WIDTH)
    await shot("verify-list-390")
    assert.equal(width.inner, 390)
    assert.ok(width.scroll <= 390 && width.body <= 390, "the list is wider than the phone: " + JSON.stringify(width))
    assert.deepEqual(width.over, [])

    // The record: every block drawn, nothing wider than the screen, the table
    // wider than its box and scrolling inside it.
    await run(`document.querySelector("#verify-open .verify-row").click()`)
    await until("the record opens with its data", `!!document.querySelector("#verify-detail .verify-table table")`)
    width = await run(WIDTH)
    await shot("verify-detail-390")
    assert.ok(width.scroll <= 390 && width.body <= 390, "the record is wider than the phone: " + JSON.stringify(width))
    assert.deepEqual(width.over, [])
    const table = await run(`(() => { const b = document.querySelector(".verify-table"); return { box: b.clientWidth, inner: b.scrollWidth, overflow: getComputedStyle(b).overflowX } })()`)
    assert.ok(table.inner > table.box && table.overflow === "auto", "the table does not scroll inside itself: " + JSON.stringify(table))
    assert.equal(await run(`document.querySelectorAll(".verify-table tbody tr").length`), 2)
    assert.equal(await run(`document.querySelector(".verify-table tbody th").textContent`), "before-setting")

    // A criterion is marked with one press, on its own index.
    sent = []
    await run(`document.querySelector(".verify-criteria > li:nth-child(1) .verify-actions button").click()`)
    await until("the criterion is sent", `true`)
    await new Promise((r) => setTimeout(r, 200))
    assert.equal(sent.length, 1)
    assert.equal(sent[0].path, `/v1/verifications/${ID}/criteria/0`)
    assert.equal(sent[0].state, "passed")
    assert.match(String(sent[0].key), /^web-/)

    // A verdict without a reason is not sent.
    sent = []
    await run(`document.getElementById("verify-accepted").click()`)
    await until("the page asks for a reason", `document.querySelector("#verify-detail .verify-failure")?.textContent === "先寫下原因。"`)
    assert.equal(sent.length, 0)

    // Delete asks first, says it is still open, and sends force only then.
    asked = []
    await run(`document.getElementById("verify-remove").click()`)
    await until("the confirmation is shown", `!!document.getElementById("verify-remove-ask")`)
    assert.match(await run(`document.getElementById("verify-remove-ask").textContent`), /還沒結案/)
    width = await run(WIDTH)
    await shot("verify-delete-390")
    assert.deepEqual(width.over, [])
    assert.equal(asked.filter((a) => a.startsWith("DELETE")).length, 0, "nothing is deleted before the second press")
    await run(`document.querySelector("#verify-remove-ask + .verify-actions button").click()`)
    await until("the page goes back to the list", `!document.getElementById("verify-detail")`)
    assert.deepEqual(asked.filter((a) => a.startsWith("DELETE")), [`DELETE /v1/verifications/${ID}?force=1`])
  } finally {
    await browser.send("Target.closeTarget", { targetId }).catch(() => undefined)
  }
})
