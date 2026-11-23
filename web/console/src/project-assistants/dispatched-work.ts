import type * as W from "@clawdline/contract"

/** Mirrors capacity.MaximumBotDispatchedStripRows (bots.dispatched_strip_rows). */
export const DispatchedStripRows = 5
/** Mirrors projectassistant.MaxDeliveredWorkSeconds (bots.dispatched_delivered_seconds). */
export const DeliveredWorkMs = 24 * 60 * 60 * 1000

export type DispatchedKind = "needsYou" | "running" | "queued" | "delivered"
export type DispatchedRow = {link: W.ProjectAssistantWorkLink; kind: DispatchedKind}

function kindOf(link: W.ProjectAssistantWorkLink, now: number): DispatchedKind | null {
  if (link.state === "waiting_for_person") return "needsYou"
  if (link.state === "executing") return "running"
  if (link.state === "accepted") return "queued"
  if (link.state === "completed") {
    const at = link.finished_at ? Date.parse(link.finished_at) : NaN
    return Number.isFinite(at) && now - at < DeliveredWorkMs ? "delivered" : null
  }
  return null
}

const rank: Record<DispatchedKind, number> = {needsYou: 0, running: 1, queued: 1, delivered: 2}

/** The Bot's dispatched Sessions the strip lists, from the latest state of
 * each work link: needs you, then running (longest first), then delivered. */
export function dispatchedRows(links: readonly W.ProjectAssistantWorkLink[], now: number): DispatchedRow[] {
  const latest = new Map<string, W.ProjectAssistantWorkLink>()
  for (const link of links) latest.set(link.id, link)
  const started = (row: DispatchedRow) => {
    const at = row.link.started_at ? Date.parse(row.link.started_at) : NaN
    return Number.isFinite(at) ? at : Infinity
  }
  return [...latest.values()].flatMap(link => {
    const kind = kindOf(link, now)
    return kind ? [{link, kind}] : []
  }).sort((a, b) => rank[a.kind] - rank[b.kind] || started(a) - started(b))
}

export function dispatchedCounts(rows: readonly DispatchedRow[]) {
  return {
    running: rows.filter(row => row.kind === "running" || row.kind === "queued").length,
    needsYou: rows.filter(row => row.kind === "needsYou").length,
    delivered: rows.filter(row => row.kind === "delivered").length,
  }
}
