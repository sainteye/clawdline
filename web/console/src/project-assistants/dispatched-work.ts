import type * as W from "@clawdline/contract"
import type {Words} from "./words"
// @ts-expect-error Node executes source extensions.
import {hasExecutingDispatch} from "./working.ts"

/** Mirrors capacity.MaximumBotDispatchedStripRows (bots.dispatched_strip_rows). */
export const DispatchedStripRows = 5

export type DispatchedKind = "needsYou" | "running" | "queued"
export type DispatchedRow = {link: W.ProjectAssistantWorkLink; kind: DispatchedKind}

function kindOf(link: W.ProjectAssistantWorkLink): DispatchedKind | null {
  if (link.state === "waiting_for_person") return "needsYou"
  if (link.state === "executing") return "running"
  if (link.state === "accepted") return "queued"
  return null
}

const rank: Record<DispatchedKind, number> = {needsYou: 0, running: 1, queued: 1}

/** Active dispatched Sessions by latest state: needs you, then running
 * or queued (longest first). */
export function dispatchedRows(links: readonly W.ProjectAssistantWorkLink[]): DispatchedRow[] {
  const latest = new Map<string, W.ProjectAssistantWorkLink>()
  for (const link of links) latest.set(link.id, link)
  const started = (row: DispatchedRow) => {
    const at = row.link.started_at ? Date.parse(row.link.started_at) : NaN
    return Number.isFinite(at) ? at : Infinity
  }
  return [...latest.values()].flatMap(link => {
    const kind = kindOf(link)
    return kind ? [{link, kind}] : []
  }).sort((a, b) => rank[a.kind] - rank[b.kind] || started(a) - started(b))
}

export function dispatchedCounts(rows: readonly DispatchedRow[]) {
  return {
    running: rows.filter(row => row.kind === "running" || row.kind === "queued").length,
    needsYou: rows.filter(row => row.kind === "needsYou").length,
  }
}

export type DispatchedCounts = ReturnType<typeof dispatchedCounts>

/** The strip's line and list. The counts are the roster row's own
 * dispatched_work, which the daemon counts over every work link the Bot holds
 * (countDispatchedWork); the chat has loaded only a page of its own-chat links,
 * so it lists what it has and says how many more there are. An older source
 * without dispatched_work falls back to the loaded links. */
export function dispatchedStrip(links: readonly W.ProjectAssistantWorkLink[], work: W.ProjectAssistantDispatchedWork | undefined) {
  const all = dispatchedRows(links)
  const n = (v: unknown) => typeof v === "number" && Number.isSafeInteger(v) && v > 0 ? v : 0
  const counts: DispatchedCounts = work
    ? {running: n(work.running), needsYou: n(work.needs_you)}
    : dispatchedCounts(all)
  const total = counts.running + counts.needsYou
  const rows = total > 0 ? all.slice(0, Math.min(DispatchedStripRows, total)) : []
  return {counts, rows, more: Math.max(0, total - rows.length), showWorking: hasExecutingDispatch(work)}
}

export function dispatchedReason(row: DispatchedRow, w: Words) {
  switch (row.kind) {
    case "needsYou": return w.dispatchedStateNeedsYou
    case "running": return w.dispatchedStateRunning
    case "queued": return w.dispatchedStateQueued
  }
}
