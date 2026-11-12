import { useState } from "react"
import type * as W from "@clawdline/contract"
import type { Words } from "./words"
// Codync's sortedOptions: allow before deny, once before persistent options.
export function sortedOptions(options:readonly W.ProjectAssistantPermissionOption[]) {
  const rank={allow_once:0,allow_always:1,reject_once:2,reject_always:3}
  return [...options].sort((a,b)=>rank[a.kind]-rank[b.kind])
}
export function PermissionCard({offer,enabled,onAnswer,w}: {offer:W.ProjectAssistantPermissionOffer;enabled:boolean;onAnswer:(offer:W.ProjectAssistantPermissionOffer,option:W.ProjectAssistantPermissionOption)=>Promise<void>;w:Words}) {
  const [busy,setBusy]=useState(false), expired=Date.parse(offer.expires_at)<=Date.now()
  const state=offer.state==="pending"&&expired?"expired":offer.state
  return <section className="pa-card pa-permission" data-permission-id={offer.id} aria-label={w.permission}><h3>{w.permission}</h3><p>{w[state]}</p><small>{offer.fence.key.machine_id} · {offer.fence.project.project_id} · {offer.turn_id}</small><div className="pa-actions">{sortedOptions(offer.options).map(option=><button key={option.id} type="button" disabled={!enabled||busy||state!=="pending"||!(option.kind==="reject_once"||option.kind==="allow_once"&&option.read_only)} onClick={async()=>{setBusy(true);try{await onAnswer(offer,option)}finally{setBusy(false)}}}>{w[option.kind]}</button>)}</div></section>
}
