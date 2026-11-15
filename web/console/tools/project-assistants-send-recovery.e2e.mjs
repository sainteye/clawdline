/**
 * Uncertain Bot send recovery, in a real browser through `clawdline browser`.
 *
 * The page mounts the production Chat with the production
 * ProjectAssistantSourceClient. Only its transport is synthetic: an in-page
 * source that records receipts and turns the way the Go source does
 * (receipt.resource_id is the admitted turn, whose idempotency_key is the
 * send's). Nothing reaches a daemon, Cloud, provider or model.
 *
 *   SEND_RECOVERY_ARTIFACTS=<dir> node tools/project-assistants-send-recovery.e2e.mjs
 *   SEND_RECOVERY_SOURCE=<other web/console/src> ...   # run the same steps against a baseline tree
 *   ... --serve-only                                    # print the URL and keep serving
 *   SEND_RECOVERY_URL=<served URL> SEND_RECOVERY_TAB=<tab> ... # drive a tab this Session already opened
 *     on that server (after a daemon restart, a tab the script opened was
 *     refused as another Session's while one opened from the shell was not)
 *
 * Exit 0 only when every step passed; the tab and server it opened are closed.
 */
import assert from "node:assert/strict"
import { execFileSync } from "node:child_process"
import { createServer } from "node:http"
import { mkdir, readFile, writeFile } from "node:fs/promises"
import path from "node:path"
import { fileURLToPath } from "node:url"
import { build } from "esbuild"

const here = path.dirname(fileURLToPath(import.meta.url))
const src = path.resolve(process.env.SEND_RECOVERY_SOURCE ?? path.join(here, "..", "src"))
const artifacts = process.env.SEND_RECOVERY_ARTIFACTS
const serveOnly = process.argv.includes("--serve-only")
if (!artifacts && !serveOnly) throw new Error("Set SEND_RECOVERY_ARTIFACTS to a task-owned evidence directory")
if (artifacts) await mkdir(artifacts, { recursive: true })

const entry = String.raw`
import React from "react"
import { createRoot } from "react-dom/client"
import { Chat } from "./project-assistants/Chat"
import { fixtureSource } from "./project-assistants/fixtures"
import { words } from "./project-assistants/words"
import { createViewState } from "./project-assistants/view-state"
import { ProjectAssistantSourceClient } from "./cloud/project-assistants"
import "./project-assistants/styles.css"

export function mountFixture() {
  const language = new URLSearchParams(location.search).get("lang") ?? "en"
  const fixture = fixtureSource("studio")
  const turns = []
  const state = { mode: "normal", sends: 0, transported: [], exports: 0, revokeSend: false }
  const live = { accountKey: "fixture-account", readerID: "fixture-reader", machineID: "studio", authorizationRevision: "grant-1", apiLevel: 61, paired: true, online: true, writable: true }
  // One grant refusal at the send's own pre-transport check, and nowhere else.
  const grant = () => { if (state.revokeSend && /ProjectAssistantSourceClient\.send\b/.test(new Error().stack ?? "")) { state.revokeSend = false; return { ...live, authorizationRevision: "replaced-grant" } } return live }
  const json = (value, status = 200) => Response.json(value, { status })
  const refusal = error => json(error, 409)
  const lose = () => { throw new TypeError("synthetic response lost after the write") }
  const transport = async (input, init = {}) => {
    const url = new URL(input, location.origin), method = init.method ?? "GET", q = Object.fromEntries(url.searchParams)
    const signal = init.signal ?? new AbortController().signal
    const match = /^\/v1\/project-assistants\/([^/]+)(.*)$/.exec(url.pathname)
    try {
      if (!match) throw new Error("unexpected path " + url.pathname)
      const bot = decodeURIComponent(match[1]), tail = match[2], key = { machine_id: "studio", bot_id: bot }
      if (bot === "status") return json(await fixture.status(signal))
      if (method === "GET" && tail === "/view") {
        const answer = await fixture.view({ key, conversation_id: q.conversation_id, mode: q.mode, source_epoch: q.source_epoch, cursor: q.cursor, after_seq: Number(q.after_seq), page_size: Number(q.page_size) }, signal)
        return json(answer)
      }
      if (method === "GET" && tail === "/export") {
        state.exports++
        const value = await fixture.export(key, signal)
        return json({ ...value, turns: [...value.turns, ...turns.filter(t => t.fence.key.bot_id === bot)] })
      }
      if (method === "POST" && tail === "/messages") {
        const request = JSON.parse(init.body)
        state.sends++; state.transported.push(request.command.idempotency_key)
        const mode = state.mode; state.mode = "normal"
        if (mode === "missing") lose()
        if (mode === "failed") { state.failed = { id: "failed-" + request.command.idempotency_key, command: request.command, event_seq: 0, resource_id: "", state: "failed" }; lose() }
        const answer = await fixture.send(request, signal)
        const receipt = answer.receipt
        if (!turns.some(t => t.id === receipt.id)) turns.push({ id: receipt.id, fence: request.fence, idempotency_key: request.command.idempotency_key, message_id: receipt.id, state: "succeeded", memory_revision: 1, instructions: { text: "", revision: 1, role_snapshot_ref: "" } })
        receipt.resource_id = receipt.id
        if (mode === "lose") lose()
        return json(answer)
      }
      if (method === "POST" && tail === "/read") return json(await fixture.markRead(JSON.parse(init.body), signal))
      if (method === "GET" && tail === "") return json(await fixture.get(key, signal))
      throw new Error("unexpected route " + method + " " + url.pathname)
    } catch (error) {
      if (error && error.contract_version) return refusal(error)
      throw error
    }
  }
  // The failed receipt is recorded only in this source's own export.
  const exportTransport = async (input, init) => {
    const response = await transport(input, init)
    if (!state.failed || !new URL(input, location.origin).pathname.endsWith("/export")) return response
    const value = await response.json()
    return Response.json({ ...value, receipts: [...value.receipts, state.failed] })
  }
  const source = new ProjectAssistantSourceClient(live, exportTransport, grant)
  const sources = [source], current = () => sources
  const view = createViewState(window.sessionStorage)
  const root = createRoot(document.getElementById("fixture"))
  let summary = null, generation = 0, shown = true
  const w = words(language)
  const button = (label, onClick) => React.createElement("button", { type: "button", onClick }, label)
  const mode = next => () => { state.mode = next; paint() }
  const paint = () => root.render(React.createElement("div", { className: "project-assistants", style: { display: "grid", gridTemplateRows: "auto 1fr", height: "100dvh" } },
    React.createElement("nav", { "aria-label": "Synthetic test controls", style: { display: "flex", flexWrap: "wrap", gap: 6, padding: 8, fontSize: 12 } },
      button("Test: lose next response", mode("lose")),
      button("Test: refuse next send before transport", () => { state.revokeSend = true; paint() }),
      button("Test: next send leaves no receipt", mode("missing")),
      button("Test: next send records failure", mode("failed")),
      button("Test: leave chat", () => { shown = false; paint() }),
      button("Test: reopen chat", () => { shown = true; generation++; paint() }),
      React.createElement("output", { "aria-label": "Transport log" }, "mode=" + (state.revokeSend ? "revoke-before" : state.mode) + " sends=" + state.sends + " exports=" + state.exports + " keys=" + state.transported.join(",")),
      // The composer's value is not page text; report it for the reader.
      React.createElement("output", { "aria-label": "Composer state" }, (() => { const box = document.querySelector(".pa-composer textarea"); return box ? "composer=" + (box.disabled ? "locked" : "editable") + " draft=[" + box.value + "]" : "composer=absent" })())),
    React.createElement("main", { className: "pa-main" }, shown && summary ? React.createElement(Chat, { key: generation, summary, source, current, accountKey: "fixture-account", readerID: "fixture-reader", view, w, language, onBack: () => {}, onBotChanged: () => {} }) : React.createElement("p", null, "Chat closed"))))
  window.sendRecovery = { state, view, turns }
  source.status(new AbortController().signal).then(() => fixture.list({ machine_id: "studio", query: "", cursor: "", page_size: 100 }, new AbortController().signal)).then(page => { summary = page.items[0]; paint() })
  setInterval(paint, 500)
}
`

const bundle = await build({ stdin: { contents: entry, resolveDir: src, loader: "tsx" }, outfile: "project-assistants-fixture.js", bundle: true, write: false, format: "esm", platform: "browser", jsx: "automatic", nodePaths: [path.join(here, "..", "node_modules"), path.join(here, "..", "..", "node_modules")], define: { "process.env.NODE_ENV": '"development"' }, loader: { ".svg": "dataurl", ".png": "dataurl" } })
const content = new Map(bundle.outputFiles.map(f => ["/" + path.basename(f.path), f.text]))
const html = await readFile(path.join(here, "fixtures", "project-assistants.html"), "utf8")
const server = createServer((request, response) => {
  const url = new URL(request.url, "http://127.0.0.1")
  const body = url.pathname === "/" ? html : content.get(url.pathname)
  if (body === undefined) { response.writeHead(404).end(); return }
  response.writeHead(200, { "Content-Type": url.pathname.endsWith(".js") ? "text/javascript" : url.pathname.endsWith(".css") ? "text/css" : "text/html; charset=utf-8", "Cache-Control": "no-store" }).end(body)
})
const external = process.env.SEND_RECOVERY_URL
if (!external) await new Promise(resolve => server.listen(0, "127.0.0.1", resolve))
const origin = external ? new URL(external).origin : `http://127.0.0.1:${server.address().port}`
if (serveOnly) { console.log(origin + "/?lang=en"); await new Promise(() => {}) }

// --- Clawdline browser driving --------------------------------------------
const steps = []
const browser = (...args) => {
  const out = execFileSync("clawdline", ["browser", ...args], { encoding: "utf8", timeout: 60000, stdio: ["inherit", "pipe", "pipe"] })
  steps.push({ args, out: out.slice(0, 4000) })
  return out
}
let tab = null
// Labels take the rest of the line, so the tab option goes right after the verb.
const withTab = ([verb, ...rest]) => tab === null ? [verb, ...rest] : [verb, "--tab", String(tab), ...rest]
// The page's text and its control labels, from the CLI's JSON answer.
const read = () => { const value = JSON.parse(browser(...withTab(["read"]))).value; return value.text + "\n" + value.elements.map(e => e.label).join("\n") }
const click = label => browser(...withTab(["click", label]))
const settle = async (ms = 900) => new Promise(resolve => setTimeout(resolve, ms))
const until = async (predicate, what, tries = 12) => { let page = ""; for (let i = 0; i < tries; i++) { page = read(); if (predicate(page)) return page; await settle(500) } throw new Error(`timed out waiting for ${what}:\n${page.slice(0, 2000)}`) }
const shot = name => artifacts && browser(...withTab(["screenshot", "--out", path.join(artifacts, `send-recovery-${name}.jpg`)]))
const log = page => /sends=(\d+) exports=(\d+) keys=(\S*)/.exec(page)
const typeDraft = async text => { click("Message"); await settle(200); browser(...withTab(["type", text])); await settle(300) }
const results = []
const record = (name, detail) => { results.push({ name, ...detail }); console.log("✔", name) }

try {
  // An open that timed out may still have run: find it by URL, never open twice.
  const url = origin + "/?lang=en"
  if (process.env.SEND_RECOVERY_TAB) tab = Number(process.env.SEND_RECOVERY_TAB)
  else try { tab = JSON.parse(browser("open", url)).value.tab }
  catch (error) {
    if (!/browser_timeout/.test(String(error.stderr ?? error.message))) throw error
    await settle(1500)
    tab = JSON.parse(browser("tabs")).value.tabs.find(t => t.url === url)?.tab ?? null
    if (tab === null) throw error
  }
  await until(page => page.includes("I split the work into five tasks"), "the synthetic chat")
  shot("01-opened")

  // 1. A pre-transport refusal leaves an editable draft, not a permanent lock.
  click("Test: refuse next send before transport"); await settle(300)
  await typeDraft("Synthetic refused draft")
  click("Send message")
  let page = await until(p => /pa_revoked/.test(p), "the pre-transport refusal")
  shot("02-refused-before-transport")
  assert.equal(log(page)?.[1], "0", "the refused send never reached the transport")
  record("pre-transport refusal", { locked: /Check with the machine/.test(page), unknownBanner: page.includes("Outcome unknown; no automatic retry"), draftKept: page.includes("draft=[Synthetic refused draft]"), composer: /composer=(\w+)/.exec(page)?.[1] })
  assert.equal(page.includes("Check with the machine"), false, "a provably unsent message is not held")
  assert.equal(page.includes("composer=editable draft=[Synthetic refused draft]"), true, "the draft is returned for editing")

  // 2. The same draft sends normally afterward (one transport request).
  click("Send message")
  page = await until(p => log(p)?.[1] === "1", "the explicit send by the person")
  record("person sends again explicitly", { sends: log(page)?.[1] })

  // 3. Accepted send whose response is lost: held, then one receipt check reattaches.
  click("Test: lose next response"); await settle(300)
  await typeDraft("Synthetic lost-response message")
  click("Send message")
  page = await until(p => p.includes("Check with the machine"), "the held uncertain send")
  shot("03-held-after-lost-response")
  const keyAfterLoss = log(page)?.[3]
  assert.equal(log(page)?.[1], "2")
  assert.match(page, /stays locked in the box/)
  assert.equal(page.includes("composer=locked draft=[Synthetic lost-response message]"), true, "the uncertain message stays visible and locked")
  // Navigation and reopening do not resend.
  click("Test: leave chat"); await settle(400); click("Test: reopen chat")
  page = await until(p => p.includes("Check with the machine"), "the held send after reopening")
  assert.equal(log(page)?.[1], "2", "reopening never resends")
  shot("04-held-after-reopen")
  click("Check with the machine")
  // The transport log repaints every 500 ms; wait for it, not only for the outcome line.
  page = await until(p => p.includes("The machine has this message. It was not sent again.") && log(p)?.[2] !== "0" && p.includes("composer=editable"), "the reattached receipt")
  shot("05-reattached")
  assert.equal(log(page)?.[1], "2", "inspection sent nothing"); assert.equal(log(page)?.[2], "1", "exactly one explicit receipt read")
  assert.equal(log(page)?.[3], keyAfterLoss)
  assert.equal(page.includes("Check with the machine"), false)
  assert.equal(page.includes("composer=editable draft=[]"), true, "the delivered copy leaves the box")
  record("lost response reattaches by its exact receipt", { sends: 2, exports: 1 })

  // 4. No receipt yet: stays held; nothing is resent.
  click("Test: next send leaves no receipt"); await settle(300)
  await typeDraft("Synthetic message with no receipt")
  click("Send message")
  await until(p => p.includes("Check with the machine"), "the held send without receipt")
  click("Check with the machine")
  page = await until(p => p.includes("The machine has no record of this message yet."), "the missing-receipt outcome")
  shot("06-missing-receipt-stays-held")
  assert.equal(log(page)?.[1], "3"); assert.equal(page.includes("Check with the machine"), true, "still held")
  assert.equal(page.includes("composer=locked draft=[Synthetic message with no receipt]"), true)
  record("missing receipt stays unknown", { sends: 3, exports: Number(log(page)?.[2]) })

  // 5. While held, the composer refuses typing and nothing is resent.
  click("Message"); await settle(200)
  try { browser(...withTab(["type", " extra"])) } catch {}
  await settle(400); page = read()
  assert.equal(log(page)?.[1], "3"); assert.equal(page.includes("draft=[Synthetic message with no receipt]"), true)
  record("held draft cannot be edited or resent", { sends: 3 })
} finally {
  if (artifacts) await writeFile(path.join(artifacts, "send-recovery-browser-steps.json"), JSON.stringify({ origin, source: src, results, steps }, null, 2))
  try { if (tab !== null && !process.env.SEND_RECOVERY_TAB) browser("close", "--tab", String(tab)) } catch (error) { console.error("close failed:", error.message) }
  if (server.listening) server.close()
}
