import { useEffect, useRef, useState } from "react"
import type * as W from "@clawdline/contract"
import {BotAvatar} from "./BotAvatar"
import {avatarPaths} from "./avatar-paths"
import {agentName} from "./fallback-settings"
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

export function PermissionCard({summary,offer:loaded,enabled,onDetail,onAnswer,context,machine,startExpanded,onExpandedChange,w}: {summary?:W.ProjectAssistantReadPermissionSummary;offer?:W.ProjectAssistantPermissionOffer;enabled:boolean;context?:string;machine?:string;onDetail?:()=>Promise<boolean>;onAnswer:(offer:W.ProjectAssistantPermissionOffer,option:W.ProjectAssistantPermissionOption)=>Promise<void>;startExpanded?:boolean;onExpandedChange?:(open:boolean)=>void;w:Words}) {
  const [busy,setBusy]=useState(false),[error,setError]=useState(""),pending=useRef(false)
  const offer=loaded&&(!summary||samePermission(summary,loaded))?loaded:undefined
  const kind=offer?.action?.tool_kind??summary?.tool_kind??""
  const team=kind==="team_create"||kind==="team_update"
  const [expanded,setExpanded]=useState(startExpanded??(!team&&(summary??loaded)?.state==="pending"))
  const [detailState,setDetailState]=useState<"idle"|"loading"|"failed">("idle")
  const [detailError,setDetailError]=useState("")
  const reading=useRef(false)
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
    if((expanded||team)&&state==="pending"&&!offer&&detailState==="idle")void load()
  },[expanded,team,state,offer,detailState])
  if(!value)return null
  const action=offer?.action,title=action?.title??summary?.title??""
  const proposal=team?teamProposal(action?.detail):undefined
  const chosen=offer?.options.find(option=>option.id===offer.selected_option_id)
  const status=state==="answered"&&team&&chosen?(chosen.allow?(kind==="team_create"?w.teamCreated:w.teamUpdated):w.teamDeclined):state&&w[state]
  const canExpand=!!(action&&(action.detail||action.diffs.length||action.shortened)||!offer&&onDetail)
  return <section className={team?"pa-card pa-permission pa-team-proposal":"pa-card pa-permission"} data-permission-id={value.id} aria-label={team?actionHeading(kind,w):w.permission} tabIndex={team?-1:undefined} aria-busy={busy||undefined} data-state={state}>{team?
      <TeamProposalSummary heading={actionHeading(kind,w)} title={title} proposal={proposal} context={context} machine={machine} status={status} pending={state==="pending"} w={w}/>:
      <><h3>{actionHeading(kind,w)}{state==="pending"&&<span className="pa-permission-dot" aria-hidden="true"/>}</h3>{title&&<p className="pa-permission-title">{title}</p>}<small className="pa-permission-source">{context||offer?.fence.key.machine_id}</small><p className="pa-permission-state" role="status">{status}</p></>}
    {kind==="source_command"&&<p className="pa-approval-detail">{w.permissionSourceAuthority}</p>}
    {canExpand&&<details className="pa-permission-detail" open={expanded} onToggle={event=>{
      const open=event.currentTarget.open
      setExpanded(open);onExpandedChange?.(open)
      if(open&&state!=="pending"&&!offer)void load()
    }}><summary>{team?w.details:w.permissionContent}</summary>
      <div className="pa-permission-content">
        {!team&&!offer&&state==="pending"&&detailState!=="failed"&&<p role="status">{w.loading}</p>}
        {(!team||state!=="pending")&&!offer&&detailState==="failed"&&<p role="alert">{w.unavailable}{detailError&&` (${detailError})`} <button type="button" onClick={()=>void load()}>{w.retry}</button></p>}
        {action?.detail&&(team?<TeamProposalDetails detail={action.detail} proposal={proposal} w={w}/>:<pre>{action.detail}</pre>)}
        {action?.diffs.map((d,i)=><div className="pa-diff" key={i}><code>{d.path}</code>{d.old_text&&<pre className="pa-diff-old">{d.old_text}</pre>}<pre className="pa-diff-new">{d.new_text}</pre></div>)}
        {action?.shortened&&<small>{w.permissionShortened}</small>}
      </div>
    </details>}
    {team&&!offer&&state==="pending"&&<p className="pa-team-loading" role={detailState==="failed"?"alert":"status"}>{detailState==="failed"?<>{w.unavailable}{detailError&&` (${detailError})`} <button type="button" onClick={()=>void load()}>{w.retry}</button></>:w.loading}</p>}
    {offer&&(!team||state==="pending")&&<div className="pa-permission-options">{sortedOptions(offer.options).map(option=>{
      const label=team?(option.allow?(kind==="team_create"?w.teamApproveCreate:w.teamApproveUpdate):w.reject_once):permissionOptionLabel(option,w),selected=offer.selected_option_id===option.id
      return <button key={option.id} type="button" data-option-kind={option.kind} data-selected={selected||undefined} disabled={!enabled||busy||state!=="pending"} onClick={async()=>{if(!enabled||pending.current||permissionCardState(value,summary)!=="pending")return;pending.current=true;setBusy(true);setError("");try{await onAnswer(offer,option)}catch(error){setError(errorCode(error))}finally{pending.current=false;setBusy(false)}}}><span>{selected&&<span aria-hidden="true">✓ </span>}{label}</span>{!team&&option.label&&option.label!==label&&<small>{option.label}</small>}</button>
    })}</div>}{error&&<p role="alert">{w.permissionAnswerFailed} ({error})</p>}</section>
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

type TeamProposal = {
  changes: Record<string, unknown>
  folder?: string
  targetName?: string
  appearance?: W.ProjectAssistantAppearance
}

export function teamProposal(detail?:string):TeamProposal|undefined {
  if(!detail)return
  try {
    const plan=JSON.parse(detail)
    const changes=plan.settings??plan.changes
    if(!changes||typeof changes!=="object"||Array.isArray(changes))return
    const look=changes.appearance
    const appearance=look&&typeof look.shape==="string"&&Object.hasOwn(avatarPaths,look.shape)&&typeof look.color==="string"?
      {shape:look.shape as W.ProjectAssistantAppearance["shape"],color:look.color}:undefined
    const folder=typeof changes.folder==="string"?changes.folder:typeof plan.folder==="string"?plan.folder:undefined
    return {changes,appearance,folder,
      targetName:typeof plan.targetName==="string"?plan.targetName:undefined}
  } catch {return undefined}
}

export function TeamProposalSummary({heading,title,proposal,context,machine,status,pending,w}: {
  heading:string;title:string;proposal?:TeamProposal;context?:string;machine?:string;status?:string;pending:boolean;w:Words
}) {
  const folder=proposal?.folder
  const agent=proposal?.changes.agent
  return <>
    <div className="pa-team-heading">
      <h3>{heading}</h3>
      <span className="pa-team-status" role="status">{pending&&<span className="pa-permission-dot" aria-hidden="true"/>}{status}</span>
    </div>
    <div className="pa-team-identity">
      {proposal?.appearance&&<BotAvatar appearance={proposal.appearance} size={36}/>}
      <div>
        <strong>{title}</strong>
        {typeof agent==="string"&&<small>{agentName(agent)}</small>}
      </div>
    </div>
    {(folder||context)&&<p className="pa-team-destination">
      <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2Z"/></svg>
      {folder?<span><strong title={folder}>{folder.split(/[\\/]/).filter(Boolean).at(-1)||folder}</strong>{machine&&<small>{machine}</small>}</span>:<span>{context}</span>}
      {folder&&<span className="pa-team-destination-label">{w.project}</span>}
    </p>}
  </>
}

export function TeamProposalDetails({detail,proposal,w}:{detail:string;proposal?:TeamProposal;w:Words}) {
  if(!proposal)return <pre>{detail}</pre>
  const labels:Record<string,string>={name:w.name,instructions:w.instructions,agent:w.agent,permission_policy:w.teamApprovalPolicy,
    mode:w.teamMode,manageTeam:w.teamManageLabel,appearance:w.teamAppearance,hidden:w.teamHidden,avatarColor:w.teamColor,avatarShape:w.teamShape}
  const entries=Object.entries(proposal.changes).filter(([key])=>key!=="botId"&&key!=="createdBy"&&key!=="folder")
  return <dl className="pa-team-details">
    {proposal.folder&&<div><dt>{w.project}</dt><dd>{proposal.folder}</dd></div>}
    {entries.filter(([key])=>key!=="instructions").map(([key,value])=><div key={key}><dt>{labels[key]??key}</dt><dd>{
      typeof value==="boolean"?(value?w.teamYes:w.teamNo):
      typeof value==="string"?(key==="agent"?agentName(value):value||"—"):
      key==="appearance"&&proposal.appearance?`${proposal.appearance.shape} · ${proposal.appearance.color}`:JSON.stringify(value)
    }</dd></div>)}
    {"instructions" in proposal.changes&&<div className="pa-team-instructions"><dt>{w.instructions}</dt><dd>{String(proposal.changes.instructions)||"—"}</dd></div>}
  </dl>
}
