import type { TranscriptEntry } from "@clawdline/contract"

// A poll replaces the newest window while earlier pages stay in memory. Find
// the shared stretch and keep the old prefix, including entries the moving
// newest window has already dropped.
export function joinTranscript(kept: TranscriptEntry[], latest: TranscriptEntry[]): TranscriptEntry[] {
  if (!kept.length) return latest
  if (!latest.length) return kept
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
