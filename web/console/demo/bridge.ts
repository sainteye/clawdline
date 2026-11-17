import { BRAND_MARK } from "../src/brand-mark.js"
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
const cwd = "~/code/shop"
const conversation = (id: string) => `demo-conversation-${id}`
let answered = false
let revision = 1
const labels = {
  checkout: pick("One-page checkout", "單頁式結帳"),
  signin: pick("Google sign-in", "Google 登入"),
  search: pick("Product search", "商品搜尋"),
}
const entries: Record<string, { role: string; text: string; at: number; kind: string }[]> = {
  checkout: [
    { role: "user", text: pick("Make checkout fit on one page, including shipping, payment and review.", "把結帳改成單頁，包含寄送、付款與訂單確認。"), at: now - 1800, kind: "message" },
    { role: "assistant", text: pick("The one-page checkout is ready. Shipping, payment and review stay together, and all 42 tests pass.\n\nShould I put it on staging first, or go live?", "單頁式結帳做好了。寄送、付款與訂單確認都在同一頁，42 個測試全部通過。\n\n要先放測試站給你看，還是直接上線？"), at: now - 90, kind: "message" },
  ],
  signin: [
    { role: "user", text: pick("Let returning customers sign in with Google.", "讓老顧客可以使用 Google 登入。"), at: now - 2400, kind: "message" },
    { role: "assistant", text: pick("The sign-in flow is connected. I am checking cancellation, expired sessions and the return to checkout.", "登入流程已接好，正在檢查取消登入、登入過期，以及返回結帳頁的情況。"), at: now - 180, kind: "message" },
  ],
  search: [
    { role: "user", text: pick("Make product search forgive common typos.", "讓商品搜尋容許常見的錯字。"), at: now - 3600, kind: "message" },
    { role: "assistant", text: pick("Done. Searching for “sneekers” now finds sneakers. The search tests pass.", "完成了，打「布鞋子」也找得到布鞋，搜尋測試已通過。"), at: now - 600, kind: "message" },
  ],
}
const row = (id: keyof typeof labels) => ({
  id, label: labels[id], backend: "tmux", cwd, sessionId: conversation(id), icon: BRAND_MARK,
  assistant: id === "signin" ? "codex" : "claude", isClaude: id !== "signin",
  state: id === "checkout" && !answered ? "waiting" : id === "search" ? "idle" : "working",
  work_state: id === "checkout" && !answered ? "waiting_you" : id === "search" ? "ready" : "working",
  attention_count: 0,
  menu: id === "checkout" && !answered ? { type: "request", question: pick("Where should I put the new checkout?", "新結帳頁要先放哪裡？"),
    options: [{ n: 1, label: pick("Staging first", "先上測試站"), can: true, selected: false }, { n: 2, label: pick("Go live", "直接上線"), can: true, selected: false }] } : null,
  evidence: "process", activity: { known: true, at: now - 60 },
})
const snapshot = () => ({
  at: Date.now(), sessions: Object.keys(labels).map(id => row(id as keyof typeof labels)),
  scan: { complete: true, completed: { complete: true, sequence: revision }, emptyAuthoritative: true, epoch: 1, generation: revision, provenance: "demo" },
})
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
  if (path === "/v1/places") return json({ places: [{ id: "demo-shop", label: pick("Shop", "網路商店"), path: cwd, available: true, icon: null }], assistants: [{ id: "claude", label: "Claude Code" }, { id: "codex", label: "Codex" }] })
  if (path === "/v1/personas") return json({ license: "MIT", personas: [] })
  if (path === "/v1/orchestrator/tasks") return json({ at: Date.now(), tasks: [] })
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
    if (decodeURIComponent(key[1]) !== "checkout" || answered || !["1", "2"].includes(String(body.key))) return unavailable()
    const selected = String(body.key) === "2" ? pick("Go live.", "直接上線。") : pick("Staging first.", "先上測試站。")
    answered = true
    entries.checkout.push({ role: "user", text: selected, at: Math.floor(Date.now() / 1000), kind: "message" })
    entries.checkout.push({ role: "assistant", text: pick("Got it. This demonstration keeps the result here; no real deployment was started.", "收到。示範結果會留在這裡，沒有執行真正的部署。"), at: Math.floor(Date.now() / 1000) + 1, kind: "message" })
    publish()
    return json({ ok: true })
  }
  const send = /^\/v1\/sessions\/([^/]+)\/send$/.exec(path)
  if (send && method === "POST") {
    const id = decodeURIComponent(send[1])
    if (!entries[id]) return unavailable()
    const body = JSON.parse(String(init?.body ?? "{}"))
    entries[id].push({ role: "user", text: String(body.text ?? ""), at: Math.floor(Date.now() / 1000), kind: "message" })
    entries[id].push({ role: "assistant", text: pick("Got it. This is a demonstration reply; no deployment or real work was started.", "收到。這是示範回覆，沒有執行真正的部署或工作。"), at: Math.floor(Date.now() / 1000) + 1, kind: "message" })
    publish()
    return json({ ok: true })
  }
  if (/^\/v1\/sessions\/[^/]+\/info$/.test(path)) {
    const id = decodeURIComponent(path.split("/")[3]) as keyof typeof labels
    return json({ info: { session: { ...row(id), title: labels[id], model: "demo" }, models: [], context: { usedPercent: 18 }, deploy: [], links: [] } })
  }
  if (/^\/v1\/sessions\/[^/]+\/git$/.test(path)) return json({ git: { ahead: 0, behind: 0, branch: "checkout-redesign", clean: true, head: "0123456789abcdef", files: [] } })
  return unavailable()
}

class DemoEventSource extends EventTarget {
  onopen: ((event: Event) => void) | null = null
  onerror: ((event: Event) => void) | null = null
  constructor(_url: string | URL) {
    super()
    listeners.add(this)
    queueMicrotask(() => { if (listeners.has(this)) { this.onopen?.(new Event("open")); this.emit("sessions", snapshot()) } })
  }
  emit(type: string, value: unknown) { this.dispatchEvent(new MessageEvent(type, { data: JSON.stringify(value) })) }
  close() { listeners.delete(this) }
}
Object.defineProperty(globalThis, "EventSource", { value: DemoEventSource })
