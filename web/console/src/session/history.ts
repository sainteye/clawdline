import type { TranscriptEntry, TranscriptPage } from "@clawdline/contract"

// A poll replaces the newest window while earlier pages stay in memory. Find
// the shared stretch and keep the old prefix, including entries the moving
// newest window has already dropped.
export function joinTranscript(kept: TranscriptEntry[], latest: TranscriptEntry[]): TranscriptEntry[] {
  if (!kept.length) return latest
  if (!latest.length) return kept
  if (kept.every((row) => row.id) && latest.every((row) => row.id)) {
    const fresh = new Map(latest.map((row) => [row.id, row]))
    const held = new Set(kept.map((row) => row.id))
    return [...kept.map((row) => fresh.get(row.id) ?? row), ...latest.filter((row) => !held.has(row.id))]
  }
  const same = (a: TranscriptEntry, b: TranscriptEntry) =>
    a.role === b.role && a.text === b.text && a.tool === b.tool && a.at === b.at && a.source === b.source
  for (let overlap = Math.min(kept.length, latest.length); overlap > 0; overlap--) {
    let matches = true
    for (let i = 0; i < overlap; i++) {
      if (!same(kept[kept.length - overlap + i], latest[i])) {
        matches = false
        break
      }
    }
    if (matches) return [...kept, ...latest.slice(overlap)]
  }
  return [...kept, ...latest]
}


/** Only unfinished tool pages need rereading after leaving the newest window. */
export function pendingHistoryPages(held: { base: TranscriptPage; pages: TranscriptPage[] }, newest: TranscriptPage): TranscriptPage[] {
  const latest = /^acp2:([^:]+):(\d+)$/.exec(newest.signature)
  if (!latest) return []
  return [held.base, ...held.pages].filter((page) => {
    const old = /^acp2:([^:]+):(\d+)$/.exec(page.signature)
    return !!page.readBefore && !!old && old[1] === latest[1] && BigInt(old[2]) < BigInt(latest[2]) &&
      page.entries.some((entry) => entry.role === "tool" && entry.activity &&
        !["completed", "failed", "cancelled"].includes(entry.activity.status ?? ""))
  })
}
