// @ts-expect-error -- Node's strip-types runner loads this owner and its focused test.
import { TerminalChannelTransport } from "./terminal-transport.ts"
// @ts-expect-error -- Node's strip-types runner loads this owner and its focused test.
import { CloudTerminalSession } from "./terminal-session.ts"
import type { TerminalHost } from "./terminal-host.js"
// @ts-expect-error -- Node's strip-types runner loads this owner and its focused test.
import { terminalHost, watchTerminalHost } from "./terminal-host.ts"
// @ts-expect-error -- Node's strip-types runner loads this owner and its focused test.
import { TerminalObservation } from "./terminal-observation.ts"

export const TERMINAL_IDLE_MS = 15_000
type Entry = { host: TerminalHost; machine: string; session: CloudTerminalSession; transport: TerminalChannelTransport;
  observation: TerminalObservation;
  users: number; starting: Promise<void> | null; idle: ReturnType<typeof setTimeout> | null }
const entries = new Map<string, Entry>()

function discard(entry: Entry): void {
  if (entries.get(entry.machine) === entry) entries.delete(entry.machine)
  if (entry.idle) clearTimeout(entry.idle)
  // A revoked or replaced line must not publish another request with its old identity.
  if (entry.session.releasable && terminalHost() === entry.host && entry.host.client.ready && !entry.host.client.retired)
    void entry.session.request("release_connection").catch(() => undefined).finally(() => {
      entry.session.dispose(); entry.transport.dispose()
    })
  else { entry.session.dispose(); entry.transport.dispose() }
}

/** One machine connection per browser tab, retained briefly across page navigation. */
export async function acquireTerminalConnection(host: TerminalHost, machine: string, tab: string, observation?: TerminalObservation):
  Promise<{ session: CloudTerminalSession; observation: TerminalObservation; release: () => void }> {
  let entry = entries.get(machine)
  if (entry && (entry.host !== host || entry.host.client !== host.client || terminalHost() !== host ||
    !host.client.ready || host.client.retired || (!entry.starting && !entry.session.reusable))) {
    discard(entry)
    entry = undefined
  }
  if (!entry) {
    const timeline = observation ?? new TerminalObservation(() => undefined)
    const transport = new TerminalChannelTransport(host.client, machine, timeline)
    entry = { host, machine, transport, observation: timeline, session: new CloudTerminalSession(transport, tab, timeline),
      users: 0, starting: null, idle: null }
    entries.set(machine, entry)
  }
  if (entry.idle) { clearTimeout(entry.idle); entry.idle = null }
  entry.users++
  const held = entry
  try {
    if (!held.starting && !held.session.reusable) held.starting = held.session.start().finally(() => { held.starting = null })
    if (held.starting) await held.starting
    if (entries.get(machine) !== held || terminalHost() !== host || !held.session.reusable) throw new Error("cloud_reconnecting")
  } catch (error) {
    held.users--
    discard(held)
    throw error
  }
  let released = false
  return { session: held.session, observation: held.observation, release: () => {
    if (released) return
    released = true
    if (--held.users === 0 && entries.get(machine) === held)
      held.idle = setTimeout(() => discard(held), TERMINAL_IDLE_MS)
  } }
}

export function invalidateTerminalConnections(): void {
  for (const entry of [...entries.values()]) discard(entry)
}

watchTerminalHost((host) => {
  for (const entry of [...entries.values()]) if (entry.host !== host) discard(entry)
})
