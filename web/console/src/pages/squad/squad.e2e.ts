// Build the console, then run this fixture in headless Chrome. Screenshots are
// written only when CLAWDLINE_SHOTS names a directory.
import { test, before, after } from "node:test"
import assert from "node:assert/strict"
import { spawn, type ChildProcess } from "node:child_process"
import { createServer, type Server, type ServerResponse } from "node:http"
import { existsSync, mkdtempSync, readFileSync, rmSync, statSync, writeFileSync } from "node:fs"
import { tmpdir } from "node:os"
import { dirname, extname, join, normalize, resolve } from "node:path"
import { fileURLToPath } from "node:url"

const here = dirname(fileURLToPath(import.meta.url))
const dist = resolve(process.env.CLAWDLINE_DIST || resolve(here, "../../../dist"))
const chrome = process.env.CHROME || "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
const shots = process.env.CLAWDLINE_SHOTS || ""
const icon = { accent: "#d97757", cells: [".##.", "#oo#", "####", ".##."].map((line) => line.split("").map((cell) => cell === "#" ? "#d97757" : cell === "o" ? "#16161a" : "#24242a")) }
const names = (text: string) => ({ en: text, "zh-Hant": text })
const definitions = Array.from({ length: 42 }, (_, index) => ({
  definition_id: "clawdline.persona.role-" + (index + 1), short_id: "role-" + (index + 1),
  version: "v1", source: "clawdline", license: "MIT", digest: "fixture",
  name: names("角色 " + String(index + 1).padStart(2, "0")),
  summary: names(index === 0 ? "一位具有很長說明的角色，可處理跨團隊的研究和設計工作。" : "協助完成不同工作。"),
  body: index === 0 ? "完整角色定義。\n".repeat(50) : "完整角色定義。",
  icon, teams: [{ id: "clawdline.team.design", version: "v1" }],
  skills: index === 0 ? [{ id: "community.skill.draft", version: "v1", enabled: true }] : [],
  builtin: true,
}))
const catalog = {
  catalog_version: 8, definitions,
  teams: [{ team_id: "clawdline.team.design", version: "v1", source: "clawdline", license: "MIT", digest: "fixture",
    name: names("設計小隊"), personas: definitions.map((row) => ({ id: row.definition_id, version: row.version })), builtin: true }],
  skills: [{ skill_id: "community.skill.draft", version: "v1", source: "community", license: "MIT", digest: "fixture",
    name: names("草稿整理"), purpose: names("整理初稿與引用"), content: "完整技能內容。\n".repeat(40), icon, builtin: false }],
}
const field = <T>(value: T, source: "default" | "global" | "project" = "default", version = 0) =>
  ({ value, source, present: source !== "default", version })
let projectAText = ""
let projectAVersion = 2
let refuseNextSettingsWrite = false
const settingsWrites: Record<string, unknown>[] = []
function settings(scope: string) {
  const project = scope !== "global"
  return {
    scope_id: scope, scope_kind: project ? "repo" : "global", motion: field(true, "global", 1),
    motion_settings_version: project ? 0 : 1,
    personas: definitions.map((row, index) => ({
      definition_id: row.definition_id, settings_version: project && scope === "project-a" && index === 0 ? projectAVersion : 0,
      handbook: index === 0 && scope === "project-a" ? field(projectAText, "project", projectAVersion) : field("全域手冊", "global", 1),
      global_handbook: field("全域手冊", "global", 1),
      auto_assign: index === 0 && scope === "project-a" ? field(false, "project", 2) : field(true, "global", 1),
      skills: field(row.skills, project ? "global" : "default", project ? 1 : 0),
    })),
  }
}
const scopes = { scopes: [
  { place_id: "place-a", scope_id: "project-a", kind: "repo", label: "Project A", path: "/work/a" },
  { place_id: "place-b", scope_id: "project-b", kind: "repo", label: "Project B", path: "/work/b" },
], scope_ids: ["project-a"] }
const types: Record<string, string> = { ".js": "text/javascript", ".css": "text/css", ".json": "application/json", ".png": "image/png", ".svg": "image/svg+xml", ".webp": "image/webp", ".ico": "image/x-icon" }
const json = (res: ServerResponse, status: number, body: unknown) => { res.writeHead(status, { "content-type": "application/json" }); res.end(JSON.stringify(body)) }
const packageRequests: { path: string; body: Record<string, unknown> }[] = []
let refuseAdoption = false
let boundFixture = false
let bindingDelayMs = 0
let bindingReads = 0
let eventHeadReads = 0
let eventPageReads = 0
const eventRows: Record<string, unknown>[] = []
function document() {
  const html = readFileSync(join(dist, "index.html"), "utf8")
  const words = JSON.parse(readFileSync(join(dist, "strings", "zh-Hant.json"), "utf8"))
  words.lang = "zh-Hant"; words.dir = "ltr"
  return html.replace("<!-- clawdline:strings -->", "<script>window.__strings=" + JSON.stringify(words).replaceAll("</", "<\\/") + "</script>").replace("<!-- clawdline:cloud -->", "")
}
function fixture(): Server {
  return createServer((req, res) => {
    const path = new URL(req.url ?? "/", "http://fixture").pathname
    if (path === "/v1/squad/catalog") return json(res, 200, catalog)
    if (path === "/v1/squad/scopes") return json(res, 200, scopes)
    if (path === "/v1/squad/settings") {
      if (req.method === "PUT") {
        const chunks: Buffer[] = []
        req.on("data", (chunk) => chunks.push(chunk))
        req.on("end", () => {
          const body = JSON.parse(Buffer.concat(chunks).toString() || "{}") as Record<string, unknown>
          settingsWrites.push(body)
          if (refuseNextSettingsWrite) {
            refuseNextSettingsWrite = false
            projectAText = "他人更新的手冊"; projectAVersion++
            return json(res, 409, { error: "version_conflict", detail: "Settings changed", current_version: projectAVersion })
          }
          return json(res, 200, { scope_id: "project-a", version: projectAVersion + 1 })
        })
        return
      }
      const url = new URL(req.url ?? "/", "http://fixture")
      const place = url.searchParams.get("place_id")
      return json(res, 200, settings(place === "place-a" ? "project-a" : place === "place-b" ? "project-b" : "global"))
    }
    if (path === "/v1/squad/events/head") { eventHeadReads++; return json(res, 200, { seq: eventRows.at(-1)?.seq ?? 0 }) }
    if (path === "/v1/squad/session-bindings") {
      bindingReads++
      setTimeout(() => json(res, 200, { bindings: boundFixture ? [{
        session_id: "terminal-1", conversation_id: "conversation-1", state: "bound", snapshot_id: "snapshot-1",
        definition_id: definitions[0].definition_id, scope_id: "global",
      }] : [] }), bindingDelayMs)
      return
    }
    if (path === "/v1/squad/events") {
      eventPageReads++
      const after = Number(new URL(req.url ?? "/", "http://fixture").searchParams.get("after") ?? 0)
      const events = eventRows.filter((row) => Number(row.seq) > after)
      return json(res, 200, { events, next_after: events.at(-1)?.seq ?? after, has_more: false })
    }
    if (path.startsWith("/v1/squad-packages/")) {
      const chunks: Buffer[] = []
      req.on("data", (chunk) => chunks.push(chunk))
      req.on("end", () => {
        const body = JSON.parse(Buffer.concat(chunks).toString() || "{}") as Record<string, unknown>
        packageRequests.push({ path, body })
        if (path.endsWith("/preview")) return json(res, 200, {
          archive_digest: "digest", preview_digest: "preview-digest", preview_token: "token", catalog_version: 8,
          scope: body.scope_id, source: "community", license: "MIT", private_scopes: ["global", "project-a"],
          changes: [{ kind: "definition", id: "community.role", version: "v1", action: "add", dependents: [] },
            { kind: "skill", id: "community.skill", version: "v1", action: "conflict", conflict_code: "same_version_different_content", dependents: ["community.role"] }],
        })
        if (path.endsWith("/adopt")) return refuseAdoption
          ? json(res, 409, { error: "version_conflict", detail: "Catalog changed", current_version: 9 })
          : json(res, 200, { catalog_version: 9, archive_digest: "digest", adopted_ids: ["community.role"], private_scopes: body.private_scopes })
        if (path.endsWith("/export")) return json(res, 200, { archive_base64: "UEsDBA==", archive_digest: "digest", file_name: "squad.zip", mime_type: "application/zip" })
        return json(res, 404, { error: "not_found" })
      })
      return
    }
    if (path === "/v1/sessions") return json(res, 200, { at: Date.now(), scan: { complete: true, emptyAuthoritative: true, epoch: 1, generation: 1 },
      sessions: boundFixture ? [{ id: "terminal-1", sessionId: "conversation-1", label: "角色 Session", state: "idle", assistant: "codex" }] : [] })
    if (path === "/v1/health") return json(res, 200, { ok: true })
    if (path === "/v1/orchestrator/tasks") return json(res, 200, { at: Date.now(), tasks: [] })
    if (path === "/v1/events") { res.writeHead(200, { "content-type": "text/event-stream" }); res.write("event: sessions\ndata: " + JSON.stringify({ at: Date.now(), scan: { complete: true, emptyAuthoritative: true, epoch: 1, generation: 1 }, sessions: [] }) + "\n\n"); return }
    if (path.startsWith("/v1/")) return json(res, 404, { error: "not_found", detail: path })
    if (path === "/") { res.writeHead(200, { "content-type": "text/html; charset=utf-8" }); return res.end(document()) }
    const file = normalize(join(dist, path))
    if (!file.startsWith(dist + "/") || !existsSync(file) || !statSync(file).isFile()) return json(res, 404, { error: "not_found", detail: path })
    res.writeHead(200, { "content-type": types[extname(file)] ?? "application/octet-stream" })
    res.end(readFileSync(file))
  })
}

class Browser {
  private seq = 0
  private pending = new Map<number, { resolve: (v: any) => void; reject: (e: Error) => void }>()
  private ws: WebSocket
  private constructor(ws: WebSocket) {
    this.ws = ws
    ws.addEventListener("message", (event) => {
      const message = JSON.parse(String(event.data))
      if (message.id === undefined) return
      const pending = this.pending.get(message.id); this.pending.delete(message.id)
      if (message.error) pending?.reject(new Error(message.error.message))
      else pending?.resolve(message.result)
    })
  }
  static async connect(url: string) {
    const ws = new WebSocket(url)
    await new Promise<void>((resolve, reject) => { ws.addEventListener("open", () => resolve()); ws.addEventListener("error", () => reject(new Error("Chrome websocket failed"))) })
    return new Browser(ws)
  }
  send(method: string, params: object = {}, sessionId?: string): Promise<any> {
    const id = ++this.seq
    this.ws.send(JSON.stringify({ id, method, params, sessionId }))
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => { this.pending.delete(id); reject(new Error(method + " timed out")) }, 15_000)
      this.pending.set(id, { resolve: (value) => { clearTimeout(timer); resolve(value) }, reject: (error) => { clearTimeout(timer); reject(error) } })
    })
  }
  close() { this.ws.close() }
}

let server: Server
let processChrome: ChildProcess
let browser: Browser
let profile = ""
let origin = ""
before(async () => {
  assert.ok(existsSync(join(dist, "index.html")))
  assert.ok(existsSync(chrome))
  server = fixture()
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve))
  const address = server.address()
  origin = "http://127.0.0.1:" + (typeof address === "object" && address ? address.port : 0)
  profile = mkdtempSync(join(tmpdir(), "clawdline-squad-"))
  processChrome = spawn(chrome, ["--headless=new", "--remote-debugging-port=0", "--user-data-dir=" + profile,
    "--no-first-run", "--no-default-browser-check", "about:blank"])
  const endpoint = await new Promise<string>((resolve, reject) => {
    let said = ""
    const timer = setTimeout(() => reject(new Error("Chrome did not start: " + said)), 20_000)
    processChrome.once("exit", (code, signal) => { clearTimeout(timer); reject(new Error("Chrome exited before DevTools: " + code + " / " + signal + " " + said)) })
    processChrome.once("error", (error) => { clearTimeout(timer); reject(error) })
    processChrome.stderr?.on("data", (chunk) => {
      said += String(chunk)
      const match = /DevTools listening on (ws:\/\/\S+)/.exec(said)
      if (match) { clearTimeout(timer); resolve(match[1]) }
    })
  })
  browser = await Browser.connect(endpoint)
})

test("a Project handbook version conflict retains the user's draft beside the new server text", async () => {
  projectAText = ""; projectAVersion = 2; refuseNextSettingsWrite = true; settingsWrites.length = 0
  await tab(1440, 900, async (evaluate) => {
    await evaluate('(() => { const el = document.querySelector("#squad .squad-scope select"); el.value = "project-a"; el.dispatchEvent(new Event("change", { bubbles: true })); })()')
    const scopeDeadline = Date.now() + 5_000
    while (!(await evaluate('document.querySelector("#squad .squad-scope select").value === "project-a" && !!document.querySelector("#squad .squad-persona-card")'))) {
      if (Date.now() > scopeDeadline) throw new Error("Project A did not load")
      await new Promise((resolve) => setTimeout(resolve, 50))
    }
    await evaluate('document.querySelector("#squad .squad-persona-card").click()')
    await evaluate('(() => { const el = document.querySelector("#squad .squad-handbook-editor textarea"); Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, "value").set.call(el, "我的未儲存草稿"); el.dispatchEvent(new Event("input", { bubbles: true })); })()')
    assert.equal(await evaluate('document.querySelector("#squad .squad-handbook-editor textarea").value'), "我的未儲存草稿")
    await evaluate('document.querySelector("#squad .squad-actions button").click()')
    const conflictDeadline = Date.now() + 5_000
    while (!(await evaluate('!!document.querySelector("#squad .squad-conflict")'))) {
      if (Date.now() > conflictDeadline) throw new Error("version conflict did not show")
      await new Promise((resolve) => setTimeout(resolve, 50))
    }
    assert.equal(await evaluate('document.querySelector("#squad .squad-handbook-editor textarea").value'), "我的未儲存草稿")
    assert.match(await evaluate('document.querySelector("#squad .squad-conflict").textContent'), /他人更新的手冊/)
  })
  assert.equal(settingsWrites.length, 1)
  assert.deepEqual(settingsWrites[0].overrides, { handbook: { present: true, value: "我的未儲存草稿" } })
  assert.equal(settingsWrites[0].expected_version, 2)
  projectAText = ""; projectAVersion = 2
})
after(async () => {
  browser?.close()
  if (processChrome && processChrome.exitCode === null && processChrome.signalCode === null) {
    const gone = new Promise((resolve) => processChrome.once("exit", resolve))
    processChrome.kill()
    await Promise.race([gone, new Promise((resolve) => setTimeout(resolve, 3000))])
    if (processChrome.exitCode === null && processChrome.signalCode === null) processChrome.kill("SIGKILL")
  }
  server?.closeAllConnections()
  await Promise.race([new Promise<void>((resolve) => server ? server.close(() => resolve()) : resolve()),
    new Promise<void>((resolve) => setTimeout(resolve, 3000))])
  if (profile) rmSync(profile, { recursive: true, force: true, maxRetries: 5 })
})

async function tab(width: number, height: number, run: (evaluate: (code: string) => Promise<any>, shot: (name: string) => Promise<void>) => Promise<void>, reducedMotion = false) {
  const { targetId } = await browser.send("Target.createTarget", { url: "about:blank" })
  const { sessionId } = await browser.send("Target.attachToTarget", { targetId, flatten: true })
  try {
    await browser.send("Page.enable", {}, sessionId)
    await browser.send("Runtime.enable", {}, sessionId)
    await browser.send("Emulation.setDeviceMetricsOverride", { width, height, mobile: width < 600, deviceScaleFactor: 1 }, sessionId)
    if (reducedMotion) await browser.send("Emulation.setEmulatedMedia", { features: [{ name: "prefers-reduced-motion", value: "reduce" }] }, sessionId)
    await browser.send("Page.navigate", { url: origin + "/#page=squad" }, sessionId)
    const evaluate = async (expression: string) => {
      const answer = await browser.send("Runtime.evaluate", { expression, returnByValue: true, awaitPromise: true }, sessionId)
      if (answer.exceptionDetails) throw new Error(answer.exceptionDetails.exception?.description ?? answer.exceptionDetails.text)
      return answer.result.value
    }
    const deadline = Date.now() + 10_000
    while (!(await evaluate('!document.documentElement.classList.contains("booting") && document.querySelectorAll("#squad:not([hidden]) .squad-persona-card").length').catch(() => 0))) {
      if (Date.now() > deadline) throw new Error("squad roster did not load")
      await new Promise((resolve) => setTimeout(resolve, 50))
    }
    const shot = async (name: string) => {
      if (!shots) return
      const { data } = await browser.send("Page.captureScreenshot", { format: "png" }, sessionId)
      writeFileSync(join(shots, name + ".png"), Buffer.from(data, "base64"))
    }
    await run(evaluate, shot)
  } finally { await browser.send("Target.closeTarget", { targetId }).catch(() => undefined) }
}

test("42 roles, Project inheritance, desktop and mobile return are visible without horizontal overflow", async () => {
  await tab(1440, 900, async (evaluate, shot) => {
    assert.equal(await evaluate('document.querySelectorAll("#squad .squad-persona-card").length'), 42)
    assert.equal(await evaluate('document.documentElement.scrollWidth <= innerWidth'), true)
    await evaluate('(() => { const el = document.querySelector("#squad .squad-search input"); Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value").set.call(el, "no matching role"); el.dispatchEvent(new Event("input", { bubbles: true })); })()')
    await evaluate('new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve)))')
    assert.equal(await evaluate('document.querySelectorAll("#squad .squad-persona-card").length'), 0)
    assert.match(await evaluate('document.querySelector("#squad .squad-detail .squad-empty").textContent'), /選擇角色/)
    await evaluate('document.querySelector("#squad .squad-roster .squad-empty button").click()')
    await evaluate('new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve)))')
    assert.equal(await evaluate('document.querySelectorAll("#squad .squad-persona-card").length'), 42)
    await shot("squad-desktop-global")
    await evaluate('document.querySelector("#squad .squad-persona-card").click()')
    assert.match(await evaluate('document.querySelector("#squad .squad-detail").textContent'), /完整角色定義/)
    await evaluate('(() => { const el = document.querySelector("#squad .squad-scope select"); el.value = "project-a"; el.dispatchEvent(new Event("change", { bubbles: true })); })()')
    const deadline = Date.now() + 5_000
    while (!(await evaluate('document.querySelector("#squad .squad-scope select").value === "project-a" && document.querySelector("#squad .squad-detail")?.textContent.includes("明確覆寫為空白")'))) {
      if (Date.now() > deadline) throw new Error("Project A settings not shown")
      await new Promise((resolve) => setTimeout(resolve, 50))
    }
    assert.match(await evaluate('document.querySelector("#squad .squad-handbook-global").textContent'), /全域手冊/)
    await shot("squad-desktop-project")
  })
  await tab(390, 844, async (evaluate, shot) => {
    assert.equal(await evaluate('document.documentElement.scrollWidth <= innerWidth'), true)
    await shot("squad-mobile-roster")
    await evaluate('document.querySelector("#squad .squad-persona-card").click()')
    await evaluate('new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve)))')
    assert.equal(await evaluate('getComputedStyle(document.querySelector("#squad .squad-back")).display !== "none"'), true)
    assert.equal(await evaluate('document.activeElement?.id'), "squad-detail-title")
    await shot("squad-mobile-detail")
    await evaluate('document.querySelector("#squad .squad-back").click()')
    await evaluate('new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve)))')
    assert.equal(await evaluate('document.activeElement?.classList.contains("squad-persona-card")'), true)
    assert.equal(await evaluate('document.documentElement.scrollWidth <= innerWidth'), true)
  })
})

test("package preview preserves explicit privacy choices and stale adoption requires a new preview", async () => {
  packageRequests.length = 0
  refuseAdoption = true
  await tab(1440, 900, async (evaluate) => {
    await evaluate('document.querySelector("#squad .squad-pack-actions button").click()')
    assert.equal(await evaluate('document.querySelector("#squad dialog").open'), true)
    await evaluate(`(() => { const input = document.querySelector('#squad dialog input[type="file"]'); const transfer = new DataTransfer(); transfer.items.add(new File([new Uint8Array([80,75,3,4])], 'squad.zip', { type: 'application/zip' })); input.files = transfer.files; input.dispatchEvent(new Event('change', { bubbles: true })); document.querySelector('#squad .squad-file + button').click(); })()`)
    const deadline = Date.now() + 5_000
    while (!(await evaluate('!!document.querySelector("#squad .squad-pack-preview")'))) {
      if (Date.now() > deadline) throw new Error("package preview did not load")
      await new Promise((resolve) => setTimeout(resolve, 50))
    }
    assert.match(await evaluate('document.querySelector("#squad .squad-pack-preview").textContent'), /community.*待審.*MIT.*待審/)
    assert.equal(await evaluate('document.querySelectorAll("#squad .squad-pack-preview input[type=checkbox]:checked").length'), 0)
    assert.equal(await evaluate('document.querySelector("#squad .squad-conflict-choice select option[value=replace]")'), null)
    await evaluate('(() => { const el = document.querySelector("#squad .squad-conflict-choice select"); el.value = "keep"; el.dispatchEvent(new Event("change", { bubbles: true })); })()')
    await evaluate('document.querySelector("#squad .squad-pack-preview button").click()')
    const staleDeadline = Date.now() + 5_000
    while (!(await evaluate('document.querySelector("#squad .squad-dialog-error")?.textContent.includes("重新預覽")'))) {
      if (Date.now() > staleDeadline) throw new Error("stale preview refusal not shown")
      await new Promise((resolve) => setTimeout(resolve, 50))
    }
    assert.equal(await evaluate('document.querySelector("#squad .squad-pack-preview")'), null)
  })
  assert.equal(packageRequests[0].path, "/v1/squad-packages/preview")
  assert.equal(packageRequests[0].body.scope_id, "global")
  assert.deepEqual(packageRequests[1].body.private_scopes, [])
  assert.deepEqual(packageRequests[1].body.choices, { "community.skill": "keep" })

  packageRequests.length = 0
  refuseAdoption = false
  await tab(1440, 900, async (evaluate) => {
    await evaluate('document.querySelectorAll("#squad .squad-pack-actions button")[1].click()')
    await evaluate('document.querySelector("#squad dialog > button").click()')
    const deadline = Date.now() + 5_000
    while (packageRequests.length < 1) {
      if (Date.now() > deadline) throw new Error("public package export did not run")
      await new Promise((resolve) => setTimeout(resolve, 50))
    }
    assert.deepEqual(packageRequests[0].body, { private_scopes: [], confirm_private: false })
  })
  packageRequests.length = 0
  await tab(1440, 900, async (evaluate) => {
    await evaluate('document.querySelectorAll("#squad .squad-pack-actions button")[1].click()')
    await evaluate('document.querySelectorAll("#squad dialog .squad-check input")[0].click()')
    await evaluate('document.querySelectorAll("#squad dialog .squad-check input")[1].click()')
    await evaluate('document.querySelector("#squad dialog > button").click()')
    assert.equal(packageRequests.length, 0, "private scopes require a second confirmation")
    assert.match(await evaluate('document.querySelector("#squad .squad-dialog-error").textContent'), /請先確認/)
    await evaluate('document.querySelectorAll("#squad dialog .squad-check input")[3].click()')
    await evaluate('document.querySelector("#squad dialog > button").click()')
    const deadline = Date.now() + 5_000
    while (packageRequests.length < 1) {
      if (Date.now() > deadline) throw new Error("private package export did not run")
      await new Promise((resolve) => setTimeout(resolve, 50))
    }
    assert.deepEqual(packageRequests[0].body, { private_scopes: ["global", "project-a"], confirm_private: true })
  })
})

test("an existing receipt and a read stay quiet while a new bound applied receipt animates once", async () => {
  boundFixture = true
  bindingDelayMs = 0
  eventRows.length = 0
  const receipt = (seq: number, status: string) => ({ seq, receipt_id: "receipt-" + seq, snapshot_id: "snapshot-1",
    conversation_id: "conversation-1", definition_id: definitions[0].definition_id, scope_id: "global",
    skill_id: "community.skill.draft", skill_version: "v1", status, at: Date.now() })
  eventRows.push(receipt(1, "applied"))
  const initialHeads = eventHeadReads
  const initialPages = eventPageReads
  await tab(1440, 900, async (evaluate) => {
    const baselineDeadline = Date.now() + 5_000
    while (eventHeadReads <= initialHeads || eventPageReads <= initialPages) {
      if (Date.now() > baselineDeadline) throw new Error("event baseline and first poll did not finish")
      await new Promise((resolve) => setTimeout(resolve, 50))
    }
    assert.equal(await evaluate('!!document.querySelector("#squad .squad-use")'), false, "the initial head is a baseline")
    eventRows.push(receipt(2, "read"))
    await new Promise((resolve) => setTimeout(resolve, 3500))
    assert.equal(await evaluate('!!document.querySelector("#squad .squad-use")'), false, "a read is not a use animation")
    eventRows.push(receipt(3, "applied"))
    const deadline = Date.now() + 5_000
    while (!(await evaluate('!!document.querySelector("#squad .squad-use[data-animate]")'))) {
      if (Date.now() > deadline) throw new Error("new bound applied receipt did not animate")
      await new Promise((resolve) => setTimeout(resolve, 50))
    }
    assert.match(await evaluate('document.querySelector("#squad .squad-use").textContent'), /角色 Session 回報使用 草稿整理/)
    await new Promise((resolve) => setTimeout(resolve, 2500))
    bindingDelayMs = 700
    const before = bindingReads
    eventRows.push(receipt(4, "applied"))
    const bindingDeadline = Date.now() + 5_000
    while (bindingReads <= before) {
      if (Date.now() > bindingDeadline) throw new Error("delayed binding read did not start")
      await new Promise((resolve) => setTimeout(resolve, 50))
    }
    await evaluate('(() => { const el = document.querySelector("#squad .squad-scope select"); el.value = "project-a"; el.dispatchEvent(new Event("change", { bubbles: true })); })()')
    await new Promise((resolve) => setTimeout(resolve, 1100))
    assert.equal(await evaluate('!!document.querySelector("#squad .squad-use")'), false, "a late global receipt cannot play after switching Project")
    bindingDelayMs = 0
  })
  await tab(390, 844, async (evaluate) => {
    assert.equal(await evaluate('matchMedia("(prefers-reduced-motion: reduce)").matches'), true)
    await evaluate('document.querySelector("#squad .squad-persona-card").click()')
    await new Promise((resolve) => setTimeout(resolve, 150))
    eventRows.push(receipt(5, "applied"))
    const deadline = Date.now() + 5_000
    while (!(await evaluate('!!document.querySelector("#squad .squad-use")'))) {
      if (Date.now() > deadline) throw new Error("reduced-motion receipt status did not appear")
      await new Promise((resolve) => setTimeout(resolve, 50))
    }
    assert.equal(await evaluate('document.querySelector("#squad .squad-use").hasAttribute("data-animate")'), false)
  }, true)
  boundFixture = false
  eventRows.length = 0
})
