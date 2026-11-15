import {useEffect,useRef,useState} from "react"
import type * as W from "@clawdline/contract"
import type {SourceClient} from "./client"
import {errorCode} from "./client"
import {readView} from "./read-views"
import {OutputCard} from "./OutputCard"
import type {Words} from "./words"

export function OutputLibrary({source,bot,messages,w,onClose}:{source:SourceClient;bot:W.ProjectAssistantBot;messages:readonly W.ProjectAssistantMessage[];w:Words;onClose:()=>void}) {
  const [rows,setRows]=useState<W.ProjectAssistantMessage[]>([]),[cursor,setCursor]=useState(""),[epoch,setEpoch]=useState(""),[busy,setBusy]=useState(false),[error,setError]=useState("")
  const active=useRef<AbortController|null>(null)
  const load=async(more=false)=>{
    active.current?.abort();const control=new AbortController();active.current=control;setBusy(true);setError("")
    const q:W.ProjectAssistantReadViewRequest={key:bot.key,conversation_id:bot.conversation_id,mode:more?"history":"snapshot",source_epoch:more?epoch:"",cursor:more?cursor:"",after_seq:0,page_size:100}
    try {
      const page=readView(await source.view(q,control.signal),q,{key:bot.key,project:bot.project,conversation_id:bot.conversation_id})
      if(control.signal.aborted)return
      setRows(old=>[...more?old:[],...page.items.flatMap(i=>i.message?[i.message]:[])]);setCursor(page.next_cursor);setEpoch(page.source_epoch)
    }catch(e){if(!control.signal.aborted)setError(errorCode(e))}finally{if(!control.signal.aborted)setBusy(false)}
  }
  useEffect(()=>{void load();return()=>active.current?.abort()},[source,bot.key.bot_id])
  const latest=new Map<string,W.ProjectAssistantMessage>()
  for(const m of [...rows,...messages].sort((a,b)=>a.seq-b.seq)){if(m.output)latest.set(m.output.artifact_id||m.output.id,m)}
  return <section className="pa-form-panel" role="dialog" aria-modal="true" aria-label={w.library}><header className="pa-toolbar"><h2>{w.library}</h2><button onClick={()=>void load()} disabled={busy}>{w.refresh}</button><button onClick={onClose}>{w.close}</button></header><div className="pa-form-body">{[...latest.values()].reverse().map(m=><OutputCard key={m.output!.artifact_id||m.output!.id} message={m} source={source} w={w}/>)}{!busy&&!latest.size&&!cursor&&<p>{w.emptyLibrary}</p>}{busy&&<p role="status">{w.openingOutput}</p>}{error&&<p role="alert">{w.outputFailed} ({error})</p>}{cursor&&<button onClick={()=>void load(true)} disabled={busy}>{w.earlier}</button>}</div></section>
}
