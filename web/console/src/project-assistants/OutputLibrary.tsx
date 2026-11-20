import {useCallback,useEffect,useRef,useState} from "react"
import type * as W from "@clawdline/contract"
import type {SourceClient} from "./client"
import {errorCode} from "./client"
import {sameConversation} from "./view-state"
import {readView} from "./read-views"
import {libraryItems,libraryGroups,libraryRevision,resolvedLibraryTitle,mergeLibraryMessages} from "./library-items"
import type {LibraryItem} from "./library-items"
import type {ArtifactComposerAccess} from "./ArtifactWorkspace"
import {OutputViewer,libraryItemName} from "./OutputViewer"
import {currentCatalogTag} from "../catalog"
import type {Words} from "./words"

export function OutputLibrary({source,bot,messages,w,onClose,embedded=false,artifactComposer}:{artifactComposer?:ArtifactComposerAccess;source:SourceClient;bot:W.ProjectAssistantBot;messages:readonly W.ProjectAssistantMessage[];w:Words;onClose:()=>void;embedded?:boolean}) {
  const [rows,setRows]=useState<W.ProjectAssistantMessage[]>([]),[cursor,setCursor]=useState(""),[busy,setBusy]=useState(false),[error,setError]=useState("")
  const active=useRef<AbortController|null>(null),paging=useRef({epoch:"",cursor:"",loaded:false})
  const [viewing,setViewing]=useState<LibraryItem|null>(null)
  const opener=useRef<HTMLElement|null>(null)
  const scope={key:bot.key,project:bot.project,conversation_id:bot.conversation_id},identity=JSON.stringify(scope)
  const owner=useRef({source,identity});owner.current={source,identity}
  const resolved=useCallback((output:W.ProjectAssistantOutput)=>{
    if(owner.current.source!==source||owner.current.identity!==identity||source.current?.()===false)return
    if(viewing?.kind!=="output"||!output.artifact_id||viewing.message.output?.artifact_id!==output.artifact_id||!sameConversation(viewing.message.fence,scope))return
    setRows(old=>resolvedLibraryTitle(mergeLibraryMessages(old,[viewing.message]),scope,output))
  },[source,identity,viewing])
  const closeViewer=useCallback(()=>{setViewing(null);requestAnimationFrame(()=>{if(opener.current?.isConnected)opener.current.focus({preventScroll:true})})},[])
  const revision=libraryRevision(messages)
  const load=async(more=false)=>{
    active.current?.abort();const control=new AbortController();active.current=control;setBusy(true);setError("")
    const q:W.ProjectAssistantReadViewRequest={key:bot.key,conversation_id:bot.conversation_id,mode:more?"history":"snapshot",source_epoch:more?paging.current.epoch:"",cursor:more?paging.current.cursor:"",after_seq:0,page_size:100}
    try {
      const page=readView(await source.view(q,control.signal),q,{key:bot.key,project:bot.project,conversation_id:bot.conversation_id})
      if(control.signal.aborted)return
      const same=paging.current.loaded&&page.source_epoch===paging.current.epoch
      setRows(old=>mergeLibraryMessages(more||same?old:[],[...page.items.flatMap(i=>i.message?[i.message]:[]),...(page.snapshot?.queued_messages??[])]))
      const next=!more&&same?paging.current.cursor:page.next_cursor;paging.current={epoch:page.source_epoch,cursor:next,loaded:true};setCursor(next)
    }catch(e){if(!control.signal.aborted)setError(errorCode(e))}finally{if(!control.signal.aborted)setBusy(false)}
  }
  useEffect(()=>{setRows([]);setCursor("");paging.current={epoch:"",cursor:"",loaded:false};setViewing(null);return()=>active.current?.abort()},[source,bot.key.machine_id,bot.key.bot_id,bot.conversation_id])
  useEffect(()=>{void load();return()=>active.current?.abort()},[source,bot.key.machine_id,bot.key.bot_id,bot.conversation_id,revision])
  const items=libraryItems([...messages,...rows])
  return <section className={embedded?"pa-embedded-form pa-library":"pa-form-panel pa-library"} role={embedded?undefined:"dialog"} aria-modal={embedded?undefined:true} aria-label={w.library}>{embedded?<header className="pa-library-head"><button type="button" className="pa-memory-refresh" onClick={()=>void load()} disabled={busy}>{w.refresh}</button></header>:<header className="pa-toolbar"><h2>{w.library}</h2><button onClick={()=>void load()} disabled={busy}>{w.refresh}</button><button onClick={onClose}>{w.close}</button></header>}<div className="pa-form-body">{libraryGroups(items).map(group=><section className="pa-library-group" key={group.date}><h3><time dateTime={group.date}>{new Intl.DateTimeFormat(currentCatalogTag(),{dateStyle:"long"}).format(new Date(group.date+"T12:00:00"))}</time></h3>{group.items.map(item=><div className="pa-output-card" key={item.key} data-library-kind={item.kind}><button type="button" disabled={item.kind==="output"?(!source.output||source.canOutputs?.()===false):item.kind==="input"?(!source.input||source.canInputs?.()===false):(!source.attachment||source.pictures?.()===false)} onClick={event=>{opener.current=event.currentTarget;setViewing(item)}}><strong>{libraryItemName(item)}</strong><small>{item.kind==="output"?(item.message.output!.artifact_id?w.artifact:w.sharedFile):item.kind==="input"?w.attachedFile:w.picture} · {(item.kind==="output"?item.message.output!.bytes:item.kind==="input"?item.file.bytes:item.attachment.bytes).toLocaleString()} B</small></button></div>)}</section>)}{!busy&&!items.length&&!cursor&&<p>{w.emptyLibrary}</p>}{busy&&<p role="status">{w.openingOutput}</p>}{error&&<p role="alert">{w.outputFailed} ({error})</p>}{cursor&&<button onClick={()=>void load(true)} disabled={busy}>{w.earlier}</button>}</div>{viewing&&<OutputViewer artifactComposer={artifactComposer} item={viewing.kind==="output"&&viewing.message.output?.artifact_id?items.find(item=>item.kind==="output"&&item.key===viewing.key)??viewing:viewing} source={source} w={w} onClose={closeViewer} onRenamed={()=>void load()} onResolved={resolved}/>}</section>
}
