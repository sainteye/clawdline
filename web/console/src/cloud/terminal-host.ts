import type { TerminalCloudClient } from "./terminal-transport.js"

export interface TerminalHost { client: TerminalCloudClient; machine: string }
let active: TerminalHost | null = null
const listeners = new Set<(host: TerminalHost | null) => void>()

/** The selected, authenticated Cloud line. Renewal replaces the client and invalidates terminal keys. */
export function terminalHost(): TerminalHost | null { return active }
export function setTerminalHost(host: TerminalHost | null): void {
  active = host
  for (const listener of listeners) listener(active)
}
export function watchTerminalHost(listener: (host: TerminalHost | null) => void): () => void {
  listeners.add(listener)
  listener(active)
  return () => listeners.delete(listener)
}
