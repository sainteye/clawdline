import type { ProjectAssistantWorkLink } from "@clawdline/contract"
import type { Words } from "./words"
export function WorkResultCard({work,w,onOpen}:{work:ProjectAssistantWorkLink;w:Words;onOpen?:(work:ProjectAssistantWorkLink)=>void}) {
  return <article className="pa-card"><small>{work.board_item_id||work.task_id}</small><h3>{w.work}</h3><p>{w[work.state]}</p>{work.landing_commit&&<code>{work.landing_commit}</code>}{onOpen&&<button onClick={()=>onOpen(work)}>{w.openWork} ›</button>}</article>
}
