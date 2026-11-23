import { useEffect, useRef, useState } from "react"
import type * as W from "@clawdline/contract"
import {errorCode} from "./client"
import {permissionState} from "./view-state"
import type { Words } from "./words"

// A question a Bot asked with ask_person. The suggested replies are the
// Bot's words; pressing one sends that full text as the person's own message
// in this chat, so the card shows exactly what will be sent.
export function DecisionCard({summary,offer,enabled,onDetail,onAnswer,askerName,requesterName,w}:{
  summary:W.ProjectAssistantReadPermissionSummary
  offer?:W.ProjectAssistantPermissionOffer
  enabled:boolean
  onDetail?:()=>Promise<boolean>
  onAnswer:(offer:W.ProjectAssistantPermissionOffer,option:W.ProjectAssistantPermissionOption|null,text?:string)=>Promise<void>
  askerName:string
  requesterName?:string
  w:Words
}) {
  const [busy,setBusy]=useState(false),[error,setError]=useState(""),[own,setOwn]=useState(false),[draft,setDraft]=useState("")
  const pending=useRef(false),requested=useRef("")
  const state=permissionState(summary)
  const decision=offer?.decision
  useEffect(()=>{
    if(offer||!onDetail||requested.current===summary.detail_cursor)return
    requested.current=summary.detail_cursor
    void onDetail().catch(()=>{})
  },[offer,onDetail,summary.detail_cursor])
  const send=async(option:W.ProjectAssistantPermissionOption|null,text?:string)=>{
    if(!offer||!enabled||pending.current||state!=="pending")return
    pending.current=true;setBusy(true);setError("")
    try{await onAnswer(offer,option,text);setOwn(false);setDraft("")}catch(e){setError(errorCode(e))}
    finally{pending.current=false;setBusy(false)}
  }
  const answered=decision?.answer_text||offer?.options.find(o=>o.id===offer.selected_option_id)?.label
  const status=state==="answered"?(answered?w.decisionAnswered.replace("{text}",answered):""):state==="expired"?w.decisionExpired:state==="pending"?"":w.decisionWithdrawn
  return <section className="pa-card pa-permission pa-decision" id={`pa-decision-${summary.id}`} data-permission-id={summary.id} data-state={state} aria-label={w.decisionTitle} aria-busy={busy||undefined}>
    <h3>{w.decisionTitle}{state==="pending"&&<span className="pa-permission-dot" aria-hidden="true"/>}</h3>
    <p className="pa-decision-question">{decision?.question??summary.title}</p>
    {decision?.request&&<details className="pa-decision-context">
      <summary>{w.decisionContext.replace("{name}",requesterName||decision.source_bot_id||"")}</summary>
      <p>{decision.request}</p>
    </details>}
    {status&&<p className="pa-permission-state" role="status">{status}</p>}
    {offer&&state==="pending"&&<>
      <small className="pa-decision-note">{w.decisionSuggested} · {w.decisionSendsAs}</small>
      <div className="pa-permission-options">
        {offer.options.map(option=><button key={option.id} type="button" data-option-kind="decision" disabled={!enabled||busy} onClick={()=>void send(option)}><span>{option.label}</span></button>)}
        {!own&&<button type="button" className="pa-decision-own" disabled={!enabled||busy} onClick={()=>setOwn(true)}><span>{w.decisionOwn}</span></button>}
      </div>
      {own&&<form className="pa-decision-form" onSubmit={e=>{e.preventDefault();const text=draft.trim();if(text)void send(null,text)}}>
        <textarea value={draft} autoFocus rows={2} maxLength={2000} disabled={!enabled||busy} aria-label={w.decisionOwnPlaceholder.replace("{name}",askerName)} placeholder={w.decisionOwnPlaceholder.replace("{name}",askerName)} onChange={e=>setDraft(e.target.value)}/>
        <button type="submit" className="pa-pill" disabled={!enabled||busy||!draft.trim()}>{w.decisionSend}</button>
      </form>}
    </>}
    {error&&<p role="alert">{w.permissionAnswerFailed} ({error})</p>}
  </section>
}

// The requester's chat only points at the waiting question; it carries no
// authority and cannot answer it.
export function DecisionPointer({name,onOpen,w}:{name:string;onOpen?:()=>void;w:Words}) {
  return <div className="pa-decision-pointer" role="status">
    <span className="pa-permission-dot" aria-hidden="true"/>
    <span>{w.decisionWaiting.replace("{name}",name)}</span>
    {onOpen&&<button type="button" onClick={onOpen}>{w.decisionGo.replace("{name}",name)} ›</button>}
  </div>
}
