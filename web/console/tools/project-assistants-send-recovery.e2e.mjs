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
 * The synthetic source keeps its own records (counters, admitted sends, a
 * recorded failure) in sessionStorage and replays them into a fresh fixture
 * on load, so a full page reload keeps the source's facts while the console
 * starts from nothing but what it journaled.
 *
 * Exit 0 only when every step passed; the tab and server it opened are closed.
 */
import assert from "node:assert/strict"
import { execFileSync } from "node:child_process"
import { createServer } from "node:http"
import { mkdir, readFile, writeFile } from "node:fs/promises"
import path from "node:path"
import { fileURLToPath, pathToFileURL } from "node:url"
import { build } from "esbuild"

const here = path.dirname(fileURLToPath(import.meta.url))
const src = path.resolve(process.env.SEND_RECOVERY_SOURCE ?? path.join(here, "..", "src"))
const artifacts = process.env.SEND_RECOVERY_ARTIFACTS
const legacy = process.argv.includes("--legacy")
const readRecovery = process.argv.includes("--read-recovery")
const refuseLegacyStorage = legacy && process.argv.includes("--refuse-storage")
const query = "?lang=en" + (legacy ? "&legacy=1" : "") + (refuseLegacyStorage ? "&refuse-storage=1" : "")
const serveOnly = process.argv.includes("--serve-only")
if (!artifacts && !serveOnly) throw new Error("Set SEND_RECOVERY_ARTIFACTS to a task-owned evidence directory")
if (artifacts) await mkdir(artifacts, { recursive: true })

const entry = String.raw`
import React from "react"
import { createRoot } from "react-dom/client"
import { Chat } from "./project-assistants/Chat"
import { fixtureSource } from "./project-assistants/fixtures"
import { words } from "./project-assistants/words"
import { createViewState, draftIdentity } from "./project-assistants/view-state"
import { ProjectAssistantSourceClient } from "./cloud/project-assistants"
import "./project-assistants/styles.css"

export function mountFixture() {
  const language = new URLSearchParams(location.search).get("lang") ?? "en"
  const fixture = fixtureSource("studio")
  const legacy = new URLSearchParams(location.search).has("legacy")
  if (legacy) fixture.seedPending(0, 1)
  const turns = []
  const readFaults = []
  const persisted = (() => { try { return JSON.parse(sessionStorage.getItem("send-recovery.synthetic-source") ?? "null") } catch { return null } })()
  const state = { mode: "normal", sends: 0, transported: [], exports: 0, revokeSend: false, admitted: [], failed: null, ...(persisted ?? {}) }
  const persist = () => sessionStorage.setItem("send-recovery.synthetic-source", JSON.stringify({ mode: state.mode, sends: state.sends, transported: state.transported, exports: state.exports, admitted: state.admitted, failed: state.failed }))
  const admit = async (request, signal) => {
    const answer = await fixture.send(request, signal)
    const receipt = answer.receipt
    if (!turns.some(t => t.id === receipt.id)) turns.push({ id: receipt.id, fence: request.fence, idempotency_key: request.command.idempotency_key, message_id: receipt.id, state: "succeeded", memory_revision: 1, instructions: { text: "", revision: 1, role_snapshot_ref: "" } })
    receipt.resource_id = receipt.id
    return answer
  }
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
        if (q.mode === "snapshot" && readFaults.length) {
          const code = readFaults.shift()
          return json({contract_version:"project-assistants.v1",error:{code,field:"synthetic_read",retry_after_seconds:0}},code==="pa_cursor_expired"?410:503)
        }
        const answer = await fixture.view({ key, conversation_id: q.conversation_id, mode: q.mode, source_epoch: q.source_epoch, cursor: q.cursor, after_seq: Number(q.after_seq), page_size: Number(q.page_size) }, signal)
        return json(answer)
      }
      if (method === "GET" && tail === "/export") {
        state.exports++; persist()
        const value = await fixture.export(key, signal)
        return json({ ...value, turns: [...value.turns, ...turns.filter(t => t.fence.key.bot_id === bot)] })
      }
      if (method === "POST" && tail === "/messages") {
        const request = JSON.parse(init.body)
        state.sends++; state.transported.push(request.command.idempotency_key)
        const mode = state.mode; state.mode = "normal"; persist()
        if (mode === "missing") lose()
        if (mode === "failed") { state.failed = { id: "failed-" + request.command.idempotency_key, command: request.command, event_seq: 0, resource_id: "", state: "failed" }; persist(); lose() }
        const answer = await admit(request, signal)
        state.admitted.push(request); persist()
        if (mode === "lose") lose()
        if (mode === "recover-read") readFaults.push("pa_cursor_expired","pa_offline","pa_offline")
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
  let sources = [source]
  const current = () => sources
  const legacyKey = draftIdentity("fixture-account", { machine_id: "studio", bot_id: "bot-0" })
  const seedLegacy = legacy && !sessionStorage.getItem("send-recovery.legacy-seeded")
  if (seedLegacy) {
    sessionStorage.setItem(legacyKey, "Legacy uncertain draft")
    sessionStorage.setItem("send-recovery.legacy-seeded", "yes")
  }
  const injectStorageRefusal = new URLSearchParams(location.search).has("refuse-storage")
  let refuseStorage = injectStorageRefusal && !sessionStorage.getItem("send-recovery.storage-allowed")
  const view = createViewState({ getItem: key => sessionStorage.getItem(key), setItem: (key, value) => { if (refuseStorage) throw new Error("synthetic storage refusal"); sessionStorage.setItem(key, value) }, removeItem: key => { if (refuseStorage) throw new Error("synthetic storage refusal"); sessionStorage.removeItem(key) } })
  if (seedLegacy) view.quotes.set(legacyKey, "bot-0-reply")
  const root = createRoot(document.getElementById("fixture"))
  let summary = null, generation = 0, shown = true, account = "fixture-account"
  const w = words(language)
  const button = (label, onClick) => React.createElement("button", { type: "button", onClick }, label)
  const mode = next => () => { state.mode = next; paint() }
  const paint = () => root.render(React.createElement("div", { className: "project-assistants pa-selected", style: { display: "grid", gridTemplateRows: "auto minmax(0,1fr)", height: "100dvh" } },
    React.createElement("nav", { "aria-label": "Synthetic test controls", style: { display: "flex", flexWrap: "wrap", gap: 6, padding: 8, fontSize: 12 } },
      injectStorageRefusal && button("Test: allow storage", () => {
        sessionStorage.setItem("send-recovery.storage-allowed", "yes"); refuseStorage = false; paint()
      }),
      !legacy && button("Test: lose next response", mode("lose")),
      !legacy && button("Test: interrupt reads after an accepted send", mode("recover-read")),
      !legacy && button("Test: refuse next send before transport", () => { state.revokeSend = true; paint() }),
      !legacy && button("Test: next send leaves no receipt", mode("missing")),
      !legacy && button("Test: next send records failure", mode("failed")),
      !legacy && button("Test: leave chat", () => { shown = false; paint() }),
      !legacy && button("Test: reopen chat", () => { shown = true; generation++; paint() }),
      button("Test: reload page", () => location.reload()),
      button("Test: switch account", () => { account = account === "fixture-account" ? "other-account" : "fixture-account"; generation++; paint() }),
      !legacy && button("Test: revoke grant", () => { sources = []; paint() }),
      React.createElement("output", { "aria-label": "Transport log" }, "account=" + account + " grants=" + sources.length + " mode=" + (state.revokeSend ? "revoke-before" : state.mode) + " sends=" + state.sends + " exports=" + state.exports + " keys=" + state.transported.join(",")),
      // The composer's value is not page text; report it for the reader.
      React.createElement("output", { "aria-label": "Composer state" }, (() => { const box = document.querySelector(".pa-composer textarea"); return box ? "composer=" + (box.disabled ? "locked" : "editable") + " draft=[" + box.value + "]" : "composer=absent" })())),
    React.createElement("main", { className: "pa-main" }, shown && summary ? React.createElement(Chat, { key: account + generation, summary, source, current, accountKey: account, readerID: "fixture-reader", view, w, language, onBack: () => {}, onBotChanged: () => {} }) : React.createElement("p", null, "Chat closed"))))
  // Observe every DOM publication, including the first validated snapshot.
  const observation = document.createElement("output"); observation.id = "legacy-observation"; document.body.append(observation)
  let unsafeLegacyFrames = 0
  new MutationObserver(() => {
    const box = document.querySelector(".pa-composer textarea")
    if (legacy && box && !box.disabled && box.value === "Legacy uncertain draft") unsafeLegacyFrames++
    const legacyText = "Legacy uncertain draft"
    const transcript = document.querySelector(".pa-chat-scroll")?.textContent ?? ""
    const footer = document.querySelector(".pa-chat-footer")?.textContent ?? ""
    const copies = text => text.split(legacyText).length - 1
    document.querySelector("#legacy-observation")?.replaceChildren(document.createTextNode(
      "unsafe-legacy-frames=" + unsafeLegacyFrames + " legacy-transcript=" + copies(transcript) + " legacy-footer=" + copies(footer)))
  }).observe(document.getElementById("fixture"), { childList: true, subtree: true, attributes: true })
  window.sendRecovery = { state, view, turns }
  // The source's own records outlive the page; the console's do not, except what it journaled.
  const replay = async () => { for (const request of state.admitted) await admit(request, new AbortController().signal) }
  replay().then(() => source.status(new AbortController().signal)).then(() => fixture.list({ machine_id: "studio", query: "", cursor: "", page_size: 100 }, new AbortController().signal)).then(page => { summary = page.items[0]; paint() })
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
if (serveOnly) { console.log(origin + "/" + query); await new Promise(() => {}) }

// --- Clawdline browser driving --------------------------------------------
const steps = []
const browser = (...args) => {
  const out = execFileSync("clawdline", ["browser", ...args], { encoding: "utf8", timeout: 60000, stdio: ["inherit", "pipe", "pipe"] })
  steps.push({ args, out })
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

if (readRecovery) {
  try {
    const module=process.env.PLAYWRIGHT_MODULE
    if (!module) throw new Error("Set PLAYWRIGHT_MODULE for the isolated mobile read recovery case")
    const {chromium}=await import(pathToFileURL(module).href)
    const engine=await chromium.launch({headless:true,args:["--disable-features=OmniboxEverywhere"]})
    try {
      const context=await engine.newContext({viewport:{width:390,height:740},isMobile:true,hasTouch:true})
      await context.route("**/*",route=>new URL(route.request().url()).origin===origin?route.continue():route.abort())
      const page=await context.newPage()
      await page.goto(origin+"/"+query)
      await page.getByText("I split the work into five tasks",{exact:false}).first().waitFor()
      const draft=page.locator(".pa-composer textarea"),send=page.getByRole("button",{name:"Send message",exact:true})
      await page.getByRole("button",{name:"Test: interrupt reads after an accepted send"}).click()
      const text="Synthetic send during a read interruption"
      await draft.fill(text);await send.click()
      const reconnect=page.getByText("Reconnecting to the conversation…",{exact:true})
      try{await reconnect.waitFor({timeout:5000})}
      catch(error){throw new Error("expected automatic recovery after one admitted send: "+await page.locator(".pa-chat-error").innerText(),{cause:error})}
      const followUp="A fresh draft during recovery"
      await draft.fill(followUp)
      assert.equal(await send.isEnabled(),true,"a new message can reach the source while reads recover")
      await send.click()
      await page.waitForFunction(()=>window.sendRecovery.state.sends===2)
      await reconnect.waitFor({state:"hidden",timeout:12000})
      await page.locator(".pa-chat-content").getByText(text,{exact:true}).waitFor()
      assert.equal(await draft.inputValue(),"")
      await page.locator(".pa-chat-content").getByText(followUp,{exact:true}).waitFor()
      const state=await page.evaluate(()=>({sends:window.sendRecovery.state.sends,exports:window.sendRecovery.state.exports}))
      assert.deepEqual(state,{sends:2,exports:0},"both explicit sends are admitted once while automatic recovery only reads")
      await page.screenshot({path:path.join(artifacts,"read-recovery.png")})
      record("accepted send recovers from cursor expiry and offline without resending",state)
    } finally { await engine.close() }
  } finally { if (server.listening) await new Promise(resolve=>server.close(resolve)) }
} else try {
  // An open that timed out may still have run: find it by URL, never open twice.
  const url = origin + "/" + query
  if (process.env.SEND_RECOVERY_TAB) tab = Number(process.env.SEND_RECOVERY_TAB)
  else try { tab = JSON.parse(browser("open", url)).value.tab }
  catch (error) {
    if (!/browser_timeout/.test(String(error.stderr ?? error.message))) throw error
    await settle(1500)
    tab = JSON.parse(browser("tabs")).value.tabs.find(t => t.url === url)?.tab ?? null
    if (tab === null) throw error
  }
  await until(page => legacy ? /legacy-transcript=1|legacy-footer=[1-9]/.test(page) : page.includes("I split the work into five tasks"), "the synthetic chat")
  shot("01-opened")

  if (legacy) {
    browser(...withTab(["viewport", "mobile"]))
    let page = await until(p => /composer=(editable|locked) draft=/.test(p) && /legacy-transcript=1|legacy-footer=[1-9]/.test(p), "the initial narrow composer")
    shot("legacy-initial")
    if (refuseLegacyStorage) {
      assert.match(page, /composer=locked draft=\[\]/, "storage refusal must keep the composer empty and locked")
      assert.match(page, /legacy-transcript=1 legacy-footer=0/)
      assert.match(page, /Could not save the old draft/)
      assert.equal(log(page)?.[1], "0"); assert.equal(log(page)?.[2], "0")
      click("Test: allow storage"); click("Save old draft")
    } else {
      assert.match(page, /composer=editable draft=\[\]/, "the composer must open empty without an extra action")
    }
    page = await until(p => p.includes("composer=editable draft=[]"), "the empty legacy composer")
    shot("legacy-new-empty")
    assert.match(page, /unsafe-legacy-frames=0/)
    assert.match(page, /legacy-transcript=1 legacy-footer=0/, "the original belongs to one row in the scrollable transcript")
    assert.match(page, /Old draft · Delivery unconfirmed/)
    assert.equal(page.includes("Check with the machine"), false, "no invented command identity")
    assert.equal(page.includes("Write a new message"), false, "no manual continuation on successful preservation")
    assert.equal(log(page)?.[1], "0"); assert.equal(log(page)?.[2], "0")
    await typeDraft("A new legacy follow-up")
    click("Test: reload page")
    page = await until(p => p.includes("composer=editable draft=[A new legacy follow-up]"), "distinct old and new drafts after reload", 20)
    assert.match(page, /legacy-transcript=1 legacy-footer=0/)
    shot("legacy-distinct-reloaded")
    assert.equal(log(page)?.[1], "0"); assert.equal(log(page)?.[2], "0")
    click("Send message")
    page = await until(p => log(p)?.[1] === "1" && p.includes("composer=editable draft=[]"), "one explicit new legacy send")
    const keys = log(page)?.[3].split(",") ?? []
    assert.equal(keys.length, 1); assert.match(keys[0], /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/)
    assert.match(page, /legacy-transcript=1 legacy-footer=0/)
    assert.match(page, /unsafe-legacy-frames=0/)
    assert.match(page, /Outcome unknown; no automatic retry/)
    assert.equal(log(page)?.[2], "0")
    shot("legacy-new-sent")
    click("Test: switch account")
    page = await until(p => p.includes("account=other-account") && p.includes("composer=editable"), "legacy other account")
    assert.match(page, /legacy-transcript=0 legacy-footer=0/)
    click("Test: switch account")
    page = await until(p => p.includes("account=fixture-account") && p.includes("legacy-transcript=1"), "legacy original account")
    click("Edit as new message")
    page = await until(p => p.includes("composer=editable draft=[Legacy uncertain draft]"), "legacy draft restored for editing")
    assert.equal(log(page)?.[1], "1"); assert.equal(log(page)?.[2], "0")
    click("Remove from this device")
    page = await until(p => p.includes("legacy-transcript=0") && p.includes("composer=editable draft=[Legacy uncertain draft]"), "local row removed without clearing the composer")
    click("Test: reload page")
    page = await until(p => p.includes("legacy-transcript=0") && p.includes("composer=editable draft=[Legacy uncertain draft]"), "removal and editable draft survive reload")
    assert.equal(log(page)?.[1], "1"); assert.equal(log(page)?.[2], "0")
    record("one archived draft, explicit new UUID, account scope, edit and local removal", { refusedInitialStorage: refuseLegacyStorage, sends: Number(log(page)?.[1]), exports: Number(log(page)?.[2]), keys })
  } else {
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

  // 6. A full page reload keeps the held send locked with its check; nothing is resent.
  const exportsBeforeReload = Number(log(page)?.[2])
  click("Test: reload page")
  page = await until(p => p.includes("I split the work into five tasks") && p.includes("Check with the machine") && p.includes("composer=locked"), "the held send after a full reload", 20)
  shot("07-held-after-reload")
  assert.equal(log(page)?.[1], "3", "a reload never resends")
  assert.equal(Number(log(page)?.[2]), exportsBeforeReload, "a reload reads no receipts by itself")
  assert.equal(page.includes("composer=locked draft=[Synthetic message with no receipt]"), true, "the uncertain intent stays noneditable after reload")
  click("Check with the machine")
  page = await until(p => p.includes("The machine has no record of this message yet.") && Number(log(p)?.[2]) === exportsBeforeReload + 1, "the missing receipt after reload")
  assert.equal(page.includes("composer=locked"), true)
  record("full reload keeps the uncertain send held", { sends: 3, exports: exportsBeforeReload + 1 })

  // 7. The person explicitly writes a new message: an empty box; the old one stays visible with its own check.
  click("Write a new message")
  page = await until(p => p.includes("composer=editable draft=[]") && p.includes("Check this earlier message"), "an empty new draft beside the earlier held send")
  shot("08-new-draft-beside-held")
  assert.equal(log(page)?.[1], "3", "setting a send aside sends nothing")
  assert.match(page, /Earlier message, outcome unknown: .?Synthetic message with no receipt/)
  assert.equal(page.includes("Check with the machine"), false, "the box itself is no longer held")
  await typeDraft("A different new message")
  click("Send message")
  page = await until(p => log(p)?.[1] === "4" && p.includes("composer=editable draft=[]"), "exactly one new send")
  shot("09-new-message-sent")
  const keys = log(page)?.[3].split(",") ?? []
  assert.equal(keys.length, 4); assert.equal(new Set(keys).size, 4, "the new message is a new command")
  assert.equal(page.includes("Check this earlier message"), true, "the earlier uncertain message stays visible")
  assert.equal(page.includes("A different new message"), true)
  record("explicit new message sends once beside the held one", { sends: 4, keys })

  // 8. A lost response, then a full reload: still held; one exact check reattaches; the earlier one is untouched.
  click("Test: lose next response"); await settle(300)
  await typeDraft("Synthetic lost before reload")
  click("Send message")
  page = await until(p => p.includes("Check with the machine") && log(p)?.[1] === "5" && p.includes("composer=locked"), "the second held send")
  const lostKey = log(page)?.[3].split(",").at(-1)
  click("Test: reload page")
  page = await until(p => p.includes("I split the work into five tasks") && p.includes("Check with the machine") && p.includes("Check this earlier message") && p.includes("composer=locked"), "both held sends after reload", 20)
  shot("10-two-held-after-reload")
  assert.equal(log(page)?.[1], "5"); assert.equal(page.includes("composer=locked draft=[Synthetic lost before reload]"), true)
  const exportsBefore = Number(log(page)?.[2])
  click("Check with the machine")
  page = await until(p => p.includes("The machine has this message. It was not sent again.") && Number(log(p)?.[2]) === exportsBefore + 1 && p.includes("composer=editable"), "reattachment after reload")
  shot("11-reattached-after-reload")
  assert.equal(log(page)?.[1], "5", "inspection after reload sent nothing")
  assert.equal(log(page)?.[3].split(",").at(-1), lostKey)
  assert.equal(page.includes("composer=editable draft=[]"), true)
  assert.equal(page.includes("Check this earlier message"), true, "the earlier unknown message is still held")
  click("Check this earlier message")
  page = await until(p => p.includes("The machine has no record of the earlier message yet.") && Number(log(p)?.[2]) === exportsBefore + 2, "the earlier message's own check")
  assert.equal(log(page)?.[1], "5")
  record("lost response survives reload and reattaches by its exact receipt", { sends: 5, exports: exportsBefore + 2 })

  // 9. Another account never sees this account's held send; switching back restores it.
  click("Test: switch account")
  page = await until(p => p.includes("account=other-account") && p.includes("I split the work into five tasks"), "the other account's chat")
  shot("12-other-account")
  assert.equal(page.includes("Check this earlier message"), false, "held sends never cross accounts")
  click("Test: switch account")
  page = await until(p => p.includes("account=fixture-account") && p.includes("Check this earlier message"), "the original account's held send")
  record("account fencing", { crossed: false })

  // 10. A revoked grant reads nothing and sends nothing, even on an explicit check.
  const beforeRevoke = log(page)
  click("Test: revoke grant"); await settle(400)
  try { click("Check this earlier message") } catch {}
  await settle(1200); page = read()
  shot("13-revoked")
  assert.equal(log(page)?.[1], beforeRevoke[1]); assert.equal(log(page)?.[2], beforeRevoke[2], "a revoked grant reads no receipts")
  record("revoked grant never reads or sends", { sends: Number(log(page)?.[1]), exports: Number(log(page)?.[2]) })
  }
} finally {
  if (artifacts) await writeFile(path.join(artifacts, "send-recovery-browser-steps.json"), JSON.stringify({ origin, source: src, results, steps }, null, 2))
  try { if (tab !== null && !process.env.SEND_RECOVERY_TAB) browser("close", "--tab", String(tab)) } catch (error) { console.error("close failed:", error.message) }
  if (server.listening) server.close()
}
