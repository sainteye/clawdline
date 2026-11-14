import { useState } from "react"
import type * as W from "@clawdline/contract"
import type { Words } from "./words"
// Codync's sortedOptions: allow before deny, once before persistent options.
export function sortedOptions(options:readonly W.ProjectAssistantPermissionOption[]) {
  const rank={allow_once:0,allow_always:1,reject_once:2,reject_always:3}
  return [...options].sort((a,b)=>rank[a.kind]-rank[b.kind])
}
export function PermissionCard({summary,offer,enabled,onDetail,onAnswer,w}: {summary?:W.ProjectAssistantReadPermissionSummary;offer?:W.ProjectAssistantPermissionOffer;enabled:boolean;onDetail?:()=>void;onAnswer:(offer:W.ProjectAssistantPermissionOffer,option:W.ProjectAssistantPermissionOption)=>Promise<void>;w:Words}) {
  const [busy,setBusy]=useState(false),value=offer??summary
  if(!value)return null
  const expired=Date.parse(value.expires_at)<=Date.now(),state=value.state==="pending"&&expired?"expired":value.state
  return <section className="pa-card pa-permission" data-permission-id={value.id} aria-label={w.permission}><h3>{w.permission}</h3><p>{w[state]}</p><small>{value.fence.key.machine_id} · {value.fence.project.project_id} · {value.turn_id}</small><div className="pa-actions">{offer?sortedOptions(offer.options).map(option=><button key={option.id} type="button" disabled={!enabled||busy||state!=="pending"||!(option.kind==="reject_once"||option.kind==="allow_once")} onClick={async()=>{setBusy(true);try{await onAnswer(offer,option)}finally{setBusy(false)}}}>{w[option.kind]}</button>):<button type="button" onClick={onDetail}>{w.details}</button>}</div></section>
}
