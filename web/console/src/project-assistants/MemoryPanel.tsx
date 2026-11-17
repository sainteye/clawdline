import { useEffect, useRef, useState } from "react"
import type * as W from "@clawdline/contract"
import { errorCode } from "./client"
import type { Words } from "./words"
import { LoadingSkeleton } from "../LoadingSkeleton"

type MemoryProps={
  pendingText?:string;pendingSources?:string[];enabled:boolean;w:Words;
  onSave:(text:string)=>Promise<void>;onForget:(ids:string[])=>Promise<void>;
  onRefresh:()=>Promise<void>;onClose:()=>void;
  readDetail:(detail:{source_epoch:string;layer:string;entry_id:string;revision:string})=>Promise<W.ProjectAssistantMemoryViewResponse>
}

/** Open as soon as it is asked for: placeholders stand where the memory will be until its read answers. */
export function MemoryPanel({value,...props}:MemoryProps&{value:W.ProjectAssistantMemoryViewResponse|null}) {
  return value?<MemoryContent value={value} {...props}/>:<MemoryLoading w={props.w} onClose={props.onClose}/>
}

function MemoryLoading({w,onClose}:{w:Words;onClose:()=>void}) {
  const panel=useRef<HTMLElement>(null)
  useEffect(()=>{panel.current?.scrollIntoView({block:"nearest"});panel.current?.querySelector<HTMLElement>("h3")?.focus({preventScroll:true})},[])
  return <section ref={panel} className="pa-card pa-memory" aria-label={w.memory} aria-busy="true">
    <header className="pa-toolbar"><h3 tabIndex={-1}>{w.memory}</h3><button type="button" onClick={onClose}>{w.close}</button></header>
    <LoadingSkeleton kind="text" id="bots-memory" label={w.memoryLoading}/>
  </section>
}

function MemoryContent({value,pendingText,pendingSources,enabled,onSave,onForget,onRefresh,onClose,readDetail,w}:MemoryProps&{value:W.ProjectAssistantMemoryViewResponse}) {
  const [text,setText]=useState(pendingText??""),[selected,setSelected]=useState<string[]>(()=>(value.view.layers.find(layer=>layer.layer==="bot")?.entries??[]).filter(entry=>entry.sources.some(source=>pendingSources?.includes(source.id))).map(entry=>entry.id))
  const [busy,setBusy]=useState(false),[error,setError]=useState("")
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
  const sourceIDs=[...new Set(entries.filter(entry=>selected.includes(entry.id)).flatMap(entry=>entry.sources.map(source=>source.id)))]
  const run=async(action:()=>Promise<void>,done:()=>void=()=>{})=>{
    if(pending.current||!enabled)return
    pending.current=true;setBusy(true);setError("")
    try{await action();done()}catch(error){setError(errorCode(error))}
    finally{pending.current=false;setBusy(false)}
  }
  return <section ref={panel} className="pa-card pa-memory" aria-label={w.memory}>
    <header className="pa-toolbar"><h3 tabIndex={-1}>{w.memory}</h3><button type="button" onClick={onClose}>{w.close}</button></header>
    <nav className="pa-toolbar" aria-label={w.memory}><button aria-pressed={layer==="project"} onClick={()=>setLayer("project")}>{w.memoryProject}</button><button aria-pressed={layer==="bot"} onClick={()=>setLayer("bot")}>{w.memoryBot}</button><button onClick={()=>void onRefresh().catch(e=>setError(errorCode(e)))}>{w.refresh}</button></nav>
    <p>{w.memoryPolicy}</p>
    {!shown?.available?<p role="status">{w.memoryUnavailable}</p>:<><p>{w.memoryUsage.replace("{used}",String(shown.used)).replace("{cap}",String(shown.cap))}</p>{shown.truncated&&<p role="status">{w.memoryTruncated}</p>}{shown.entries.length===0&&<p>{w.emptyMemory}</p>}{shown.entries.map(entry=><article key={entry.id} className="pa-memory-entry"><button type="button" aria-label={w.memoryRead+": "+entry.id} onClick={()=>void open(entry)}>{entry.text}</button><small>{entry.author==="human"?w.memoryAuthorHuman:entry.author==="bot"?w.memoryAuthorBot:entry.author||w.memoryAuthorUnknown} · {entry.updated_at||w.memoryTimeUnknown}</small></article>)}</>}
    {reading&&<p role="status">{w.memoryLoading}</p>}
    {detail&&<section aria-label={w.memoryRead}><h4>{detail.id}</h4><pre style={{whiteSpace:"pre-wrap",overflowWrap:"anywhere"}}>{detail.text}</pre>{detail.truncated&&<p>{w.memoryTruncated}</p>}</section>}
    {layer==="bot"&&<><p>{w.forgetScope}</p>
    <form onSubmit={event=>{event.preventDefault();void run(()=>onSave(text.trim()),()=>setText(""))}}>
      <label>{w.memoryNote}<textarea value={text} disabled={!enabled||busy} onChange={event=>setText(event.target.value)}/></label>
      <button type="submit" disabled={!enabled||busy||!text.trim()}>{w.remember}</button>
    </form>
    <fieldset disabled={!enabled||busy}><legend>{w.savedMemory}</legend>
      {entries.length?entries.map(entry=><label className="pa-memory-entry" key={entry.id}>
        <input type="checkbox" checked={selected.includes(entry.id)} disabled={!entry.sources.length} onChange={event=>setSelected(old=>event.target.checked?[...old,entry.id]:old.filter(id=>id!==entry.id))}/>
        <span>{entry.text}</span>
      </label>):<p>{w.emptyMemory}</p>}
      <button type="button" disabled={!enabled||busy||!sourceIDs.length} onClick={()=>void run(()=>onForget(sourceIDs),()=>setSelected([]))}>{w.forget}</button>
    </fieldset>
    </>}<p>{w.memoryRevision}: {value.view.memory.revision} · {w.forgotten}: {value.view.memory.forgotten_count}</p>
    {error&&<p role="alert">{error==="pa_outcome_unknown"?w.unknown:error==="pa_context_stale"?w.memoryDetailStale:w.unavailable} ({error}) <button type="button" disabled={busy} onClick={()=>{setError("");void onRefresh().catch(error=>setError(errorCode(error)))}}>{w.refresh}</button></p>}
    {busy&&<p role="status">{w.savingMemory}</p>}
  </section>
}
