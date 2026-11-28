import { legacyMemoryProposalTitle } from "./notice-text"
import { MEMORY_SAVE, MEMORY_SKIP } from "./chat-items"
import { useEffect, useRef, useState } from "react"
import type * as W from "@clawdline/contract"
import {quietFleetStream} from "../client"
import {errorCode,errorField,provablyNotSent,sendBlockedByInterruptedTurn} from "./client"
import {permissionState,samePermission} from "./view-state"
import type { Words } from "./words"
import type {DecisionFollowUp} from "./decision-focus"
import {DECISION_TRACE_SLOW_MS,newDecisionAttempt} from "./decision-trace"
import {traceDecision} from "./decision-trace-send"

/** The daemon's bound on a typed answer, MaxDecisionAnswerBytes (512 KiB minus the quoted question). */
export const DECISION_ANSWER_BYTES=512*1024-16*1024
const utf8Bytes=(text:string)=>new TextEncoder().encode(text).length

/**
 * What a failed press means for the person. `retry` offers Try again only
 * where pressing again can succeed; `check` means the reply may already be
 * on the computer, and the card asks again with the same answer, which the
 * daemon replays from its one receipt per card instead of sending twice.
 */
export type DecisionFailureKind="interrupted"|"tooLong"|"answered"|"expired"|"withdrawn"|"format"|"offline"|"check"|"changed"|"refused"|"notSent"
export type DecisionFailure={kind:DecisionFailureKind;code:string;retry:boolean}
type DecisionChoice={option:string|null;text:string;phase:"sending"|"sent"|"failed"}
export function decisionFailure(error:unknown):DecisionFailure{
  const code=errorCode(error),field=errorField(error)
  const kind=((): DecisionFailureKind=>{
    if(sendBlockedByInterruptedTurn(error))return "interrupted"
    if(field==="decision_answer_bytes")return "tooLong"
    if(field==="decision_answered")return "answered"
    if(code==="pa_expired")return "expired"
    if(field==="decision_withdrawn")return "withdrawn"
    if(code==="pa_invalid"||code==="pa_unsupported"||code==="provider_version_unsupported")return "format"
    if(code==="pa_offline"||code==="pa_capacity_full"||code==="unavailable"&&provablyNotSent(error))return "offline"
    if(code==="pa_outcome_unknown"||code==="execution_effect_unknown"||code==="unavailable")return "check"
    if(code==="pa_stale_generation"||code==="pa_stale_version"||code==="pa_context_stale"||code==="pa_idempotency_conflict")return "changed"
    if(code==="pa_revoked"||code==="pa_unpaired"||code==="pa_disabled"||code==="pa_read_only"||code==="pa_scope_denied"||code==="pa_not_found"||code.startsWith("auth_")||code==="cli_not_installed"||code==="adapter_not_found")return "refused"
    return "notSent"
  })()
  return {kind,code,retry:kind==="offline"||kind==="check"||kind==="changed"||kind==="notSent"}
}
/** Delays before the card asks again about a reply that may have been sent. */
export const DECISION_CHECK_DELAYS_MS=[1500,4000,10000] as const

export function decisionFailureText(f:DecisionFailure,w:Words,checking:boolean,draftBytes=0){
  switch(f.kind){
    case "interrupted":return w.decisionFailInterrupted
    case "tooLong":return w.decisionFailTooLong.replace("{n}",String(draftBytes)).replace("{limit}",String(DECISION_ANSWER_BYTES))
    case "answered":return w.decisionHandled
    case "expired":return w.decisionExpired
    case "withdrawn":return w.decisionWithdrawn
    case "format":return w.decisionFailFormat
    case "offline":return w.decisionFailOffline
    case "check":return checking?w.decisionFailChecking:w.decisionFailUnconfirmed
    case "changed":return w.decisionFailChanged
    case "refused":return w.decisionFailRefused.replace("{code}",f.code)
    default:return `${w.decisionSendFailed} (${f.code})`
  }
}

function useDecisionDetail(summary:W.ProjectAssistantReadPermissionSummary,choice:DecisionChoice|null,offer?:W.ProjectAssistantPermissionOffer,onDetail?:()=>Promise<boolean>){
  const requested=useRef("")
  const [detailIssue,setDetailIssue]=useState("")
  const [retryCount,setRetryCount]=useState(0)
  const retryDetail=()=>{requested.current="";setRetryCount(n=>n+1)}
  // The immutable proposal stays visible while the collection and detail
  // catch up to one another after an answer.
  const [knownMemory,setKnownMemory]=useState("")
  const proposal=offer?.decision?.memory,memory=proposal||knownMemory
  useEffect(()=>{if(proposal)setKnownMemory(proposal)},[proposal])
  useEffect(()=>{
    const isMemory=isMemoryProposal(summary,offer)
    const needsAnswer=isMemory&&summary.state==="answered"&&offer?.state!=="answered"
    if(offer&&!needsAnswer){
      // The answer event can invalidate this detail before the collection
      // updates. The same cursor reads the current offer again.
      if(isMemory)requested.current=""
      return
    }
    const reference=isMemory?JSON.stringify([summary.detail_cursor,summary.state]):summary.detail_cursor
    if(!onDetail||requested.current===reference)return
    requested.current=reference
    setDetailIssue("")
    void onDetail().then(ok=>{if(!ok&&requested.current===reference)setDetailIssue("pa_outcome_unknown")},error=>{
      if(requested.current===reference)setDetailIssue(errorCode(error))
    })
  },[offer,onDetail,summary,retryCount])
  const [confirmedAnswer,setConfirmedAnswer]=useState<MemoryAnswer>()
  const observedAnswer=memory&&offer?.state==="answered"?memoryOption(offer.selected_option_id):undefined
  // A permission event can evict the answered detail before its replacement arrives.
  if(observedAnswer&&!confirmedAnswer)setConfirmedAnswer(observedAnswer)
  const selected=memory?(confirmedAnswer??observedAnswer??(choice?.phase==="sent"?memoryOption(choice.option):undefined)):undefined
  return {memory,selected,detailIssue,retryDetail}
}

// A question a Bot asked with ask_person. The suggested replies are the
// Bot's words; pressing one sends that full text as the person's own message
// in this chat, so the card shows exactly what will be sent.
export function DecisionCard({summary,offer:loaded,enabled,onDetail,onAnswer,askerName,requesterName,followUp,highlight,botID="",w}:{
  summary:W.ProjectAssistantReadPermissionSummary
  offer?:W.ProjectAssistantPermissionOffer
  enabled:boolean
  onDetail?:()=>Promise<boolean>
  onAnswer:(offer:W.ProjectAssistantPermissionOffer,option:W.ProjectAssistantPermissionOption|null,text?:string,attempt?:string)=>Promise<void>
  askerName:string
  requesterName?:string
  /** The Bot's progress on the turn this page's reply started; absent when unobserved. */
  followUp?:DecisionFollowUp
  /** The card a push notification opened the chat at. */
  highlight?:boolean
  /** Names the Bot in this card's send-path trace. */
  botID?:string
  w:Words
}) {
  const [busy,setBusy]=useState(false),[error,setError]=useState<DecisionFailure|null>(null),[own,setOwn]=useState(false),[draft,setDraft]=useState("")
  // How many automatic checks a possibly-sent reply has had; null when none is due.
  const [checks,setChecks]=useState<number|null>(null)
  // The reply the person pressed, kept from the press until the server's
  // state replaces it: "sending" before the answer is accepted, "sent" once
  // it is, "failed" when it was not. A reload shows the server's answer.
  const [choice,setChoice]=useState<DecisionChoice|null>(null)
  const pending=useRef(false)
  // The press being traced, so a card unmounted mid-send says so in the log.
  const traced=useRef<{attempt:string;at:number;option:string}|null>(null)
  useEffect(()=>()=>{const t=traced.current;if(pending.current&&t)traceDecision({attempt:t.attempt,phase:"unmounted",bot_id:botID,offer_id:summary.id,option:t.option,ms:performance.now()-t.at})},[])
  const offer=loaded&&samePermission(summary,loaded)?loaded:undefined
  const {memory,selected,detailIssue,retryDetail}=useDecisionDetail(summary,choice,offer,onDetail)
  // A pending summary can lag the latest detail or the committed answer receipt.
  const state=selected?"answered":permissionState(summary.state==="pending"&&offer?offer:summary)
  const decision=offer?.decision
  const send=async(option:W.ProjectAssistantPermissionOption|null,text?:string,check=-1)=>{
    if(!offer||!enabled||pending.current||state!=="pending")return
    if(!option&&text!==undefined&&utf8Bytes(text)>DECISION_ANSWER_BYTES){setError({kind:"tooLong",code:"pa_capacity_full",retry:false});setOwn(true);return}
    pending.current=true;setBusy(true)
    if(check<0||!traced.current)traced.current={attempt:newDecisionAttempt(),at:performance.now(),option:option?.id??""}
    const trace=traced.current
    traceDecision({attempt:trace.attempt,phase:check<0?"press":"check",bot_id:botID,offer_id:offer.id,option:trace.option,ms:performance.now()-trace.at})
    const slow=setTimeout(()=>{if(pending.current&&traced.current===trace)traceDecision({attempt:trace.attempt,phase:"timeout",bot_id:botID,offer_id:offer.id,option:trace.option,ms:performance.now()-trace.at})},DECISION_TRACE_SLOW_MS)
    if(check<0){setError(null);setChecks(null)}
    setChoice({option:option?.id??null,text:option?.label??text??"",phase:"sending"})
    // The roster and session streams share the browser's HTTP/1.1 pool.
    // Free this tab's session stream slot for the answer and its preflight reads.
    const restoreFleet=memory?quietFleetStream():undefined
    try{await onAnswer(offer,option,text,trace.attempt);setError(null);setChecks(null);setChoice(c=>c&&{...c,phase:"sent"});setOwn(false);setDraft("")}
    catch(e){
      const f=decisionFailure(e)
      // A check that finds the card answered means this reply (or another
      // device's) arrived; the refreshed card shows which.
      setError(f);setChoice(c=>c&&{...c,phase:"failed"});if(!option)setOwn(true)
      setChecks(f.kind==="check"?check+1:null)
    }
    finally{restoreFleet?.();clearTimeout(slow);pending.current=false;setBusy(false)}
  }
  const checking=error?.kind==="check"&&checks!==null&&checks<DECISION_CHECK_DELAYS_MS.length
  useEffect(()=>{
    if(!checking||!choice||state!=="pending"||checks===null)return
    const option=choice.option===null?null:offer?.options.find(x=>x.id===choice.option)??null
    const timer=setTimeout(()=>void send(option,option?undefined:choice.text,checks),DECISION_CHECK_DELAYS_MS[checks])
    return()=>clearTimeout(timer)
  })
  const draftBytes=own?utf8Bytes(draft.trim()):0,tooLong=draftBytes>DECISION_ANSWER_BYTES
  // The roster can focus the summary before the detail has arrived.
  if(isMemoryProposal(summary,offer))return <MemoryCard summary={summary} offer={offer} memory={memory}
    state={state} selected={selected} choice={choice} enabled={enabled} busy={busy} checking={checking} error={error}
    detailIssue={detailIssue} retryDetail={retryDetail}
    askerName={askerName} requesterName={requesterName} onSend={option=>void send(option)} highlight={highlight} w={w}/>
  const answered=decision?.answer_text||offer?.options.find(o=>o.id===offer.selected_option_id)?.label
  const sent=state==="pending"&&choice?.phase==="sent"
  const locked=busy||sent
  const status=state==="answered"?(answered?w.decisionAnswered.replace("{text}",answered):""):state==="expired"?w.decisionExpired:state==="pending"?"":state==="unknown"?w.unknown:decision?.withdrawn_reason?w.decisionWithdrawnReason.replace("{reason}",decision.withdrawn_reason):w.decisionWithdrawn
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
        <textarea value={draft} autoFocus rows={2} disabled={!enabled||locked} aria-invalid={tooLong||undefined} aria-label={w.decisionOwnPlaceholder.replace("{name}",askerName)} placeholder={w.decisionOwnPlaceholder.replace("{name}",askerName)} onChange={e=>{setDraft(e.target.value);if(error?.kind==="tooLong")setError(null)}}/>
        {tooLong&&<small className="pa-decision-too-long" role="status">{w.decisionFailTooLong.replace("{n}",String(draftBytes)).replace("{limit}",String(DECISION_ANSWER_BYTES))}</small>}
        <button type="submit" className="pa-pill" data-choice={optionState(null)} disabled={!enabled||locked||!draft.trim()||tooLong}>
          {choice?.option===null&&busy&&<span className="pa-decision-spinner" aria-hidden="true"/>}
          {choice?.option===null&&busy?w.decisionSending:w.decisionSend}
        </button>
      </form>}
    </>}
    {error&&state==="pending"&&!(error.kind==="tooLong"&&tooLong)&&<div className="pa-decision-failed" role="alert" data-failure={error.kind} aria-busy={checking||undefined}>
      <p>{checking&&<span className="pa-decision-spinner" aria-hidden="true"/>}{decisionFailureText(error,w,checking,draftBytes)}</p>
      {error.retry&&!checking&&choice&&choice.option!==null&&<button type="button" className="pa-pill" disabled={!enabled||busy} onClick={()=>{const o=offer?.options.find(x=>x.id===choice.option);if(o)void send(o)}}>{w.decisionRetry}</button>}
    </div>}
  </section>
}

// A short fact the Bot proposed to remember while answering another Bot.
// Only Save writes it, as the person's own memory entry; the card offers no
// typed reply.
function MemoryCard({summary,offer,memory,state,selected,choice,enabled,busy,checking,error,askerName,requesterName,onSend,detailIssue,retryDetail,highlight,w}:{
  summary:W.ProjectAssistantReadPermissionSummary
  offer?:W.ProjectAssistantPermissionOffer
  memory:string
  state:W.ProjectAssistantPermissionState
  selected:MemoryAnswer|undefined
  choice:DecisionChoice|null
  enabled:boolean
  busy:boolean
  checking:boolean
  error:DecisionFailure|null
  askerName:string
  requesterName?:string
  onSend:(option:W.ProjectAssistantPermissionOption)=>void
  detailIssue:string
  retryDetail:()=>void
  highlight?:boolean
  w:Words
}) {
  const awaiting=state==="answered"&&!selected||state==="pending"&&error?.kind==="answered"
  const problem=state==="pending"&&!awaiting?error:null
  const retry=problem?.retry&&!checking?offer?.options.find(o=>o.id===choice?.option):undefined
  const locked=awaiting||state==="pending"&&(busy||checking)
  const label=(id:string)=>id===MEMORY_SAVE?w.memoryCardSave.replace("{name}",askerName):w.memoryCardSkip
  const status=state==="answered"?memoryReceiptText(selected,askerName,w):
    state==="expired"?w.decisionExpired:state==="pending"?"":state==="unknown"?w.unknown:w.decisionWithdrawn
  const sending=locked&&!status&&!checking
  const message=problem&&(problem.kind==="notSent"?w.decisionSendFailed:problem.kind==="refused"?w.retryReasonRefused:decisionFailureText(problem,w,checking))
  return <section className="pa-card pa-permission pa-decision pa-memory-proposal" id={`pa-decision-${summary.id}`} data-permission-id={summary.id} data-state={state}
    aria-label={w.memoryCardTitle} aria-busy={locked||state==="pending"&&!offer&&!detailIssue||undefined} data-focused={highlight||undefined} tabIndex={highlight?-1:undefined}>
    {memory&&<blockquote className="pa-decision-question">{memory}</blockquote>}
    {state==="pending"&&!offer&&<p className="pa-decision-note" role="status">{detailIssue?<>{w.unavailable} ({detailIssue}) <button type="button" className="pa-pill" onClick={retryDetail}>{w.retry}</button></>:w.memoryLoading}</p>}
    <p className="pa-decision-note">{requesterName?w.memoryCardFromBot.replace("{name}",requesterName):w.memoryCardFrom}</p>
    {offer?.decision?.request&&<details className="pa-decision-context">
      <summary>{w.memoryCardContext}</summary>
      <p>{offer.decision.request}</p>
    </details>}
    {(status||sending)&&<p className="pa-permission-state" role="status">
      {sending?<span className="pa-decision-working"><span className="pa-decision-spinner" aria-hidden="true"/>{w.decisionSending}</span>:status}
    </p>}
    {state==="pending"&&offer&&<div className="pa-permission-options">
      {offer.options.filter(o=>o.id===MEMORY_SAVE||o.id===MEMORY_SKIP).map(option=><button key={option.id} type="button" data-option-kind="decision" disabled={!enabled||locked} onClick={()=>onSend(option)}><span>{label(option.id)}</span></button>)}
    </div>}
    {message&&<div className="pa-decision-failed" role="alert" aria-busy={checking||undefined}>
      <p>{checking&&<span className="pa-decision-spinner" aria-hidden="true"/>}{message}</p>
      {retry&&<button type="button" className="pa-pill" disabled={!enabled||locked} onClick={()=>onSend(retry)}>{w.decisionRetry}</button>}
    </div>}
  </section>
}

type MemoryAnswer=typeof MEMORY_SAVE|typeof MEMORY_SKIP
function memoryOption(option?:string|null):MemoryAnswer|undefined{
  return option===MEMORY_SAVE||option===MEMORY_SKIP?option:undefined
}

export function isMemoryProposal(summary:W.ProjectAssistantReadPermissionSummary,offer?:W.ProjectAssistantPermissionOffer) {
  if(offer?.decision)return !!offer.decision.memory
  if(summary.decision_kind)return summary.decision_kind==="memory_save"
  return summary.tool_kind==="decision"&&legacyMemoryProposalTitle(summary.title)
}

function memoryReceiptText(option:MemoryAnswer|undefined,name:string,w:Words) {
  return option===MEMORY_SAVE?w.memoryCardSaved.replace("{name}",name):option===MEMORY_SKIP?w.memoryCardSkipped:""
}

export function MemoryProposalReceipt({summary,offer,onDetail,name,w}:{
  summary:W.ProjectAssistantReadPermissionSummary
  offer?:W.ProjectAssistantPermissionOffer
  onDetail?:()=>Promise<boolean>
  name:string
  w:Words
}) {
  const observed=memoryOption(summary.selected_option_id)
  const {selected}=useDecisionDetail(summary,null,offer,observed?undefined:onDetail)
  return (selected??observed)===MEMORY_SAVE?<p className="pa-memory-proposal-receipt">{w.memoryCardSaved.replace("{name}",name)}</p>:null
}

/** Only the exchange started in the person's current turn gets a chat link. */
export function memorySuggestionInChat(exchange:W.ProjectAssistantExchange,latestPersonTurn:string|undefined,exchangeIDs:ReadonlySet<string>) {
  return !!latestPersonTurn&&exchange.source_turn_id===latestPersonTurn&&exchangeIDs.has(exchange.id)
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
    return id&&!isMemoryProposal(card.summary,card.offer)&&capsules.has(id)&&!inline.has(card.summary.id)?[[id,card] as const]:[]
  }))
}
