/**
 * A Bot's question for the person, in a real browser with the production
 * components and stylesheet.
 *
 * Two chats side by side on a phone: the asked Bot (Beacon) shows the
 * "Needs your decision" card under the exchange capsule from Atlas, and the
 * asking Bot (Atlas) shows that Beacon is waiting for the person, with a
 * shortcut there. The answer transport is synthetic and slow on purpose, so
 * the waiting state can be seen and photographed; nothing reaches a daemon,
 * Cloud, provider or model.
 *
 *   node tools/project-assistants-decision-card.e2e.mjs --serve-only
 *     prints the URL and keeps serving. Open it in a 390px-wide tab:
 *       ?scene=beacon   the asked Bot's chat, decision pending
 *       ?scene=atlas    the asking Bot's chat, waiting for the person
 *       ?scene=check    presses the real buttons and prints PASS/FAIL lines
 *     add &lang=zh-Hant for Taiwan Traditional Chinese.
 *   DECISION_CARD_SOURCE=<other web/console/src> ... runs against another tree.
 */
import { createServer } from "node:http"
import path from "node:path"
import { fileURLToPath } from "node:url"
import { build } from "esbuild"

const here = path.dirname(fileURLToPath(import.meta.url))
const src = path.resolve(process.env.DECISION_CARD_SOURCE ?? path.join(here, "..", "src"))
if (!process.argv.includes("--serve-only")) throw new Error("Run with --serve-only and drive the printed URL from a browser tab")

const entry = String.raw`
import React, { useState } from "react"
import { createRoot } from "react-dom/client"
import { ChatRows } from "./project-assistants/ChatRows"
import { DecisionCard } from "./project-assistants/DecisionCard"
import { words } from "./project-assistants/words"
import "./project-assistants/styles.css"

const params = new URLSearchParams(location.search)
const language = params.get("lang") === "zh-Hant" ? "zh-Hant" : "en"
const w = words(language)
const project = { machine_id: "studio", project_id: "project", canonical_id: "canonical", kind: "repository" }
const atlas = { machine_id: "studio", bot_id: "atlas" }, beacon = { machine_id: "studio", bot_id: "beacon" }
const at = "2026-11-23T08:00:00Z"
const fence = key => ({ key, project, conversation_id: key.bot_id + "-chat", binding_id: "", generation: 0 })
const bot = (key, name, color) => ({ key, project, conversation_id: key.bot_id + "-chat", name, agent: "claude", appearance: { shape: "blob", color }, permission_policy: "ask", instructions: { revision: 1, text: "", role_snapshot_ref: "" }, created_at: at, updated_at: at, version: 1 })
const roster = [bot(atlas, "Atlas", "#e39567"), bot(beacon, "Beacon", "#7fb5e3")]
const zh = language === "zh-Hant"
const request = zh ? "Release 1.8 的 CI 全綠，可以今天發版嗎？請問使用者。" : "CI for release 1.8 is green. Can we ship today? Please ask the person."
const question = zh ? "Release 1.8 要今天發版，還是等 QA 明天回報？" : "Ship release 1.8 today, or wait for QA's report tomorrow?"
const exchange = outcome => ({ id: "x1", project, source: { key: atlas, conversation_id: "atlas-chat" }, target: { key: beacon, conversation_id: "beacon-chat" }, source_turn_id: "s", request_turn_id: "r", request, outcome, revision: 1, hops: 1 })
let seq = 0
const message = (owner, kind, text, extra = {}) => ({ id: owner.bot_id + "-" + (++seq), seq, at, kind, text, turn_id: "t" + seq, quote_message_id: "", fence: fence(owner), ...extra })
const rows = messages => messages.map(m => ({ id: m.id, kind: "message", message: m, anchor_message_seq: m.seq, observed_event_seq: m.seq }))
const options = [{ id: "ship", kind: "allow_once", label: zh ? "今天發版" : "Ship it today", allow: true, read_only: true }, { id: "wait", kind: "reject_once", label: zh ? "等 QA 回報再說" : "Wait for QA", allow: false, read_only: true }]
const pendingOffer = { id: "d1", native_request_id: "", turn_id: "t", fence: fence(beacon), state: "pending", expires_at: "2099-01-01T00:00:00Z", selected_option_id: "", kind: "decision", options, decision: { question, exchange_id: "x1", source_bot_id: "atlas", request } }
const summaryOf = offer => ({ id: offer.id, turn_id: offer.turn_id, fence: offer.fence, state: offer.state, expires_at: offer.expires_at, detail_cursor: "c1", tool_kind: "decision", title: question })
const source = { machineID: "studio", authorizationRevision: "grant", current: () => true }
const log = []
window.__decision = { log, release: null }

// The card's answer goes through the console's answer path; here it is held
// until released so the waiting state stays on screen.
function Beacon({ hold }) {
  const [offer, setOffer] = useState(pendingOffer)
  const onAnswer = async (o, option, text) => {
    log.push({ answer: option ? option.id : null, text: text ?? null })
    if (hold) await new Promise(resolve => { window.__decision.release = resolve })
    setOffer({ ...o, state: "answered", selected_option_id: option?.id ?? "", decision: { ...o.decision, answer_text: option?.label ?? text } })
  }
  const timeline = rows([message(beacon, "notice", "", { exchange: exchange("needs_person") }), message(beacon, "assistant", zh ? "我需要你決定 Release 1.8 什麼時候發。" : "I need your call on when release 1.8 ships.")])
  return <ChatRows timeline={timeline} w={w} language={language} source={source} botKey={beacon} roster={roster}
    renderExchangeDecision={id => id === "x1" ? <DecisionCard summary={summaryOf(offer)} offer={offer} enabled onAnswer={onAnswer} askerName="Beacon" requesterName="Atlas" w={w}/> : null}/>
}
function Atlas() {
  const timeline = rows([message(atlas, "user", zh ? "請 Beacon 確認能不能今天發版。" : "Ask Beacon whether we can ship today."), message(atlas, "notice", "", { exchange: exchange("needs_person") }), message(atlas, "assistant", zh ? "Beacon 需要你的決定，我會等你回覆。" : "Beacon needs your decision; I'll wait for your reply.")])
  return <ChatRows timeline={timeline} w={w} language={language} source={source} botKey={atlas} roster={roster} onOpenBot={key => log.push({ open: key.bot_id })}/>
}
const Phone = ({ title, children }) => <section className="project-assistants" style={{ width: 390, minHeight: 600, border: "1px solid #303033", borderRadius: 18, overflow: "hidden", background: "var(--pa-bg)" }}>
  <header style={{ padding: "14px 16px", borderBottom: "1px solid var(--pa-line)", fontWeight: 600 }}>{title}</header>
  <div className="pa-chat"><div className="pa-chat-scroll"><div className="pa-chat-content" style={{ padding: 12 }}>{children}</div></div></div>
</section>

const wait = ms => new Promise(resolve => setTimeout(resolve, ms))
const until = async (test, label) => { for (let i = 0; i < 50; i++) { if (test()) return; await wait(20) } throw new Error("timed out: " + label) }
const press = (root, text) => { const b = [...root.querySelectorAll("button")].find(x => x.textContent.includes(text)); if (!b) throw new Error("no button " + text); b.click() }
async function check(out) {
  const results = [], record = (name, ok, detail = "") => results.push((ok ? "PASS " : "FAIL ") + name + (detail ? " — " + detail : ""))
  const mount = node => { const host = document.createElement("div"); document.body.appendChild(host); const root = createRoot(host); root.render(node); return { host, root } }
  const step = async (name, fn) => { try { await fn() ; record(name, true) } catch (e) { record(name, false, String(e.message ?? e)) } }
  await step("pressing a suggested reply answers with that option and shows the waiting state until the answer lands", async () => {
    log.length = 0; const { host, root } = mount(<Beacon hold/>)
    await until(() => host.querySelector(".pa-decision"), "card")
    const capsule = host.querySelector(".pa-exchange-attribution")
    if (!capsule?.contains(host.querySelector(".pa-decision"))) throw new Error("card not under the capsule")
    press(host, options[1].label)
    await until(() => host.querySelector(".pa-decision[aria-busy='true']"), "busy")
    if (![...host.querySelectorAll(".pa-decision button")].every(b => b.disabled)) throw new Error("buttons stay enabled while waiting")
    if (host.textContent.includes(w.decisionAnswered.replace("{text}", options[1].label))) throw new Error("answered before the answer landed")
    if (JSON.stringify(log) !== JSON.stringify([{ answer: "wait", text: null }])) throw new Error(JSON.stringify(log))
    window.__decision.release()
    await until(() => host.textContent.includes(w.decisionAnswered.replace("{text}", options[1].label)), "answered")
    root.unmount(); host.remove()
  })
  await step("writing an own reply sends that text, not an option", async () => {
    log.length = 0; const { host, root } = mount(<Beacon/>)
    await until(() => host.querySelector(".pa-decision"), "card")
    press(host, w.decisionOwn)
    await until(() => host.querySelector(".pa-decision textarea"), "textarea")
    const area = host.querySelector(".pa-decision textarea")
    const setter = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, "value").set
    setter.call(area, "  Ship after the smoke test  "); area.dispatchEvent(new Event("input", { bubbles: true }))
    await until(() => !host.querySelector(".pa-decision-form button[type=submit]").disabled, "send enabled")
    press(host, w.decisionSend)
    await until(() => log.length === 1, "answer")
    if (JSON.stringify(log) !== JSON.stringify([{ answer: null, text: "Ship after the smoke test" }])) throw new Error(JSON.stringify(log))
    await until(() => host.textContent.includes("Ship after the smoke test"), "answered text")
    root.unmount(); host.remove()
  })
  await step("the asking Bot shows it is waiting for the person, and the shortcut opens the asked Bot", async () => {
    log.length = 0; const { host, root } = mount(<Atlas/>)
    await until(() => host.querySelector(".pa-decision-pointer"), "pointer")
    if (!host.textContent.includes(w.decisionWaiting.replace("{name}", "Beacon"))) throw new Error("no waiting text")
    press(host, w.decisionGo.replace("{name}", "Beacon"))
    if (JSON.stringify(log) !== JSON.stringify([{ open: "beacon" }])) throw new Error(JSON.stringify(log))
    root.unmount(); host.remove()
  })
  out.textContent = results.join("\n") + "\n" + (results.every(r => r.startsWith("PASS")) ? "ALL PASS" : "SOME FAILED")
}

export function mountFixture() {
  const scene = params.get("scene") ?? "beacon"
  const root = createRoot(document.getElementById("fixture"))
  if (scene === "check") { root.render(<pre id="results" style={{ color: "#fafafa", whiteSpace: "pre-wrap", padding: 12 }}>running…</pre>); setTimeout(() => void check(document.getElementById("results")), 50); return }
  root.render(scene === "atlas" ? <Phone title="Atlas"><Atlas/></Phone> : <Phone title="Beacon"><Beacon hold/></Phone>)
}
`

const bundle = await build({ stdin: { contents: entry, resolveDir: src, loader: "tsx" }, outfile: "decision-fixture.js", bundle: true, write: false, format: "esm", platform: "browser", jsx: "automatic", nodePaths: [path.join(here, "..", "node_modules"), path.join(here, "..", "..", "node_modules")], define: { "process.env.NODE_ENV": '"development"' }, loader: { ".svg": "dataurl", ".png": "dataurl" } })
const content = new Map(bundle.outputFiles.map(f => ["/" + path.basename(f.path), f.text]))
const html = `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Decision card fixture</title><link rel="stylesheet" href="/decision-fixture.css"><style>html,body{margin:0;background:#080808;color:#fafafa;font-family:-apple-system,BlinkMacSystemFont,sans-serif}#fixture{display:flex;justify-content:center}</style></head><body><div id="fixture"></div><script type="module">import {mountFixture} from '/decision-fixture.js';mountFixture();</script></body></html>`
const server = createServer((request, response) => {
  const url = new URL(request.url, "http://127.0.0.1")
  const body = url.pathname === "/" ? html : content.get(url.pathname)
  if (body === undefined) { response.writeHead(404).end(); return }
  response.writeHead(200, { "Content-Type": url.pathname.endsWith(".js") ? "text/javascript" : url.pathname.endsWith(".css") ? "text/css" : "text/html; charset=utf-8", "Cache-Control": "no-store" }).end(body)
})
await new Promise(resolve => server.listen(0, "127.0.0.1", resolve))
console.log(`http://127.0.0.1:${server.address().port}/`)
await new Promise(() => {})
