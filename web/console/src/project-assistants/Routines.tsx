import { useEffect, useRef, useState } from "react"
import type * as W from "@clawdline/contract"
import type { SourceClient } from "./client"
import { command, errorCode, failure, provablyNotSent } from "./client"
import type { Words } from "./words"

import {readRoutineIntent,type RoutineIntent as Intent} from "./routine-intent"
import {cleanRoutineTrigger,isHookTrigger,newRoutineTrigger as blankTrigger,routineEventLimits,routineEventSources,validRoutineTrigger} from "./routine-events"
import {performRoutineHook,routineScopeIdentity} from "./routine-hook-intent"
import {RoutineHook} from "./RoutineHook"
import {routineWord as tr} from "./routine-words"
type Props={source:SourceClient;bot:W.ProjectAssistantBot;enabled:boolean;w:Words;onClose:()=>void;storageKey:string;embedded?:boolean}

export function Routines(props:Props) {
  const capture=useRef({source:props.source,epoch:0})
  if(capture.current.source!==props.source)capture.current={source:props.source,epoch:capture.current.epoch+1}
  return <RoutinePanel key={routineScopeIdentity(props.source,props.bot)+":"+capture.current.epoch} {...props}/>
}

function RoutineTriggerSummary({trigger:t,w}:{trigger:W.ProjectAssistantRoutineTrigger;w:Words}) {
  if(t.type==="event")return <><p>{tr("event")} · {tr("source")}: {t.source} · {tr("eventName")}: {t.event}</p>{!!t.filters?.length&&<div><p>{tr("filters")}</p><dl>{t.filters.map((row,n)=><div key={n}><dt>{row.key}</dt><dd>{row.value}</dd></div>)}</dl></div>}</>
  if(t.type==="webhook")return <p>{tr("webhook")}</p>
  if(t.type==="interval")return <p>{w.routineSeconds}: {t.seconds}</p>
  if(t.type==="once")return <p>{w.routineOnce}: {new Date(t.at).toLocaleString()}</p>
  if(t.type==="cron")return <p>{w.routineCron}: {t.expression} · {t.timeZone}</p>
  return <p>{t.type}</p>
}

function RoutinePanel({source,bot,enabled,w,onClose,storageKey,embedded=false}:Props) {
  const [page,setPage]=useState<W.ProjectAssistantRoutineListResponse|null>(null),[editing,setEditing]=useState<W.ProjectAssistantRoutineDefinition|null|undefined>(undefined)
  const [name,setName]=useState(""),[instruction,setInstruction]=useState(""),[triggers,setTriggers]=useState<W.ProjectAssistantRoutineTrigger[]>([blankTrigger()]),[timeout,setTimeout]=useState(3600),[active,setActive]=useState(true)
  const [busy,setBusy]=useState(false),[error,setError]=useState(""),[history,setHistory]=useState<{id:string;page:W.ProjectAssistantRoutineListResponse}|null>(null),[confirm,setConfirm]=useState<string|null>(null),[held,setHeld]=useState<Intent|null>(null),[journalBlocked,setJournalBlocked]=useState(false)
  const [hookID,setHookID]=useState<string|null>(null)
  const live=useRef(enabled);live.current=enabled
  const panel=useRef<HTMLElement>(null),pending=useRef(false),control=useRef(new AbortController()),mounted=useRef(true)
  const durableKey="clawdline:routine-command:"+storageKey
  const supported=!!source.canRoutines?.()&&!!source.routines&&!!source.saveRoutine&&!!source.enableRoutine&&!!source.deleteRoutine&&!!source.testRoutine
  const events=!!source.canRoutineEvents?.()&&!!source.routineHook
  const authority=enabled&&source.current?.()!==false&&!!page?.source.writable&&page.source.fresh&&page.source.access==="interactive"
  const writable=authority&&!held&&!journalBlocked
  const query=(id="",cursor=""):W.ProjectAssistantRoutineListRequest=>({key:bot.key,project:bot.project,conversation_id:bot.conversation_id,routine_id:id,cursor,page_size:100})
  const load=async(id="",cursor="")=>{
    if(!supported)throw failure("pa_unsupported")
    const answer=await source.routines!(query(id,cursor),control.current.signal)
    if(!mounted.current)return
    if(id)setHistory(old=>({id,page:{...answer,runs:cursor&&old?.id===id?[...old.page.runs,...answer.runs]:answer.runs}}))
    else setPage(old=>({...answer,definitions:cursor&&old?[...old.definitions,...answer.definitions]:answer.definitions}))
  }
  useEffect(()=>{
    mounted.current=true
    panel.current?.querySelector<HTMLElement>("h3")?.focus()
    const stored=readRoutineIntent({getItem:key=>sessionStorage.getItem(key)},durableKey,bot)
    if(stored.state==="held"){setHeld(stored.intent);if(stored.intent.action==="hook")setHookID(stored.intent.request.id)}
    if(stored.state==="blocked"){setJournalBlocked(true);setError("pa_outcome_unknown")}
    if(supported)void load().catch(e=>{if(mounted.current)setError(errorCode(e))})
    return ()=>{mounted.current=false;control.current.abort()}
  },[])
  const edit=(d:W.ProjectAssistantRoutineDefinition|null)=>{setEditing(d);setName(d?.name??"");setInstruction(d?.instruction??"");setTriggers(d?.triggers.map(cleanRoutineTrigger)??[blankTrigger()]);setTimeout(d?.timeout_seconds??3600);setActive(d?.enabled??true);setError("")}
  const run=async(intent:Intent,replay=false):Promise<W.ProjectAssistantRoutineHookResponse|null>=>{
    if(pending.current||!live.current||!authority||!supported||journalBlocked||(!replay&&held)||source.current?.()===false)return null
    if(intent.action==="hook"&&!events)return null
    pending.current=true;setBusy(true);setError("")
    // Persist the exact command before crossing the transport. A reload or
    // uncertain response can only expose an explicit replay of these bytes.
    try {
      if(!replay) {
        const current=await source.routines!({...query(),page_size:1},control.current.signal)
        intent=intent.action==="hook"?{action:"hook",request:{...intent.request,fence:current.fence}}:{...intent,request:{...intent.request,fence:current.fence}}
      }
      if(!mounted.current||!live.current||control.current.signal.aborted||source.current?.()===false)return null
      try {sessionStorage.setItem(durableKey,JSON.stringify(intent))} catch(error) {setJournalBlocked(true);throw error}
      setHeld(intent)
      const signal=control.current.signal
      if(intent.action==="save")await source.saveRoutine!(intent.request as W.ProjectAssistantRoutineSaveRequest,signal)
      if(intent.action==="enable")await source.enableRoutine!(intent.request as W.ProjectAssistantRoutineEnabledRequest,signal)
      if(intent.action==="delete")await source.deleteRoutine!(intent.request,signal)
      if(intent.action==="test")await source.testRoutine!(intent.request,signal)
      let hook:W.ProjectAssistantRoutineHookResponse|null=null
      if(intent.action==="hook") {
        hook=await performRoutineHook(intent.request,{
          current:()=>mounted.current&&live.current&&!signal.aborted&&source.current?.()!==false,
          persist:value=>sessionStorage.setItem(durableKey,JSON.stringify(value)),
          send:request=>source.routineHook!(request,signal),
        })
        if(!hook)throw failure("pa_outcome_unknown")
      }
      sessionStorage.removeItem(durableKey)
      if(!mounted.current||!live.current||source.current?.()===false)return null
      setHeld(null);setEditing(undefined);setConfirm(null)
      if(intent.action==="hook")return hook
      await load();if(intent.action==="test")await load(intent.request.id)
    } catch(e) {
      if(!mounted.current)return null
      const code=errorCode(e),unknown=!provablyNotSent(e)&&["pa_outcome_unknown","execution_effect_unknown","pa_revoked","unavailable"].includes(code)
      if(!unknown) { try {sessionStorage.removeItem(durableKey);setHeld(null)} catch { /* Keep the durable command held. */ } }
      setError(code)
    } finally {pending.current=false;if(mounted.current)setBusy(false)}
    return null
  }
  const action=(d:W.ProjectAssistantRoutineDefinition,kind:"enable"|"delete"|"test")=>{
    if(!page)return
    const request={command:command(bot.key,"edit"),fence:page.fence,id:d.id,expected_revision:d.version,...(kind==="enable"?{enabled:!d.enabled}:{})}
    void run({action:kind,request})
  }
  const refresh=async()=>{setBusy(true);try{await load();if(history)await load(history.id);setError("")}catch(e){setError(errorCode(e))}finally{setBusy(false)}}
  return <section ref={panel} className={embedded?"pa-routines pa-routines-embedded":"pa-card pa-routines"} aria-label={w.routines}>
    {!embedded&&<header className="pa-toolbar"><h3 tabIndex={-1}>{w.routines}</h3><button type="button" onClick={onClose}>{w.close}</button></header>}
    <p className="pa-routines-hint">{w.routineHint}</p>
    {!supported?<p role="status">{w.routinesUnsupported}</p>:<>
      <div className="pa-toolbar pa-routines-actions"><button type="button" disabled={!writable||busy} onClick={()=>edit(null)}>{w.routineNew}</button><button type="button" disabled={busy} onClick={()=>void refresh()}>{w.routineRefresh}</button></div>
      {!page&&!error&&<p role="status">{w.loading}</p>}
      {page&&page.definitions.length===0&&<p>{w.routinesEmpty}</p>}
      {page?.definitions.filter(d=>!d.follow_up).map(d=><article className="pa-routine" key={d.id}>
        <h4>{d.name}</h4><p>{d.enabled?w.routineEnabled:w.routinePaused} · {d.next_due?`${w.routineNext}: ${new Date(d.next_due).toLocaleString()}`:d.triggers.some(isHookTrigger)?tr("waitingEvent"):w.routineSpent}</p>
        <p>{d.instruction}</p>{d.triggers.map((trigger,n)=><RoutineTriggerSummary key={n} trigger={trigger} w={w}/>)}
        <div className="pa-toolbar">{d.triggers.some(isHookTrigger)&&<button type="button" disabled={!authority||busy||!events||!!held||journalBlocked} onClick={()=>setHookID(d.id)}>{tr("manageHook")}</button>}<button type="button" disabled={!writable||busy||(d.triggers.some(isHookTrigger)&&!events)} onClick={()=>edit(d)}>{w.routineEdit}</button><button type="button" disabled={!writable||busy} onClick={()=>action(d,"enable")}>{d.enabled?w.routinePause:w.routineEnable}</button><button type="button" disabled={!writable||busy} onClick={()=>action(d,"test")}>{w.routineTest}</button><button type="button" disabled={busy} onClick={()=>void load(d.id).catch(e=>setError(errorCode(e)))}>{w.routineHistory}</button><button type="button" disabled={!writable||busy} onClick={()=>setConfirm(d.id)}>{w.routineDelete}</button></div>
        {confirm===d.id&&<div role="alert"><p>{w.routineDeleteConfirm}</p><button type="button" disabled={!writable||busy} onClick={()=>action(d,"delete")}>{w.routineConfirmDelete}</button><button type="button" onClick={()=>setConfirm(null)}>{w.cancel}</button></div>}
      </article>)}
      {!events&&<p role="status">{tr("eventUnsupported")}</p>}
      {page?.definitions.some(d=>d.follow_up&&d.enabled&&!!d.next_due)&&<section aria-label={tr("followUps")}><h4>{tr("followUps")}</h4>{page.definitions.filter(d=>d.follow_up&&d.enabled&&!!d.next_due).map(d=><article key={d.id}><h5>{d.name}</h5><p>{d.instruction}</p><p>{d.next_due?new Date(d.next_due).toLocaleString():w.routineSpent} · {tr("followUpChain")}: {d.follow_up_chain??0}</p><button type="button" disabled={!writable||busy} aria-label={`${w.cancel}: ${d.name}`} onClick={()=>action(d,"delete")}>{w.cancel}</button><button type="button" disabled={busy} onClick={()=>void load(d.id).catch(e=>setError(errorCode(e)))}>{w.routineHistory}</button></article>)}</section>}
      {hookID&&events&&<RoutineHook key={hookID} id={hookID} allowed={!!authority} busy={busy} held={held} blocked={journalBlocked} w={w} onClose={()=>setHookID(null)} onRun={async(rotate,replay)=>{
        if(replay)return held?.action==="hook"&&held.request.id===hookID?run(held,true):null
        const d=page?.definitions.find(d=>d.id===hookID)
        if(!d||!page||!writable)return null
        return run({action:"hook",request:{command:command(bot.key,"edit"),fence:page.fence,id:d.id,expected_revision:d.version,rotate}})
      }}/>}
      {page?.next_cursor&&<button type="button" disabled={busy} onClick={()=>void load("",page.next_cursor).catch(e=>setError(errorCode(e)))}>{w.more}</button>}
      {editing!==undefined&&<form onSubmit={event=>{event.preventDefault();if(!triggers.every(t=>validRoutineTrigger(t,events))){setError("routine_event_fields");return}if(page)void run({action:"save",request:{command:command(bot.key,"edit"),fence:page.fence,id:editing?.id??"",expected_revision:editing?.version??0,name,instruction,triggers:triggers.map(cleanRoutineTrigger),timeout_seconds:timeout,enabled:active}})}}>
        <fieldset disabled={!writable||busy}>
          <legend>{editing?w.routineEdit:w.routineNew}</legend>
          <label>{w.name}<input value={name} onChange={e=>setName(e.target.value)} required/></label>
          <label>{w.routineInstruction}<textarea value={instruction} onChange={e=>setInstruction(e.target.value)} required/></label>
          <p>{w.routineOr}</p>
          {triggers.map((t,i)=>{const change=(value:Partial<W.ProjectAssistantRoutineTrigger>)=>setTriggers(old=>old.map((v,n)=>n===i?cleanRoutineTrigger({...v,...value}):v));return <fieldset className="pa-routine-trigger" key={i}>
            <legend>{w.routineTrigger} {i+1}</legend><label>{w.routineType}<select value={t.type} onChange={e=>change(blankTrigger(e.target.value))}><option value="interval">{w.routineInterval}</option><option value="once">{w.routineOnce}</option><option value="cron">{w.routineCron}</option>{events&&<><option value="event">{tr("event")}</option><option value="webhook">{tr("webhook")}</option></>}</select></label>
            {t.type==="interval"&&<label>{w.routineSeconds}<input type="number" min={1} max={31536000} value={t.seconds} onChange={e=>change({seconds:Number(e.target.value)})} required/></label>}
            {t.type==="once"&&<label>{w.routineAt}<input type="datetime-local" step="0.001" value={t.at?new Date(t.at-new Date(t.at).getTimezoneOffset()*60000).toISOString().slice(0,-1):""} onChange={e=>change({at:e.target.value?new Date(e.target.value).getTime():0})} required/></label>}
            {t.type==="cron"&&<><label>{w.routineExpression}<input value={t.expression} placeholder="0 9 * * 1-5" onChange={e=>change({expression:e.target.value})} required/></label><label>{w.routineZone}<input value={t.timeZone} placeholder="Asia/Taipei" onChange={e=>change({timeZone:e.target.value})} required/></label></>}
            {t.type==="event"&&<>
              <p>{tr("eventHint")}</p><label>{tr("source")}<select value={t.source} onChange={e=>change({source:e.target.value})}>{routineEventSources.map(source=><option key={source} value={source}>{source}</option>)}</select></label>
              <label>{tr("eventName")}<input required value={t.event??""} onChange={e=>change({event:e.target.value})}/></label>
              <fieldset><legend>{tr("filters")}</legend>{t.filters?.map((row,n)=><div key={n}><label>{tr("filterKey")}<input required value={row.key} onChange={e=>change({filters:t.filters!.map((r,k)=>k===n?{key:e.target.value,value:r.value}:r)})}/></label><label>{tr("filterValue")}<input value={row.value} onChange={e=>change({filters:t.filters!.map((r,k)=>k===n?{key:r.key,value:e.target.value}:r)})}/></label><button type="button" onClick={()=>change({filters:t.filters!.filter((_,k)=>k!==n)})}>{tr("removeFilter")}</button></div>)}<button type="button" disabled={(t.filters?.length??0)>=routineEventLimits.filters} onClick={()=>change({filters:[...(t.filters??[]),{key:"",value:""}]})}>{tr("addFilter")}</button></fieldset>
            </>}
            <button type="button" disabled={triggers.length===1} onClick={()=>setTriggers(old=>old.filter((_,n)=>n!==i))}>{w.routineRemoveTrigger}</button>
          </fieldset>})}
          <button type="button" disabled={triggers.length>=20} onClick={()=>setTriggers(old=>[...old,blankTrigger()])}>{w.routineAddTrigger}</button>
          <label>{w.routineTimeout}<input type="number" min={1} max={86400} value={timeout} onChange={e=>setTimeout(Number(e.target.value))} required/></label>
          <label><input type="checkbox" checked={active} onChange={e=>setActive(e.target.checked)}/>{w.routineEnabled}</label>
          <button type="submit">{w.save}</button><button type="button" onClick={()=>setEditing(undefined)}>{w.cancel}</button>
        </fieldset>
      </form>}
      {journalBlocked&&<p role="alert">{w.routineJournalUnreadable}</p>}
      {held?.action==="hook"&&!hookID&&<button type="button" onClick={()=>setHookID(held.request.id)}>{tr("retryHook")}</button>}
      {held&&held.action!=="hook"&&!journalBlocked&&<div role="alert"><p>{w.routineHeld}</p><button type="button" disabled={busy||!authority||!supported} onClick={()=>void run(held,true)}>{w.routineReplay}</button></div>}
      {history&&<section aria-label={w.routineHistory}><h4>{w.routineHistory}</h4>{history.page.runs.length===0?<p>{w.routineNoRuns}</p>:history.page.runs.map(run=><article className="pa-routine-run" key={run.id}><p>{w[run.state as keyof Words]??run.state} · {new Date(run.created_at).toLocaleString()} · {run.source==="test"?tr("testRun"):run.source==="schedule"?tr("scheduledRun"):run.source==="webhook"?tr("eventRun"):run.source==="follow_up"?tr("followUp"):`${tr("otherRun")}: ${run.source}`}</p>{run.delivery_id&&<p>{tr("delivery")}: {run.delivery_id}</p>}{run.retry_count!==undefined&&<p>{tr("retryCount")}: {run.retry_count}</p>}{run.not_before&&<p>{tr("retryAt")}: {new Date(run.not_before).toLocaleString()}</p>}{run.state==="unknown"&&<p role="alert">{tr("unknownRun")}</p>}{run.result&&<p className="pa-routine-result">{run.result}</p>}{run.failure&&<p role="alert">{run.failure.code} ({run.failure.field})</p>}</article>)}{history.page.next_cursor&&<button type="button" disabled={busy} onClick={()=>void load(history.id,history.page.next_cursor).catch(e=>setError(errorCode(e)))}>{w.more}</button>}</section>}
    </>}
    {error&&!journalBlocked&&<p role="alert">{error==="routine_event_fields"?tr("invalidEvent"):error==="resume_unsupported"?w.routineResumeUnsupported:error==="pa_outcome_unknown"?w.unknown:w.unavailable} ({error})</p>}
    {busy&&<p role="status">{w.routineSaving}</p>}
  </section>
}
