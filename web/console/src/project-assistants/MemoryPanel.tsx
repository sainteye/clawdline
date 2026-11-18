import { useEffect, useRef, useState } from "react"
import type * as W from "@clawdline/contract"
import { errorCode } from "./client"
import type { Words } from "./words"
import { LoadingSkeleton } from "../LoadingSkeleton"
import "./topic-memory.css"

export type MemoryNoteDraft={entry_id:string;title:string;summary:string;text:string;expected_revision:number}

type MemoryProps={
  embedded?:boolean;pendingText?:string;pendingSources?:string[];enabled:boolean;w:Words;
  onSave:(text:string)=>Promise<void>;
  onSaveNote?:(note:MemoryNoteDraft)=>Promise<void>;noteDraft?:MemoryNoteDraft|null;onNoteDraft?:(draft:MemoryNoteDraft|null)=>void;pendingNote?:MemoryNoteDraft;
  onForget:(ids:string[])=>Promise<void>;
  onRefresh:()=>Promise<void>;onClose:()=>void;
  readDetail:(detail:{source_epoch:string;layer:string;entry_id:string;revision:string})=>Promise<W.ProjectAssistantMemoryViewResponse>
}

/** Open as soon as it is asked for: placeholders stand where the memory will be until its read answers. */
export function MemoryPanel({value,...props}:MemoryProps&{value:W.ProjectAssistantMemoryViewResponse|null}) {
  return value?<MemoryContent value={value} {...props}/>:<MemoryLoading w={props.w} onClose={props.onClose} embedded={props.embedded}/>
}

function MemoryLoading({w,onClose,embedded=false}:{w:Words;onClose:()=>void;embedded?:boolean}) {
  const panel=useRef<HTMLElement>(null)
  useEffect(()=>{panel.current?.scrollIntoView({block:"nearest"});panel.current?.querySelector<HTMLElement>("h3")?.focus({preventScroll:true})},[])
  return <section ref={panel} className={embedded?"pa-memory pa-memory-embedded":"pa-card pa-memory"} aria-label={w.memory} aria-busy="true">
    {!embedded&&<header className="pa-toolbar"><h3 tabIndex={-1}>{w.memory}</h3><button type="button" onClick={onClose}>{w.close}</button></header>}
    <LoadingSkeleton kind="text" id="bots-memory" label={w.memoryLoading}/>
  </section>
}

function MemoryContent({value,embedded=false,pendingText,pendingSources,pendingNote,noteDraft,onNoteDraft,enabled,onSave,onSaveNote,onForget,onRefresh,onClose,readDetail,w}:MemoryProps&{value:W.ProjectAssistantMemoryViewResponse}) {
  const [text,setText]=useState(pendingText??""),[selected,setSelected]=useState<string[]>(()=>(value.view.layers.find(layer=>layer.layer==="bot")?.entries??[]).filter(entry=>entry.sources.some(source=>pendingSources?.includes(source.id))).map(entry=>entry.id))
  const [busy,setBusy]=useState(false),[error,setError]=useState(pendingNote?"pa_outcome_unknown":""),[uncertain,setUncertain]=useState(!!pendingNote)
  const [localNote,setLocalNote]=useState<MemoryNoteDraft|null>(pendingNote??null)
  const note=noteDraft===undefined?localNote:noteDraft,setNote=onNoteDraft??setLocalNote
  const noteForm=useRef<HTMLFormElement>(null)
  useEffect(()=>{if(note)noteForm.current?.querySelector<HTMLInputElement>("input")?.focus({preventScroll:true})},[note?.entry_id])
  const [layer,setLayer]=useState("bot"),[detail,setDetail]=useState<W.ProjectAssistantMemoryDetail|null>(null),[reading,setReading]=useState(false)
  const detailSequence=useRef(0),detailAlive=useRef(true)
  useEffect(()=>{detailAlive.current=true;return()=>{detailAlive.current=false;detailSequence.current++}},[])
  useEffect(()=>{setDetail(null);setReading(false);detailSequence.current++},[layer,value.source_epoch,value.view.layers.map(l=>l.revision).join("/")])
  const open=async(entry:W.ProjectAssistantMemoryRow)=>{
    const owner=value.view.layers.find(l=>l.layer===layer);if(!owner)return
    const sequence=++detailSequence.current;setReading(true);setDetail(null);setError("")
    try{const answer=await readDetail({source_epoch:value.source_epoch,layer,entry_id:entry.id,revision:owner.revision});if(detailAlive.current&&sequence===detailSequence.current)setDetail(answer.detail??null)}catch(e){if(detailAlive.current&&sequence===detailSequence.current)setError(errorCode(e))}finally{if(detailAlive.current&&sequence===detailSequence.current)setReading(false)}
  }
  const shown=value.view.layers.find(l=>l.layer===layer)
  const pending=useRef(false),panel=useRef<HTMLElement>(null)
  useEffect(()=>{
    const target=panel.current?.querySelector<HTMLElement>("textarea:not(:disabled)")??panel.current?.querySelector<HTMLElement>("h3")
    panel.current?.scrollIntoView({block:"nearest"})
    target?.focus({preventScroll:true})
  },[])
  const entries=(value.view.layers.find(layer=>layer.layer==="bot")?.entries??[])
  const sourceIDs=[...new Set(entries.filter(entry=>selected.includes(entry.id)).flatMap(entry=>[...(entry.kind==="note"?[entry.id]:[]),...entry.sources.map(source=>source.id)]))]
  const run=async(action:()=>Promise<void>,done:()=>void=()=>{})=>{
    if(pending.current||!enabled)return
    pending.current=true;setBusy(true);setError("")
    try{await action();setUncertain(false);done()}catch(error){const code=errorCode(error);setError(code);setUncertain(["unavailable","pa_outcome_unknown","execution_effect_unknown","pa_revoked"].includes(code)||(error as Error)?.name==="AbortError")}
    finally{pending.current=false;setBusy(false)}
  }
  return <section ref={panel} className={embedded?"pa-memory pa-memory-embedded":"pa-card pa-memory"} aria-label={w.memory}>
    {!embedded&&<header className="pa-toolbar"><h3 tabIndex={-1}>{w.memory}</h3><button type="button" onClick={onClose}>{w.close}</button></header>}
    <nav className="pa-memory-scopes" aria-label={w.memory}><button aria-pressed={layer==="project"} onClick={()=>setLayer("project")}>{w.memoryProject}</button><button aria-pressed={layer==="bot"} onClick={()=>setLayer("bot")}>{w.memoryBot}</button></nav><div className="pa-memory-refresh"><button type="button" onClick={()=>void onRefresh().catch(e=>setError(errorCode(e)))}>{w.memoryRefresh}</button></div>
    {shown?.available&&<div className="pa-memory-content-head"><p className="pa-memory-usage">{w.memoryUsage.replace("{used}",String(shown.used)).replace("{cap}",String(shown.cap))}</p>{layer==="bot"&&onSaveNote&&<button className="pa-memory-create-note" type="button" disabled={!enabled||busy||!!note||uncertain} onClick={()=>setNote({entry_id:crypto.randomUUID(),title:"",summary:"",text:"",expected_revision:value.view.memory.revision})}>{w.memoryCreateNote}</button>}</div>}
    {!shown?.available?<p role="status">{w.memoryUnavailable}</p>:<>{shown.truncated&&<p role="status">{w.memoryTruncated}</p>}{shown.entries.length===0&&<p>{w.emptyMemory}</p>}{shown.entries.map(entry=><article key={entry.id} className="pa-memory-entry"><button type="button" className="pa-memory-index" aria-label={w.memoryRead+": "+entry.id} onClick={()=>void open(entry)}>{entry.title&&<strong>{entry.title}</strong>}<span>{entry.text}</span></button><small>{entry.author==="human"?w.memoryAuthorHuman:entry.author==="bot"?w.memoryAuthorBot:entry.author||w.memoryAuthorUnknown} · {entry.updated_at||w.memoryTimeUnknown}</small></article>)}</>}
    {reading&&<p role="status">{w.memoryLoading}</p>}
    {detail&&<section className="pa-memory-detail" aria-label={w.memoryRead}><h4>{detail.title||detail.id}</h4>{layer==="bot"&&detail.kind==="note"&&onSaveNote&&<div className="pa-toolbar"><button type="button" disabled={!enabled||busy||!!note||uncertain} onClick={()=>setNote({entry_id:detail.id,title:detail.title??"",summary:detail.summary??"",text:detail.text,expected_revision:value.view.memory.revision})}>{w.memoryEditNote}</button><button type="button" disabled={!enabled||busy||!!note||uncertain} onClick={()=>{const row=entries.find(entry=>entry.id===detail.id);void run(()=>onForget([detail.id,...(row?.sources.map(source=>source.id)??[])]),()=>setDetail(null))}}>{w.forget}</button></div>}<pre style={{whiteSpace:"pre-wrap",overflowWrap:"anywhere"}}>{detail.text}</pre>{detail.truncated&&<p>{w.memoryTruncated}</p>}</section>}
    {layer==="bot"&&note&&onSaveNote&&<form ref={noteForm} className="pa-memory-note-editor" aria-label={w.memoryEditNote} onSubmit={event=>{event.preventDefault();void run(()=>onSaveNote(note),()=>setNote(null))}}>
      <label>{w.memoryNoteTitle}<input value={note.title} disabled={!enabled||busy||uncertain} onChange={event=>setNote({...note,title:event.target.value})}/></label>
      <label>{w.memoryNoteSummary}<textarea value={note.summary} disabled={!enabled||busy||uncertain} onChange={event=>setNote({...note,summary:event.target.value})}/></label>
      <p>{w.memoryNoteHint}</p>
      <label>{w.memoryNoteBody}<textarea className="pa-memory-note-body" value={note.text} disabled={!enabled||busy||uncertain} onChange={event=>setNote({...note,text:event.target.value})}/></label>
      <div className="pa-toolbar"><button type="button" disabled={busy||uncertain} onClick={()=>setNote(null)}>{w.cancel}</button><button type="submit" disabled={!enabled||busy||!note.title.trim()||!note.summary.trim()||!note.text.trim()}>{uncertain?w.retry:w.save}</button></div>
    </form>}
    <details className="pa-memory-policy"><summary>{w.memoryPolicyLabel}</summary><p>{w.memoryPolicy}</p></details>
    {layer==="bot"&&<><details className="pa-memory-short-facts" open={!!pendingText||undefined}><summary>{w.memoryShortFacts}</summary><p>{w.forgetScope}</p>
    <form onSubmit={event=>{event.preventDefault();void run(()=>onSave(text.trim()),()=>setText(""))}}>
      <label>{w.memoryNote}<textarea value={text} disabled={!enabled||busy} onChange={event=>setText(event.target.value)}/></label>
      <button type="submit" disabled={!enabled||busy||!text.trim()}>{w.remember}</button>
    </form>
    </details><details className="pa-memory-forget"><summary>{w.forget}</summary><fieldset disabled={!enabled||busy}><legend>{w.savedMemory}</legend>
      {entries.length?entries.map(entry=><label className="pa-memory-entry" key={entry.id}>
        <input type="checkbox" checked={selected.includes(entry.id)} disabled={!entry.sources.length} onChange={event=>setSelected(old=>event.target.checked?[...old,entry.id]:old.filter(id=>id!==entry.id))}/>
        <span>{entry.title||entry.text}</span>
      </label>):<p>{w.emptyMemory}</p>}
      <button type="button" disabled={!enabled||busy||!sourceIDs.length} onClick={()=>void run(()=>onForget(sourceIDs),()=>setSelected([]))}>{w.forget}</button>
    </fieldset></details>
    </>}<p className="pa-memory-revision">{w.memoryRevision}: {value.view.memory.revision} · {w.forgotten}: {value.view.memory.forgotten_count}</p>
    {error&&<p role="alert">{uncertain?w.unknown:["pa_context_stale","pa_stale_version"].includes(error)?w.memoryDetailStale:w.unavailable} ({error}) <button type="button" disabled={busy} onClick={()=>{setError("");void onRefresh().catch(error=>setError(errorCode(error)))}}>{w.memoryRefresh}</button></p>}
    {busy&&<p role="status">{w.savingMemory}</p>}
  </section>
}
