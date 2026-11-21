import { useEffect, useRef, useState } from "react"
import type * as W from "@clawdline/contract"
import {errorCode} from "./client"
import {permissionState,samePermission,type PermissionStatus} from "./view-state"
import type { Words } from "./words"

// The tower answers offers too, so the identity and expiry rules live in view-state.
export {permissionState,samePermission}

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
  const team=kind==="team_create"||kind==="team_update"
  const chosen=offer?.options.find(option=>option.id===offer.selected_option_id)
  const status=state==="answered"&&team&&chosen?(chosen.allow?(kind==="team_create"?w.teamCreated:w.teamUpdated):w.teamDeclined):state&&w[state]
  const canExpand=!!(action&&(action.detail||action.diffs.length||action.shortened)||!offer&&onDetail)
  return <section className="pa-card pa-permission" data-permission-id={value.id} aria-label={team?actionHeading(kind,w):w.permission} tabIndex={team?-1:undefined} aria-busy={busy||undefined} data-state={state}><h3>{actionHeading(kind,w)}{state==="pending"&&<span className="pa-permission-dot" aria-hidden="true"/>}</h3>{title&&<p className="pa-permission-title">{title}</p>}<small className="pa-permission-source">{context||offer?.fence.key.machine_id}</small><p className="pa-permission-state" role="status">{status}</p>
    {kind==="source_command"&&<p className="pa-approval-detail">{w.permissionSourceAuthority}</p>}
    {canExpand&&<details className="pa-permission-detail" open={expanded} onToggle={event=>{
      const open=event.currentTarget.open
      setExpanded(open);onExpandedChange?.(open)
      if(open&&state!=="pending"&&!offer)void load()
    }}><summary>{w.permissionContent}</summary>
      <div className="pa-permission-content">
        {!offer&&state==="pending"&&detailState!=="failed"&&<p role="status">{w.loading}</p>}
        {!offer&&detailState==="failed"&&<p role="alert">{w.unavailable}{detailError&&` (${detailError})`} <button type="button" onClick={()=>void load()}>{w.retry}</button></p>}
        {action?.detail&&(team?<TeamDetails detail={action.detail} w={w}/>:<pre>{action.detail}</pre>)}
        {action?.diffs.map((d,i)=><div className="pa-diff" key={i}><code>{d.path}</code>{d.old_text&&<pre className="pa-diff-old">{d.old_text}</pre>}<pre className="pa-diff-new">{d.new_text}</pre></div>)}
        {action?.shortened&&<small>{w.permissionShortened}</small>}
      </div>
    </details>}
    <div className="pa-permission-options">{offer&&(!team||state==="pending")&&sortedOptions(offer.options).map(option=>{
      const label=team?(option.allow?(kind==="team_create"?w.teamApproveCreate:w.teamApproveUpdate):w.reject_once):permissionOptionLabel(option,w),selected=offer.selected_option_id===option.id
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
  const heading:Record<string,string>={execute:w.permissionExecute,source_command:w.permissionExecute,edit:w.permissionEdit,delete:w.permissionDelete,move:w.permissionMove,read:w.permissionRead,search:w.permissionSearch,fetch:w.permissionFetch,switch_mode:w.permissionSwitchMode,team_create:w.teamCreateTitle,team_update:w.teamUpdateTitle}
  return heading[kind]??w.permission
}

function TeamDetails({detail,w}:{detail:string;w:Words}) {
  try {
    const plan=JSON.parse(detail)
    const change=plan.settings??plan.changes
    if(!change||typeof change!=="object"||Array.isArray(change))return <pre>{detail}</pre>
    const labels:Record<string,string>={name:w.name,instructions:w.instructions,agent:w.agent,permission_policy:w.teamApprovalPolicy,
      mode:w.teamMode,manageTeam:w.teamManageLabel,appearance:w.teamAppearance,hidden:w.teamHidden,avatarColor:w.teamColor,avatarShape:w.teamShape}
    const entries=Object.entries(change).filter(([key])=>key!=="botId"&&key!=="createdBy")
    return <dl className="pa-team-details">{entries.map(([key,value])=><div key={key}><dt>{labels[key]??key}</dt><dd>{
      typeof value==="boolean"?(value?w.teamYes:w.teamNo):typeof value==="string"?<span>{value||"—"}</span>:key==="appearance"&&value&&typeof value==="object"&&"shape" in value&&"color" in value?
        <span>{String(value.shape)} · {String(value.color)}</span>:JSON.stringify(value)
    }</dd></div>)}</dl>
  } catch {return <pre>{detail}</pre>}
}
