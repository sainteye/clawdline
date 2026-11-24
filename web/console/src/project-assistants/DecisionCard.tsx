import { useEffect, useRef, useState } from "react"
import type * as W from "@clawdline/contract"
import {errorCode} from "./client"
import {permissionState,samePermission} from "./view-state"
import type { Words } from "./words"
import type {DecisionFollowUp} from "./decision-focus"

// A question a Bot asked with ask_person. The suggested replies are the
// Bot's words; pressing one sends that full text as the person's own message
// in this chat, so the card shows exactly what will be sent.
export function DecisionCard({summary,offer,enabled,onDetail,onAnswer,askerName,requesterName,followUp,highlight,w}:{
  summary:W.ProjectAssistantReadPermissionSummary
  offer?:W.ProjectAssistantPermissionOffer
  enabled:boolean
  onDetail?:()=>Promise<boolean>
  onAnswer:(offer:W.ProjectAssistantPermissionOffer,option:W.ProjectAssistantPermissionOption|null,text?:string)=>Promise<void>
  askerName:string
  requesterName?:string
  /** The Bot's progress on the turn this page's reply started; absent when unobserved. */
  followUp?:DecisionFollowUp
  /** The card a push notification opened the chat at. */
  highlight?:boolean
  w:Words
}) {
  const [busy,setBusy]=useState(false),[error,setError]=useState(""),[own,setOwn]=useState(false),[draft,setDraft]=useState("")
  // The reply the person pressed, kept from the press until the server's
  // state replaces it: "sending" before the answer is accepted, "sent" once
  // it is, "failed" when it was not. A reload shows the server's answer.
  const [choice,setChoice]=useState<{option:string|null;text:string;phase:"sending"|"sent"|"failed"}|null>(null)
  const pending=useRef(false),requested=useRef("")
  const summaryState=permissionState(summary)
  const state=summaryState==="pending"&&offer&&samePermission(summary,offer)?permissionState(offer):summaryState
  const decision=offer?.decision
  useEffect(()=>{
    if(offer||!onDetail||requested.current===summary.detail_cursor)return
    requested.current=summary.detail_cursor
    void onDetail().catch(()=>{})
  },[offer,onDetail,summary.detail_cursor])
  const send=async(option:W.ProjectAssistantPermissionOption|null,text?:string)=>{
    if(!offer||!enabled||pending.current||state!=="pending")return
    pending.current=true;setBusy(true);setError("")
    setChoice({option:option?.id??null,text:option?.label??text??"",phase:"sending"})
    try{await onAnswer(offer,option,text);setChoice(c=>c&&{...c,phase:"sent"});setOwn(false);setDraft("")}
    catch(e){setError(errorCode(e));setChoice(c=>c&&{...c,phase:"failed"});if(!option)setOwn(true)}
    finally{pending.current=false;setBusy(false)}
  }
  const answered=decision?.answer_text||offer?.options.find(o=>o.id===offer.selected_option_id)?.label
  const sent=state==="pending"&&choice?.phase==="sent"
  const locked=busy||sent
  const status=state==="answered"?(answered?w.decisionAnswered.replace("{text}",answered):""):state==="expired"?w.decisionExpired:state==="pending"?"":state==="unknown"?w.unknown:w.decisionWithdrawn
  // Right after the press the reply is "sent" before any turn is observed.
  const progress=state==="answered"?followUp:sent?followUp??"working":undefined
  const optionState=(id:string|null)=>choice&&choice.option===id&&choice.phase!=="failed"?"chosen":choice&&choice.phase!=="failed"?"other":undefined
  return <section className="pa-card pa-permission pa-decision" id={`pa-decision-${summary.id}`} data-permission-id={summary.id} data-state={state} data-reply={state==="pending"?choice?.phase:undefined} data-follow-up={progress} data-focused={highlight||undefined} tabIndex={highlight?-1:undefined} aria-label={w.decisionTitle} aria-busy={busy||undefined}>
    <h3>{w.decisionTitle}{state==="pending"&&!sent&&<span className="pa-permission-dot" aria-hidden="true"/>}</h3>
    <p className="pa-decision-question">{decision?.question??summary.title}</p>
    {decision?.request&&<details className="pa-decision-context">
      <summary>{w.decisionContext.replace("{name}",requesterName||decision.source_bot_id||"")}</summary>
      <p>{decision.request}</p>
    </details>}
    {(status||sent&&choice)&&<p className={`pa-permission-state${progress?" pa-decision-sent":""}`} role="status">
      <span>{sent&&choice?w.decisionAnswered.replace("{text}",choice.text):status}</span>
      {progress==="working"&&<span className="pa-decision-working"><span className="pa-decision-spinner" aria-hidden="true"/>{w.decisionSentWorking.replace("{name}",askerName)}</span>}
      {progress==="done"&&<span className="pa-decision-done"><span className="pa-decision-mark" aria-hidden="true">✓</span>{w.decisionDone.replace("{name}",askerName)}</span>}
    </p>}
    {offer&&state==="pending"&&!sent&&<>
      <small className="pa-decision-note">{w.decisionSuggested} · {w.decisionSendsAs}</small>
      <div className="pa-permission-options">
        {offer.options.map(option=>{const mark=optionState(option.id)
          return <button key={option.id} type="button" data-option-kind="decision" data-choice={mark} aria-pressed={mark==="chosen"||undefined} disabled={!enabled||locked} onClick={()=>void send(option)}>
            <span className="pa-decision-mark" aria-hidden="true">{mark==="chosen"?(busy?<span className="pa-decision-spinner"/>:"✓"):null}</span>
            <span>{option.label}</span>
            {mark==="chosen"&&busy&&<span className="pa-visually-hidden">{w.decisionSending}</span>}
          </button>})}
        {!own&&<button type="button" className="pa-decision-own" data-choice={choice&&choice.phase!=="failed"?"other":undefined} disabled={!enabled||locked} onClick={()=>setOwn(true)}><span>{w.decisionOwn}</span></button>}
        <button type="button" className="pa-decision-own pa-decision-dismiss" data-choice={choice&&choice.phase!=="failed"?"other":undefined} disabled={!enabled||locked} title={w.decisionDismissText} onClick={()=>void send(null,w.decisionDismissText)}><span>{w.decisionDismiss}</span></button>
      </div>
      {own&&<form className="pa-decision-form" onSubmit={e=>{e.preventDefault();const text=draft.trim();if(text)void send(null,text)}}>
        <textarea value={draft} autoFocus rows={2} maxLength={2000} disabled={!enabled||locked} aria-label={w.decisionOwnPlaceholder.replace("{name}",askerName)} placeholder={w.decisionOwnPlaceholder.replace("{name}",askerName)} onChange={e=>setDraft(e.target.value)}/>
        <button type="submit" className="pa-pill" data-choice={optionState(null)} disabled={!enabled||locked||!draft.trim()}>
          {choice?.option===null&&busy&&<span className="pa-decision-spinner" aria-hidden="true"/>}
          {choice?.option===null&&busy?w.decisionSending:w.decisionSend}
        </button>
      </form>}
    </>}
    {error&&state==="pending"&&<div className="pa-decision-failed" role="alert">
      <p>{error==="pa_outcome_unknown"?w.unknown:`${w.decisionSendFailed} (${error})`}</p>
      {choice&&choice.option!==null&&<button type="button" className="pa-pill" disabled={!enabled||busy} onClick={()=>{const o=offer?.options.find(x=>x.id===choice.option);if(o)void send(o)}}>{w.decisionRetry}</button>}
    </div>}
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

// A decision raised for a Bot-to-Bot exchange sits under that exchange's
// capsule in the asked Bot's chat (approved spec item 4). One whose capsule is
// not in the loaded timeline, or whose detail has not been read yet, stays in
// the permission section so it is never hidden.
export function decisionsUnderCapsules<T extends {summary:W.ProjectAssistantReadPermissionSummary;offer?:W.ProjectAssistantPermissionOffer}>(
  cards:readonly T[],messages:readonly W.ProjectAssistantMessage[],inline:ReadonlySet<string>,
):Map<string,T> {
  const capsules=new Set(messages.flatMap(m=>m.exchange?[m.exchange.id]:[]))
  return new Map(cards.flatMap(card=>{
    const id=card.summary.tool_kind==="decision"?card.offer?.decision?.exchange_id:undefined
    return id&&capsules.has(id)&&!inline.has(card.summary.id)?[[id,card] as const]:[]
  }))
}
