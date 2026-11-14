import type { SessionRow } from "@clawdline/contract"
import type { MachineListPresentation, SessionDestination, SessionProjectionSource } from "../cloud/all-machine-sessions.js"
import { destinationKey } from "../cloud/all-machine-sessions.js"
import { WORK_STATES, type SourceSession, type ViewSource, type WorkState } from "./bridge.js"
import { PluginError } from "./package.js"

const state = (v: unknown): WorkState => WORK_STATES.includes(v as WorkState) ? v as WorkState : "unknown"
const basename = (v: string) => v.replace(/[\\/]+$/, "").split(/[\\/]/).pop() || v
const project = (key: string | undefined) => key ? { key, label: basename(key) } : null
export function localPluginRows(rows: readonly SessionRow[], connected: boolean): SourceSession[] {
  return rows.map(row => ({ key: JSON.stringify([row.id, row.execution_generation ?? row.sessionId ?? ""]),
    title: row.label || "Session", project: project(row.project_key), workState: state(row.work_state),
    freshness: !connected ? "stale" : row.evidence === "none" ? "unknown" : "fresh" }))
}
export function cloudPluginRows(rows: readonly MachineListPresentation[]): SourceSession[] {
  return rows.map(p => ({ key: destinationKey(p.destination), title: p.title, project: project(p.projectKey),
    workState: state(p.status?.work_state),
    freshness: p.projection?.freshness === "current" && p.observedAt !== undefined ? "fresh" :
      p.projection?.freshness === "stale" ? "stale" : "unknown" }))
}

/** Fresh display authorization comes from the existing pinned read, never from ss/ names. */
export function cloudPluginSources(options: {
  owner: string
  machines: readonly { id: string; label: string; name?: string; pairing: string; selectable: boolean; operationAllowed?: boolean | null }[]
  source: SessionProjectionSource
  current: (key: string) => boolean
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
        return { sessions: cloudPluginRows(rows), connected: true }
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
