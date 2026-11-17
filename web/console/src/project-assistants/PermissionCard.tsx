import { useRef, useState } from "react"
import type * as W from "@clawdline/contract"
import {errorCode} from "./client"
import type { Words } from "./words"
// Codync's sortedOptions: allow before deny, once before persistent options.
export function sortedOptions(options:readonly W.ProjectAssistantPermissionOption[]) {
  const rank={allow_once:0,allow_always:1,reject_once:2,reject_always:3}
  return [...options].sort((a,b)=>rank[a.kind]-rank[b.kind])
}
export function PermissionCard({summary,offer,enabled,onDetail,onAnswer,context,w}: {summary?:W.ProjectAssistantReadPermissionSummary;offer?:W.ProjectAssistantPermissionOffer;enabled:boolean;context?:string;onDetail?:()=>void;onAnswer:(offer:W.ProjectAssistantPermissionOffer,option:W.ProjectAssistantPermissionOption)=>Promise<void>;w:Words}) {
  const [busy,setBusy]=useState(false),[error,setError]=useState(""),pending=useRef(false),value=offer??summary
  if(!value)return null
  const expired=Date.parse(value.expires_at)<=Date.now(),state=value.state==="pending"&&expired?"expired":value.state
  const action=offer?.action,kind=action?.tool_kind??summary?.tool_kind??"",title=action?.title??summary?.title??""
  return <section className="pa-card pa-permission" data-permission-id={value.id} aria-label={w.permission} aria-busy={busy||undefined} data-state={state}><h3>{actionHeading(kind,w)}{state==="pending"&&<span className="pa-permission-dot" aria-hidden="true"/>}</h3>{title&&<p className="pa-permission-title">{title}</p>}<small className="pa-permission-source">{context||offer?.fence.key.machine_id}</small><p className="pa-permission-state" role="status">{w[state]}</p>
    {action&&(action.detail||action.diffs.length>0)&&<details className="pa-permission-detail" ><summary>{w.permissionContent}</summary>
      {action.detail&&<pre>{action.detail}</pre>}
      {action.diffs.map((d,i)=><div className="pa-diff" key={i}><code>{d.path}</code>{d.old_text&&<pre className="pa-diff-old">{d.old_text}</pre>}<pre className="pa-diff-new">{d.new_text}</pre></div>)}
      {action.shortened&&<small>{w.permissionShortened}</small>}
    </details>}
    <div className="pa-permission-options">{offer?sortedOptions(offer.options).map(option=><button key={option.id} type="button" data-option-kind={option.kind} disabled={!enabled||busy||state!=="pending"} onClick={async()=>{if(pending.current||Date.parse(offer.expires_at)<=Date.now())return;pending.current=true;setBusy(true);setError("");try{await onAnswer(offer,option)}catch(error){setError(errorCode(error))}finally{pending.current=false;setBusy(false)}}}><span>{w[option.kind]}</span>{option.label&&option.label!==w[option.kind]&&<small>{option.label}</small>}</button>):<button type="button" onClick={onDetail}>{w.details}</button>}</div>{error&&<p role="alert">{w.permissionAnswerFailed} ({error})</p>}</section>
}

// Codync titles a card by what the tool would do; an unknown kind still says
// it asks for permission.
function actionHeading(kind:string,w:Words){
  const heading:Record<string,string>={execute:w.permissionExecute,edit:w.permissionEdit,delete:w.permissionDelete,move:w.permissionMove,read:w.permissionRead,search:w.permissionSearch,fetch:w.permissionFetch,switch_mode:w.permissionSwitchMode}
  return heading[kind]??w.permission
}
