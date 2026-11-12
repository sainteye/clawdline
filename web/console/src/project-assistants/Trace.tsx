import type { ProjectAssistantTrace } from "@clawdline/contract"
import type { Words } from "./words"
export function Trace({items,w}:{items:readonly ProjectAssistantTrace[];w:Words}) {
  if(!items.length)return null
  const turns=new Map<string,ProjectAssistantTrace[]>()
  for(const item of items)turns.set(item.turn_id,[...(turns.get(item.turn_id)??[]),item])
  return <details className="pa-trace"><summary>{w.trace} · {items.length}</summary>{[...turns].map(([id,rows])=><section key={id}><h4>{id}</h4>{rows.map((row,i)=><div key={i}><small>{row.category}</small><pre>{row.text}</pre></div>)}</section>)}</details>
}
