import { PLUGIN_LIMITS as B } from "./limits.js"
import { exact, plain, text, PluginError, type Capability } from "./package.js"

export const WORK_STATES = ["ready", "working", "holding", "waiting_you", "waiting_session", "unknown", "milestone_complete", "work_complete"] as const
export type WorkState = typeof WORK_STATES[number]
export type Freshness = "fresh" | "stale" | "unknown"
export interface SourceSession {
  key: string; title: string; project: { key: string; label: string } | null
  workState: WorkState; freshness: Freshness; connected?: boolean
}
export interface ViewReading { sessions: SourceSession[]; connected: boolean; partial?: boolean }
export interface ViewSource {
  key: string; label: string
  read(signal: AbortSignal): Promise<ViewReading>
  subscribe(listener: () => void): () => void
  prepareOpen(key: string, signal: AbortSignal): Promise<() => void>
}
export interface ViewContext {
  language: string; theme: "dark" | "light"; reducedMotion: boolean; visible: boolean
  scopeLabel: string; connection: "connected" | "disconnected"
}
const clipped = (s: string) => { if ([...s].length > B.textCharacters) throw new PluginError("message_too_large"); return s }
const size = (v: unknown) => new TextEncoder().encode(JSON.stringify(v)).length
const errorCode = (e: unknown) => e instanceof PluginError ? e.code : "host_unavailable"

/** One port, one frame, one explicit scope grant. No REST or DOM access crosses it. */
export function pluginBridge(options: {
  port: MessagePort; instanceId: string; capabilities: readonly Capability[]
  reading: ViewReading; context: ViewContext
  open: (key: string) => Promise<void>; close: () => void; fault: (code: string) => void
  now?: () => number
}) {
  const { port, instanceId } = options
  const now = options.now ?? Date.now
  let alive = true, subscribed = false, navigating = false, revision = 1, context = options.context, reading = options.reading
  const streamId = crypto.randomUUID(), refs = new Map<string, string>(), projects = new Map<string, string>()
  let byRef = new Map<string, SourceSession>(), pending = 0, windowAt = now(), requests = 0
  let notification: ReturnType<typeof setTimeout> | undefined
  const reply = (id: string, payload: object) => { if (alive) port.postMessage({ v: 1, instanceId, id, ...payload }) }
  const event = (name: string, data: unknown) => { if (alive) port.postMessage({ v: 1, instanceId, event: name, data }) }
  function snapshot() {
    if (reading.sessions.length > B.sessions) throw new PluginError("too_many_sessions")
    const currentKeys = new Set(reading.sessions.map(s => s.key))
    const projectKeys = new Set(reading.sessions.flatMap(s => s.project ? [s.project.key] : []))
    for (const key of refs.keys()) if (!currentKeys.has(key)) refs.delete(key)
    for (const key of projects.keys()) if (!projectKeys.has(key)) projects.delete(key)
    byRef = new Map()
    const sessions = reading.sessions.map(s => {
      if (!refs.has(s.key)) refs.set(s.key, crypto.randomUUID())
      const ref = refs.get(s.key)!
      byRef.set(ref, s)
      let project = null
      if (s.project) {
        if (!projects.has(s.project.key)) projects.set(s.project.key, crypto.randomUUID())
        project = { ref: projects.get(s.project.key)!, label: clipped(s.project.label) }
      }
      return { ref, title: clipped(s.title), project,
        workState: WORK_STATES.includes(s.workState) ? s.workState : "unknown",
        freshness: (s.connected ?? reading.connected) ? s.freshness : "stale" }
    })
    const result = { streamId, revision, sessions }
    if (size(result) > B.messageBytes) throw new PluginError("message_too_large")
    return result
  }
  // Validate the first projection before a frame receives any private title.
  snapshot()
  function dispose() {
    if (!alive) return
    alive = false; clearTimeout(notification); port.onmessage = null; port.close()
    refs.clear(); projects.clear(); byRef.clear()
  }
  const fault = (code: string) => { dispose(); options.fault(code) }
  port.onmessageerror = () => fault("invalid_request")
  port.onmessage = async ({ data }: MessageEvent<unknown>) => {
    if (!alive || !plain(data) || data.v !== 1 || data.instanceId !== instanceId) return
    if (!text(data.id) || !exact(data, ["v", "instanceId", "id", "method", "params"]) || !plain(data.params)) { fault("invalid_request"); return }
    const id = data.id, method = data.method, params = data.params
    if (now() - windowAt >= 1000) { windowAt = now(); requests = 0 }
    if (++requests > B.requestsPerSecond || pending >= B.pending) { reply(id, { error: { code: "busy", message: "Plugin request capacity reached" } }); fault("busy"); return }
    pending++
    try {
      if (method === "sessions.subscribe" && exact(params, [])) {
        if (!options.capabilities.includes("sessions.read")) throw new PluginError("permission_denied")
        const result = snapshot(); subscribed = true; reply(id, { result })
      } else if (method === "sessions.open" && exact(params, ["ref"]) && text(params.ref)) {
        if (!options.capabilities.includes("sessions.open")) throw new PluginError("permission_denied")
        if (navigating) throw new PluginError("busy")
        const previous = byRef.get(params.ref)
        const row = previous && reading.sessions.find(s => s.key === previous.key)
        if (!row) throw new PluginError("session_unavailable")
        if (!(row.connected ?? reading.connected)) throw new PluginError("host_unavailable")
        // The source rechecks exact machine/execution identity before navigating.
        navigating = true
        try { await options.open(row.key) } finally { navigating = false }
        if (!alive) return
        reply(id, { result: { opened: true } })
        options.close()
      } else if (method === "view.close" && exact(params, [])) {
        reply(id, { result: { closed: true } }); options.close()
      } else throw new PluginError(typeof method === "string" && ["sessions.subscribe", "sessions.open", "view.close"].includes(method) ? "invalid_request" : "method_not_supported")
    } catch (e) { reply(id, { error: { code: errorCode(e), message: errorCode(e) } }) }
    finally { pending-- }
  }
  port.start()
  return {
    dispose,
    update(next: ViewReading, nextContext: ViewContext) {
      if (!alive) return
      reading = next; context = nextContext
      if (++revision >= Number.MAX_SAFE_INTEGER) { fault("protocol_mismatch"); return }
      event("host.context", context)
      if (subscribed && notification === undefined) notification = setTimeout(() => {
        notification = undefined
        try { event("sessions.snapshot", snapshot()) } catch (e) { fault(errorCode(e)) }
      }, 0)
    },
  }
}

/** Author HTML only runs in an opaque srcdoc frame. No package URL is served on
 * the authenticated application origin, including a top-level URL. */
export function pluginDocument(html: string): string {
  const policy = "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data: blob:; font-src data:; media-src data: blob:; connect-src 'none'; worker-src 'none'; object-src 'none'; frame-src 'none'; base-uri 'none'; form-action 'none'"
  return '<!doctype html><meta http-equiv="Content-Security-Policy" content="' + policy + '"><meta name="referrer" content="no-referrer">' + html
}
