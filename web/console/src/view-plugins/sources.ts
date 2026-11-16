import type { SessionRow } from "@clawdline/contract"
import type { MachineListPresentation, SessionDestination, SessionProjectionSource } from "../cloud/all-machine-sessions.js"
import { destinationKey } from "../cloud/all-machine-sessions.js"
import { WORK_STATES, type SourceSession, type StateTime, type ViewReading, type ViewSource, type WorkState } from "./bridge.js"
import { PluginError } from "./package.js"

const state = (v: unknown): WorkState => WORK_STATES.includes(v as WorkState) ? v as WorkState : "unknown"
const basename = (v: string) => v.replace(/[\\/]+$/, "").split(/[\\/]/).pop() || v
const project = (key: string | undefined) => key ? { key, label: basename(key) } : null
const callbackWait = (row: Partial<SessionRow> | undefined): { waitKind?: "callback" } =>
  row?.waiting_for?.some(target => target.kind === "callback" || target.kind === "heavy") ? { waitKind: "callback" } : {}
const stateTime = (row: Partial<SessionRow> | undefined): StateTime | undefined => {
  if (!row) return
  const basis = row.work_provenance === "self" && row.work_since ? "declaration" : row.work_state === "working" && row.working_since ? "turn" : undefined
  const seconds = basis === "declaration" ? row.work_since : basis === "turn" ? row.working_since : undefined
  if (!basis || typeof seconds !== "number" || !Number.isFinite(seconds) || seconds <= 0 || !Number.isFinite(new Date(seconds * 1000).getTime())) return
  return { at: seconds * 1000, basis }
}
export function localPluginRows(rows: readonly SessionRow[], connected: boolean, focused?: string | null): SourceSession[] {
  return rows.map(row => ({ key: JSON.stringify([row.id, row.execution_generation ?? row.sessionId ?? ""]),
    title: row.label || "Session", project: project(row.project_key), workState: state(row.work_state),
    stateTime: stateTime(row), ...callbackWait(row), ...(focused !== undefined ? { focused: row.id === focused } : {}),
    freshness: !connected ? "stale" : row.evidence === "none" ? "unknown" : "fresh" }))
}
export function cloudPluginRows(rows: readonly MachineListPresentation[], focused?: SessionDestination | null): SourceSession[] {
  return rows.map(p => ({ key: destinationKey(p.destination), title: p.title, project: project(p.projectKey),
    workState: state(p.status?.work_state), stateTime: stateTime(p.status), ...callbackWait(p.status),
    ...(focused !== undefined ? { focused: !!focused && destinationKey(p.destination) === destinationKey(focused) } : {}),
    freshness: p.projection?.freshness === "current" && p.observedAt !== undefined ? "fresh" :
      p.projection?.freshness === "stale" ? "stale" : "unknown" }))
}

/** Fresh display authorization comes from the existing pinned read, never from ss/ names. */
export function cloudPluginSources(options: {
  owner: string
  machines: readonly { id: string; label: string; name?: string; pairing: string; selectable: boolean; operationAllowed?: boolean | null }[]
  source: SessionProjectionSource
  current: (key: string) => boolean
  focused?: () => SessionDestination | null
  open: (destination: SessionDestination) => void
}): ViewSource[] {
  return options.machines.filter(m => m.pairing === "paired" && m.selectable && m.operationAllowed === true).map(machine => {
    const key = JSON.stringify([options.owner, machine.id, machine.pairing, machine.operationAllowed])
    const valid = () => { if (!options.current(key)) throw new PluginError("scope_changed") }
    return {
      key, label: machine.name || machine.label,
      async read(signal) {
        valid()
        const projection = await options.source.readMachine(machine.id, signal)
        valid()
        if (projection.kind !== "ready") throw new PluginError("host_unavailable")
        const rows = await options.source.readMachinePresentations?.(machine.id, signal)
        valid()
        if (!rows) throw new PluginError("permission_denied")
        return { sessions: cloudPluginRows(rows, options.focused?.()), connected: true }
      },
      subscribe(fn) { return options.source.subscribe(e => { if (e.machineID === machine.id && e.kind !== "detail_changed") fn() }) },
      async prepareOpen(ref, signal) {
        valid()
        const projection = await options.source.readMachine(machine.id, signal, true)
        valid()
        if (projection.kind !== "ready") throw new PluginError("host_unavailable")
        const row = projection.rows.find(r => destinationKey(r.destination) === ref && r.freshness === "current")
        if (!row) throw new PluginError("session_unavailable")
        return () => { valid(); options.open(row.destination) }
      },
    }
  })
}

/** Every view uses every currently authorized source. Private identities stay in the host. */
export function fleetPluginSource(sources: readonly ViewSource[], label: string): ViewSource {
  const ordered = [...sources].sort((a, b) => a.key.localeCompare(b.key))
  return {
    key: JSON.stringify(ordered.map(source => source.key)), label,
    async read(signal) {
      if (signal.aborted) throw new PluginError("scope_changed")
      if (!ordered.length) throw new PluginError("permission_denied")
      const results = await Promise.allSettled(ordered.map(async source => source.read(signal)))
      if (signal.aborted) throw new PluginError("scope_changed")
      const sessions: SourceSession[] = [], readings: ViewReading[] = []
      for (const [index, result] of results.entries()) {
        if (result.status === "rejected") {
          if (result.reason instanceof PluginError && ["permission_denied", "scope_changed"].includes(result.reason.code)) throw result.reason
          continue
        }
        const source = ordered[index], reading = result.value
        readings.push(reading)
        sessions.push(...reading.sessions.map(row => ({ ...row,
          key: JSON.stringify([source.key, row.key]),
          project: row.project ? { ...row.project, key: JSON.stringify([source.key, row.project.key]) } : null,
          connected: row.connected ?? reading.connected,
        })))
      }
      if (!readings.length) throw new PluginError("host_unavailable")
      return { sessions, connected: readings.some(reading => reading.connected),
        partial: readings.length !== ordered.length || readings.some(reading => !reading.connected || reading.partial) }
    },
    subscribe(fn) {
      const stop = ordered.map(source => source.subscribe(fn))
      return () => { for (const unsubscribe of stop) unsubscribe() }
    },
    async prepareOpen(key, signal) {
      let parts: unknown
      try { parts = JSON.parse(key) } catch { throw new PluginError("session_unavailable") }
      if (!Array.isArray(parts) || parts.length !== 2 || parts.some(part => typeof part !== "string")) throw new PluginError("session_unavailable")
      const source = ordered.find(source => source.key === parts[0])
      if (!source) throw new PluginError("scope_changed")
      return source.prepareOpen(parts[1], signal)
    },
  }
}
