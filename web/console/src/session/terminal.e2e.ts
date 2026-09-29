// The terminal page (plan v3 §6 F3) against a real daemon, driven in headless
// Chrome as a person drives it: open a terminal from the Projects page, type
// into programs that asked for application cursor keys, a pager and vim, type
// Chinese through the IME, paste, resize, leave and come back, share it with
// a second tab, and do the same on a phone.
//
// Terminals are the daemon's own tmux server, so there is no stand-in here.
// Start a throwaway daemon first — an empty CLAWDLINE_NEXT_DIR, a port other
// than the one in use, a private TMUX_TMPDIR, one project — then:
//
//   (cd web && npm run build)
//   CLAWDLINE_TERMINAL_ORIGIN=http://127.0.0.1:<port> \
//   CLAWDLINE_TERMINAL_TOKEN="$(cat $CLAWDLINE_NEXT_DIR/local-token)" \
//   CLAWDLINE_TERMINAL_PROJECT=<folder name of a throwaway project added with \`clawdline project add\`> \
//   CLAWDLINE_SHOTS=<dir> node --test --experimental-strip-types web/console/src/session/terminal.e2e.ts
//
// The daemon must serve this worktree's build (CLAWDLINE_NEXT_WEB=…/dist).
// With CLAWDLINE_TERMINAL_DAEMON_PID set to that throwaway daemon's pid, the
// stale-screen check stops it for a few seconds and continues it.
// Every terminal this opens it closes again; it touches no other.
//
// Named `.e2e.ts` rather than `.test.ts` so the unit run does not start a browser.
import { test, before, after, afterEach } from "node:test"
import assert from "node:assert/strict"
import { spawn, type ChildProcess } from "node:child_process"
import { existsSync, mkdtempSync, rmSync, writeFileSync } from "node:fs"
import { tmpdir } from "node:os"
import { join } from "node:path"

const origin = process.env.CLAWDLINE_TERMINAL_ORIGIN || ""
const token = process.env.CLAWDLINE_TERMINAL_TOKEN || ""
const shots = process.env.CLAWDLINE_SHOTS || ""
/** The folder name of the one project this may open terminals in: a throwaway, never somebody's work. */
const fixture = process.env.CLAWDLINE_TERMINAL_PROJECT || "f3-fixture"
const chrome = process.env.CHROME || "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
const skip = !origin || !token ? "set CLAWDLINE_TERMINAL_ORIGIN and CLAWDLINE_TERMINAL_TOKEN to a throwaway daemon" : false

const pause = (ms: number) => new Promise((r) => setTimeout(r, ms))

// ---- the daemon, read directly

async function api(path: string, init: RequestInit = {}): Promise<any> {
  const res = await fetch(origin + path, {
    ...init,
    headers: { cookie: "clawdline-next=" + token, origin, "content-type": "application/json", ...(init.headers ?? {}) },
  })
  const body = await res.json().catch(() => null)
  if (!res.ok) throw new Error(path + " → " + res.status + " " + JSON.stringify(body))
  return body
}

const opened = new Set<string>()

async function closeAll(): Promise<void> {
  for (const id of opened) {
    try {
      const c = await api(`/v1/terminals/${id}/control`, { method: "POST", body: JSON.stringify({ action: "takeover", client: "e2e-cleanup" }) })
      await api(`/v1/terminals/${id}`, { method: "DELETE", body: JSON.stringify({ epoch: c.epoch, client: "e2e-cleanup" }) })
    } catch {
      /* already closed */
    }
  }
}

// ---- the browser, over CDP (the harness of archive.e2e.ts)

type Pending = { resolve: (v: any) => void; reject: (e: Error) => void }
type Event = { method: string; sessionId?: string; params?: any }

class Browser {
  private seq = 0
  private pending = new Map<number, Pending>()
  events: Event[] = []
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
      const timer = setTimeout(() => (this.pending.delete(id), reject(new Error(method + " had no answer"))), 15_000)
      this.pending.set(id, {
        resolve: (v) => (clearTimeout(timer), resolve(v)),
        reject: (e) => (clearTimeout(timer), reject(e)),
      })
    })
  }

  async loaded(sessionId: string, mark: number): Promise<void> {
    const deadline = Date.now() + 10_000
    while (!this.events.slice(mark).some((e) => e.sessionId === sessionId && e.method === "Page.loadEventFired")) {
      if (Date.now() > deadline) throw new Error("the page did not load")
      await pause(50)
    }
  }

  mark(): number {
    return this.events.length
  }

  close() {
    this.ws.close()
  }
}

/** What the page shows: the screen's rows as text, and the header. */
interface Seen {
  hash: string
  rows: string[]
  holder: string
  fresh: string
  status: string
  ended: string
  keys: boolean
  scrollWidth: number
  width: number
  stdin: boolean
}

const PROBE = `(() => {
  const page = document.querySelector("#terminal")
  const rows = [...(page?.querySelectorAll(".terminal-host .xterm-rows > div") ?? [])]
    .map((d) => (d.textContent ?? "").replace(/\\u00a0/g, " ").trimEnd())
  const keys = page?.querySelector(".terminal-keys")
  return {
    hash: location.hash,
    rows,
    holder: page?.querySelector(".terminal-holder")?.textContent ?? "",
    fresh: page?.querySelector(".terminal-fresh")?.textContent ?? "",
    status: page?.querySelector(".terminal-view .terminal-status-line")?.textContent ?? "",
    ended: page?.querySelector(".terminal-ended")?.textContent ?? "",
    keys: !!keys && getComputedStyle(keys).display !== "none",
    scrollWidth: document.documentElement.scrollWidth,
    width: window.innerWidth,
    stdin: !(page?.querySelector(".terminal-key")?.disabled ?? true),
  }
})()`

const KEYS: Record<string, { key: string; code: string; vk: number; text?: string }> = {
  up: { key: "ArrowUp", code: "ArrowUp", vk: 38 },
  down: { key: "ArrowDown", code: "ArrowDown", vk: 40 },
  enter: { key: "Enter", code: "Enter", vk: 13, text: "\r" },
  escape: { key: "Escape", code: "Escape", vk: 27 },
  tab: { key: "Tab", code: "Tab", vk: 9 },
}

/** Every tab still open: a failed test leaves none behind to hold connections. */
const live = new Set<Tab>()

class Tab {
  readonly b: Browser
  readonly session: string
  private target: string

  constructor(b: Browser, session: string, target: string) {
    this.b = b
    this.session = session
    this.target = target
  }

  static async open(b: Browser, phone = false): Promise<Tab> {
    const { targetId } = await b.send("Target.createTarget", { url: "about:blank" })
    const { sessionId } = await b.send("Target.attachToTarget", { targetId, flatten: true })
    await b.send("Page.enable", {}, sessionId)
    await b.send("Runtime.enable", {}, sessionId)
    await b.send("Network.enable", {}, sessionId)
    const tab = new Tab(b, sessionId, targetId)
    live.add(tab)
    await tab.size(phone ? 375 : 1280, phone ? 812 : 800, phone)
    if (phone) await b.send("Emulation.setTouchEmulationEnabled", { enabled: true, maxTouchPoints: 1 }, sessionId)
    return tab
  }

  size(width: number, height: number, mobile = false): Promise<void> {
    return this.b.send("Emulation.setDeviceMetricsOverride", { width, height, mobile, deviceScaleFactor: mobile ? 2 : 1 }, this.session)
  }

  close(): Promise<void> {
    live.delete(this)
    return this.b.send("Target.closeTarget", { targetId: this.target }).then(() => undefined, () => undefined)
  }

  async go(address: string): Promise<void> {
    const mark = this.b.mark()
    await this.b.send("Page.navigate", { url: origin + "/" + address }, this.session)
    await this.b.loaded(this.session, mark)
  }

  async run(expression: string): Promise<any> {
    const { result, exceptionDetails } = await this.b.send(
      "Runtime.evaluate", { expression, returnByValue: true, awaitPromise: true }, this.session)
    if (exceptionDetails) throw new Error(exceptionDetails.exception?.description ?? exceptionDetails.text)
    return result.value
  }

  async until(what: string, ok: (s: Seen) => boolean, ms = 8_000): Promise<Seen> {
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
      await pause(60)
    }
  }

  press(selector: string): Promise<void> {
    return this.run(`(() => {
      const el = document.querySelector(${JSON.stringify(selector)})
      if (!el) throw new Error("nothing at " + ${JSON.stringify(selector)})
      el.click()
    })()`)
  }

  /** The tab in front, as the one a person types into is, and the terminal focused. */
  async focusTerminal(): Promise<void> {
    await this.b.send("Page.bringToFront", {}, this.session)
    await this.run(`document.querySelector("#terminal .xterm-helper-textarea")?.focus()`)
  }

  /** A key as the keyboard sends it, to whatever has focus. */
  async key(name: keyof typeof KEYS, modifiers = 0): Promise<void> {
    const k = KEYS[name]
    await this.b.send("Input.dispatchKeyEvent", {
      type: k.text ? "keyDown" : "rawKeyDown", key: k.key, code: k.code, windowsVirtualKeyCode: k.vk,
      nativeVirtualKeyCode: k.vk, text: k.text, unmodifiedText: k.text, modifiers,
    }, this.session)
    await this.b.send("Input.dispatchKeyEvent", { type: "keyUp", key: k.key, code: k.code, windowsVirtualKeyCode: k.vk, modifiers }, this.session)
  }

  async ctrl(letter: string): Promise<void> {
    const upper = letter.toUpperCase()
    const base = { key: letter, code: "Key" + upper, windowsVirtualKeyCode: upper.charCodeAt(0), modifiers: 2 }
    await this.b.send("Input.dispatchKeyEvent", { type: "rawKeyDown", ...base }, this.session)
    await this.b.send("Input.dispatchKeyEvent", { type: "keyUp", ...base }, this.session)
  }

  /** Text as a keyboard or an IME commits it. */
  text(text: string): Promise<void> {
    return this.b.send("Input.insertText", { text }, this.session)
  }

  async line(text: string): Promise<void> {
    await this.text(text)
    await pause(30)
    await this.key("enter")
  }

  async shot(name: string): Promise<void> {
    if (!shots) return
    await pause(250)
    const { data } = await this.b.send("Page.captureScreenshot", { format: "png" }, this.session)
    writeFileSync(join(shots, name + ".png"), Buffer.from(data, "base64"))
  }

  /** Requests this tab started that have not finished: its long-lived connections. */
  open(): string[] {
    const mine = this.b.events.filter((e) => e.sessionId === this.session)
    const done = new Set(mine.filter((e) => e.method === "Network.loadingFinished" || e.method === "Network.loadingFailed").map((e) => e.params?.requestId))
    return mine.filter((e) => e.method === "Network.requestWillBeSent" && !done.has(e.params?.requestId))
      .map((e) => new URL(String(e.params?.request?.url)).pathname)
  }

  requests(mark: number, part: string): { url: string; body: string }[] {
    return this.b.events.slice(mark)
      .filter((e) => e.sessionId === this.session && e.method === "Network.requestWillBeSent")
      .map((e) => ({ url: String(e.params?.request?.url ?? ""), body: String(e.params?.request?.postData ?? "") }))
      .filter((r) => r.url.includes(part))
  }
}

const has = (s: Seen, text: string) => s.rows.some((r) => r.includes(text))
/** The shell has drawn its prompt, which names the project's folder: typing now is not typed ahead. */
const prompted = (s: Seen) => s.rows.some((r) => r.includes(fixture))
const YOU = "你（這個分頁）"
const OTHER_TAB = "另一個分頁"

let project = ""
let address = ""
let browserProcess: ChildProcess
let browser: Browser
let profile: string

before(async () => {
  if (skip) return
  assert.ok(existsSync(chrome), "no Chrome at " + chrome + "; set CHROME")
  await api("/v1/health")
  const place = (await api("/v1/places")).places.find((p: { path: string }) => p.path.endsWith("/" + fixture))
  assert.ok(place, "the throwaway project " + fixture + " is one of this machine's projects")
  project = place.id
  profile = mkdtempSync(join(tmpdir(), "clawdline-terminal-"))
  browserProcess = spawn(chrome, [
    "--headless=new", "--remote-debugging-port=0", "--user-data-dir=" + profile,
    "--no-first-run", "--no-default-browser-check", "--lang=zh-TW", "about:blank",
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
  const host = new URL(origin).hostname
  const first = await Tab.open(browser)
  await first.b.send("Network.setCookie", { name: "clawdline-next", value: token, domain: host, path: "/" }, first.session)
  await first.close()
})

afterEach(async () => {
  for (const tab of [...live]) await tab.close()
})

after(async () => {
  if (skip) return
  await closeAll()
  browser?.close()
  if (browserProcess && browserProcess.exitCode === null) {
    const gone = new Promise((ok) => browserProcess.once("exit", ok))
    browserProcess.kill()
    await gone
  }
  if (profile) rmSync(profile, { recursive: true, force: true, maxRetries: 5 })
})

test("a terminal opens from the Projects page and takes keys the way its programs asked", { skip }, async () => {
  const tab = await Tab.open(browser)
  await tab.go("#page=projects")
  // The Projects page: every row has 終端, and it leads to that project's
  // list. Its rows are this machine's real projects, so nothing is opened
  // there — the list is only read.
  await tab.run(`new Promise((ok, fail) => {
    const t = setTimeout(() => fail(new Error("no 終端 button on the project rows")), 8000)
    const look = () => document.querySelector("button.project-row-terminal") ? (clearTimeout(t), ok(true)) : setTimeout(look, 50)
    look()
  })`)
  await tab.shot("01-projects-terminal-entry")
  await tab.press("button.project-row-terminal")
  await tab.run(`new Promise((ok, fail) => {
    const t = setTimeout(() => fail(new Error("no terminal list")), 8000)
    const look = () => document.querySelector("#terminal:not([hidden]) .terminal-list-title") ? (clearTimeout(t), ok(true)) : setTimeout(look, 50)
    look()
  })`)
  assert.match(await tab.run("location.hash"), /^#page=terminal&project=%2F/, "the Projects page links by folder")
  assert.ok(await tab.run(`!/project-/.test(document.querySelector("#terminal .terminal-list-title").textContent)`), "the list is titled with the project's name")
  await tab.shot("02-terminal-list-from-projects")

  // The work page's project scope, on the throwaway project: 終端, then 開新終端.
  await tab.run(`location.hash = ${JSON.stringify("#page=work&project=" + encodeURIComponent(project))}`)
  await tab.run(`new Promise((ok, fail) => {
    const t = setTimeout(() => fail(new Error("no 終端 under the work page's project scope")), 10000)
    const look = () => { const d = document.querySelector("#work .terminal-entry"); if (d) { clearTimeout(t); d.open = true; ok(true) } else setTimeout(look, 50) }
    look()
  })`)
  await tab.run(`new Promise((ok, fail) => {
    const t = setTimeout(() => fail(new Error("no 開新終端 under the work page")), 8000)
    const look = () => document.querySelector("#work .terminal-entry .terminal-open-new") ? (clearTimeout(t), ok(true)) : setTimeout(look, 50)
    look()
  })`)
  await tab.shot("03-work-page-terminal-entry")
  await tab.press("#work .terminal-entry .terminal-open-new")
  let seen = await tab.until("the new terminal is shown and this tab holds it", (s) => s.hash.includes("terminal=") && s.holder === YOU && s.rows.some((r) => r.trim().length > 0), 12_000)
  address = seen.hash
  await tab.until("the shell's prompt", prompted, 15_000)
  opened.add(new URLSearchParams(address.slice(1)).get("terminal")!)
  assert.match(await tab.run(`document.querySelector("#terminal .terminal-session-note").textContent`), /不會成為 Session/)
  await tab.focusTerminal()

  // Application cursor keys: the program asks, and ↑ arrives as ESC O A.
  await tab.line(String.raw`printf '\e[?1h'; cat -v`)
  await pause(800)
  await tab.key("up")
  await tab.key("enter")
  seen = await tab.until("↑ in application cursor mode reaches cat as ^[OA", (s) => has(s, "^[OA"))
  await tab.shot("03-cat-v-app-cursor")
  await tab.ctrl("c")
  await pause(300)

  // A pager scrolls with the arrows.
  await tab.line("clear; seq 1 400 | less")
  await tab.until("less shows the first lines", (s) => s.rows[0]?.trim() === "1")
  for (let i = 0; i < 5; i++) await tab.key("down")
  seen = await tab.until("↓ scrolls less by five lines", (s) => s.rows[0]?.trim() === "6")
  await tab.shot("04-less-scrolled")
  await tab.text("q")
  await pause(300)

  // vim: insert, Escape (which must not leave the page), write, quit.
  const file = "f3-e2e-" + Date.now() + ".txt"
  await tab.line("vim -u NONE " + file)
  await tab.until("vim is open", (s) => s.rows.some((r) => r.startsWith("~")))
  await tab.text("i")
  await tab.text("written in vim through the console")
  await tab.key("escape")
  await pause(200)
  await tab.shot("05-vim")
  assert.match(await tab.run("location.hash"), /page=terminal/, "Escape stayed in the terminal")
  await tab.text(":wq")
  await tab.key("enter")
  await pause(400)
  await tab.line("clear; cat " + file + "; rm " + file)
  await tab.until("the file vim wrote holds what was typed", (s) => s.rows.some((r) => r.trim() === "written in vim through the console"))

  // Chinese through the IME.
  await tab.text("echo ")
  await tab.b.send("Input.imeSetComposition", { text: "ㄓㄨㄥ", selectionStart: 3, selectionEnd: 3 }, tab.session)
  await pause(100)
  await tab.b.send("Input.imeSetComposition", { text: "中文", selectionStart: 2, selectionEnd: 2 }, tab.session)
  await pause(100)
  await tab.text("中文")
  await pause(100)
  await tab.key("enter")
  seen = await tab.until("the IME's Chinese is echoed", (s) => s.rows.some((r) => r.replace(/\s/g, "") === "中文"))
  await tab.shot("06-ime-chinese")

  // A multi-line paste goes to /paste as one request.
  await tab.line("clear")
  await pause(200)
  const mark = tab.b.mark()
  await tab.run(`(() => {
    const data = new DataTransfer()
    data.setData("text/plain", "echo pasted-one\\necho pasted-two\\n")
    document.querySelector("#terminal .xterm-helper-textarea").dispatchEvent(new ClipboardEvent("paste", { clipboardData: data, bubbles: true, cancelable: true }))
  })()`)
  // The shell asked for bracketed paste, so both lines wait on its command
  // line, as in any terminal, until Enter runs them.
  await tab.until("both pasted lines are on the command line", (s) => has(s, "echo pasted-one") && has(s, "echo pasted-two"))
  await tab.key("enter")
  await tab.until("both pasted lines ran", (s) => s.rows.some((r) => r.trim() === "pasted-one") && s.rows.some((r) => r.trim() === "pasted-two"))
  const pastes = tab.requests(mark, "/paste")
  const inputs = tab.requests(mark, "/input")
  assert.equal(pastes.length, 1, "one /paste request")
  assert.match(pastes[0].body, /pasted-one\\necho pasted-two/)
  const typed = inputs.map((r) => Buffer.from(JSON.parse(r.body).data, "base64").toString())
  assert.ok(typed.every((t) => !t.includes("pasted")), "no keystroke request carried the paste: " + JSON.stringify(typed))
  await tab.shot("07-paste")

  // Resizing the window resizes the terminal.
  let asked = 0
  const size = async () => {
    const tag = "size" + ++asked
    await tab.line(`clear; echo ${tag} cols=$(tput cols) lines=$(tput lines)`)
    const answer = new RegExp("^" + tag + " cols=\\d+ lines=\\d+$")
    const s = await tab.until("tput answers", (s) => s.rows.some((r) => answer.test(r.trim())))
    return s.rows.find((r) => answer.test(r.trim()))!.trim().replace(tag + " ", "")
  }
  const wide = await size()
  await tab.size(820, 560)
  await pause(900)
  const narrow = await size()
  assert.notEqual(narrow, wide, "tput reads the new size")
  if (shots) writeFileSync(join(shots, "resize.json"), JSON.stringify({ at1280x800: wide, at820x560: narrow }))
  await tab.shot("08-resized")
  await tab.size(1280, 800)
  await pause(900)

  // A long job keeps running with the tab gone.
  await tab.line("clear")
  await pause(300)
  await tab.line("sleep 600")
  await tab.until("the sleep is running", (s) => has(s, "sleep 600"))
  await pause(300)
  await tab.close()
})

test("closing the tab and opening the address again finds the same shell and screen", { skip }, async () => {
  assert.ok(address, "the first test opened a terminal")
  // The closed tab gave its lease back from pagehide, with a keepalive request.
  const id0 = new URLSearchParams(address.slice(1)).get("terminal")
  const deadline = Date.now() + 5_000
  while ((await api(`/v1/terminals/${id0}?client=e2e`)).control.held) {
    assert.ok(Date.now() < deadline, "the closed tab released its lease")
    await pause(100)
  }
  const tab = await Tab.open(browser)
  await tab.go(address)
  const seen = await tab.until("the same sleep is on the screen", (s) => has(s, "sleep 600"), 10_000)
  assert.equal(seen.holder, YOU, "the lease the closed tab released is free for this one")
  const id = new URLSearchParams(address.slice(1)).get("terminal")
  const t = await api(`/v1/terminals/${id}?client=e2e`)
  assert.equal(t.status, "running")
  await tab.shot("09-reopened-same-screen")

  // A second tab watches, says who holds input, and can take over.
  const second = await Tab.open(browser)
  await second.go(address)
  await second.until("the second tab says another tab holds input", (s) => s.holder === OTHER_TAB && has(s, "sleep 600"))
  await second.shot("10-second-tab-watching")
  await second.press("#terminal .terminal-actions .board-button:first-child")
  await second.until("the second tab took over", (s) => s.holder === YOU)
  await tab.until("the first tab now watches", (s) => s.holder === OTHER_TAB)
  await tab.shot("11-first-tab-after-takeover")

  if (process.env.E2E_DEBUG) console.log("open", JSON.stringify({ tab: tab.open(), second: second.open() }))
  // Three terminal tabs and two console tabs, and typing still lands.
  const more: Tab[] = []
  for (let i = 0; i < 1; i++) {
    const made = await api("/v1/terminals", { method: "POST", body: JSON.stringify({ project_id: project, cols: 80, rows: 24 }) })
    opened.add(made.id)
    const t3 = await Tab.open(browser)
    await t3.go(`#page=terminal&project=${encodeURIComponent(project)}&terminal=${made.id}`)
    await t3.until("the third terminal tab draws", (s) => s.rows.some((r) => r.trim().length > 0), 10_000)
    more.push(t3)
  }
  for (let i = 0; i < 2; i++) {
    const c = await Tab.open(browser)
    await c.go("#page=sessions")
    more.push(c)
  }
  await pause(800)
  await second.focusTerminal()
  await second.ctrl("c")
  await pause(200)
  await second.line("clear; echo five-tabs-$((40+2))")
  if (process.env.E2E_DEBUG) console.log("five", JSON.stringify(Object.fromEntries([...live].map((t, i) => [i, t.open()]))))
  await second.until("typing lands with five tabs open", (s) => s.rows.some((r) => r.trim() === "five-tabs-42"))
  await second.shot("12-five-tabs-typing")

  // A daemon that stops answering: no beat for 6 s, the screen is said to be
  // stale and no key is taken. Only with the throwaway daemon's pid, which is
  // stopped and continued; the terminal's shell is tmux's and keeps running.
  const pid = Number(process.env.CLAWDLINE_TERMINAL_DAEMON_PID || 0)
  if (pid > 0) {
    process.kill(pid, "SIGSTOP")
    try {
      await second.until("the header says the screen may be stale", (s) => s.fresh.includes("畫面可能過期"), 12_000)
      const stale = (await second.run(PROBE)) as Seen
      assert.equal(stale.stdin, false, "no key is taken while stale")
      assert.equal(await second.run(`document.querySelector("#terminal .xterm").closest(".terminal-view").hasAttribute("data-stale")`), true)
      await second.shot("13-stale")
    } finally {
      process.kill(pid, "SIGCONT")
    }
    await second.until("fresh again", (s) => !s.fresh.includes("畫面可能過期"), 15_000)
  }

  // Closing asks first, and the other tab is told who closed it.
  await second.press("#terminal .terminal-close")
  await second.until("closing asks first", () => true)
  assert.ok(await second.run(`!!document.querySelector("#terminal .terminal-ask")`), "a confirmation is shown")
  await second.shot("14-close-confirm")
  await second.press("#terminal .terminal-ask .terminal-danger")
  await tab.until("the watching tab is told the terminal was closed", (s) => s.ended.length > 0, 10_000)
  await tab.shot("15-closed-seen-by-other-tab")
  for (const t of more) await t.close()
  await second.close()
  await tab.close()
})

test("on a phone the special keys type through the same client and nothing is wider than the screen", { skip }, async () => {
  const made = await api("/v1/terminals", { method: "POST", body: JSON.stringify({ project_id: project, cols: 44, rows: 20 }) })
  opened.add(made.id)
  const tab = await Tab.open(browser, true)
  await tab.go(`#page=terminal&project=${encodeURIComponent(project)}&terminal=${made.id}`)
  let seen = await tab.until("the phone holds the terminal", (s) => s.holder === YOU && s.keys && prompted(s), 15_000)
  await tab.focusTerminal()
  const sentFrom = tab.b.mark()
  await tab.line(String.raw`printf '\e[?1h'; cat -v`)
  await pause(800)
  if (process.env.E2E_DEBUG) setTimeout(() => console.log("sent", tab.requests(sentFrom, "/input").map((r) => JSON.stringify(Buffer.from(JSON.parse(r.body).data, "base64").toString()))), 3000)
  await tab.press(`#terminal .terminal-key[aria-label]`) // Ctrl, armed
  assert.equal(await tab.run(`document.querySelector("#terminal .terminal-key[aria-label]").getAttribute("aria-pressed")`), "true")
  await tab.press(`#terminal .terminal-key[aria-label]`) // and off again
  const keys = await tab.run(`[...document.querySelectorAll("#terminal .terminal-key")].map((b) => b.textContent)`)
  assert.deepEqual(keys, ["Esc", "Ctrl", "Tab", "←", "↑", "↓", "→"])
  await tab.run(`[...document.querySelectorAll("#terminal .terminal-key")].find((b) => b.textContent === "↑").click()`)
  await tab.run(`[...document.querySelectorAll("#terminal .terminal-key")].find((b) => b.textContent === "Esc").click()`)
  await tab.key("enter")
  seen = await tab.until("↑ and Esc from the key row reach cat", (s) => has(s, "^[OA^["))
  assert.ok(seen.scrollWidth <= seen.width, `no sideways page scroll (${seen.scrollWidth} > ${seen.width})`)
  await tab.shot("16-phone-375-key-row")
  // Ctrl from the row, then c: an interrupt.
  await tab.press(`#terminal .terminal-key[aria-label]`)
  await tab.text("c")
  await pause(300)
  await tab.line("echo after-ctrl-c")
  await tab.until("Ctrl then c from the row interrupted cat", (s) => s.rows.some((r) => r.trim() === "after-ctrl-c"))
  seen = (await tab.run(PROBE)) as Seen
  assert.ok(seen.scrollWidth <= seen.width, "still no sideways page scroll")
  await tab.shot("17-phone-after-ctrl-c")
  await tab.run(`location.hash = ${JSON.stringify("#page=terminal&project=" + encodeURIComponent(project))}`)
  await tab.run(`new Promise((ok, fail) => {
    const t = setTimeout(() => fail(new Error("no terminal list on the phone")), 8000)
    const look = () => document.querySelector("#terminal .terminal-rows") ? (clearTimeout(t), ok(true)) : setTimeout(look, 50)
    look()
  })`)
  const list = (await tab.run(PROBE)) as Seen
  assert.ok(list.scrollWidth <= list.width, "the list fits the phone")
  await tab.shot("18-phone-375-list")
  await tab.close()
})

test("the header's controls are reached with the keyboard alone", { skip }, async () => {
  const made = await api("/v1/terminals", { method: "POST", body: JSON.stringify({ project_id: project, cols: 80, rows: 24 }) })
  opened.add(made.id)
  const tab = await Tab.open(browser)
  await tab.go(`#page=terminal&project=${encodeURIComponent(project)}&terminal=${made.id}`)
  await tab.until("drawn", (s) => s.holder === YOU, 10_000)
  await tab.run(`document.activeElement?.blur(); document.querySelector("#terminal .terminal-head").setAttribute("tabindex", "-1"); document.querySelector("#terminal .terminal-head").focus()`)
  const reached: string[] = []
  for (let i = 0; i < 7; i++) {
    await tab.key("tab")
    reached.push(await tab.run(`(() => { const a = document.activeElement; return (a?.getAttribute("aria-label") || a?.textContent || a?.className || "").trim().slice(0, 40) })()`))
  }
  const visibleRing = await tab.run(`getComputedStyle(document.activeElement).outlineStyle`)
  if (shots) writeFileSync(join(shots, "keyboard-pass.json"), JSON.stringify({ reached, visibleRing }, null, 2))
  for (const want of ["返回", "釋放", "歷史", "關閉終端"]) assert.ok(reached.includes(want), `${want} is reached by Tab: ${reached.join(" → ")}`)
  await tab.shot("19-keyboard-focus")
  await tab.close()
})

test("a paired device's card on the Devices page grants and takes back terminal access", { skip }, async () => {
  const devices = (await api("/v1/auth/devices")).devices as { id: string; name: string; terminal?: boolean }[]
  if (devices.length === 0) return // `clawdline open --print` against the throwaway daemon makes one
  const device = devices[0]
  const tab = await Tab.open(browser)
  await tab.go("#page=devices")
  const SWITCH = `document.querySelector(${JSON.stringify(`[data-device="${device.id}"] .terminal-grant-switch`)})`
  await tab.run(`new Promise((ok, fail) => {
    const t = setTimeout(() => fail(new Error("no 終端 switch on the device card")), 8000)
    const look = () => ${SWITCH}?.getAttribute("aria-checked") ? (clearTimeout(t), ok(true)) : setTimeout(look, 50)
    look()
  })`)
  const before = device.terminal === true
  assert.equal(await tab.run(`${SWITCH}.getAttribute("aria-checked")`), String(before))
  await tab.run(`${SWITCH}.scrollIntoView({ block: "center" })`)
  await tab.run(`${SWITCH}.click()`)
  await tab.run(`new Promise((ok, fail) => {
    const t = setTimeout(() => fail(new Error("the switch did not turn")), 8000)
    const look = () => ${SWITCH}.getAttribute("aria-checked") === ${JSON.stringify(String(!before))} ? (clearTimeout(t), ok(true)) : setTimeout(look, 50)
    look()
  })`)
  const after = (await api("/v1/auth/devices")).devices.find((d: { id: string }) => d.id === device.id)
  assert.equal(after.terminal === true, !before, "the machine holds what the switch says")
  await tab.shot("20-devices-terminal-grant")
  await tab.run(`${SWITCH}.click()`)
  await tab.run(`new Promise((ok) => { const look = () => ${SWITCH}.getAttribute("aria-checked") === ${JSON.stringify(String(before))} ? ok(true) : setTimeout(look, 50); look() })`)
  await tab.close()
})
