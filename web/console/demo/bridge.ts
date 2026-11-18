import designURL from "./session-design.png"
import story from "./story.json" with { type: "json" }
import type { TranscriptEntry } from "@clawdline/contract"
import { carryPictures } from "../src/legacy/images-bridge.js"
import english from "../public/catalogs/en.json" with { type: "json" }
import chinese from "../public/catalogs/zh-Hant.json" with { type: "json" }

// All state belongs to this document. Never read the visitor's saved console,
// drafts, account, pairing, or language preference on the marketing origin.
function memoryStorage(): Storage {
  const values = new Map<string, string>()
  return {
    get length() { return values.size },
    clear: () => values.clear(),
    getItem: key => values.get(key) ?? null,
    key: index => [...values.keys()][index] ?? null,
    removeItem: key => { values.delete(key) },
    setItem: (key, value) => { values.set(key, String(value)) },
  }
}
// Registering the installed console's worker on the marketing origin is unavailable.
Object.defineProperty(navigator, "serviceWorker", { value: Object.assign(new EventTarget(), {
  register: async () => { throw new Error("demo_unavailable") },
  getRegistration: async () => undefined,
  getRegistrations: async () => [],
}) })
const storage = memoryStorage()
Object.defineProperty(window, "localStorage", { value: storage })
Object.defineProperty(window, "sessionStorage", { value: memoryStorage() })
const zh = new URL(location.href).searchParams.get("lang") !== "en"
storage.setItem("ui_language", zh ? "zh-Hant" : "en")
const words = zh ? chinese : english
const pick = (en: string, tw: string) => zh ? tw : en
const now = Math.floor(Date.now() / 1000)
const cases = zh ? story.rows["zh-Hant"] : story.rows.en
const conversation = (id: string) => `demo-conversation-${id}`
let answered = false
let revision = 1
const icon = (project: string) => {
  const mark = story.marks[project as keyof typeof story.marks]
  const palette = mark.palette as Record<string, string>
  return { accent: mark.accent, cells: mark.grid.map(line => [...line].map(cell => palette[cell] ?? null)) }
}
const artifact = { id: "10000000-0000-4000-8000-000000000012", media_type: "image/png", byte_count: story.attachmentBytes,
  width: 780, height: 1320, expires_at: now + 86400 }
carryPictures(async image => {
  if (image.id !== artifact.id) throw new Error("demo_unavailable")
  return { url: designURL, release() {} }
})
const entries: Record<string, TranscriptEntry[]> = Object.fromEntries(cases.map(item => [item.id, [
  { role: "user", text: item.terminalRequest, at: now - 1800 },
  { role: "assistant", text: item.terminalAction, at: now - 180 },
  { role: "tool", tool: item.terminalTool, text: item.terminalFile, at: now - 150 },
  { role: "assistant", text: item.terminalResult, at: now - 90,
    ...(item.id === "b" ? { artifacts: [artifact] } : {}) },
]]))
const row = (id: string) => {
  const item = cases.find(item => item.id === id)!
  const state = item.id === "e" && answered ? "ready" : item.state
  return {
    id, label: item.title, backend: "tmux", cwd: item.cwd, tty: item.tty, sessionId: conversation(id), icon: icon(item.project),
    assistant: item.who, isClaude: item.who === "claude",
    state: state === "waiting_you" ? "waiting" : state === "working" || state === "unknown" ? state : "idle",
    work_state: state, work_provenance: "self", work_note: item.status, work_moved_by: item.title, work_person_needed: state === "waiting_you",
    attention_count: 0, ...(item.coordinator ? { coordinator: { label: "Clawdfather", status: "online", commands: [] } } : {}),
    ...(item.depth ? { parent_session_id: "a", taskId: `homepage-task-${id}` } : {}),
    ...(state === "milestone_complete" ? { disposition: { scope: "task", taskId: `homepage-task-${id}`, evidence: "authenticated_task_delivery", title: item.terminalResult } } : {}),
    ...(state === "waiting_session" ? { coordination: { waitingOn: [{ id: "homepage-review-wait", ownerSessionId: "b", ownerLabel: cases[1].title, releaseCondition: item.status, paths: ["website/session-list"] }], waitedOnBy: [] } } : {}),
    menu: state === "waiting_you" ? { type: "request", question: pick("Deploy the approved build now?", "現在要部署已核准的版本嗎？"),
      options: [{ n: 1, label: pick("Approve deployment", "核准部署"), can: true, selected: false }, { n: 2, label: pick("Choose another window", "改個發佈時段"), can: true, selected: false }] } : null,
    evidence: "process", source: { freshness: "current", observed_at: now, provenance: "demo" }, activity: { known: true, at: now - 60 },
  }
}
const tasks = () => cases.filter(item => item.depth).map(item => ({
  id: `homepage-task-${item.id}`, kind: "custom", title: item.title,
  state: item.state === "milestone_complete" ? "success" : "briefed", created: now - 900,
  ...(item.state === "milestone_complete" ? { finishedAt: now - 60, landing: { state: "pending" } } : {}),
  root: { terminalId: "a", sessionId: conversation("a") }, child: { terminalId: item.id }, claims: item.id === "b" ? ["website/session-list"] : [],
}))
const snapshot = () => ({
  at: Date.now(), sessions: cases.map(item => row(item.id)),
  scan: { complete: true, completed: { complete: true, sequence: revision }, emptyAuthoritative: true, epoch: 1, generation: revision, provenance: "demo" },
})

// Both website views address these stable IDs. The normal App still owns navigation.
if (typeof history !== "undefined" && !location.hash) history.replaceState(null, "", location.pathname + location.search + "#session=a")
if (typeof window.addEventListener === "function") {
  const announce = () => {
    if (window.parent !== window) window.parent.postMessage({ type: "clawdline-demo-selection", id: new URLSearchParams(location.hash.slice(1)).get("session") }, location.origin)
  }
  window.addEventListener("hashchange", announce)
  window.addEventListener("clawdline:session-address", announce)
  window.addEventListener("message", event => {
    if (event.source !== window.parent || event.origin !== location.origin || event.data?.type !== "clawdline-demo-select") return
    const id = event.data.id
    if (!cases.some(item => item.id === id)) return
    const target = document.querySelector<HTMLElement>(`.row[data-id="${id}"]`)
    if (target) target.click()
    else if (location.hash !== `#session=${id}`) location.hash = `session=${id}`
  })
}
const listeners = new Set<DemoEventSource>()
function publish() {
  revision++
  for (const listener of listeners) listener.emit("sessions", snapshot())
}
function json(value: unknown, status = 200) {
  return new Response(JSON.stringify(value), { status, headers: { "content-type": "application/json" } })
}
const unavailable = () => json({ error: "demo_unavailable", detail: pick("This action is unavailable in the demo. All conversations here are examples.", "示範版未提供這項操作，這裡的對話皆為範例。") }, 503)

// Fail closed: no request falls through to a real daemon, relay or model provider.
// The document's CSP independently blocks network connections.
globalThis.fetch = async (input, init) => {
  const url = new URL(typeof input === "string" ? input : input instanceof URL ? input.href : input.url, location.href)
  const method = init?.method ?? (input instanceof Request ? input.method : "GET")
  const path = url.pathname
  if (url.origin !== location.origin) return unavailable()
  if (path === "/v1/strings") return json(words)
  if (path === "/v1/health") return json({ ok: true, authed: true, write: true })
  if (path === "/v1/sessions") return json(snapshot())
  if (path === "/v1/transcript") {
    const id = url.searchParams.get("session") ?? ""
    return json({ entries: entries[id] ?? [], evidence: "process", id, signature: `demo-${revision}` })
  }
  if (path === "/v1/places") return json({ places: [...new Map(cases.map(item => [item.project, { id: item.project, label: item.project, path: item.cwd, available: true, icon: icon(item.project) }])).values()], assistants: [{ id: "claude", label: "Claude Code" }, { id: "codex", label: "Codex" }] })
  if (path === "/v1/personas") return json({ license: "MIT", personas: [] })
  if (path === "/v1/orchestrator/tasks") return json({ at: Date.now(), tasks: tasks() })
  if (path === "/v1/orchestrator/schedules") return json({ at: Date.now(), schedules: [] })
  if (path === "/v1/provider-quotas") return json({ providers: [], at: Date.now() })
  if (path.startsWith("/v1/work/v2/human-interventions/")) return json({ ok: true, rows: [], pruned_resolved: 0 })
  if (path.startsWith("/v1/work/v2/session-todos/")) return json({ ok: true, assigned_items: [], recent_items: [], direct_todos: [], open_decisions: [], decisions_error: null, truncated: false })
  if (path === "/v1/work/v2/items") return json({ ok: true, rows: [], counts: {}, truncated: false })
  if (path === "/v1/work/v2/proposals") return json({ rows: [], truncated: false })
  if (path === "/v1/work/decisions") return json({ counts: {}, rows: [], next_cursor: null })
  const key = /^\/v1\/sessions\/([^/]+)\/key$/.exec(path)
  if (key && method === "POST") {
    const body = JSON.parse(String(init?.body ?? "{}"))
    if (decodeURIComponent(key[1]) !== "e" || answered || !["1", "2"].includes(String(body.key))) return unavailable()
    const selected = String(body.key) === "2" ? pick("Choose another release window.", "改個發佈時段。") : pick("Approve deployment.", "核准部署。")
    answered = true
    entries.e.push({ role: "user", text: selected, at: Math.floor(Date.now() / 1000) })
    entries.e.push({ role: "assistant", text: pick("Got it. This demonstration keeps the result here; no real deployment was started.", "收到。示範結果會留在這裡，沒有執行真正的部署。"), at: Math.floor(Date.now() / 1000) + 1 })
    publish()
    return json({ ok: true })
  }
  const send = /^\/v1\/sessions\/([^/]+)\/send$/.exec(path)
  if (send && method === "POST") {
    const id = decodeURIComponent(send[1])
    if (!entries[id]) return unavailable()
    const body = JSON.parse(String(init?.body ?? "{}"))
    entries[id].push({ role: "user", text: String(body.text ?? ""), at: Math.floor(Date.now() / 1000) })
    entries[id].push({ role: "assistant", text: pick("Got it. This is a demonstration reply; no deployment or real work was started.", "收到。這是示範回覆，沒有執行真正的部署或工作。"), at: Math.floor(Date.now() / 1000) + 1 })
    publish()
    return json({ ok: true })
  }
  if (/^\/v1\/sessions\/[^/]+\/info$/.test(path)) {
    const id = decodeURIComponent(path.split("/")[3])
    return json({ info: { session: { ...row(id), title: row(id).label, model: "demo" }, models: [], context: { usedPercent: 18 }, deploy: [], links: [] } })
  }
  if (/^\/v1\/sessions\/[^/]+\/git$/.test(path)) return json({ git: { ahead: 0, behind: 0, branch: "homepage-sessions", clean: true, head: "0123456789abcdef", files: [] } })
  return unavailable()
}

class DemoEventSource extends EventTarget {
  onopen: ((event: Event) => void) | null = null
  onerror: ((event: Event) => void) | null = null
  constructor(_url: string | URL) {
    super()
    listeners.add(this)
    queueMicrotask(() => { if (listeners.has(this)) { this.onopen?.(new Event("open")); this.emit("sessions", snapshot()); this.emit("orchestrator", { at: Date.now(), tasks: tasks() }) } })
  }
  emit(type: string, value: unknown) { this.dispatchEvent(new MessageEvent(type, { data: JSON.stringify(value) })) }
  close() { listeners.delete(this) }
}
Object.defineProperty(globalThis, "EventSource", { value: DemoEventSource })
