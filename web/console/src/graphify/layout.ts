import type { GraphifyNode, GraphifyLink } from "@clawdline/contract"

export type Positioned = GraphifyNode & { x: number; y: number; color: number }
type Island = { group: string; count: number }
type Box = { x: number; y: number; width: number; height: number }
/** Give a populous community enough area without a force simulation or timer. */
function allocateIslands(islands: Island[], box: Box, result: Map<string, Box>): void {
 if (!islands.length) return
 if (islands.length === 1) {result.set(islands[0].group, box);return}
 const total = islands.reduce((sum, island) => sum + island.count, 0)
 let cut = 1, weight = islands[0].count
 while (cut < islands.length - 1 && Math.abs(total / 2 - weight - islands[cut].count) < Math.abs(total / 2 - weight)) {
  weight += islands[cut].count;cut++
 }
 const ratio = weight / total
 const horizontal = box.width >= box.height
 const first = horizontal ? {...box, width:box.width*ratio} : {...box, height:box.height*ratio}
 const second = horizontal ? {...box, x:box.x+first.width, width:box.width-first.width} : {...box, y:box.y+first.height, height:box.height-first.height}
 allocateIslands(islands.slice(0,cut),first,result)
 allocateIslands(islands.slice(cut),second,result)
}
/** Stable community clusters, with no simulation, worker or external script. */
export function layoutGraph(nodes: GraphifyNode[]): Positioned[] {
 const groups = [...new Set(nodes.map(node => node.community))].sort()
 const boxes = new Map<string, Box>()
 allocateIslands(groups.map(group=>({group,count:nodes.filter(node=>node.community===group).length})),{x:0,y:0,width:900,height:600},boxes)
 return groups.flatMap((group, index) => {
  const members = nodes.filter(node => node.community === group)
  const { x, y, width, height } = boxes.get(group)!
  const cx = x + width / 2, cy = y + height / 2
  return members.map((node, i) => {
   const theta = i * 2.399963229728653
   const radius = Math.sqrt((i + 0.5) / members.length) * (groups.length === 1 ? 245 : Math.min(width, height) * .4)
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
