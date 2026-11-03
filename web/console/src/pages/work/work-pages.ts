/** Append a keyset page once, replacing a repeated row with its fresher copy. */
export function appendWorkPage<T extends { id: string }>(current: T[], incoming: T[]): T[] {
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
