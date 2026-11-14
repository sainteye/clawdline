import type { ProjectAssistantTrace,ProjectAssistantReadTraceSummary,ProjectAssistantPermissionOffer } from "@clawdline/contract"
import type { Words } from "./words"
export function Trace({items=[],summaries=[],details={},total=items.length,more=false,onMore,onDetail,w}:{items?:readonly ProjectAssistantTrace[];summaries?:readonly ProjectAssistantReadTraceSummary[];details?:Readonly<Record<string,ProjectAssistantTrace|ProjectAssistantPermissionOffer>>;total?:number;more?:boolean;onMore?:()=>void;onDetail?:(summary:ProjectAssistantReadTraceSummary)=>void;w:Words}) {
  if(summaries.length||!items.length&&total>0)return <details className="pa-trace"><summary>{w.trace} · {total}</summary>{summaries.map(row=>{const value=details[row.id],full=value&&"text" in value?value:undefined;return <section key={row.id} data-trace-id={row.id}><h4>{traceLabel(row.category,w)}</h4>{full?<pre>{full.text}</pre>:<button type="button" onClick={()=>onDetail?.(row)}>{w.details}</button>}</section>})}{more&&<button type="button" onClick={onMore}>{w.more}</button>}</details>
  if(!items.length)return null
  const turns=new Map<string,ProjectAssistantTrace[]>()
  for(const item of items)turns.set(item.turn_id,[...(turns.get(item.turn_id)??[]),item])
  return <details className="pa-trace"><summary>{w.trace} · {items.length}</summary>{[...turns].map(([id,rows])=><section key={id}>{rows.map((row,i)=><div key={i}><small>{traceLabel(row.category,w)}</small><pre>{row.text}</pre></div>)}</section>)}</details>
}

/** What one trace step is, in the person's words; the provider's own category name stays out of view. */
export function traceLabel(category:string,w:Words){
  const said:Record<string,string>={agent_message_chunk:w.traceReply,agent_thought_chunk:w.traceThought,tool_call:w.traceTool,tool_call_update:w.traceTool,tool:w.traceTool,plan:w.tracePlan,no_visible_reply:w.traceNoReply}
  return said[category]??w.traceStep
}
