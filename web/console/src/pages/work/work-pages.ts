import type { WorkV2Item } from "./api.js"

/** Append a keyset page once, replacing a repeated row with its fresher copy. */
export function appendWorkPage(current: WorkV2Item[], incoming: WorkV2Item[]): WorkV2Item[] {
  const fresh = new Map(incoming.map((item) => [item.id, item]))
  const seen = new Set<string>()
  const rows = current.map((item) => {
    seen.add(item.id)
    return fresh.get(item.id) ?? item
  })
  for (const item of incoming) {
    if (!seen.has(item.id)) rows.push(item)
  }
  return rows
}
