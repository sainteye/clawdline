import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react"
import "../session/jump.css"
import type * as W from "@clawdline/contract"
import type { SourceClient } from "./client"
import { authorized, command, errorCode, failure } from "./client"
import { botIdentity, canRead, canWrite, draftIdentity, sameConversation, sameFence, type ViewState } from "./view-state"
import { partitionMessages } from "./chat-items"
import { readView, mergeTimeline, eventTimeline, turnSummary } from "./read-views"
import { BotAvatar } from "./BotAvatar"
import { BotSettings } from "./BotSettings"
import { MemoryPanel } from "./MemoryPanel"
import { ChatRows } from "./ChatRows"
import { Composer, MAX_PICTURES } from "./Composer"
import { PermissionCard } from "./PermissionCard"
import { Trace } from "./Trace"
import { WorkResultCard } from "./WorkResultCard"
import type { Words } from "./words"

export const EventRefreshMs = 3000
/** Covers every await of a permission attempt, including the second detail preflight. */
export async function withPermissionRecovery<T>(run:()=>Promise<T>,current:()=>boolean,recover:()=>void):Promise<T> {
  try{return await run()}
  catch(error){
    const code=errorCode(error)
    if(current()&&(code==="pa_cursor_expired"||code==="pa_stale_generation"))recover()
    throw error
  }
}
export interface ChatProps {
  summary: W.ProjectAssistantBotSummary; source:SourceClient; current:()=>readonly SourceClient[];
  accountKey:string;readerID?:string;projectName?:string;view:ViewState;w:Words;language:string;onBack:()=>void;
  onBotChanged:(bot:W.ProjectAssistantBot)=>void;onOpenWork?:(work:W.ProjectAssistantWorkLink)=>void
}
export function Chat({summary,source,current,accountKey,readerID,view,w,language,onBack,onBotChanged,onOpenWork,projectName}:ChatProps) {
  const key=summary.bot.key,identity=botIdentity(key),draftKey=draftIdentity(accountKey,key)
  const grant=useRef(source.authorizationRevision).current
  const scope=useRef({key:{...key},project:{...summary.bot.project},conversation_id:summary.bot.conversation_id}).current
  const [recoveryRequired,setRecoveryRequired]=useState(()=>view.recoveryRequired.has(draftKey)),recovery=useRef(recoveryRequired)
  const [data,setData]=useState<W.ProjectAssistantGetResponse|null>(null)
  const [timeline,setTimeline]=useState<W.ProjectAssistantReadViewItem[]>([]),timelineRef=useRef(timeline)
  const [permissions,setPermissions]=useState<W.ProjectAssistantReadPermissionSummary[]>([]),[turns,setTurns]=useState<W.ProjectAssistantReadTurnSummary[]>([])
  const permissionRows=useRef(permissions)
  const [traceSummaries,setTraceSummaries]=useState<W.ProjectAssistantReadTraceSummary[]>([])
  const traceRows=useRef(traceSummaries)
  const [details,setDetails]=useState<Record<string,W.ProjectAssistantPermissionOffer|W.ProjectAssistantTrace>>({})
  const detailRows=useRef(details)
  const [permissionMore,setPermissionMore]=useState(""),[traceMore,setTraceMore]=useState("")
  const [head,setHead]=useState<W.ProjectAssistantReadViewHead|null>(null),headRef=useRef(head)
  const [text,setText]=useState(()=>view.draft(draftKey)),[quote,setQuote]=useState(view.quotes.get(draftKey)??"")
  const [error,setError]=useState(()=>recoveryRequired?"pa_read_only":""),[receipt,setReceipt]=useState<W.ProjectAssistantReceipt|null>(()=>view.heldOutcomes.get(draftKey)?.receipt??null),[busy,setBusy]=useState(false)
  const [unknown,setUnknown]=useState(()=>view.heldOutcomes.has(draftKey)),[settings,setSettings]=useState(false),[memory,setMemory]=useState<W.ProjectAssistantExport|null>(null)
  const [more,setMore]=useState(""),[following,setFollowing]=useState(true),[draftDurable,setDraftDurable]=useState(true)
  const scroll=useRef<HTMLDivElement>(null),alive=useRef(true),currentData=useRef(data),lastSeq=useRef(0),epoch=useRef("")
  const stopping=useRef(false)
  const memorySave=useRef<W.ProjectAssistantMemoryWriteRequest|null>(null),memoryForget=useRef<W.ProjectAssistantForgetRequest|null>(null),memoryShown=useRef(false)
  const refreshBusy=useRef(false),timer=useRef<ReturnType<typeof setTimeout>|null>(null),eventControl=useRef<AbortController|null>(null),activeMutations=useRef(new Set<AbortController>())
  const reads=useRef(new Set<AbortController>()),collectionBusy=useRef(new Map<string,AbortController>()),historyCursors=useRef(new Set<string>())
  const collectionCursors=useRef({permissions:new Set<string>(),traces:new Set<string>()})
  const lastRead=useRef(0),restored=useRef(false),followingRef=useRef(true)
  const messages=timeline.flatMap(row=>row.message?[row.message]:[]),works=timeline.flatMap(row=>row.work?[row.work]:[])
  currentData.current=data;timelineRef.current=timeline;headRef.current=head;permissionRows.current=permissions;traceRows.current=traceSummaries;detailRows.current=details
  const fence=useCallback((d:W.ProjectAssistantGetResponse):W.ProjectAssistantFence=>({key:d.bot.key,project:d.bot.project,conversation_id:d.conversation.id,binding_id:d.conversation.binding_id,generation:d.conversation.generation}),[])
  const valid=useCallback(()=>alive.current&&current().some(s=>s===source&&s.machineID===key.machine_id&&s.authorizationRevision===grant),[source,current,grant,key.machine_id])
  const fail=useCallback((e:unknown)=>{if(valid())setError(errorCode(e))},[valid])
  const changeText=(value:string)=>{view.stage(draftKey,value);setText(value)}
  const flush=()=>setDraftDurable(view.flush(draftKey))
  const lock=useCallback(()=>{
    view.recoveryRequired.add(draftKey);recovery.current=true;setRecoveryRequired(true)
    activeMutations.current.forEach(control=>control.abort());setTurns([]);setPermissions([]);permissionRows.current=[];setTraceSummaries([]);traceRows.current=[];setDetails({});detailRows.current={};setPermissionMore("");setTraceMore("")
    for(const control of reads.current)control.abort()
    if(timer.current){clearTimeout(timer.current);timer.current=null}
  },[view,draftKey])
  const request=useCallback((mode:W.ProjectAssistantReadViewMode,cursor="",page_size=100):W.ProjectAssistantReadViewRequest=>({key:scope.key,conversation_id:scope.conversation_id,mode,source_epoch:mode==="snapshot"?"":epoch.current,cursor,after_seq:mode==="events"?lastSeq.current:0,page_size}),[scope])
  const read=useCallback(async(q:W.ProjectAssistantReadViewRequest,signal:AbortSignal)=>{
    const answer=await authorized(source,current,s=>typeof source.view==="function"?source.view(q,s):Promise.reject(failure("pa_unsupported")),signal)
    return readView(answer,q,scope)
  },[source,current,scope])
  const adoptHead=useCallback((page:W.ProjectAssistantReadViewResponse)=>{
    const before=headRef.current
    if(before&&(page.head.event_frontier<before.event_frontier||page.head.bot_version<before.bot_version))return
    headRef.current=page.head;setHead(page.head)
    const d=currentData.current
    if(d){const next={...d,bot:page.snapshot?.bot??d.bot,conversation:page.head.conversation,source:page.source};currentData.current=next;setData(next)}
  },[])
  const recoverSnapshot=useCallback(async(signal:AbortSignal)=>{
    const page=await read(request("snapshot"),signal)
    if(!valid()||signal.aborted)return
    const snapshot=page.snapshot!,fresh:W.ProjectAssistantGetResponse={contract_version:page.contract_version,bot:snapshot.bot,conversation:page.head.conversation,source:page.source}
    activeMutations.current.forEach(control=>control.abort())
    for(const control of reads.current)control.abort()
    reads.current.clear();collectionBusy.current.clear()
    collectionCursors.current={permissions:new Set(),traces:new Set()};historyCursors.current=new Set()
    setPermissions([]);permissionRows.current=[];setTraceSummaries([]);traceRows.current=[];setDetails({});detailRows.current={};setPermissionMore("");setTraceMore("")
    setTurns([...snapshot.pending_turns,...snapshot.last_turn?[snapshot.last_turn]:[]])
    const queued=snapshot.queued_messages.map(message=>({id:message.id,kind:"message" as const,anchor_message_seq:message.seq,observed_event_seq:page.head.event_frontier,message}))
    const rows=mergeTimeline([page.items,queued]);timelineRef.current=rows;setTimeline(rows)
    epoch.current=page.source_epoch;lastSeq.current=page.head.event_frontier
    headRef.current=page.head;setHead(page.head);currentData.current=fresh;setData(fresh)
    setMore(page.next_cursor);lastRead.current=0;setError("")
    // No receipts in a bounded snapshot means an unknown local intent remains unknown.
    view.recoveryRequired.delete(draftKey);recovery.current=false;setRecoveryRequired(false)
    return page
  },[read,request,valid,view,draftKey])
  const collection=useCallback(async(mode:"permissions"|"traces",cursor="")=>{
    const before=headRef.current,capturedEpoch=epoch.current
    if(!before||recovery.current||!valid()||collectionBusy.current.has(mode))return
    const control=new AbortController();reads.current.add(control);collectionBusy.current.set(mode,control)
    try{
      const page=await read(request(mode,cursor),control.signal)
      if(!valid()||control.signal.aborted||epoch.current!==capturedEpoch||!headRef.current||!sameFence(before.current_fence,headRef.current.current_fence)||recovery.current)return
      if(!sameFence(page.head.current_fence,before.current_fence)){lock();throw failure("pa_stale_generation")}
      const cursors=collectionCursors.current[mode]
      if(page.has_more&&cursors.has(page.next_cursor))throw failure("pa_invalid")
      if(cursor)cursors.add(cursor)
      adoptHead(page)
      if(mode==="permissions"){
        const added=page.items.map(row=>row.permission_summary!)
        const base=cursor?permissionRows.current:[],ids=new Set(base.map(row=>row.id));if(added.some(row=>ids.has(row.id)))throw failure("pa_invalid");const rows=[...base,...added];permissionRows.current=rows;setPermissions(rows);setPermissionMore(page.next_cursor)
      }else{
        const added=page.items.map(row=>row.trace_summary!)
        const base=cursor?traceRows.current:[],ids=new Set(base.map(row=>row.id));if(added.some(row=>ids.has(row.id)))throw failure("pa_invalid");const rows=[...base,...added];traceRows.current=rows;setTraceSummaries(rows);setTraceMore(page.next_cursor)
      }
    }catch(e){if(!control.signal.aborted&&valid()){fail(e);if(errorCode(e)!=="pa_capacity_full")lock()}}
    finally{reads.current.delete(control);if(collectionBusy.current.get(mode)===control)collectionBusy.current.delete(mode)}
  },[read,request,valid,adoptHead,lock,fail])
  const refresh=useCallback(async(manual=false)=>{
    if(refreshBusy.current||!valid()||document.visibilityState==="hidden"||recovery.current&&!manual)return
    if(timer.current){clearTimeout(timer.current);timer.current=null}
    refreshBusy.current=true
    const control=new AbortController(),rebuilding=recovery.current||!currentData.current;eventControl.current=control
    try {
      if(recovery.current||!currentData.current){await recoverSnapshot(control.signal);if(!valid()||control.signal.aborted)return;await Promise.all([collection("permissions"),collection("traces")])}
      else {
        const before=headRef.current!,capturedEpoch=epoch.current
        const page=await read(request("events","",256),control.signal)
        if(!valid()||control.signal.aborted||capturedEpoch!==epoch.current||recovery.current)return
        if(!sameFence(before.current_fence,page.head.current_fence)) {
          lock();await recoverSnapshot(control.signal)
          if(!valid()||control.signal.aborted)return
          await Promise.all([collection("permissions"),collection("traces")])
        }else{
          adoptHead(page)
          const rows=eventTimeline(timelineRef.current,page.items);timelineRef.current=rows;setTimeline(rows)
          for(const row of page.items){const event=row.event!
            if(!sameFence(event.fence,page.head.current_fence))continue
            if(event.turn&&sameFence(event.turn.fence,page.head.current_fence))setTurns(old=>[...old.filter(t=>t.id!==event.turn!.id),turnSummary(event.turn!)])
            if(event.permission){const next={...detailRows.current};delete next[event.permission.id];detailRows.current=next;setDetails(next)}
            if(event.kind==="feature_disabled"||event.source&&!canWrite(event.source))activeMutations.current.forEach(control=>control.abort())
          }
          lastSeq.current=page.next_seq;setError("")
          await Promise.all([
            before.pending_permission_count!==page.head.pending_permission_count||page.items.some(row=>row.event?.permission)?collection("permissions"):undefined,
            before.trace_category_count!==page.head.trace_category_count||page.items.some(row=>row.event?.trace)?collection("traces"):undefined,
          ])
        }
      }
      if(!recovery.current&&currentData.current&&canWrite(currentData.current.source)&&valid()&&!control.signal.aborted)timer.current=setTimeout(()=>void refresh(),EventRefreshMs)
    }catch(e){if(!control.signal.aborted&&valid()){
      lock()
      if(errorCode(e)==="pa_cursor_expired"&&!rebuilding){try{await recoverSnapshot(control.signal);if(valid()&&!control.signal.aborted){await Promise.all([collection("permissions"),collection("traces")]);if(!recovery.current&&currentData.current&&canWrite(currentData.current.source))timer.current=setTimeout(()=>void refresh(),EventRefreshMs)}}catch(snapshotError){fail(snapshotError)}}else fail(e)
    }}finally{refreshBusy.current=false}
  },[valid,read,request,recoverSnapshot,collection,adoptHead,lock,fail])
  useEffect(()=>{
    alive.current=true;const control=new AbortController()
    // Initial entry uses only the new consistent bounded snapshot.
    if(!recovery.current)void recoverSnapshot(control.signal).then(async()=>{if(!valid()||control.signal.aborted)return;await Promise.all([collection("permissions"),collection("traces")]);if(!recovery.current&&currentData.current&&canWrite(currentData.current.source)&&valid()&&!control.signal.aborted)timer.current=setTimeout(()=>void refresh(),EventRefreshMs)}).catch(e=>{if(!control.signal.aborted&&valid()){lock();fail(e)}})
    // pagehide may run while visibilityState is still visible. Leaving must
    // pause IO rather than start a refresh that the unloading page cannot read.
    const pause=()=>{eventControl.current?.abort();if(timer.current){clearTimeout(timer.current);timer.current=null}for(const read of reads.current)read.abort();view.flush(draftKey)}
    const visibility=()=>{if(document.visibilityState==="hidden")pause();else if(!recovery.current)void refresh()}
    const show=(event:PageTransitionEvent)=>{if(event.persisted)visibility()}
    document.addEventListener("visibilitychange",visibility);window.addEventListener("pagehide",pause);window.addEventListener("pageshow",show)
    return()=>{alive.current=false;control.abort();eventControl.current?.abort();activeMutations.current.forEach(control=>control.abort());for(const read of reads.current)read.abort();if(timer.current)clearTimeout(timer.current);view.flush(draftKey);document.removeEventListener("visibilitychange",visibility);window.removeEventListener("pagehide",pause);window.removeEventListener("pageshow",show)}
  },[identity,source,current,refresh,valid,fail,draftKey,view,recoverSnapshot,collection,lock])

  useLayoutEffect(()=>{
    const el=scroll.current;if(!el||!data)return
    if(!restored.current){const saved=view.position.get(draftKey);el.scrollTop=saved?.following===false?saved.top:el.scrollHeight;followingRef.current=saved?.following??true;setFollowing(followingRef.current);restored.current=true}
    else if(followingRef.current)el.scrollTop=el.scrollHeight
  },[messages,data,draftKey,view])
  const markVisible=()=>{
    const el=scroll.current,d=currentData.current
    if(!el||!d||!readerID||!valid()||!canRead(d.source)||d.source.state!=="online"||document.visibilityState==="hidden"||!document.hasFocus())return
    const rect=el.getBoundingClientRect()
    const visible=[...el.querySelectorAll<HTMLElement>("[data-message-seq]")].filter(row=>{const r=row.getBoundingClientRect();return r.top>=rect.top&&r.bottom<=rect.bottom})
    const seq=Math.max(lastRead.current,...visible.map(row=>Number(row.dataset.messageSeq)))
    if(seq<=lastRead.current)return
    lastRead.current=seq
    void authorized(source,current,s=>source.markRead({command:command(key,"read"),cursor:{key,conversation_id:d.conversation.id,message_seq:seq,reader_id:readerID}},s),new AbortController().signal).catch(fail)
  }
  useEffect(()=>{markVisible()},[messages,data])
  useEffect(()=>{scroll.current?.parentElement?.querySelector<HTMLElement>(".pa-chat-header h2")?.focus({preventScroll:true})},[identity])
  useEffect(()=>{
    if(!settings)return
    const dialog=scroll.current?.parentElement?.querySelector<HTMLElement>('[role="dialog"]')
    if(!dialog)return
    const opener=document.activeElement instanceof HTMLElement?document.activeElement:null
    dialog.querySelector<HTMLButtonElement>("button")?.focus()
    const key=(event:KeyboardEvent)=>{
      if(event.key==="Escape"){event.preventDefault();setSettings(false);return}
      if(event.key!=="Tab")return
      const nodes=[...dialog.querySelectorAll<HTMLElement>('button:not(:disabled),input:not(:disabled),textarea:not(:disabled),select:not(:disabled)')]
      if(event.shiftKey&&document.activeElement===nodes[0]){event.preventDefault();nodes.at(-1)?.focus()}
      else if(!event.shiftKey&&document.activeElement===nodes.at(-1)){event.preventDefault();nodes[0]?.focus()}
    }
    dialog.addEventListener("keydown",key)
    return()=>{dialog.removeEventListener("keydown",key);if(opener?.isConnected)opener.focus({preventScroll:true})}
  },[settings])
  const onScroll=()=>{const el=scroll.current;if(!el)return;const end=el.scrollHeight-el.scrollTop-el.clientHeight<=1;followingRef.current=end;setFollowing(end);view.position.set(draftKey,{top:el.scrollTop,following:end});markVisible()}
  const mutate=async<T,>(operation:(d:W.ProjectAssistantGetResponse,s:AbortSignal)=>Promise<T>):Promise<T>=>{
    const captured=currentData.current,capturedEpoch=epoch.current
    if(recovery.current||!captured||!canWrite(captured.source)||!valid())throw failure("pa_read_only")
    const control=new AbortController();activeMutations.current.add(control)
    try{return await authorized(source,current,async s=>{
      let page:W.ProjectAssistantReadViewResponse
      try{page=await read(request("snapshot"),s)}catch(e){if(!s.aborted&&valid())lock();throw e}
      if(recovery.current||s.aborted||!valid()||epoch.current!==capturedEpoch)throw failure("pa_read_only")
      if(!canWrite(page.source))throw failure("pa_read_only")
      if(page.source_epoch!==capturedEpoch){lock();throw failure("pa_cursor_expired")}
      if(!sameFence(fence(captured),page.head.current_fence)){lock();throw failure("pa_stale_generation")}
      adoptHead(page)
      const fresh:W.ProjectAssistantGetResponse={contract_version:page.contract_version,bot:page.snapshot!.bot,conversation:page.head.conversation,source:page.source}
      // Preflight cannot replace the visible window or advance replay past unseen facts.
      if(recovery.current||s.aborted||!valid()||!headRef.current||!sameFence(page.head.current_fence,headRef.current.current_fence))throw failure("pa_read_only")
      return operation(fresh,s)
    },control.signal)}finally{activeMutations.current.delete(control)}
  }
  const [images,setImages]=useState<string[]>([])
  const canAttach=!!source.pictures?.()
  const addImages=(files:readonly File[])=>{void Promise.all(files.slice(0,MAX_PICTURES).map(file=>new Promise<string>((resolve,reject)=>{const r=new FileReader();r.onload=()=>resolve(String(r.result));r.onerror=()=>reject(r.error);r.readAsDataURL(file)}))).then(urls=>{if(valid())setImages(old=>[...old,...urls].slice(0,MAX_PICTURES))}).catch(fail)}
  const send=async()=>{
    if(busy||unknown||view.heldOutcomes.has(draftKey)||(!text.trim()&&images.length===0)||!currentData.current)return
    const submitted=text,pictures=images,intent:W.ProjectAssistantSendRequest={command:command(key,"send"),fence:fence(currentData.current),text:submitted,quote_message_id:quote,...(pictures.length?{images:pictures}:{})}
    let attempted=false;setBusy(true)
    try{
      const result=await mutate((_,s)=>{
        // An earlier unknown turn stays visible and is never resent; a new
        // message from the person is a new turn. Only this tab's own
        // uncertain send is held.
        if(view.heldOutcomes.has(draftKey))throw failure("pa_read_only")
        attempted=true;view.heldOutcomes.set(draftKey,{intent,receipt:null})
        return source.send(intent,s)
      })
      if(result.receipt.state==="unknown")view.heldOutcomes.set(draftKey,{intent,receipt:result.receipt})
      else view.heldOutcomes.delete(draftKey)
      if(!valid()||recovery.current)return
      setReceipt(result.receipt);setUnknown(result.receipt.state==="unknown")
      if(result.receipt.state!=="unknown"&&result.receipt.state!=="failed"){
        if(view.draft(draftKey)===submitted)changeText("")
        setImages(old=>old===pictures?[]:old)
        setQuote("");view.quotes.delete(draftKey);flush()
      }
      void refresh()
    }catch(e){
      const code=errorCode(e),unresolved=attempted&&(code==="pa_outcome_unknown"||code==="execution_effect_unknown"||code==="unavailable"||code==="pa_revoked"||(e as Error)?.name==="AbortError")
      if(attempted&&!unresolved)view.heldOutcomes.delete(draftKey)
      if(valid()){fail(e);if(unresolved)setUnknown(true)}
    }finally{if(valid())setBusy(false)}
  }

  const stop=async()=>{
    if(stopping.current||busy||!valid()||recovery.current)return
    stopping.current=true;setBusy(true)
    const action=command(key,"stop")
    try{
      const result=await mutate((d,s)=>source.stop({command:action,fence:fence(d),turn_id:d.conversation.active_turn_id},s))
      if(valid()){setReceipt(result.receipt);void refresh()}
    }catch(e){fail(e)}
    finally{stopping.current=false;if(valid())setBusy(false)}
  }

  const detail=async(kind:"permission"|"trace",id:string,reference:string)=>{
    const captured=headRef.current,capturedEpoch=epoch.current
    if(!captured||!valid()||recovery.current||collectionBusy.current.has(reference))return
    const control=new AbortController();reads.current.add(control);collectionBusy.current.set(reference,control)
    try{
      const page=await read(request("detail",reference,1),control.signal)
      if(!valid()||control.signal.aborted||recovery.current||capturedEpoch!==epoch.current||!headRef.current||!sameFence(captured.current_fence,headRef.current.current_fence))return
      if(!sameFence(page.head.current_fence,captured.current_fence)){lock();throw failure("pa_stale_generation")}
      const row=page.items[0]
      if(row.kind!==kind||kind==="permission"&&row.permission?.id!==id||kind==="trace"&&!traceSummaries.some(t=>t.id===id&&t.turn_id===row.trace?.turn_id&&t.category===row.trace?.category))throw failure("pa_invalid")
      adoptHead(page);const next={...detailRows.current,[id]:row.permission??row.trace!};detailRows.current=next;setDetails(next)
    }catch(e){if(!control.signal.aborted&&valid()){fail(e);if(errorCode(e)!=="pa_capacity_full")lock()}}
    finally{reads.current.delete(control);if(collectionBusy.current.get(reference)===control)collectionBusy.current.delete(reference)}
  }
  const answer=async(offer:W.ProjectAssistantPermissionOffer,option:W.ProjectAssistantPermissionOption)=>{
    let attempted=false
    try{
      const result=await withPermissionRecovery(()=>mutate(async(d,s)=>{
        if(view.heldOutcomes.has(draftKey)||detailRows.current[offer.id]!==offer)throw failure("pa_read_only")
        const summary=permissionRows.current.find(p=>p.id===offer.id),capturedEpoch=epoch.current
        if(!summary)throw failure("pa_stale_generation")
        const page=await read(request("detail",summary.detail_cursor,1),s),live=page.items[0].permission
        // The second read also has an effect fence; ignored cancellation grants no action.
        if(!valid()||s.aborted||recovery.current||capturedEpoch!==epoch.current||view.heldOutcomes.has(draftKey)||detailRows.current[offer.id]!==offer)throw failure("pa_read_only")
        if(!canWrite(page.source)||!headRef.current||!sameFence(page.head.current_fence,headRef.current.current_fence)||!sameFence(page.head.current_fence,fence(d)))throw failure("pa_stale_generation")
        if(!live||live.id!==offer.id||live.turn_id!==offer.turn_id||live.state!=="pending"||Date.parse(live.expires_at)<=Date.now()||!sameFence(live.fence,fence(d)))throw failure("pa_stale_generation")
        // One action at a time, as in CodyNC; a standing rule is never answered from here.
        if(!live.options.some(o=>o.id===option.id&&(o.kind==="reject_once"||o.kind==="allow_once")))throw failure("pa_scope_denied")
        const intent:W.ProjectAssistantPermissionAnswerRequest={command:command(key,"answer"),fence:live.fence,offer_id:live.id,turn_id:live.turn_id,option_id:option.id}
        attempted=true;view.heldOutcomes.set(draftKey,{intent,receipt:null})
        return source.answer(intent,s)
      }),valid,lock)
      const held=view.heldOutcomes.get(draftKey)
      if(result.receipt.state==="unknown"&&held){view.heldOutcomes.set(draftKey,{...held,receipt:result.receipt});if(valid())setUnknown(true)}else view.heldOutcomes.delete(draftKey)
      if(valid()&&!recovery.current){
        setReceipt(result.receipt)
        const next={...detailRows.current,[offer.id]:{...offer,state:result.receipt.state==="unknown"?"unknown" as const:"answered" as const,selected_option_id:option.id}}
        detailRows.current=next;setDetails(next)
      }
    }catch(e){
      const code=errorCode(e)
      if(attempted&&code!=="pa_outcome_unknown"&&code!=="execution_effect_unknown"&&code!=="unavailable"&&code!=="pa_revoked"&&(e as Error)?.name!=="AbortError")view.heldOutcomes.delete(draftKey)
      if(valid()&&view.heldOutcomes.has(draftKey))setUnknown(true)
      fail(e)
    }
  }

  const earlier=async()=>{
    const captured=headRef.current,capturedEpoch=epoch.current,cursor=more
    if(!data||!cursor||!captured||recovery.current||collectionBusy.current.has("history"))return
    const control=new AbortController();reads.current.add(control);collectionBusy.current.set("history",control)
    const el=scroll.current,oldHeight=el?.scrollHeight??0,oldTop=el?.scrollTop??0
    try{
      const page=await read(request("history",cursor),control.signal)
      if(!valid()||control.signal.aborted||epoch.current!==capturedEpoch||recovery.current)return
      if(page.has_more&&historyCursors.current.has(page.next_cursor))throw failure("pa_invalid")
      if(!headRef.current||!sameFence(captured.current_fence,headRef.current.current_fence))return
      if(!sameFence(page.head.current_fence,captured.current_fence)){lock();throw failure("pa_stale_generation")}
      historyCursors.current.add(cursor)
      const rows=mergeTimeline([timelineRef.current,page.items]);timelineRef.current=rows;setTimeline(rows)
      setMore(page.next_cursor)
      requestAnimationFrame(()=>{if(el&&valid()&&epoch.current===capturedEpoch)el.scrollTop=oldTop+el.scrollHeight-oldHeight})
    }catch(e){if(!control.signal.aborted&&valid()){fail(e);if(errorCode(e)!=="pa_capacity_full")lock()}}
    finally{reads.current.delete(control);if(collectionBusy.current.get("history")===control)collectionBusy.current.delete("history")}
  }
  const loadExport=async()=>{
    const value=await authorized(source,current,s=>source.export(key,s),new AbortController().signal)
    if(!valid()||!sameConversation({key:value.bot.key,project:value.bot.project,conversation_id:value.conversation.id},scope))throw failure("pa_scope_denied")
    if(memoryShown.current)setMemory(previous=>previous&&previous.memory.revision>value.memory.revision?previous:value)
  }
  const memoryOutcomeUnknown=(error:unknown)=>["unavailable","pa_outcome_unknown","execution_effect_unknown","pa_revoked"].includes(errorCode(error))||(error as Error)?.name==="AbortError"
  const saveMemory=async(text:string)=>{
    if(!memory||!text.trim())return
    if(memoryForget.current)throw failure("pa_outcome_unknown")
    // Keep the same request after an uncertain reply; a second click cannot
    // turn one note into two entries or move it onto a newer revision.
    let intent=memorySave.current
    if(intent&&intent.entry.text!==text)throw failure("pa_outcome_unknown")
    if(!intent){
      const id=crypto.randomUUID()
      intent={command:command(key,"memory_save"),expected_revision:memory.memory.revision,entry:{id,key,project:memory.bot.project,text,revision:memory.memory.revision,sources:[],forgotten:false}}
      memorySave.current=intent
    }
    const request=intent
    let attempted=false
    try{await mutate((_,signal)=>{attempted=true;return source.saveMemory(request,signal)})}
    catch(error){if(!attempted||!memoryOutcomeUnknown(error))memorySave.current=null;throw error}
    memorySave.current=null
    await loadExport().catch(fail)
  }
  const forgetMemory=async(sourceIDs:string[])=>{
    if(!memory||!sourceIDs.length)return
    if(memorySave.current)throw failure("pa_outcome_unknown")
    let intent=memoryForget.current
    const ids=[...new Set(sourceIDs)].sort()
    if(intent&&JSON.stringify(intent.source_ids)!==JSON.stringify(ids))throw failure("pa_outcome_unknown")
    if(!intent){intent={command:command(key,"forget"),project:memory.bot.project,expected_revision:memory.memory.revision,source_ids:ids};memoryForget.current=intent}
    const request=intent
    let attempted=false
    try{await mutate((_,signal)=>{attempted=true;return source.forget(request,signal)})}
    catch(error){if(!attempted||!memoryOutcomeUnknown(error))memoryForget.current=null;throw error}
    memoryForget.current=null
    await loadExport().catch(fail)
    void refresh(true)
  }
  const bot=data?.bot??summary.bot,enabled=!recoveryRequired&&!!data&&canWrite(data.source), parts=partitionMessages(messages,turns)
  return <section className="pa-chat" aria-label={bot.name}>
    <header className="pa-chat-header"><button className="pa-circle pa-chat-back" onClick={()=>{flush();onBack()}} aria-label={w.back}>‹</button><BotAvatar appearance={bot.appearance} size={36}/><div><h2 tabIndex={-1}>{bot.name}</h2><small>{projectName||bot.project.project_id} · {bot.agent==="claude"?"Claude Code":"Codex"} · {key.machine_id}</small></div><button className="pa-circle" aria-label={w.settings} onClick={()=>setSettings(true)}>···</button></header>
    <div className="pa-chat-scroll" ref={scroll} onScroll={onScroll} onFocus={markVisible} tabIndex={0} aria-label={w.compose+" "+bot.name}>
      <div className="pa-chat-content">{!data&&!error&&<p role="status">{w.loading}</p>}{data&&!canWrite(data.source)&&<p role="status">{w.history_only} · {w[data.source.state]}</p>}{error&&<p role="alert">{error==="cli_not_installed"?w.setup_cli_not_installed:error==="provider_version_unsupported"?w.setup_version_unsupported:<>{w.unavailable} ({error})</>} <button onClick={()=>void refresh(true)}>{w.refresh}</button></p>}
      {!!more&&<button className="pa-pill" onClick={()=>void earlier()}>{w.earlier}</button>}<ChatRows attachmentURL={id=>source.attachmentURL?.(key,id)??null} messages={parts.chat} onQuote={m=>{setQuote(m.id);view.quotes.set(draftKey,m.id);scroll.current?.parentElement?.querySelector<HTMLTextAreaElement>(".pa-composer textarea")?.focus()}} w={w} language={language}/>
      {works.map(work=><WorkResultCard key={work.id} work={work} w={w} onOpen={onOpenWork}/>)}<Trace summaries={traceSummaries} details={details} total={head?.trace_category_count??0} more={!!traceMore} onMore={()=>void collection("traces",traceMore)} onDetail={row=>void detail("trace",row.id,row.detail_cursor)} w={w}/>
      {(head?.pending_permission_count??0)>0&&<section className="pa-permissions"><h3>{w.permission} · {head?.pending_permission_count}</h3>{permissions.map(row=>{const value=details[row.id],offer=value&&"fence" in value?value:undefined;return <PermissionCard key={row.id} summary={row} offer={offer} enabled={enabled&&!unknown} onDetail={()=>void detail("permission",row.id,row.detail_cursor)} onAnswer={answer} w={w}/>})}{permissionMore&&<button className="pa-pill" onClick={()=>void collection("permissions",permissionMore)}>{w.more}</button>}</section>}
      {turns.filter(t=>t.state!=="queued"&&t.state!=="accepted").map(t=><p className="pa-turn" key={t.id}>{w[t.state]}{t.failure_code&&` (${t.failure_code})`}</p>)}
      {memory&&<MemoryPanel value={memory} pendingText={memorySave.current?.entry.text} pendingSources={memoryForget.current?.source_ids} enabled={enabled} onSave={saveMemory} onForget={forgetMemory} onRefresh={loadExport} onClose={()=>{memoryShown.current=false;setMemory(null)}} w={w}/>}
      </div>
      {/* The same floating capsule as an ordinary Session's transcript (session/jump.css): it takes no line of its own. */}
      <div className="tx-jump pa-jump" data-end="bottom" data-shown={following?"off":"on"} data-fresh="off">
        <button type="button" className="tx-jump-go" tabIndex={following?-1:0} aria-hidden={following?true:undefined} title={w.jump} onClick={()=>{followingRef.current=true;setFollowing(true);if(scroll.current)scroll.current.scrollTop=scroll.current.scrollHeight}}>
          <svg viewBox="0 0 16 16" aria-hidden="true" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round"><path d="M8 3v10M3.5 8.5 8 13l4.5-4.5"/></svg>
          <span>{w.jump}</span>
        </button>
      </div>
    </div>
    <div className="pa-chat-footer">{parts.queued.map(m=><div className="pa-queued" key={m.id}>{w.queued}: {m.text}</div>)}{receipt&&<p role="status">{w[receipt.state]} · {receipt.id}</p>}{(unknown||(head?.unknown_turn_count??0)>0)&&<p role="status">{w.unknown}{(head?.unknown_turn_count??0)>0&&` · ${head!.unknown_turn_count}`}</p>}{!draftDurable&&<p role="status">{w.draftUnavailable}</p>}
      <div className="pa-chat-tools"><button onClick={()=>void refresh(true)}>{w.refresh}</button><button onClick={()=>{memoryShown.current=true;void loadExport().catch(fail)}}>{w.memory}</button><button onClick={()=>void authorized(source,current,s=>source.export(key,s),new AbortController().signal).then(value=>{const url=URL.createObjectURL(new Blob([JSON.stringify(value,null,2)],{type:"application/json"}));const a=document.createElement("a");a.href=url;a.download="bot-export.json";a.click();URL.revokeObjectURL(url)}).catch(fail)}>{w.export}</button>{data?.conversation.active_turn_id&&<button disabled={!enabled||busy} onClick={()=>void stop()}>{w.stop}</button>}</div>
      <Composer key={draftKey} text={text} onText={changeText} onSend={()=>void send()} disabled={!enabled||unknown} busy={busy} quote={quote?messages.find(m=>m.id===quote)?.text??w.quoteMissing:undefined} onClearQuote={()=>{setQuote("");view.quotes.delete(draftKey)}} w={w} images={images} onImages={canAttach?addImages:undefined} onRemoveImage={i=>setImages(old=>old.filter((_,j)=>j!==i))}/>
    </div>
    {settings&&<BotSettings bot={bot} enabled={enabled} w={w} onClose={()=>setSettings(false)} onSave={async value=>{try{const r=await mutate((_,s)=>source.edit({command:command(key,"edit"),settings:value},s));if(valid()){setData(old=>old?{...old,bot:r.bot}:old);onBotChanged(r.bot);setSettings(false)}}catch(e){fail(e);throw e}}}/>}
  </section>
}
