// A card that is still sending when the page is reloaded, through a real
// browser and a real `localStorage` (F4):
//
//   (cd web && npm run build)
//   node --test web/console/src/session/pending-reload.e2e.ts
//
// The built console is served by a stand-in daemon whose send never answers —
// which is the state the card is lost in — and headless Chrome (CHROME, else
// the usual macOS install) reloads the page on top of it. What is checked is
// not only that the words come back: it is that the card comes back under the
// **same request**, so that "send again" is still answered by the Mac's receipt
// for the first attempt rather than typed a second time (F2). That is the whole
// point of keeping the card, and only a real browser can show it, because what
// keeps it is the browser's own store.
//
// Named `.e2e.ts` rather than `.test.ts` so that the unit run over
// `session/*.test.ts` does not start a browser.
import { test, before, after } from "node:test"
import assert from "node:assert/strict"
import { spawn, type ChildProcess } from "node:child_process"
import { createServer, type IncomingMessage, type Server, type ServerResponse } from "node:http"
import { existsSync, mkdtempSync, readFileSync, rmSync, statSync } from "node:fs"
import { tmpdir } from "node:os"
import { dirname, extname, join, normalize, resolve } from "node:path"
import { fileURLToPath } from "node:url"

const dist = resolve(dirname(fileURLToPath(import.meta.url)), "../../dist")
const chrome = process.env.CHROME || "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"

// A tty id: no `%`, which `tools/check-private.sh` reads as a pane copied from
// somebody's machine, and nothing to escape in a fragment.
const SESSION = "ttys012"
const SAID = "ship the reload fix"

function row() {
  return {
    id: SESSION,
    label: "A session to write to",
    backend: "owned",
    state: "idle",
    work_state: "ready",
    evidence: "process",
    isClaude: true,
    cwd: "/tmp/fixture",
    closeability: {
      activity_generation: 1,
      attestation_id: null,
      mover: null,
      obligation_generation: 1,
      observed_at: 1,
      provenance: [],
      reasons: [],
      session_generation: 1,
      source: "broker",
      state: "safe",
      version: "fixture",
    },
  }
}

// ---- the stand-in daemon

/** Every Idempotency-Key a send arrived under, in order. */
let keys: string[] = []
/** The first send is never answered, which is how a card is lost mid-flight. */
let held: ServerResponse[] = []
/** What the transcript holds, which the test moves. */
let turns: { role: string; text: string; at: number; kind?: string }[] = []
let generation = 0

function snapshot() {
  generation++
  return {
    at: Date.now(),
    scan: {
      complete: true,
      completed: { complete: true, sequence: generation },
      emptyAuthoritative: true,
      epoch: 1,
      generation,
      provenance: "fixture",
    },
    sessions: [row()],
  }
}

const TYPES: Record<string, string> = {
  ".js": "text/javascript",
  ".css": "text/css",
  ".json": "application/json",
  ".png": "image/png",
  ".ico": "image/x-icon",
  ".svg": "image/svg+xml",
  ".webmanifest": "application/manifest+json",
}

function json(res: ServerResponse, status: number, body: unknown) {
  res.writeHead(status, { "content-type": "application/json" })
  res.end(JSON.stringify(body))
}

/** The document with its words written in, as `page.go` writes them. */
function document(): string {
  const html = readFileSync(join(dist, "index.html"), "utf8")
  const words = JSON.parse(readFileSync(join(dist, "strings", "zh-Hant.json"), "utf8"))
  words.lang = "zh-Hant"
  words.dir = "ltr"
  const slot = "<script>window.__strings=" + JSON.stringify(words).replaceAll("</", "<\\/") + "</script>"
  return html.replace("<!-- clawdline:strings -->", slot).replace("<!-- clawdline:cloud -->", "")
}

function send(req: IncomingMessage, res: ServerResponse) {
  const key = String(req.headers["idempotency-key"] ?? "")
  keys.push(key)
  req.resume()
  // The first attempt is the one the page is still waiting for when it goes.
  if (keys.length === 1) {
    held.push(res)
    return
  }
  req.on("end", () => json(res, 200, { ok: true }))
}

function daemon(): Server {
  return createServer((req, res) => {
    const url = new URL(req.url ?? "/", "http://fixture")
    const path = url.pathname
    if (req.method === "POST" && path === "/v1/sessions/" + SESSION + "/send") return send(req, res)
    if (path === "/v1/sessions") return json(res, 200, snapshot())
    if (path === "/v1/health") return json(res, 200, { ok: true })
    if (path === "/v1/events") {
      res.writeHead(200, { "content-type": "text/event-stream", "cache-control": "no-cache" })
      res.write("event: sessions\ndata: " + JSON.stringify(snapshot()) + "\n\n")
      return // held open, as the daemon's stream is
    }
    if (path === "/v1/transcript") {
      return json(res, 200, { entries: turns, evidence: "process", id: url.searchParams.get("session"), signature: "fixture:" + turns.length })
    }
    if (path.startsWith("/v1/")) return json(res, 404, { error: { code: "not_found", message: path } })
    if (path === "/") {
      res.writeHead(200, { "content-type": "text/html; charset=utf-8" })
      return res.end(document())
    }
    const file = normalize(join(dist, path))
    if (!file.startsWith(dist + "/") || !existsSync(file) || !statSync(file).isFile()) {
      return json(res, 404, { error: { code: "not_found", message: path } })
    }
    res.writeHead(200, { "content-type": TYPES[extname(file)] ?? "application/octet-stream" })
    res.end(readFileSync(file))
  })
}

// ---- a browser, over the DevTools protocol

type Pending = { resolve: (v: any) => void; reject: (e: Error) => void }

class Browser {
  private seq = 0
  private pending = new Map<number, Pending>()
  private events: { method: string; params: any; sessionId?: string }[] = []
  private waiters: (() => void)[] = []
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
      this.events.push(msg)
      for (const w of this.waiters.splice(0)) w()
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

  send(method: string, params: object = {}, sessionId?: string, ms = 15_000): Promise<any> {
    const id = ++this.seq
    this.ws.send(JSON.stringify({ id, method, params, sessionId }))
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(id)
        reject(new Error(method + " had no answer within " + ms + "ms"))
      }, ms)
      this.pending.set(id, {
        resolve: (v) => (clearTimeout(timer), resolve(v)),
        reject: (e) => (clearTimeout(timer), reject(e)),
      })
    })
  }

  async event(sessionId: string, method: string, mark: number, ms = 10_000): Promise<void> {
    const deadline = Date.now() + ms
    for (;;) {
      if (this.events.slice(mark).some((e) => e.sessionId === sessionId && e.method === method)) return
      if (Date.now() > deadline) throw new Error("no " + method + " within " + ms + "ms")
      await new Promise<void>((ok) => {
        this.waiters.push(ok)
        setTimeout(ok, 100)
      })
    }
  }

  mark(): number {
    return this.events.length
  }

  close() {
    this.ws.close()
  }
}

/** What the page shows, read in one go so a failure can say all of it. */
const PROBE = `(() => {
  const cards = [...document.querySelectorAll(".entry.pending")].map((n) => ({
    state: n.dataset.send,
    words: (n.querySelector(".body")?.textContent || "").trim(),
    look: !!n.querySelector("[data-pending-look]"),
    again: !!n.querySelector("[data-pending-retry]"),
  }))
  let kept = null
  try { kept = window.localStorage.getItem("clawdline.session.cards") } catch (e) { kept = "threw" }
  return {
    console: !!document.getElementById("app"),
    cards,
    turns: document.querySelectorAll('#tx .entry[data-role="user"]:not(.pending)').length,
    ready: !document.getElementById("send")?.disabled,
    kept,
  }
})()`

type Seen = {
  console: boolean
  cards: { state: string; words: string; look: boolean; again: boolean }[]
  turns: number
  /** The send button is there and takes a press. */
  ready: boolean
  kept: string | null
}

class Tab {
  private b: Browser
  private session: string
  private target: string
  readonly origin: string

  constructor(b: Browser, session: string, target: string, origin: string) {
    this.b = b
    this.session = session
    this.target = target
    this.origin = origin
  }

  static async open(b: Browser, origin: string): Promise<Tab> {
    const { targetId } = await b.send("Target.createTarget", { url: "about:blank" })
    const { sessionId } = await b.send("Target.attachToTarget", { targetId, flatten: true })
    await b.send("Page.enable", {}, sessionId)
    await b.send("Runtime.enable", {}, sessionId)
    await b.send("Emulation.setDeviceMetricsOverride", { width: 1280, height: 800, mobile: false, deviceScaleFactor: 1 }, sessionId)
    return new Tab(b, sessionId, targetId, origin)
  }

  close(): Promise<void> {
    return this.b.send("Target.closeTarget", { targetId: this.target }).then(
      () => undefined,
      () => undefined,
    )
  }

  async go(address: string): Promise<void> {
    const mark = this.b.mark()
    await this.b.send("Page.navigate", { url: this.origin + address }, this.session)
    await this.b.event(this.session, "Page.loadEventFired", mark)
  }

  async reload(): Promise<void> {
    const mark = this.b.mark()
    await this.b.send("Page.reload", {}, this.session)
    await this.b.event(this.session, "Page.loadEventFired", mark)
  }

  async run(expression: string): Promise<any> {
    const { result, exceptionDetails } = await this.b.send(
      "Runtime.evaluate",
      { expression, returnByValue: true, awaitPromise: true },
      this.session,
    )
    if (exceptionDetails) throw new Error(exceptionDetails.exception?.description ?? exceptionDetails.text)
    return result.value
  }

  seen(): Promise<Seen> {
    return this.run(PROBE)
  }

  async until(what: string, ok: (s: Seen) => boolean, ms = 10_000): Promise<Seen> {
    const deadline = Date.now() + ms
    let last: Seen | null = null
    for (;;) {
      try {
        last = await this.seen()
        if (ok(last)) return last
      } catch {
        /* between documents */
      }
      if (Date.now() > deadline) assert.fail(what + "; the page showed " + JSON.stringify(last))
      await new Promise((r) => setTimeout(r, 60))
    }
  }

  /**
   * Words typed into the box and the send pressed, as a person does it — and
   * the wait between them that a person takes without noticing: the button is
   * disabled until the draw that the typing causes, and a press before that
   * lands on nothing.
   */
  async say(said: string): Promise<void> {
    await this.run(`(() => {
      const box = document.getElementById("msg")
      if (!box) throw new Error("no box")
      box.focus()
      box.textContent = ${JSON.stringify(said)}
      box.dispatchEvent(new InputEvent("input", { bubbles: true }))
    })()`)
    await this.until("the words are in the box and the send takes a press", (s) => s.ready)
    await this.press("#send")
  }

  press(what: string): Promise<void> {
    return this.run(`(() => {
      const button = document.querySelector(${JSON.stringify(what)})
      if (!button) throw new Error("no " + ${JSON.stringify(what)})
      button.click()
    })()`)
  }
}

// ---- the run

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
  profile = mkdtempSync(join(tmpdir(), "clawdline-pending-"))
  browserProcess = spawn(chrome, [
    "--headless=new",
    "--remote-debugging-port=0",
    "--user-data-dir=" + profile,
    "--no-first-run",
    "--no-default-browser-check",
    "about:blank",
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
  for (const res of held.splice(0)) res.destroy()
  server?.closeAllConnections()
  await new Promise<void>((ok) => (server ? server.close(() => ok()) : ok()))
  if (profile) rmSync(profile, { recursive: true, force: true, maxRetries: 5 })
})

/**
 * A browser that has kept nothing, before the page that would read it loads.
 * Clearing it from inside the page is not the same thing: a page on its way out
 * still settles the request the navigation aborted, and writes its card as it
 * goes — which is the product behaving correctly and the test racing it.
 */
async function wipe(): Promise<void> {
  // A page of this origin that is not the console, so no card is on it and
  // nothing writes one back as the tab goes.
  const tab = await Tab.open(browser, origin)
  try {
    await tab.go("/v1/health")
    await tab.run(`window.localStorage.clear()`)
  } finally {
    await tab.close()
  }
}

async function fresh(): Promise<void> {
  keys = []
  turns = []
  for (const res of held.splice(0)) res.destroy()
  await wipe()
}

test("a card still sending survives the reload, and is sent again under the same request", async () => {
  await fresh()
  const tab = await Tab.open(browser, origin)
  try {
    await tab.go("/#session=" + SESSION)
    await tab.until("the session opens with its box", (s) => s.console && s.cards.length === 0)
    await tab.say(SAID)

    // The daemon is not answering, so the card sits there saying it is sending.
    const sending = await tab.until("the card says it is sending", (s) => s.cards.length === 1 && s.cards[0].state === "sending")
    assert.match(sending.cards[0].words, new RegExp(SAID))
    assert.equal(keys.length, 1, "one attempt, under one key")
    const first = keys[0]
    assert.ok(first, "the attempt carried an Idempotency-Key")
    assert.ok(sending.kept && sending.kept.includes(first), "and the card, with that key, is in this browser's store")

    // The page goes: a reload is the mildest of the four ways it happens.
    await tab.reload()
    const back = await tab.until("the card is back after the reload", (s) => s.console && s.cards.length === 1)
    assert.match(back.cards[0].words, new RegExp(SAID), "the words came back")
    assert.equal(back.cards[0].state, "unknown", "nothing answered the attempt, so the card does not claim it went")
    assert.equal(back.cards[0].look, true, "it offers a look")
    assert.equal(back.cards[0].again, false, "and not a send, before anything has been read")
    assert.equal(keys.length, 1, "and the page sent nothing of its own accord on the way back")

    // A look reads the conversation: it is not there, so sending is offered.
    await tab.press("[data-pending-look]")
    await tab.until("the look says it is not in the conversation", (s) => s.cards.length === 1 && s.cards[0].again)
    assert.equal(keys.length, 1, "looking reads; it does not send")

    await tab.press("[data-pending-retry]")
    await tab.until("the second attempt is taken", (s) => s.cards.length === 1 && s.cards[0].state === "accepted")
    assert.equal(keys.length, 2, "one more attempt")
    assert.equal(keys[1], first, "under the same request as the first — which is what stops the message being typed twice")

    // And when the turn turns up, the card goes, as any settled card does.
    turns = [{ role: "user", text: SAID, at: Math.floor(Date.now() / 1000), kind: "message" }]
    const settled = await tab.until("the turn takes the card's place", (s) => s.cards.length === 0 && s.turns >= 1)
    assert.ok(settled.kept === null || !settled.kept.includes(first), "and the store does not hold it any more")
  } finally {
    await tab.close()
  }
})

test("a browser with nothing kept is the page as it was: no card, and no white screen", async () => {
  await fresh()
  const tab = await Tab.open(browser, origin)
  try {
    await tab.go("/#session=" + SESSION)
    await tab.until("the session opens", (s) => s.console)
    await tab.say(SAID + " twice")
    await tab.until("the card says it is sending", (s) => s.cards.length === 1 && s.cards[0].state === "sending")
    await tab.reload()
    await tab.until("the card comes back", (s) => s.cards.length === 1)
    // Site data cleared, a private window, a browser that will not keep any of
    // it: the card is gone and the console is not.
    await wipe()
    await tab.reload()
    const after = await tab.until("the console is drawn", (s) => s.console, 8_000)
    assert.equal(after.cards.length, 0, "nothing was kept, so nothing comes back — today's behaviour, not a crash")
    assert.equal(after.kept, null)
  } finally {
    await tab.close()
  }
})

test('two tabs: "no thanks" in one takes the card out of the other', async () => {
  await fresh()
  const one = await Tab.open(browser, origin)
  const two = await Tab.open(browser, origin)
  try {
    await one.go("/#session=" + SESSION)
    await one.until("the session opens", (s) => s.console && s.cards.length === 0)
    await one.say(SAID + " once")
    await one.until("the card says it is sending", (s) => s.cards.length === 1 && s.cards[0].state === "sending")
    // A card still sending has no close button — there is nothing to be done
    // about it yet. After the reload it is an unknown card, which has one.
    await one.reload()
    await one.until("the card is back, with a way to put it away", (s) => s.cards.length === 1 && s.cards[0].state === "unknown")

    await two.go("/#session=" + SESSION)
    await two.until("the second tab opens on the same card", (s) => s.cards.length === 1 && s.cards[0].state === "unknown")

    await one.press("[data-pending-dismiss]")
    await one.until("it is gone from the tab that put it away", (s) => s.cards.length === 0)
    await two.until("and from the one that was only showing it", (s) => s.cards.length === 0)

    // And it does not come back from the tab that still had it a moment ago.
    await two.reload()
    const after = await two.until("the console is drawn", (s) => s.console)
    assert.equal(after.cards.length, 0, "put away means put away, including the kept copy")
  } finally {
    await two.close()
    await one.close()
  }
})
