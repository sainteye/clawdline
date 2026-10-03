/** Content-free close outcomes shared by a terminal detail and the global list. */
export interface TerminalCloseState {
  machine: string
  terminal: string
  requestID: string
  submittedAt: number
  status: "pending" | "unknown" | "ok" | "refused" | "ended"
  error: string
}

const states = new Map<string, TerminalCloseState>()
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
export function clearTerminalCloseStates(): void { if (states.size) { states.clear(); changed() } }
export function beginTerminalClose(machine: string, terminal: string, requestID: string): void {
  states.set(key(machine, terminal), { machine, terminal, requestID, submittedAt: Date.now(), status: "pending", error: "" })
  changed()
}
export function settleTerminalClose(machine: string, terminal: string, requestID: string,
  status: TerminalCloseState["status"], error = ""): boolean {
  const held = states.get(key(machine, terminal))
  if (!held || held.requestID !== requestID) return false
  states.set(key(machine, terminal), { ...held, status, error })
  changed()
  return true
}
/** A later authoritative read can establish that a terminal ended, not who ended it. */
export function observeTerminalEnded(machine: string, terminal: string): void {
  const held = terminalCloseState(machine, terminal)
  if (held && held.status !== "ok") settleTerminalClose(machine, terminal, held.requestID, "ended")
}
