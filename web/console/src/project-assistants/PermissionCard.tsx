import { useEffect, useRef, useState } from "react"
import type * as W from "@clawdline/contract"
import {errorCode} from "./client"
import {sameFence} from "./view-state"
import type { Words } from "./words"

type PermissionIdentity=Pick<W.ProjectAssistantReadPermissionSummary,"id"|"fence"|"turn_id">
type PermissionStatus=Pick<W.ProjectAssistantReadPermissionSummary,"state"|"expires_at">

export function samePermission(a:PermissionIdentity,b:PermissionIdentity) {
  return a.id===b.id&&a.turn_id===b.turn_id&&sameFence(a.fence,b.fence)
}

export function permissionState(value:PermissionStatus,now=Date.now()) {
  return value.state==="pending"&&!(Date.parse(value.expires_at)>now)?"expired":value.state
}

// Codync's sortedOptions: allow before deny, once before persistent options.
export function sortedOptions(options:readonly W.ProjectAssistantPermissionOption[]) {
  const rank={allow_once:0,allow_always:1,reject_once:2,reject_always:3}
  return [...options].sort((a,b)=>rank[a.kind]-rank[b.kind])
}
function permissionOptionLabel(option:W.ProjectAssistantPermissionOption,w:Words) {
  if(option.kind==="allow_always"&&option.label.trim().toLowerCase()==="allow for this session")return w.allow_session
  return w[option.kind]
}

export function PermissionCard({summary,offer:loaded,enabled,onDetail,onAnswer,context,startExpanded,onExpandedChange,w}: {summary?:W.ProjectAssistantReadPermissionSummary;offer?:W.ProjectAssistantPermissionOffer;enabled:boolean;context?:string;onDetail?:()=>Promise<boolean>;onAnswer:(offer:W.ProjectAssistantPermissionOffer,option:W.ProjectAssistantPermissionOption)=>Promise<void>;startExpanded?:boolean;onExpandedChange?:(open:boolean)=>void;w:Words}) {
  const [busy,setBusy]=useState(false),[error,setError]=useState(""),pending=useRef(false)
  const [expanded,setExpanded]=useState(startExpanded??(summary??loaded)?.state==="pending")
  const [detailState,setDetailState]=useState<"idle"|"loading"|"failed">("idle")
  const [detailError,setDetailError]=useState("")
  const reading=useRef(false)
  const offer=loaded&&(!summary||samePermission(summary,loaded))?loaded:undefined
  const value=offer??summary
  const request=useRef(onDetail);request.current=onDetail
  const state=value?permissionCardState(value,summary):undefined
  const load=async()=>{
    if(!request.current||reading.current)return
    reading.current=true
    setDetailState("loading")
    setDetailError("")
    try{setDetailState(await request.current()?"idle":"failed")}
    catch(error){setDetailError(errorCode(error));setDetailState("failed")}
    finally{reading.current=false}
  }
  useEffect(()=>{
    if(expanded&&state==="pending"&&!offer&&detailState==="idle")void load()
  },[expanded,state,offer,detailState])
  if(!value)return null
  const action=offer?.action,kind=action?.tool_kind??summary?.tool_kind??"",title=action?.title??summary?.title??""
  const canExpand=!!(action&&(action.detail||action.diffs.length||action.shortened)||!offer&&onDetail)
  return <section className="pa-card pa-permission" data-permission-id={value.id} aria-label={w.permission} aria-busy={busy||undefined} data-state={state}><h3>{actionHeading(kind,w)}{state==="pending"&&<span className="pa-permission-dot" aria-hidden="true"/>}</h3>{title&&<p className="pa-permission-title">{title}</p>}<small className="pa-permission-source">{context||offer?.fence.key.machine_id}</small><p className="pa-permission-state" role="status">{state&&w[state]}</p>
    {canExpand&&<details className="pa-permission-detail" open={expanded} onToggle={event=>{
      const open=event.currentTarget.open
      setExpanded(open);onExpandedChange?.(open)
      if(open&&state!=="pending"&&!offer)void load()
    }}><summary>{w.permissionContent}</summary>
      <div className="pa-permission-content">
        {!offer&&state==="pending"&&detailState!=="failed"&&<p role="status">{w.loading}</p>}
        {!offer&&detailState==="failed"&&<p role="alert">{w.unavailable}{detailError&&` (${detailError})`} <button type="button" onClick={()=>void load()}>{w.retry}</button></p>}
        {action?.detail&&<pre>{action.detail}</pre>}
        {action?.diffs.map((d,i)=><div className="pa-diff" key={i}><code>{d.path}</code>{d.old_text&&<pre className="pa-diff-old">{d.old_text}</pre>}<pre className="pa-diff-new">{d.new_text}</pre></div>)}
        {action?.shortened&&<small>{w.permissionShortened}</small>}
      </div>
    </details>}
    <div className="pa-permission-options">{offer&&sortedOptions(offer.options).map(option=>{
      const label=permissionOptionLabel(option,w),selected=offer.selected_option_id===option.id
      return <button key={option.id} type="button" data-option-kind={option.kind} data-selected={selected||undefined} disabled={!enabled||busy||state!=="pending"} onClick={async()=>{if(!enabled||pending.current||permissionCardState(value,summary)!=="pending")return;pending.current=true;setBusy(true);setError("");try{await onAnswer(offer,option)}catch(error){setError(errorCode(error))}finally{pending.current=false;setBusy(false)}}}><span>{selected&&<span aria-hidden="true">✓ </span>}{label}</span>{option.label&&option.label!==label&&<small>{option.label}</small>}</button>
    })}</div>{error&&<p role="alert">{w.permissionAnswerFailed} ({error})</p>}</section>
}

function permissionCardState(value:PermissionStatus,summary?:PermissionStatus) {
  if(!summary)return permissionState(value)
  const state=permissionState(summary)
  return state==="pending"?permissionState(value):state
}

// Codync titles a card by what the tool would do; an unknown kind still says
// it asks for permission.
function actionHeading(kind:string,w:Words){
  const heading:Record<string,string>={execute:w.permissionExecute,edit:w.permissionEdit,delete:w.permissionDelete,move:w.permissionMove,read:w.permissionRead,search:w.permissionSearch,fetch:w.permissionFetch,switch_mode:w.permissionSwitchMode}
  return heading[kind]??w.permission
}
