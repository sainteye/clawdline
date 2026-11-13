import { useEffect, useRef, useState } from "react"
import type * as W from "@clawdline/contract"
import { errorCode } from "./client"
import type { Words } from "./words"

export function MemoryPanel({value,pendingText,pendingSources,enabled,onSave,onForget,onRefresh,onClose,w}:{
  value:W.ProjectAssistantExport;pendingText?:string;pendingSources?:string[];enabled:boolean;w:Words;
  onSave:(text:string)=>Promise<void>;onForget:(ids:string[])=>Promise<void>;
  onRefresh:()=>Promise<void>;onClose:()=>void
}) {
  const [text,setText]=useState(pendingText??""),[selected,setSelected]=useState<string[]>(()=>value.memory_entries.filter(entry=>entry.sources.some(source=>pendingSources?.includes(source.id))).map(entry=>entry.id))
  const [busy,setBusy]=useState(false),[error,setError]=useState("")
  const pending=useRef(false),panel=useRef<HTMLElement>(null)
  useEffect(()=>{
    const target=panel.current?.querySelector<HTMLElement>("textarea:not(:disabled)")??panel.current?.querySelector<HTMLElement>("h3")
    panel.current?.scrollIntoView({block:"nearest"})
    target?.focus({preventScroll:true})
  },[])
  const entries=value.memory_entries.filter(entry=>!entry.forgotten)
  const sourceIDs=[...new Set(entries.filter(entry=>selected.includes(entry.id)).flatMap(entry=>entry.sources.map(source=>source.id)))]
  const run=async(action:()=>Promise<void>,done:()=>void=()=>{})=>{
    if(pending.current||!enabled)return
    pending.current=true;setBusy(true);setError("")
    try{await action();done()}catch(error){setError(errorCode(error))}
    finally{pending.current=false;setBusy(false)}
  }
  return <section ref={panel} className="pa-card pa-memory" aria-label={w.memory}>
    <header className="pa-toolbar"><h3 tabIndex={-1}>{w.memory}</h3><button type="button" onClick={onClose}>{w.close}</button></header>
    <p>{w.forgetScope}</p>
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
    <p>{w.memoryRevision}: {value.memory.revision} · {w.forgotten}: {value.memory.forgotten_source_ids.length}</p>
    {error&&<p role="alert">{error==="pa_outcome_unknown"?w.unknown:w.unavailable} ({error}) <button type="button" disabled={busy} onClick={()=>{setError("");void onRefresh().catch(error=>setError(errorCode(error)))}}>{w.refresh}</button></p>}
    {busy&&<p role="status">{w.savingMemory}</p>}
  </section>
}
