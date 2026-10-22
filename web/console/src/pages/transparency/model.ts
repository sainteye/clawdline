/** Selection within a facet is OR; different facets are AND. Empty means all. */
export interface Entry {
  id: string
  scope: string
  agents: string[]
  stages: string[]
  scenes: string[]
}
export type Facet = "scope" | "agents" | "stages" | "scenes"
export type Filters = Record<Facet, string[]>
export const facets: Record<Facet, readonly string[]> = {
  scope: ["default", "automatic", "conditional", "common"],
  agents: ["claude", "codex", "antigravity"],
  stages: ["start", "turn", "finish"],
  scenes: ["general", "memory", "role", "skill", "board", "todo", "task", "handoff", "machine", "workflow"],
}
export function filtersFromHash(hash: string): Filters {
  const params = new URLSearchParams(hash.replace(/^#/, ""))
  return Object.fromEntries(Object.entries(facets).map(([facet, choices]) =>
    [facet, [...new Set((params.get(facet) ?? "").split(",").filter(value => choices.includes(value)))]])) as Filters
}
export function matches(entry: Entry, filters: Filters): boolean {
  return (Object.keys(facets) as Facet[]).every(facet => {
    const values = facet === "scope" ? [entry.scope] : entry[facet]
    return !filters[facet].length || filters[facet].some(value => values.includes(value))
  })
}
export function toggleFilter(filters: Filters, facet: Facet, value: string): Filters {
  if (!facets[facet].includes(value)) return filters
  return { ...filters, [facet]: filters[facet].includes(value)
    ? filters[facet].filter(current => current !== value) : [...filters[facet], value] }
}
export function filterHash(filters: Filters): string {
  const params = new URLSearchParams({ page: "transparency" })
  for (const facet of Object.keys(facets) as Facet[]) {
    if (filters[facet].length) params.set(facet, filters[facet].join(","))
  }
  return "#" + params.toString()
}
