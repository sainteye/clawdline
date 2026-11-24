import { useSyncExternalStore } from "react"

export interface ClosingRow {
  id: string
  execution_generation?: string
  sessionId?: string
}

export function closingRowKey(machine: string | null, row: ClosingRow): string {
  return JSON.stringify([machine, row.id, row.execution_generation ?? row.sessionId ?? null])
}

type Close = { machine: string | null; confirmed: boolean; hidden: boolean }
let rows: ReadonlyMap<string, Close> = new Map()
const listeners = new Set<() => void>()

function publish(next: ReadonlyMap<string, Close>) {
  rows = next
  for (const listener of listeners) listener()
}

export function useClosingRows(): ReadonlyMap<string, Close> {
  return useSyncExternalStore(subscribe, () => rows)
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener)
  return () => { listeners.delete(listener) }
}

export function beginClosingRow(key: string, machine: string | null, hideWhilePending: boolean): boolean {
  if (rows.has(key)) return false
  publish(new Map(rows).set(key, { machine, confirmed: false, hidden: hideWhilePending }))
  return true
}

export function settleClosingRow(key: string, confirmed: boolean): void {
  const close = rows.get(key)
  if (!close) return
  const next = new Map(rows)
  if (confirmed) next.set(key, { ...close, confirmed, hidden: true })
  else next.delete(key)
  publish(next)
}

/** Keep a confirmed close hidden through any retained row from before its receipt. */
export function observeClosingRows(machine: string | null, visibleKeys: ReadonlySet<string>): void {
  let next: Map<string, Close> | undefined
  for (const [key, close] of rows) {
    if (close.machine !== machine || !close.confirmed || visibleKeys.has(key)) continue
    next ??= new Map(rows)
    next.delete(key)
  }
  if (next) publish(next)
}
