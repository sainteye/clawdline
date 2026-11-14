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
  const action=offer?.action,kind=action?.tool_kind??summary?.tool_kind??"",title=action?.title??summary?.title??""
  return <section className="pa-card pa-permission" data-permission-id={value.id} aria-label={w.permission}><h3>{actionHeading(kind,w)}</h3>{title&&<p className="pa-permission-title">{title}</p>}<p>{w[state]}</p>
    {action&&(action.detail||action.diffs.length>0)&&<details className="pa-permission-detail" open={action.diffs.length===0&&action.detail.length<240}><summary>{w.permissionContent}</summary>
      {action.detail&&<pre>{action.detail}</pre>}
      {action.diffs.map((d,i)=><div className="pa-diff" key={i}><code>{d.path}</code>{d.old_text&&<pre className="pa-diff-old">{d.old_text}</pre>}<pre className="pa-diff-new">{d.new_text}</pre></div>)}
      {action.shortened&&<small>{w.permissionShortened}</small>}
    </details>}
    <small>{value.fence.key.machine_id} · {value.fence.project.project_id} · {value.turn_id}</small><div className="pa-actions">{offer?sortedOptions(offer.options).map(option=><button key={option.id} type="button" disabled={!enabled||busy||state!=="pending"} onClick={async()=>{setBusy(true);try{await onAnswer(offer,option)}finally{setBusy(false)}}}>{w[option.kind]}</button>):<button type="button" onClick={onDetail}>{w.details}</button>}</div></section>
}

// Codync titles a card by what the tool would do; an unknown kind still says
// it asks for permission.
function actionHeading(kind:string,w:Words){
  const heading:Record<string,string>={execute:w.permissionExecute,edit:w.permissionEdit,delete:w.permissionDelete,move:w.permissionMove,read:w.permissionRead,search:w.permissionSearch,fetch:w.permissionFetch,switch_mode:w.permissionSwitchMode}
  return heading[kind]??w.permission
}
