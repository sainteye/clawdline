import type * as W from "@clawdline/contract"
import type {Words} from "./words"

/** Mirrors capacity.MaximumBotDispatchedStripRows (bots.dispatched_strip_rows). */
export const DispatchedStripRows = 5

export type DispatchedKind = "needsYou" | "running" | "queued" | "delivered" | "failed"
export type DispatchedRow = {link: W.ProjectAssistantWorkLink; kind: DispatchedKind}

/** Mirrors projectassistant.SettledLanding: nothing more is owed. */
const settled = new Set(["landed", "incorporated", "abandoned", "nothing_to_land"])

/** Mirrors projectassistant.WorkLink.Unsettled: a finished link stays listed,
 * with no time limit, until the broker records its landing as settled. A
 * completed link from an older source has no landing state and stays owed;
 * a failed link is owed only when the broker opened a pending landing. */
function kindOf(link: W.ProjectAssistantWorkLink): DispatchedKind | null {
  if (link.state === "waiting_for_person") return "needsYou"
  if (link.state === "executing") return "running"
  if (link.state === "accepted") return "queued"
  if (link.state === "completed") return settled.has(link.landing ?? "") ? null : "delivered"
  if (link.state === "failed") return link.landing === "pending" ? "failed" : null
  return null
}

const rank: Record<DispatchedKind, number> = {needsYou: 0, running: 1, queued: 1, delivered: 2, failed: 2}

/** The Bot's dispatched Sessions the strip lists, from the latest state of
 * each work link: needs you, then running (longest first), then finished
 * work whose landing is still owed. */
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
    delivered: rows.filter(row => row.kind === "delivered" || row.kind === "failed").length,
  }
}

/** What a listed row waits for, in plain words, from the task record and its
 * landing state only. A delivery without a landing record says so instead of
 * guessing who it waits for. */
export function dispatchedReason(row: DispatchedRow, w: Words) {
  switch (row.kind) {
    case "needsYou": return w.dispatchedStateNeedsYou
    case "running": return w.dispatchedStateRunning
    case "queued": return w.dispatchedStateQueued
    case "failed": return w.dispatchedStateFailedOwed
    case "delivered": return row.link.landing === "pending" ? w.dispatchedStateDelivered : w.dispatchedStateLandingUnknown
  }
}
