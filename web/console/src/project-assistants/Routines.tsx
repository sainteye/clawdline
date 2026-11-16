import { useEffect, useRef, useState } from "react"
import type * as W from "@clawdline/contract"
import type { SourceClient } from "./client"
import { command, errorCode, failure, provablyNotSent } from "./client"
import type { Words } from "./words"

import {readRoutineIntent,type RoutineIntent as Intent} from "./routine-intent"
const blankTrigger=():W.ProjectAssistantRoutineTrigger=>({type:"interval",seconds:3600,at:0,expression:"",timeZone:""})

export function Routines({source,bot,enabled,w,onClose,storageKey}:{source:SourceClient;bot:W.ProjectAssistantBot;enabled:boolean;w:Words;onClose:()=>void;storageKey:string}) {
  const [page,setPage]=useState<W.ProjectAssistantRoutineListResponse|null>(null),[editing,setEditing]=useState<W.ProjectAssistantRoutineDefinition|null|undefined>(undefined)
  const [name,setName]=useState(""),[instruction,setInstruction]=useState(""),[triggers,setTriggers]=useState<W.ProjectAssistantRoutineTrigger[]>([blankTrigger()]),[timeout,setTimeout]=useState(3600),[active,setActive]=useState(true)
  const [busy,setBusy]=useState(false),[error,setError]=useState(""),[history,setHistory]=useState<{id:string;page:W.ProjectAssistantRoutineListResponse}|null>(null),[confirm,setConfirm]=useState<string|null>(null),[held,setHeld]=useState<Intent|null>(null),[journalBlocked,setJournalBlocked]=useState(false)
  const panel=useRef<HTMLElement>(null),pending=useRef(false),control=useRef(new AbortController()),mounted=useRef(true)
  const durableKey="clawdline:routine-command:"+storageKey
  const supported=!!source.canRoutines?.()&&!!source.routines&&!!source.saveRoutine&&!!source.enableRoutine&&!!source.deleteRoutine&&!!source.testRoutine
  const writable=enabled&&page?.source.writable&&page.source.access==="interactive"&&!held&&!journalBlocked
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
    if(stored.state==="held")setHeld(stored.intent)
    if(stored.state==="blocked"){setJournalBlocked(true);setError("pa_outcome_unknown")}
    if(supported)void load().catch(e=>{if(mounted.current)setError(errorCode(e))})
    return ()=>{mounted.current=false;control.current.abort()}
  },[])
  const edit=(d:W.ProjectAssistantRoutineDefinition|null)=>{setEditing(d);setName(d?.name??"");setInstruction(d?.instruction??"");setTriggers(d?.triggers.map(t=>({...t}))??[blankTrigger()]);setTimeout(d?.timeout_seconds??3600);setActive(d?.enabled??true);setError("")}
  const run=async(intent:Intent,replay=false)=>{
    if(pending.current||!enabled||!supported||journalBlocked||(!replay&&held))return
    pending.current=true;setBusy(true);setError("")
    // Persist the exact command before crossing the transport. A reload or
    // uncertain response can only expose an explicit replay of these bytes.
    try {
      if(!replay) {
        const current=await source.routines!({...query(),page_size:1},control.current.signal)
        intent={...intent,request:{...intent.request,fence:current.fence}}
      }
      if(!mounted.current||control.current.signal.aborted)return
      try {sessionStorage.setItem(durableKey,JSON.stringify(intent))} catch(error) {setJournalBlocked(true);throw error}
      setHeld(intent)
      const signal=control.current.signal
      if(intent.action==="save")await source.saveRoutine!(intent.request as W.ProjectAssistantRoutineSaveRequest,signal)
      if(intent.action==="enable")await source.enableRoutine!(intent.request as W.ProjectAssistantRoutineEnabledRequest,signal)
      if(intent.action==="delete")await source.deleteRoutine!(intent.request,signal)
      if(intent.action==="test")await source.testRoutine!(intent.request,signal)
      sessionStorage.removeItem(durableKey)
      if(!mounted.current)return
      setHeld(null);setEditing(undefined);setConfirm(null)
      await load();if(intent.action==="test")await load(intent.request.id)
    } catch(e) {
      if(!mounted.current)return
      const code=errorCode(e),unknown=!provablyNotSent(e)&&["pa_outcome_unknown","execution_effect_unknown","pa_revoked","unavailable"].includes(code)
      if(!unknown) { try {sessionStorage.removeItem(durableKey);setHeld(null)} catch { /* Keep the durable command held. */ } }
      setError(code)
    } finally {pending.current=false;if(mounted.current)setBusy(false)}
  }
  const action=(d:W.ProjectAssistantRoutineDefinition,kind:Intent["action"])=>{
    if(!page)return
    const request={command:command(bot.key,"edit"),fence:page.fence,id:d.id,expected_revision:d.version,...(kind==="enable"?{enabled:!d.enabled}:{})}
    void run({action:kind,request})
  }
  const refresh=async()=>{setBusy(true);try{await load();if(history)await load(history.id);setError("")}catch(e){setError(errorCode(e))}finally{setBusy(false)}}
  return <section ref={panel} className="pa-card pa-routines" aria-label={w.routines}>
    <header className="pa-toolbar"><h3 tabIndex={-1}>{w.routines}</h3><button type="button" onClick={onClose}>{w.close}</button></header>
    <p>{w.routineHint}</p>
    {!supported?<p role="status">{w.routinesUnsupported}</p>:<>
      <div className="pa-toolbar"><button type="button" disabled={!writable||busy} onClick={()=>edit(null)}>{w.routineNew}</button><button type="button" disabled={busy} onClick={()=>void refresh()}>{w.routineRefresh}</button></div>
      {!page&&!error&&<p role="status">{w.loading}</p>}
      {page&&page.definitions.length===0&&<p>{w.routinesEmpty}</p>}
      {page?.definitions.map(d=><article className="pa-routine" key={d.id}>
        <h4>{d.name}</h4><p>{d.enabled?w.routineEnabled:w.routinePaused} · {d.next_due?`${w.routineNext}: ${new Date(d.next_due).toLocaleString()}`:w.routineSpent}</p>
        <div className="pa-toolbar"><button type="button" disabled={!writable||busy} onClick={()=>edit(d)}>{w.routineEdit}</button><button type="button" disabled={!writable||busy} onClick={()=>action(d,"enable")}>{d.enabled?w.routinePause:w.routineEnable}</button><button type="button" disabled={!writable||busy} onClick={()=>action(d,"test")}>{w.routineTest}</button><button type="button" disabled={busy} onClick={()=>void load(d.id).catch(e=>setError(errorCode(e)))}>{w.routineHistory}</button><button type="button" disabled={!writable||busy} onClick={()=>setConfirm(d.id)}>{w.routineDelete}</button></div>
        {confirm===d.id&&<div role="alert"><p>{w.routineDeleteConfirm}</p><button type="button" disabled={!writable||busy} onClick={()=>action(d,"delete")}>{w.routineConfirmDelete}</button><button type="button" onClick={()=>setConfirm(null)}>{w.cancel}</button></div>}
      </article>)}
      {page?.next_cursor&&<button type="button" disabled={busy} onClick={()=>void load("",page.next_cursor).catch(e=>setError(errorCode(e)))}>{w.more}</button>}
      {editing!==undefined&&<form onSubmit={event=>{event.preventDefault();if(page)void run({action:"save",request:{command:command(bot.key,"edit"),fence:page.fence,id:editing?.id??"",expected_revision:editing?.version??0,name,instruction,triggers,timeout_seconds:timeout,enabled:active}})}}>
        <fieldset disabled={!writable||busy}>
          <legend>{editing?w.routineEdit:w.routineNew}</legend>
          <label>{w.name}<input value={name} onChange={e=>setName(e.target.value)} required/></label>
          <label>{w.routineInstruction}<textarea value={instruction} onChange={e=>setInstruction(e.target.value)} required/></label>
          <p>{w.routineOr}</p>
          {triggers.map((t,i)=>{const change=(value:Partial<W.ProjectAssistantRoutineTrigger>)=>setTriggers(old=>old.map((v,n)=>n===i?{...v,...value}:v));return <fieldset className="pa-routine-trigger" key={i}>
            <legend>{w.routineTrigger} {i+1}</legend><label>{w.routineType}<select value={t.type} onChange={e=>change({...blankTrigger(),type:e.target.value,seconds:e.target.value==="interval"?3600:0})}><option value="interval">{w.routineInterval}</option><option value="once">{w.routineOnce}</option><option value="cron">{w.routineCron}</option></select></label>
            {t.type==="interval"&&<label>{w.routineSeconds}<input type="number" min={1} max={31536000} value={t.seconds} onChange={e=>change({seconds:Number(e.target.value)})} required/></label>}
            {t.type==="once"&&<label>{w.routineAt}<input type="datetime-local" step="0.001" value={t.at?new Date(t.at-new Date(t.at).getTimezoneOffset()*60000).toISOString().slice(0,-1):""} onChange={e=>change({at:e.target.value?new Date(e.target.value).getTime():0})} required/></label>}
            {t.type==="cron"&&<><label>{w.routineExpression}<input value={t.expression} placeholder="0 9 * * 1-5" onChange={e=>change({expression:e.target.value})} required/></label><label>{w.routineZone}<input value={t.timeZone} placeholder="Asia/Taipei" onChange={e=>change({timeZone:e.target.value})} required/></label></>}
            <button type="button" disabled={triggers.length===1} onClick={()=>setTriggers(old=>old.filter((_,n)=>n!==i))}>{w.routineRemoveTrigger}</button>
          </fieldset>})}
          <button type="button" disabled={triggers.length>=20} onClick={()=>setTriggers(old=>[...old,blankTrigger()])}>{w.routineAddTrigger}</button>
          <label>{w.routineTimeout}<input type="number" min={1} max={86400} value={timeout} onChange={e=>setTimeout(Number(e.target.value))} required/></label>
          <label><input type="checkbox" checked={active} onChange={e=>setActive(e.target.checked)}/>{w.routineEnabled}</label>
          <button type="submit">{w.save}</button><button type="button" onClick={()=>setEditing(undefined)}>{w.cancel}</button>
        </fieldset>
      </form>}
      {journalBlocked&&<p role="alert">{w.routineJournalUnreadable}</p>}
      {held&&!journalBlocked&&<div role="alert"><p>{w.routineHeld}</p><button type="button" disabled={busy||!enabled} onClick={()=>void run(held,true)}>{w.routineReplay}</button></div>}
      {history&&<section aria-label={w.routineHistory}><h4>{w.routineHistory}</h4>{history.page.runs.length===0?<p>{w.routineNoRuns}</p>:history.page.runs.map(run=><article className="pa-routine-run" key={run.id}><p>{w[run.state as keyof Words]??run.state} · {new Date(run.created_at).toLocaleString()} · {run.source==="test"?w.routineTest:w.routineScheduled}</p>{run.result&&<p className="pa-routine-result">{run.result}</p>}{run.failure&&<p role="alert">{run.failure.code} ({run.failure.field})</p>}</article>)}{history.page.next_cursor&&<button type="button" disabled={busy} onClick={()=>void load(history.id,history.page.next_cursor).catch(e=>setError(errorCode(e)))}>{w.more}</button>}</section>}
    </>}
    {error&&!journalBlocked&&<p role="alert">{error==="resume_unsupported"?w.routineResumeUnsupported:error==="pa_outcome_unknown"?w.unknown:w.unavailable} ({error})</p>}
    {busy&&<p role="status">{w.routineSaving}</p>}
  </section>
}
