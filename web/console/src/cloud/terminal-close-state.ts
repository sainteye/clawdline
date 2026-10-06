/** Content-free close outcomes shared by a terminal detail and the global list. */
export interface TerminalCloseState {
  machine: string
  terminal: string
  requestID: string
  submittedAt: number
  status: "pending" | "unknown" | "ok" | "refused" | "ended"
  error: string
}

/**
 * How long an unconfirmed close keeps the close action withheld when no read resolves it. The
 * page re-reads the terminal as soon as a close turns unknown; this bound covers the read that
 * never answers, so the action comes back without a reload.
 */
export const TERMINAL_CLOSE_UNKNOWN_MS = 60_000

const states = new Map<string, TerminalCloseState>()
const expiries = new Map<string, ReturnType<typeof setTimeout>>()
const listeners = new Set<() => void>()
let revision = 0
const key = (machine: string, terminal: string) => `${machine}\0${terminal}`
const changed = () => { revision++; for (const listener of listeners) listener() }

export function terminalCloseRevision(): number { return revision }
export function watchTerminalClose(listener: () => void): () => void {
  listeners.add(listener)
  return () => listeners.delete(listener)
}
export function terminalCloseState(machine: string, terminal: string): TerminalCloseState | null {
  return states.get(key(machine, terminal)) ?? null
}
export function recentTerminalCloseStates(): TerminalCloseState[] { return [...states.values()].sort((a, b) => b.submittedAt - a.submittedAt) }
const disarm = (at: string) => { const timer = expiries.get(at); if (timer !== undefined) { clearTimeout(timer); expiries.delete(at) } }
export function clearTerminalCloseStates(): void {
  for (const at of [...expiries.keys()]) disarm(at)
  if (states.size) { states.clear(); changed() }
}
export function beginTerminalClose(machine: string, terminal: string, requestID: string): void {
  disarm(key(machine, terminal))
  states.set(key(machine, terminal), { machine, terminal, requestID, submittedAt: Date.now(), status: "pending", error: "" })
  changed()
}
export function settleTerminalClose(machine: string, terminal: string, requestID: string,
  status: TerminalCloseState["status"], error = ""): boolean {
  const held = states.get(key(machine, terminal))
  if (!held || held.requestID !== requestID) return false
  const at = key(machine, terminal)
  disarm(at)
  states.set(at, { ...held, status, error })
  if (status === "unknown") {
    const timer = setTimeout(() => {
      expiries.delete(at)
      const now = states.get(at)
      if (now?.requestID === requestID && now.status === "unknown") { states.delete(at); changed() }
    }, TERMINAL_CLOSE_UNKNOWN_MS)
    ;(timer as { unref?: () => void }).unref?.()
    expiries.set(at, timer)
  }
  changed()
  return true
}
/** A later authoritative read can establish that a terminal ended, not who ended it. */
export function observeTerminalEnded(machine: string, terminal: string): void {
  const held = terminalCloseState(machine, terminal)
  if (held && held.status !== "ok") settleTerminalClose(machine, terminal, held.requestID, "ended")
}

/**
 * A later read found the terminal still there: a close whose outcome was unknown did not end it,
 * so the record goes and the close action is offered again. A pending, refused or confirmed
 * outcome is left as it is.
 */
export function observeTerminalRunning(machine: string, terminal: string): void {
  const at = key(machine, terminal)
  if (states.get(at)?.status !== "unknown") return
  disarm(at)
  states.delete(at)
  changed()
}
