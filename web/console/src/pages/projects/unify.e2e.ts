// The 「Claude 與 Codex 共用」 block and its preview, through a real browser:
//
//   (cd web && npm run build)
//   node --test web/console/src/pages/projects/unify.e2e.ts
//   CLAWDLINE_SHOTS=<directory> … also writes screenshots there
//
// The built console is served by a stand-in daemon that answers the session
// list, one Project, its files and its unify plan — a drifting Project:
// CLAUDE.md without the import, one skill only in `.agents/skills`, one only
// in `.claude/skills`, and lines in CLAUDE.md Codex cannot see. Headless Chrome
// (CHROME, else the usual macOS install) opens the Project gear, reads the
// preview, applies it and sees 已共用. The harness is `session/address.e2e.ts`'s,
// cut down to what this page needs.
//
// Named `.e2e.ts` rather than `.test.ts` so that a unit run does not start a browser.
import { test, before, after } from "node:test"
import assert from "node:assert/strict"
import { spawn, type ChildProcess } from "node:child_process"
import { createServer, type IncomingMessage, type Server, type ServerResponse } from "node:http"
import { existsSync, mkdtempSync, readFileSync, rmSync, statSync, writeFileSync } from "node:fs"
import { tmpdir } from "node:os"
import { dirname, extname, join, normalize, resolve } from "node:path"
import { fileURLToPath } from "node:url"

const dist = resolve(dirname(fileURLToPath(import.meta.url)), "../../../dist")
const shots = process.env.CLAWDLINE_SHOTS || ""
const chrome = process.env.CHROME || "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"

// ---- the stand-in daemon

const CLAUDE_TEXT = "Use the staging database for tests.\nNever deploy on Fridays.\n"

function plan(state: "drifting" | "unified" | "unknown", version: string) {
  const unified = state === "unified"
  return {
    status: state,
    version,
    links_available: true,
    rules: {
      agents: "present",
      claude: state === "unknown" ? "unreadable" : "present",
      claude_imports_agents: unified,
      claude_only_files: [],
      claude_only_lines: CLAUDE_TEXT.trim().split("\n"),
      now: unified ? { claude: ["CLAUDE.md", "AGENTS.md"], codex: ["AGENTS.md"] } : { claude: ["CLAUDE.md"], codex: ["AGENTS.md"] },
      after: { claude: ["CLAUDE.md", "AGENTS.md"], codex: ["AGENTS.md"] },
    },
    skills: [
      { name: "deploy-checklist", place: unified ? "both_same" : "agents", linked: unified, copy: false,
        now: { claude: unified, codex: true }, after: { claude: true, codex: true }, ...(unified ? {} : { action: "skill_link" }) },
      { name: "release-notes", place: unified ? "both_same" : "claude", linked: unified, copy: false,
        now: { claude: true, codex: unified }, after: { claude: true, codex: true }, ...(unified ? {} : { action: "skill_move_and_link" }) },
    ],
    actions: state !== "drifting" ? [] : [
      { kind: "rules_add_import", paths: ["CLAUDE.md"], description: "Add an @AGENTS.md line at the top of CLAUDE.md.",
        edits: [{ path: "CLAUDE.md", before: CLAUDE_TEXT, after: "@AGENTS.md\n" + CLAUDE_TEXT }] },
      { kind: "skill_link", paths: [".claude/skills/deploy-checklist"], link_target: "../../.agents/skills/deploy-checklist",
        description: "Link .claude/skills/deploy-checklist to .agents/skills/deploy-checklist.", edits: [] },
      { kind: "skill_move_and_link", paths: [".claude/skills/release-notes", ".agents/skills/release-notes"], link_target: "../../.agents/skills/release-notes",
        description: "Move .claude/skills/release-notes to .agents/skills/release-notes.", edits: [] },
    ],
    conflicts: state === "drifting"
      ? [{ kind: "claude_only_lines", path: "CLAUDE.md", detail: "Codex does not see these lines; move the shared ones into AGENTS.md." }]
      : state === "unknown" ? [{ kind: "unreadable", path: "CLAUDE.md", detail: "This file could not be read as UTF-8 text." }] : [],
  }
}

let unifyState: "drifting" | "unified" | "unknown" = "drifting"
let unifyVersion = "plan-one"
let failPlanReads = 0
let changeBeforeNextApply = false
// What the next apply does instead of succeeding: stop after the first action,
// or apply and lose the answer on the way back.
let nextApply: "apply" | "stop" | "lose" = "apply"
let applies: { key: string; version: unknown; status: number }[] = []

function reset() {
  unifyState = "drifting"; unifyVersion = "plan-one"; failPlanReads = 0; changeBeforeNextApply = false; nextApply = "apply"; applies = []
}

const TYPES: Record<string, string> = {
  ".js": "text/javascript", ".css": "text/css", ".json": "application/json", ".png": "image/png",
  ".ico": "image/x-icon", ".svg": "image/svg+xml", ".webmanifest": "application/manifest+json",
}

function json(res: ServerResponse, status: number, body: unknown) {
  res.writeHead(status, { "content-type": "application/json" })
  res.end(JSON.stringify(body))
}

function requestJSON(req: IncomingMessage): Promise<Record<string, unknown>> {
  return new Promise((ok, fail) => {
    let raw = ""
    req.setEncoding("utf8")
    req.on("data", (chunk) => { raw += chunk })
    req.on("end", () => { try { ok(JSON.parse(raw) as Record<string, unknown>) } catch (error) { fail(error) } })
    req.on("error", fail)
  })
}

// The document with its words written in, as `page.go` writes them.
function page(): string {
  const html = readFileSync(join(dist, "index.html"), "utf8")
  const words = JSON.parse(readFileSync(join(dist, "strings", "zh-Hant.json"), "utf8"))
  words.lang = "zh-Hant"
  words.dir = "ltr"
  const slot = "<script>window.__strings=" + JSON.stringify(words).replaceAll("</", "<\\/") + "</script>"
  return html.replace("<!-- clawdline:strings -->", slot).replace("<!-- clawdline:cloud -->", "")
}

let generation = 0
function daemon(): Server {
  return createServer((req, res) => {
    const url = new URL(req.url ?? "/", "http://fixture")
    const path = url.pathname
    if (path === "/v1/sessions") {
      generation++
      return json(res, 200, { at: Date.now(), sessions: [], scan: { complete: true, completed: { complete: true, sequence: generation },
        emptyAuthoritative: true, epoch: 1, generation, provenance: "fixture" } })
    }
    if (path === "/v1/health") return json(res, 200, { ok: true })
    // The Board summary is unavailable here, as on a machine without one; the rows come from the places.
    if (path === "/v1/projects" && req.method === "GET") return json(res, 503, { error: "board_unavailable" })
    if (path === "/v1/places") return json(res, 200, {
      at: Date.now(),
      assistants: [{ id: "claude", label: "Claude Code", availability: "unknown" }],
      places: [{ id: "fixture-place", label: "Example project", path: "/tmp/fixture", at: Date.now(),
        icon: { accent: "#D97757", cells: [["#D97757", "#D97757"], ["#D97757", "#141416"]] },
        repo: "github.com/example/project",
        setup: { icon: "generated", deploy: "ready", deploy_activity: "idle", servers: "missing", server_count: 0, sync: "ready" } }],
    })
    if (path === "/v1/projects/fixture-place/files" && req.method === "GET") return json(res, 200, { files: [
      { id: "agents-file", name: "AGENTS.md", location: "AGENTS.md", source: "project", assistant: "codex", kind: "instruction", status: "ready", editable: true },
      { id: "claude-file", name: "CLAUDE.md", location: "CLAUDE.md", source: "project", assistant: "claude", kind: "instruction", status: "ready", editable: true },
    ], truncated: false, skipped: [] })
    if (path === "/v1/projects/fixture-place/unify" && req.method === "GET") {
      if (failPlanReads > 0) { failPlanReads--; return json(res, 503, { error: "unavailable", detail: "the fixture refused one read" }) }
      return json(res, 200, plan(unifyState, unifyVersion))
    }
    if (path === "/v1/projects/fixture-place/unify" && req.method === "POST") {
      const key = String(req.headers["idempotency-key"] ?? "")
      void requestJSON(req).then((body) => {
        if (changeBeforeNextApply) {
          // The files moved on between the preview and the press.
          changeBeforeNextApply = false
          unifyVersion = "plan-two"
        }
        if (body.version !== unifyVersion) {
          applies.push({ key, version: body.version, status: 409 })
          return json(res, 409, { error: "plan_changed", detail: "The files changed since this plan was read." })
        }
        const ran = plan("drifting", unifyVersion).actions
        if (nextApply === "stop") {
          nextApply = "apply"
          applies.push({ key, version: body.version, status: 500 })
          unifyVersion = "plan-after-stop"
          const after = plan("drifting", unifyVersion)
          after.actions = after.actions.slice(1)
          return json(res, 500, { outcome: "stopped", ran: ran.slice(0, 1), failed: ran[1], error: "name_taken",
            detail: "Unify stopped: something already uses that name.", plan: after })
        }
        if (nextApply === "lose") {
          nextApply = "apply"
          unifyState = "unified"
          unifyVersion = "plan-unified"
          applies.push({ key, version: body.version, status: 504 })
          return json(res, 504, { error: "cloud_unanswered", detail: "No answer arrived.", outcome: "unknown" })
        }
        unifyState = "unified"
        unifyVersion = "plan-unified"
        applies.push({ key, version: body.version, status: 200 })
        return json(res, 200, { outcome: "applied", ran, plan: plan("unified", unifyVersion) })
      }, () => json(res, 400, { error: "bad_request", detail: "bad JSON" }))
      return
    }
    if (path.startsWith("/v1/")) return json(res, 404, { error: { code: "not_found", message: path } })
    if (path === "/") {
      res.writeHead(200, { "content-type": "text/html; charset=utf-8" })
      return res.end(page())
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
      const timer = setTimeout(() => { this.pending.delete(id); reject(new Error(method + " had no answer within " + ms + "ms")) }, ms)
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
      await new Promise<void>((ok) => { this.waiters.push(ok); setTimeout(ok, 100) })
    }
  }

  mark(): number { return this.events.length }
  close() { this.ws.close() }
}

type Size = { width: number; height: number; mobile: boolean }

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

  static async open(b: Browser, origin: string, size: Size): Promise<Tab> {
    const { targetId } = await b.send("Target.createTarget", { url: "about:blank" })
    const { sessionId } = await b.send("Target.attachToTarget", { targetId, flatten: true })
    await b.send("Page.enable", {}, sessionId)
    await b.send("Runtime.enable", {}, sessionId)
    await b.send("Emulation.setDeviceMetricsOverride", { ...size, deviceScaleFactor: 1 }, sessionId)
    await b.send("Emulation.setEmulatedMedia", { features: [{ name: "prefers-color-scheme", value: "dark" }] }, sessionId)
    return new Tab(b, sessionId, targetId, origin)
  }

  close(): Promise<void> {
    return this.b.send("Target.closeTarget", { targetId: this.target }).then(() => undefined, () => undefined)
  }

  async go(address: string): Promise<void> {
    const mark = this.b.mark()
    await this.b.send("Page.navigate", { url: this.origin + address }, this.session)
    await this.b.event(this.session, "Page.loadEventFired", mark)
  }

  async run(expression: string): Promise<any> {
    const { result, exceptionDetails } = await this.b.send("Runtime.evaluate",
      { expression, returnByValue: true, awaitPromise: true }, this.session)
    if (exceptionDetails) throw new Error(exceptionDetails.exception?.description ?? exceptionDetails.text)
    return result.value
  }

  /** Until `expression` is truthy in the page; its last value is the failure's message. */
  async until(what: string, expression: string, ms = 6_000): Promise<any> {
    const deadline = Date.now() + ms
    let last: any = null
    for (;;) {
      try { last = await this.run(expression); if (last) return last } catch (error) { last = String(error) }
      if (Date.now() > deadline) assert.fail(what + "; the page showed " + JSON.stringify(last))
      await new Promise((r) => setTimeout(r, 50))
    }
  }

  async press(key: string, shift = false): Promise<void> {
    const virtual = key === "Tab" ? 9 : key === "Enter" ? 13 : key === "Escape" ? 27 : 0
    const event = { key, code: key, windowsVirtualKeyCode: virtual, nativeVirtualKeyCode: virtual, modifiers: shift ? 8 : 0 }
    // Enter has to carry its text for the browser to press the focused button with it.
    await this.b.send("Input.dispatchKeyEvent", key === "Enter" ? { type: "keyDown", text: "\r", ...event } : { type: "rawKeyDown", ...event }, this.session)
    await this.b.send("Input.dispatchKeyEvent", { type: "keyUp", ...event }, this.session)
  }

  async shot(name: string): Promise<void> {
    if (!shots) return
    await new Promise((r) => setTimeout(r, 200))
    const { data } = await this.b.send("Page.captureScreenshot", { format: "png" }, this.session)
    writeFileSync(join(shots, name + ".png"), Buffer.from(data, "base64"))
  }
}

let server: Server
let browserProcess: ChildProcess
let browser: Browser
let profile: string
let origin: string
const DESK = { width: 1280, height: 900, mobile: false }
const PHONE = { width: 390, height: 844, mobile: true }

async function inTab(size: Size, body: (tab: Tab) => Promise<void>) {
  const tab = await Tab.open(browser, origin, size)
  try { await body(tab) } finally { await tab.close() }
}

before(async () => {
  assert.ok(existsSync(join(dist, "index.html")), "no built console at " + dist + "; run `npm run build` in web first")
  assert.ok(existsSync(chrome), "no Chrome at " + chrome + "; set CHROME")
  server = daemon()
  await new Promise<void>((ok) => server.listen(0, "127.0.0.1", ok))
  const address = server.address()
  origin = "http://127.0.0.1:" + (typeof address === "object" && address ? address.port : 0)
  profile = mkdtempSync(join(tmpdir(), "clawdline-unify-"))
  browserProcess = spawn(chrome, ["--headless=new", "--remote-debugging-port=0", "--user-data-dir=" + profile,
    "--no-first-run", "--no-default-browser-check", "about:blank"])
  const endpoint = await new Promise<string>((ok, fail) => {
    let said = ""
    const timer = setTimeout(() => fail(new Error("Chrome did not start: " + said)), 20_000)
    browserProcess.stderr?.on("data", (chunk) => {
      said += String(chunk)
      const found = /DevTools listening on (ws:\/\/\S+)/.exec(said)
      if (found) { clearTimeout(timer); ok(found[1]) }
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

// ---- page helpers, as expressions

const STATUS = `document.querySelector(".project-unify > .project-unify-status")?.textContent || ""`
const button = (text: string, scope = "document") =>
  `[...${scope}.querySelectorAll("button")].find(b => b.textContent.trim() === ${JSON.stringify(text)})`

async function openGear(tab: Tab) {
  await tab.go("/#page=projects")
  await tab.until("the Project gear appears", `(() => { const g = document.querySelector('button.project-row-settings[data-place-id="fixture-place"]'); if (!g) return false; g.click(); return true })()`)
  await tab.until("the settings dialog opens", `document.querySelector(".project-setup-dialog")?.open`)
}

/** Each column's rows as "name: mark", read as a person would. */
const COLUMNS = `[...document.querySelectorAll(".project-unify-dialog .project-unify-column")].map(col => ({
  title: col.querySelector("h4").textContent,
  rows: [...col.querySelectorAll(".project-unify-seen")].map(row => row.querySelector("code").textContent + ": " + row.querySelector(".project-unify-seen-mark").textContent.replace(/^[＝＋！]/, "")),
}))`

const LAYOUT = `(() => {
  const body = document.querySelector(".project-unify-dialog-body")
  const dialog = document.querySelector(".project-unify-dialog")
  const cols = [...document.querySelectorAll(".project-unify-dialog .project-unify-column")].map(c => c.getBoundingClientRect())
  const wide = [...dialog.querySelectorAll("*")].filter(n => n.getBoundingClientRect().right > dialog.getBoundingClientRect().right + 1).map(n => n.className || n.tagName)
  return { page: document.documentElement.scrollWidth > innerWidth, body: body.scrollWidth > body.clientWidth, wide: wide.slice(0, 5),
    stacked: cols.length === 2 && cols[0].left === cols[1].left && cols[1].top > cols[0].top,
    sideBySide: cols.length === 2 && cols[0].top === cols[1].top && cols[1].left > cols[0].left }
})()`

/**
 * Opens the preview from the keyboard, as a person's press: a press is a user
 * activation, and without one Chrome groups this dialog's Escape with the
 * settings dialog's and closes both. Focus is checked before the key, because
 * the settings dialog moves focus while it settles.
 */
async function openPreview(tab: Tab) {
  await tab.until("檢視變更 takes focus", `(() => { const b = ${button("檢視變更")}; if (!b || b.disabled) return false; b.focus(); return document.activeElement === b })()`)
  await tab.press("Enter")
  await tab.until("the preview opens", `document.querySelector(".project-unify-dialog")?.open`)
}

async function scrollShots(tab: Tab, name: string) {
  await tab.run(`document.querySelector(".project-unify-dialog-body").scrollTop = 0`)
  await tab.shot(name + "-1")
  const pages = await tab.run(`(() => { const b = document.querySelector(".project-unify-dialog-body"); return Math.ceil(b.scrollHeight / b.clientHeight) })()`)
  for (let i = 1; i < Math.min(pages, 4); i++) {
    await tab.run(`(() => { const b = document.querySelector(".project-unify-dialog-body"); b.scrollTop = ${i} * (b.clientHeight - 40) })()`)
    await tab.shot(`${name}-${i + 1}`)
  }
  await tab.run(`document.querySelector(".project-unify-dialog-body").scrollTop = 0`)
}

test("desk: a failed read says 無法判斷, the preview shows both columns, and apply after a changed plan reaches 已共用", () =>
  inTab(DESK, async (tab) => {
    reset()
    failPlanReads = 1
    await openGear(tab)
    const failed = await tab.until("the failed read is named", `(() => { const s = ${STATUS}; return s.startsWith("無法判斷") && s })()`)
    assert.match(failed, /讀不到共用狀態/)
    assert.equal(await tab.run(`${button("檢視變更")}.disabled`), true, "a failed read offers no preview of nothing")
    assert.equal(await tab.run(`document.querySelector(".project-unify").compareDocumentPosition(document.querySelector(".project-files")) & Node.DOCUMENT_POSITION_FOLLOWING`), 4,
      "the block sits above the file editor")
    await tab.shot("unify-desk-0-read-failed")
    await tab.run(`${button("重新檢查")}.click()`)
    assert.equal(await tab.until("the plan loads", `(() => { const s = ${STATUS}; return s.startsWith("有落差") && s })()`), "有落差（4 項）")

    await openPreview(tab)
    assert.equal(await tab.run(`document.activeElement?.id`), "project-unify-dialog-title", "focus moves into the preview")
    assert.deepEqual(await tab.run(COLUMNS), [
      { title: "Claude 看得到", rows: ["CLAUDE.md: 不變", "AGENTS.md: 新增（套用後才看得到）", "deploy-checklist: 新增（套用後才看得到）", "release-notes: 不變"] },
      { title: "Codex 看得到", rows: ["AGENTS.md: 不變", "deploy-checklist: 不變", "release-notes: 新增（套用後才看得到）"] },
    ])
    const preview = await tab.run(`(() => {
      const d = document.querySelector(".project-unify-dialog")
      return {
        actions: [...d.querySelectorAll(".project-unify-action > p")].map(p => p.textContent),
        inserted: [...d.querySelectorAll(".project-unify-line.is-added")].map(n => n.textContent.trim()),
        arrows: [...d.querySelectorAll(".project-unify-arrow")].map(a => a.getAttribute("aria-label")),
        moves: d.querySelector(".project-unify-moves").textContent,
        conflict: d.querySelector(".project-unify-conflicts").textContent,
        git: d.querySelector(".project-unify-git").textContent,
      }
    })()`)
    assert.equal(preview.actions.length, 3)
    assert.match(preview.actions[0], /最上面加一行 @AGENTS\.md/)
    assert.deepEqual(preview.inserted, ["+@AGENTS.md"])
    assert.deepEqual(preview.arrows, [
      ".claude/skills/deploy-checklist 連結到 .agents/skills/deploy-checklist",
      ".claude/skills/release-notes 搬過去，原處留連結到 .agents/skills/release-notes",
    ])
    assert.match(preview.moves, /會搬移：.claude\/skills\/release-notes → .agents\/skills\/release-notes/)
    assert.match(preview.moves, /不會刪除任何東西/)
    assert.match(preview.conflict, /Codex 看不到/)
    assert.match(preview.conflict, /Never deploy on Fridays\./)
    assert.match(preview.git, /不會 commit 到 git/)
    const layout = await tab.run(LAYOUT)
    assert.equal(layout.sideBySide, true, "two columns side by side on a desk: " + JSON.stringify(layout))
    assert.equal(layout.body, false, "nothing scrolls sideways: " + JSON.stringify(layout))
    await scrollShots(tab, "unify-desk-1-before-apply")

    // Keyboard only: Tab from the heading reaches the apply button.
    let reached = false
    for (let i = 0; i < 40 && !reached; i++) {
      await tab.press("Tab")
      reached = await tab.run(`document.activeElement?.textContent === "套用這些變更"`)
    }
    assert.equal(reached, true, "apply is reachable by Tab")
    assert.equal(await tab.run(`getComputedStyle(document.activeElement).outlineStyle !== "none"`), true, "the focused apply button shows a focus ring")

    changeBeforeNextApply = true
    await tab.press("Enter")
    await tab.until("a changed plan is reread, not applied", `document.querySelector(".project-unify-notice")?.textContent.includes("已重新讀取最新的預覽")`)
    assert.deepEqual(applies.map(a => [a.version, a.status]), [["plan-one", 409]])
    await tab.run(`${button("套用這些變更")}.click()`)
    await tab.until("apply reaches 已共用", `document.querySelector(".project-unify-dialog .project-unify-verdict.is-ready")?.textContent === "Claude 和 Codex 已看到相同的規則與 skills"`)
    assert.deepEqual(applies.map(a => [a.version, a.status]), [["plan-one", 409], ["plan-two", 200]])
    assert.notEqual(applies[0].key, applies[1].key, "each press carries a fresh Idempotency-Key")
    assert.ok(applies.every(a => /^[0-9a-f-]{36}$/.test(a.key)), "the key is a UUID")
    assert.equal(await tab.run(STATUS), "已共用")
    assert.equal(await tab.run(`!!${button("套用這些變更")}`), false, "a unified plan offers nothing to apply")
    assert.match(await tab.run(`document.querySelector(".project-unify-notice").textContent`), /已套用 3 項變更/)
    await tab.shot("unify-desk-2-after-apply")

    await tab.press("Escape")
    await new Promise((r) => setTimeout(r, 300))
    const closed = await tab.run(`({ unify: document.querySelector(".project-unify-dialog").open, setup: document.querySelector(".project-setup-dialog").open, active: document.activeElement?.textContent })`)
    assert.deepEqual(closed, { unify: false, setup: true, active: "檢視變更" }, "Escape closes only the preview and focus returns to its opener")
    assert.equal(await tab.run(`document.querySelector(".project-setup-dialog").open`), true, "closing the preview leaves the settings open")
  }))

test("phone: at 390px the columns stack, nothing scrolls sideways, and apply reaches 已共用", () =>
  inTab(PHONE, async (tab) => {
    reset()
    await openGear(tab)
    assert.equal(await tab.until("the plan loads", `(() => { const s = ${STATUS}; return s.startsWith("有落差") && s })()`), "有落差（4 項）")
    assert.equal(await tab.run(`document.documentElement.scrollWidth > innerWidth`), false)
    await tab.run(`document.querySelector(".project-unify").scrollIntoView({ block: "start" })`)
    await tab.shot("unify-phone-0-block")
    await tab.run(`${button("檢視變更")}.click()`)
    await tab.until("the preview opens", `document.querySelector(".project-unify-dialog")?.open`)
    const layout = await tab.run(LAYOUT)
    assert.equal(layout.stacked, true, "the two columns stack: " + JSON.stringify(layout))
    assert.equal(layout.page || layout.body, false, "nothing scrolls sideways: " + JSON.stringify(layout))
    assert.deepEqual(layout.wide, [], "nothing pokes past the dialog's edge")
    assert.equal(await tab.run(`document.querySelector(".project-unify-dialog").getBoundingClientRect().width`), 390, "the preview fills the phone")
    await scrollShots(tab, "unify-phone-1-before-apply")
    await tab.run(`${button("套用這些變更")}.click()`)
    await tab.until("apply reaches 已共用", `!!document.querySelector(".project-unify-dialog .project-unify-verdict.is-ready")`)
    assert.equal(await tab.run(STATUS), "已共用")
    assert.equal((await tab.run(LAYOUT)).body, false)
    await tab.shot("unify-phone-2-after-apply")
    await tab.run(`${button("關閉", `document.querySelector(".project-unify-dialog")`)}.click()`)
    await tab.until("focus returns to the opener", `document.activeElement?.textContent === "檢視變更"`)
    await new Promise((r) => setTimeout(r, 200))
    assert.deepEqual(await tab.run(`({ setup: document.querySelector(".project-setup-dialog").open, block: !!document.querySelector(".project-setup-dialog .project-unify"), active: document.activeElement?.textContent })`),
      { setup: true, block: true, active: "檢視變更" }, "closing the preview leaves the Project settings as they were")
  }))

test("phone: an unknown plan says what could not be read and offers no apply", () =>
  inTab(PHONE, async (tab) => {
    reset()
    unifyState = "unknown"
    await openGear(tab)
    assert.equal(await tab.until("the unknown plan is named", `(() => { const s = ${STATUS}; return s.startsWith("無法判斷") && s })()`), "無法判斷（讀不到 CLAUDE.md）")
    await tab.run(`${button("檢視變更")}.click()`)
    await tab.until("the preview opens", `document.querySelector(".project-unify-dialog")?.open`)
    assert.match(await tab.run(`document.querySelector(".project-unify-verdict.is-unknown").textContent`), /讀不到 CLAUDE\.md/)
    assert.equal(await tab.run(`!!${button("套用這些變更")}`), false)
    assert.match(await tab.run(`document.querySelector(".project-unify-dialog-foot").textContent`), /不能套用/)
    await tab.shot("unify-phone-3-unknown")
    assert.deepEqual(applies, [])
  }))

test("desk: a run that stops part-way shows what ran, what failed and the plan read again", () =>
  inTab(DESK, async (tab) => {
    reset()
    nextApply = "stop"
    await openGear(tab)
    await tab.until("the plan loads", `${STATUS}.startsWith("有落差")`)
    await openPreview(tab)
    await tab.run(`${button("套用這些變更")}.click()`)
    const stopped = await tab.until("the stop is shown", `(() => { const n = document.querySelector(".project-unify-stopped"); return n && {
      text: n.textContent, ran: [...n.querySelectorAll(".is-ran")].map(li => li.textContent), failed: [...n.querySelectorAll(".is-failed")].map(li => li.textContent) } })()`)
    assert.match(stopped.text, /套用到一半停下來了/)
    assert.match(stopped.text, /要建立連結的位置已經有別的東西/)
    assert.equal(stopped.ran.length, 1)
    assert.match(stopped.ran[0], /@AGENTS\.md/)
    assert.equal(stopped.failed.length, 1)
    assert.match(stopped.failed[0], /deploy-checklist/)
    assert.equal(await tab.run(`document.querySelectorAll(".project-unify-dialog .project-unify-action").length`), 2, "the reread plan has the two actions left")
    assert.equal(await tab.run(STATUS), "有落差（3 項）")
    await tab.shot("unify-desk-3-stopped")
  }))

test("desk: a lost apply answer rereads the plan and says 已共用 only because the reread says so", () =>
  inTab(DESK, async (tab) => {
    reset()
    nextApply = "lose"
    await openGear(tab)
    await tab.until("the plan loads", `${STATUS}.startsWith("有落差")`)
    await openPreview(tab)
    await tab.run(`${button("套用這些變更")}.click()`)
    await tab.until("the reread confirms", `document.querySelector(".project-unify-notice")?.textContent === "重新讀取後確認：已共用。"`)
    assert.equal(await tab.run(STATUS), "已共用")
    assert.deepEqual(applies.map(a => a.status), [504])
  }))
