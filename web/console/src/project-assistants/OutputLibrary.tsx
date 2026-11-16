import {useEffect,useRef,useState} from "react"
import type * as W from "@clawdline/contract"
import type {SourceClient} from "./client"
import {errorCode} from "./client"
import {readView} from "./read-views"
import {libraryItems,libraryGroups,libraryRevision} from "./library-items"
import type {LibraryItem} from "./library-items"
import {OutputViewer,libraryItemName} from "./OutputViewer"
import {currentCatalogTag} from "../catalog"
import type {Words} from "./words"

export function OutputLibrary({source,bot,messages,w,onClose}:{source:SourceClient;bot:W.ProjectAssistantBot;messages:readonly W.ProjectAssistantMessage[];w:Words;onClose:()=>void}) {
  const [rows,setRows]=useState<W.ProjectAssistantMessage[]>([]),[cursor,setCursor]=useState(""),[busy,setBusy]=useState(false),[error,setError]=useState("")
  const active=useRef<AbortController|null>(null),paging=useRef({epoch:"",cursor:"",loaded:false})
  const [viewing,setViewing]=useState<LibraryItem|null>(null)
  const revision=libraryRevision(messages)
  const load=async(more=false)=>{
    active.current?.abort();const control=new AbortController();active.current=control;setBusy(true);setError("")
    const q:W.ProjectAssistantReadViewRequest={key:bot.key,conversation_id:bot.conversation_id,mode:more?"history":"snapshot",source_epoch:more?paging.current.epoch:"",cursor:more?paging.current.cursor:"",after_seq:0,page_size:100}
    try {
      const page=readView(await source.view(q,control.signal),q,{key:bot.key,project:bot.project,conversation_id:bot.conversation_id})
      if(control.signal.aborted)return
      const same=paging.current.loaded&&page.source_epoch===paging.current.epoch
      setRows(old=>[...new Map([...more||same?old:[],...page.items.flatMap(i=>i.message?[i.message]:[]),...(page.snapshot?.queued_messages??[])].map(m=>[m.id,m])).values()])
      const next=!more&&same?paging.current.cursor:page.next_cursor;paging.current={epoch:page.source_epoch,cursor:next,loaded:true};setCursor(next)
    }catch(e){if(!control.signal.aborted)setError(errorCode(e))}finally{if(!control.signal.aborted)setBusy(false)}
  }
  useEffect(()=>{setRows([]);setCursor("");paging.current={epoch:"",cursor:"",loaded:false};setViewing(null);return()=>active.current?.abort()},[source,bot.key.machine_id,bot.key.bot_id,bot.conversation_id])
  useEffect(()=>{void load();return()=>active.current?.abort()},[source,bot.key.machine_id,bot.key.bot_id,bot.conversation_id,revision])
  const items=libraryItems([...rows,...messages])
  return <section className="pa-form-panel pa-library" role="dialog" aria-modal="true" aria-label={w.library}><header className="pa-toolbar"><h2>{w.library}</h2><button onClick={()=>void load()} disabled={busy}>{w.refresh}</button><button onClick={onClose}>{w.close}</button></header><div className="pa-form-body">{libraryGroups(items).map(group=><section className="pa-library-group" key={group.date}><h3><time dateTime={group.date}>{new Intl.DateTimeFormat(currentCatalogTag(),{dateStyle:"long"}).format(new Date(group.date+"T12:00:00"))}</time></h3>{group.items.map(item=><div className="pa-output-card" key={item.key} data-library-kind={item.kind}><button type="button" disabled={item.kind==="output"?(!source.output||source.canOutputs?.()===false):item.kind==="input"?(!source.input||source.canInputs?.()===false):(!source.attachment||source.pictures?.()===false)} onClick={()=>setViewing(item)}><strong>{libraryItemName(item)}</strong><small>{item.kind==="output"?(item.message.output!.artifact_id?w.artifact:w.sharedFile):item.kind==="input"?w.attachedFile:w.picture} · {(item.kind==="output"?item.message.output!.bytes:item.kind==="input"?item.file.bytes:item.attachment.bytes).toLocaleString()} B</small></button></div>)}</section>)}{!busy&&!items.length&&!cursor&&<p>{w.emptyLibrary}</p>}{busy&&<p role="status">{w.openingOutput}</p>}{error&&<p role="alert">{w.outputFailed} ({error})</p>}{cursor&&<button onClick={()=>void load(true)} disabled={busy}>{w.earlier}</button>}</div>{viewing&&<OutputViewer item={viewing} source={source} w={w} onClose={()=>setViewing(null)}/>}</section>
}
