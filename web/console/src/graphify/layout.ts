import type { GraphifyNode, GraphifyLink } from "@clawdline/contract"

export type Positioned = GraphifyNode & { x: number; y: number; color: number }
/** Stable community clusters, with no simulation, worker or external script. */
export function layoutGraph(nodes: GraphifyNode[]): Positioned[] {
 const groups = [...new Set(nodes.map(node => node.community))].sort()
 return groups.flatMap((group, index) => {
  const members = nodes.filter(node => node.community === group)
  const angle = index * Math.PI * 2 / groups.length
  const cx = groups.length === 1 ? 450 : 450 + Math.cos(angle) * 270
  const cy = groups.length === 1 ? 300 : 300 + Math.sin(angle) * 190
  return members.map((node, i) => {
   const theta = i * 2.399963229728653
   const radius = Math.sqrt((i + 0.5) / members.length) * (groups.length === 1 ? 245 : 100)
   return { ...node, x: cx + Math.cos(theta) * radius, y: cy + Math.sin(theta) * radius, color: index % 8 }
  })
 })
}
export function graphSelection(nodes: Positioned[], links: GraphifyLink[], search: string, group: string) {
 const text = search.trim().toLocaleLowerCase()
 const visible = nodes.filter(node => (group === "*" || node.community === group) &&
  (!text || `${node.label} ${node.file}`.toLocaleLowerCase().includes(text)))
 const ids = new Set(visible.map(node => node.id))
 return { nodes: visible, links: links.filter(link => ids.has(link.source) && ids.has(link.target)) }
}
