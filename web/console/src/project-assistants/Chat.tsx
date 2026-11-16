import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react"
import "../session/jump.css"
import type * as W from "@clawdline/contract"
import type { SourceClient } from "./client"
import { authorized, command, errorCode, failure, provablyNotSent } from "./client"
import { botIdentity, canRead, canWrite, draftIdentity, inspectHeldSend, sameConversation, sameFence, type HeldInspection, type HeldOutcome, type ViewState } from "./view-state"
import { partitionMessages } from "./chat-items"
import { readView, mergeTimeline, eventTimeline, turnSummary } from "./read-views"
import { BotAvatar } from "./BotAvatar"
import { BotSettings } from "./BotSettings"
import { Routines } from "./Routines"
import { MemoryPanel } from "./MemoryPanel"
import {OutputLibrary} from "./OutputLibrary"
import { ChatRows } from "./ChatRows"
import {uploadInputs,MAX_INPUTS,MAX_INPUT_BYTES,type DraftInput} from "./inputs"
import { Composer, MAX_PICTURES } from "./Composer"
import { PermissionCard } from "./PermissionCard"
import { Trace } from "./Trace"
import { activityOf, chatMood, liveActivity, rosterMood } from "./working"
import { WorkingTransition } from "./WorkingTransition"
import { readOnlyReason } from "./source-errors"
import { RuleList } from "./RuleList"
import { BotManage } from "./BotManage"
import { seenThrough } from "./read-position"
import { ProjectMark } from "./ProjectMark"
import { PaMenu } from "./PaSelect"
import type { Words } from "./words"
import { ChatSkeleton, useShowAfter } from "./PaLoading"
import { memoryOpener } from "./loading"

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
  accountKey:string;readerID?:string;projectName?:string;projectIcon?:W.Icon;machineName?:string;view:ViewState;w:Words;language:string;onBack:()=>void;
  onBotChanged:(bot:W.ProjectAssistantBot)=>void;onOpenWork?:(work:W.ProjectAssistantWorkLink)=>void;onDeleted?:()=>void;onRead?:()=>void
}
export function Chat({summary,source,current,accountKey,readerID,view,w,language,onBack,onBotChanged,onOpenWork,onDeleted,onRead,projectName,projectIcon,machineName}:ChatProps) {
  const key=summary.bot.key,identity=botIdentity(key),draftKey=draftIdentity(accountKey,key)
  const grant=useRef(source.authorizationRevision).current
  const scope=useRef({key:{...key},project:{...summary.bot.project},conversation_id:summary.bot.conversation_id}).current
  const [recoveryRequired,setRecoveryRequired]=useState(()=>view.recoveryRequired.has(draftKey)),recovery=useRef(recoveryRequired)
  const [library,setLibrary]=useState(false)
  const [data,setData]=useState<W.ProjectAssistantGetResponse|null>(null)
  const [timeline,setTimeline]=useState<W.ProjectAssistantReadViewItem[]>([]),timelineRef=useRef(timeline)
  const [permissions,setPermissions]=useState<W.ProjectAssistantReadPermissionSummary[]>([]),[turns,setTurns]=useState<W.ProjectAssistantReadTurnSummary[]>([]),[activity,setActivity]=useState<Record<string,string>>({})
  const permissionRows=useRef(permissions)
  const [traceSummaries,setTraceSummaries]=useState<W.ProjectAssistantReadTraceSummary[]>([])
  const traceRows=useRef(traceSummaries)
  const [details,setDetails]=useState<Record<string,W.ProjectAssistantPermissionOffer|W.ProjectAssistantTrace>>({})
  const detailRows=useRef(details)
  const [permissionMore,setPermissionMore]=useState(""),[traceMore,setTraceMore]=useState("")
  const [head,setHead]=useState<W.ProjectAssistantReadViewHead|null>(null),headRef=useRef(head)
  // A held send read back after a reload shows its own text and quote, locked, whatever the draft store says.
  const heldAtMount=useRef(view.heldOutcomes.get(draftKey)).current,heldSendAtMount=heldAtMount?.intent.command.operation==="send"?heldAtMount.intent as W.ProjectAssistantSendRequest:null
  // A send this page still has a request open for, from a Chat that was left: shown as sending until its own answer lands.
  const awaiting=useRef(heldSendAtMount&&view.heldOutcomes.transporting(draftKey,heldSendAtMount.command.idempotency_key)?heldSendAtMount:null),inFlightAtMount=awaiting.current
  // Capture the entry draft before any asynchronous source read or composer edit.
  const entryDraft=useRef({text:view.draft(draftKey),quote:view.quotes.get(draftKey)??""}).current
  const entryChecked=useRef(false),entryUnknown=useRef(false)
  const legacyAtMount=useRef(view.unverifiedDrafts.active(draftKey,scope)).current
  const lockedAtMount=inFlightAtMount?null:heldSendAtMount
  const [text,setText]=useState(()=>lockedAtMount?lockedAtMount.text:legacyAtMount?.text??entryDraft.text),[quote,setQuote]=useState(lockedAtMount?lockedAtMount.quote_message_id:legacyAtMount?.quote??entryDraft.quote)
  const [error,setError]=useState(""),[receipt,setReceipt]=useState<W.ProjectAssistantReceipt|null>(()=>view.heldOutcomes.get(draftKey)?.receipt??null),[busy,setBusy]=useState(!!inFlightAtMount),[sending,setSending]=useState<string|null>(inFlightAtMount?.text??null)
  const [unknown,setUnknown]=useState(()=>(view.heldOutcomes.has(draftKey)&&!inFlightAtMount)||view.heldOutcomes.problem(draftKey)==="unreadable"),[settings,setSettings]=useState(false),[memory,setMemory]=useState<W.ProjectAssistantExport|null>(null),[memoryLoading,setMemoryLoading]=useState(false),[earlierLoading,setEarlierLoading]=useState(false)
  const [routines,setRoutines]=useState(false)
  const [more,setMore]=useState(""),[following,setFollowing]=useState(true),[draftDurable,setDraftDurable]=useState(true)
  // One explicit read of the source's receipts for this tab's held send; never a poll or a retry.
  const [inspection,setInspection]=useState<{state:HeldInspection["state"]|"checking"|"unavailable";code?:string}|null>(null),inspecting=useRef<AbortController|null>(null)
  // Sends set aside to write a new message, each with its own explicit check; repainted from the host journal.
  const [asideChecks,setAsideChecks]=useState<Record<string,{state:HeldInspection["state"]|"checking"|"unavailable";code?:string}>>({}),[,setJournalRevision]=useState(0)
  const repaintJournal=()=>setJournalRevision(n=>n+1)
  const legacyBlocked=()=>!!view.unverifiedDrafts.active(draftKey,scope)||!!view.unverifiedDrafts.problem(draftKey)
    || (entryUnknown.current&&!view.unverifiedDrafts.continuing(draftKey,scope))
  const scroll=useRef<HTMLDivElement>(null),alive=useRef(true),currentData=useRef(data),lastSeq=useRef(0),epoch=useRef("")
  const stopping=useRef(false)
  const memorySave=useRef<W.ProjectAssistantMemoryWriteRequest|null>(null),memoryForget=useRef<W.ProjectAssistantForgetRequest|null>(null),memoryShown=useRef(false)
  const refreshBusy=useRef(false),timer=useRef<ReturnType<typeof setTimeout>|null>(null),eventControl=useRef<AbortController|null>(null),activeMutations=useRef(new Set<AbortController>())
  const reads=useRef(new Set<AbortController>()),collectionBusy=useRef(new Map<string,AbortController>()),historyCursors=useRef(new Set<string>())
  const collectionCursors=useRef({permissions:new Set<string>(),traces:new Set<string>()})
  const lastRead=useRef(0),restored=useRef(false),followingRef=useRef(true)
  const messages=timeline.flatMap(row=>row.message?[row.message]:[])
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
    if(!entryChecked.current){
      entryChecked.current=true
      entryUnknown.current=!!entryDraft.text&&page.head.unknown_turn_count>0&&!heldAtMount&&view.heldOutcomes.problem(draftKey)!=="unreadable"&&!view.unverifiedDrafts.continuing(draftKey,scope)
      if(entryUnknown.current)view.unverifiedDrafts.capture(draftKey,scope,entryDraft.text,entryDraft.quote)
    }
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
  },[read,request,valid,view,draftKey,scope,entryDraft,heldAtMount])
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
            if(event.trace){const now=activityOf(event.trace),id=event.trace.turn_id;if(now)setActivity(old=>({...old,[id]:now}))}
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
    // Reopening a Bot retries only its bounded read. Keep the recovery latch
    // until that snapshot validates; unknown effects are never replayed.
    void recoverSnapshot(control.signal).then(async()=>{if(!valid()||control.signal.aborted)return;await Promise.all([collection("permissions"),collection("traces")]);if(!recovery.current&&currentData.current&&canWrite(currentData.current.source)&&valid()&&!control.signal.aborted)timer.current=setTimeout(()=>void refresh(),EventRefreshMs)}).catch(e=>{if(!control.signal.aborted&&valid()){lock();fail(e)}})
    // pagehide may run while visibilityState is still visible. Leaving must
    // pause IO rather than start a refresh that the unloading page cannot read.
    const pause=()=>{eventControl.current?.abort();if(timer.current){clearTimeout(timer.current);timer.current=null}for(const read of reads.current)read.abort();view.flush(draftKey)}
    const visibility=()=>{if(document.visibilityState==="hidden")pause();else void refresh(true)}
    const show=(event:PageTransitionEvent)=>{if(event.persisted)visibility()}
    document.addEventListener("visibilitychange",visibility);window.addEventListener("pagehide",pause);window.addEventListener("pageshow",show)
    return()=>{alive.current=false;uploadControl.current?.abort();control.abort();eventControl.current?.abort();activeMutations.current.forEach(control=>control.abort());for(const read of reads.current)read.abort();if(timer.current)clearTimeout(timer.current);view.flush(draftKey);document.removeEventListener("visibilitychange",visibility);window.removeEventListener("pagehide",pause);window.removeEventListener("pageshow",show)}
  },[identity,source,current,refresh,valid,fail,draftKey,view,recoverSnapshot,collection,lock])
  // A send made by a Chat that was left lands in the host journal by its own
  // command identity; this Chat shows that outcome once and never resends.
  const refreshRef=useRef(refresh);refreshRef.current=refresh
  useEffect(()=>{
    const landed=(id:string,answer:W.ProjectAssistantReceipt|null)=>{
      const was=awaiting.current
      if(!alive.current||!was||was.command.idempotency_key!==id)return
      awaiting.current=null
      const held=view.heldOutcomes.get(draftKey)
      // The left Chat already put an unsettled or failed message back in an empty box; a delivered one leaves the box as the person has it.
      setText(view.draft(draftKey));setQuote(view.quotes.get(draftKey)??"")
      setReceipt(held?.receipt??answer);setUnknown(view.heldOutcomes.has(draftKey))
      setBusy(false);setSending(null);repaintJournal()
      if(valid())void refreshRef.current(true)
    }
    const stop=view.heldOutcomes.subscribe(draftKey,n=>{if(alive.current)repaintJournal();landed(n.idempotency_key,n.receipt)})
    // It may have landed between this Chat's first render and this subscription.
    const was=awaiting.current
    if(was&&!view.heldOutcomes.transporting(draftKey,was.command.idempotency_key))landed(was.command.idempotency_key,null)
    return stop
  },[view,draftKey,valid])

  useLayoutEffect(()=>{
    const el=scroll.current;if(!el||!data)return
    if(!restored.current){const saved=view.position.get(draftKey);el.scrollTop=saved?.following===false?saved.top:el.scrollHeight;followingRef.current=saved?.following??true;setFollowing(followingRef.current);restored.current=true}
    else if(followingRef.current)el.scrollTop=el.scrollHeight
  },[messages,data,draftKey,view])
  const markVisible=()=>{
    const el=scroll.current,d=currentData.current
    if(!el||!d||!readerID||!valid()||!canRead(d.source)||d.source.state!=="online"||document.visibilityState==="hidden"||!document.hasFocus())return
    const rect=el.getBoundingClientRect()
    const rows=[...el.querySelectorAll<HTMLElement>("[data-message-seq]")].map(row=>{const r=row.getBoundingClientRect();return{top:r.top,bottom:r.bottom,seq:Number(row.dataset.messageSeq)}})
    const seq=Math.max(lastRead.current,seenThrough(rows,rect.top,rect.bottom))
    if(seq<=lastRead.current)return
    lastRead.current=seq
    void authorized(source,current,s=>source.markRead({command:command(key,"read"),cursor:{key,conversation_id:d.conversation.id,message_seq:seq,reader_id:readerID}},s),new AbortController().signal).then(()=>{if(valid())onRead?.()}).catch(fail)
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
  const marker="clawdline.bot-inputs."+draftKey
  const [files,setFiles]=useState<DraftInput[]>([]),[filesLost,setFilesLost]=useState(()=>{try{return localStorage.getItem(marker)==="selected"}catch{return true}})
  const [uploading,setUploading]=useState(""),[fileError,setFileError]=useState("")
  const uploadControl=useRef<AbortController|null>(null)
  const rememberFiles=(selected:boolean)=>{try{if(selected)localStorage.setItem(marker,"selected");else localStorage.removeItem(marker)}catch{setDraftDurable(false)}}
  const canInput=!!source.canInputs?.()&&!!source.upload
  const addFiles=(selected:readonly File[])=>{
    if(!canInput||busy||unknown||legacyBlocked())return false
    if(files.length+selected.length>MAX_INPUTS||selected.some(f=>f.size>MAX_INPUT_BYTES)){setFileError(w.inputLimits);return false}
    if(selected.some(f=>!f.name||f.name==="."||f.name===".."||/[\/\\\p{Cc}]/u.test(f.name)||new TextEncoder().encode(f.name).length>128)){setFileError(w.invalidFileName);return false}
    const next=[...files,...selected.map(file=>({file,id:crypto.randomUUID()}))];setFiles(next);setFileError("");setFilesLost(false);rememberFiles(true);return true
  }
  const canAttach=!!source.pictures?.()
  const addImages=(files:readonly File[])=>{void Promise.all(files.slice(0,MAX_PICTURES).map(file=>new Promise<string>((resolve,reject)=>{const r=new FileReader();r.onload=()=>resolve(String(r.result));r.onerror=()=>reject(r.error);r.readAsDataURL(file)}))).then(urls=>{if(valid())setImages(old=>[...old,...urls].slice(0,MAX_PICTURES))}).catch(fail)}
  const send=async()=>{
    if(busy||unknown||legacyBlocked()||view.heldOutcomes.has(draftKey)||filesLost||(!text.trim()&&images.length===0&&files.length===0)||!currentData.current)return
    const submitted=text,pictures=images,selected=files,intent:W.ProjectAssistantSendRequest={command:command(key,"send"),fence:fence(currentData.current),text:submitted,quote_message_id:quote,...(pictures.length?{images:pictures}:{})}
    const id=intent.command.idempotency_key
    let attempted=false,answered:W.ProjectAssistantReceiptResponse|null=null;setBusy(true);setSending(submitted);setInspection(null)
    const hold=(receipt:W.ProjectAssistantReceipt|null):HeldOutcome=>({intent,receipt,...(pictures.length?{pictures:pictures.length}:{}),...(selected.length?{files:selected.map(f=>f.file.name)}:{})})
    // This send's own answer changes only its own journal entry: never a newer
    // held send, and a send set aside stays aside. Then any Chat showing this
    // Bot, including one opened after this one was left, is told once.
    const land=(outcome:HeldOutcome|null)=>{
      const owner=view.heldOutcomes.settle(draftKey,id,outcome)
      view.heldOutcomes.endTransport(draftKey,id,answered?.receipt??outcome?.receipt??null);repaintJournal()
      return owner
    }
    // The message leaves the box at once and shows above as sending, as in a
    // messaging app. A send that failed outright gives it back to the box.
    const quoted=quote
    // Anything but an accepted send puts the message back, as before this
    // change: an unknown one is shown there, disabled, and never resent. The
    // durable copy is only cleared once the send is accepted.
    const restore=()=>{
      if(view.draft(draftKey)==="")view.stage(draftKey,submitted)
      if(quoted&&!view.quotes.get(draftKey))view.quotes.set(draftKey,quoted)
      if(!valid())return
      setText(view.draft(draftKey));setQuote(view.quotes.get(draftKey)??"");setImages(old=>old.length?old:pictures);setFiles(old=>old.length?old:selected)
    }
    changeText("");setImages([]);setQuote("");view.quotes.delete(draftKey)
    try{
      const result=await mutate(async(d,s)=>{
        if(selected.length){
          const upload=new AbortController();uploadControl.current=upload;const cancel=()=>upload.abort();s.addEventListener("abort",cancel,{once:true})
          try{setUploading("0%");intent.fence=fence(d);intent.upload_ids=await uploadInputs(source,intent.fence,selected,upload.signal,(n,total)=>{if(valid()&&!upload.signal.aborted)setUploading((total?Math.floor(n*100/total):100)+"%")},d.bot.instructions.revision)}
          finally{s.removeEventListener("abort",cancel);uploadControl.current=null;if(valid())setUploading("")}
          if(s.aborted||!valid()||recovery.current)throw failure("pa_revoked")
        }
        // An earlier unknown turn stays visible and is never resent; a new
        // message from the person is a new turn. Only this tab's own
        // uncertain send is held.
        if(view.heldOutcomes.has(draftKey)||legacyBlocked())throw failure("pa_read_only")
        // Journaled before the transport, so a full reload still holds it.
        attempted=true;view.heldOutcomes.set(draftKey,hold(null));view.heldOutcomes.beginTransport(draftKey,id);repaintJournal()
        // Once bytes may cross, leaving this Chat no longer drops the request:
        // the host journal outlives the Chat and takes the receipt. Authority
        // changes while the Chat is shown still cancel it, and the source
        // client itself turns a later revocation into outcome unknown.
        const effect=new AbortController(),follow=()=>{if(alive.current)effect.abort()}
        if(s.aborted)follow();else s.addEventListener("abort",follow,{once:true})
        try{answered=await source.send(intent,effect.signal)}finally{s.removeEventListener("abort",follow)}
        return answered
      })
      const owner=land(result.receipt.state==="unknown"?hold(result.receipt):null)
      if((result.receipt.state==="unknown"||result.receipt.state==="failed")&&owner==="active")restore()
      if(!valid()||recovery.current)return
      setReceipt(result.receipt);setUnknown(view.heldOutcomes.has(draftKey))
      if(result.receipt.state!=="unknown"&&result.receipt.state!=="failed"){setFiles([]);rememberFiles(false);flush()}
      await refresh()
    }catch(e){
      // The source's own receipt outlives a later authority or abort check.
      const kept=answered as W.ProjectAssistantReceiptResponse|null
      if(kept&&kept.receipt.state!=="unknown"&&kept.receipt.state!=="failed"){if(land(null)==="active"){rememberFiles(false);view.flush(draftKey)}if(valid()){setReceipt(kept.receipt);setUnknown(view.heldOutcomes.has(draftKey));setFiles([]);setError("")}return}
      // Only a refusal that provably sent nothing releases the hold; anything
      // that may have crossed the transport stays unknown and is never resent.
      const code=errorCode(e),unresolved=attempted&&!provablyNotSent(e)&&(kept?.receipt.state==="unknown"||code==="pa_outcome_unknown"||code==="execution_effect_unknown"||code==="unavailable"||code==="pa_revoked"||(e as Error)?.name==="AbortError")
      const owner=attempted?land(!unresolved?null:kept?hold(kept.receipt):view.heldOutcomes.find(draftKey,id)??null):"none"
      if(!attempted||owner==="active")restore()
      if(valid()){if((!attempted||provablyNotSent(e))&&(e as Error)?.name==="AbortError")setError("");else fail(e);setUnknown(view.heldOutcomes.has(draftKey))}
    }finally{if(valid()){setBusy(false);setSending(null)}}
  }

  // Reads the source's durable receipts once, on the person's request. A
  // matching admitted receipt reattaches to its turn; only a recorded failure
  // returns the draft to editing; anything else keeps the hold unchanged.
  const inspect=async()=>{
    const held=view.heldOutcomes.get(draftKey)
    if(!held||held.intent.command.operation!=="send"||inspecting.current||!valid())return
    const control=new AbortController();inspecting.current=control;reads.current.add(control);setInspection({state:"checking"})
    try{
      const value=await authorized(source,current,s=>source.export(key,s),control.signal)
      if(!valid()||control.signal.aborted)return
      if(view.heldOutcomes.get(draftKey)!==held)return setInspection(null)
      const result=inspectHeldSend(value,held,scope)
      if(result.state==="delivered"){
        view.heldOutcomes.delete(draftKey)
        const intent=held.intent as W.ProjectAssistantSendRequest
        // The restored copy is the delivered message, not a new draft.
        if(view.draft(draftKey)===intent.text){view.stage(draftKey,"")}
        if(text===intent.text)setText("")
        if((view.quotes.get(draftKey)??"")===intent.quote_message_id){view.quotes.delete(draftKey)}
        if(quote===intent.quote_message_id)setQuote("")
        setImages([]);setFiles([]);rememberFiles(false);setFilesLost(false);flush()
        setReceipt(result.receipt);setUnknown(false);setError("");setInspection({state:"delivered"})
        void refresh(true)
      }else if(result.state==="failed"){
        view.heldOutcomes.delete(draftKey);setReceipt(result.receipt);setUnknown(false);setInspection({state:"failed"})
      }else setInspection({state:result.state==="invalid"?"unavailable":result.state,code:result.state==="invalid"?"pa_invalid":undefined})
    }catch(e){if(valid())setInspection({state:"unavailable",code:control.signal.aborted?"aborted":errorCode(e)})}
    finally{reads.current.delete(control);if(inspecting.current===control)inspecting.current=null;repaintJournal()}
  }
  // The same single read for a send the person set aside; it never touches the new draft.
  const inspectAside=async(held:HeldOutcome)=>{
    const id=held.intent.command.idempotency_key
    if(held.intent.command.operation!=="send"||inspecting.current||!valid())return
    const control=new AbortController();inspecting.current=control;reads.current.add(control);setAsideChecks(old=>({...old,[id]:{state:"checking"}}))
    try{
      const value=await authorized(source,current,s=>source.export(key,s),control.signal)
      if(!valid()||control.signal.aborted)return
      if(!view.heldOutcomes.aside(draftKey).includes(held))return
      const result=inspectHeldSend(value,held,scope)
      if(result.state==="delivered"||result.state==="failed"){view.heldOutcomes.settleAside(draftKey,id);if(result.state==="delivered")void refresh(true)}
      setAsideChecks(old=>({...old,[id]:{state:result.state==="invalid"?"unavailable":result.state,code:result.state==="invalid"?"pa_invalid":undefined}}))
    }catch(e){if(valid())setAsideChecks(old=>({...old,[id]:{state:"unavailable",code:control.signal.aborted?"aborted":errorCode(e)}}))}
    finally{reads.current.delete(control);if(inspecting.current===control)inspecting.current=null;repaintJournal()}
  }
  // The person's explicit choice to continue: the uncertain send leaves the box
  // and stays visible with its own check; the box starts empty, and the next
  // send is a new command. Nothing is copied or sent. A permission answer is
  // not a message and is never released here.
  const writeNew=()=>{
    const held=view.heldOutcomes.get(draftKey)
    if(busy||!valid()||inspecting.current)return
    if(held&&held.intent.command.operation!=="send")return
    if(legacyBlocked()){
      if(!view.unverifiedDrafts.writeNew(draftKey,scope)){repaintJournal();return}
    }else if(!view.heldOutcomes.setAside(draftKey)){repaintJournal();return}
    view.stage(draftKey,"");view.quotes.delete(draftKey);flush()
    setText("");setQuote("");setImages([]);setFiles([]);rememberFiles(false);setFilesLost(false)
    setReceipt(null);setUnknown(false);setError("");setInspection(null);repaintJournal()
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

  // Codync's unqueue: Stop naming one queued turn takes that message back.
  const unqueue=async(turnID:string)=>{
    if(busy||!valid()||recovery.current||!source.unqueue)return
    setBusy(true)
    try{
      const result=await mutate((d,s)=>source.unqueue!({command:command(key,"stop"),fence:fence(d),turn_id:turnID},s))
      if(valid()){setReceipt(result.receipt);void refresh()}
    }catch(e){fail(e)}
    finally{if(valid())setBusy(false)}
  }

  // Codync's steer: the queued message joins the turn running now.
  const sendNow=async(turnID:string)=>{
    if(busy||!valid()||recovery.current||!source.sendNow)return
    setBusy(true)
    try{
      const result=await mutate((d,s)=>source.sendNow!({command:command(key,"send_now"),fence:fence(d),turn_id:turnID},s))
      if(valid()){setReceipt(result.receipt);void refresh()}
    }catch(e){fail(e)}
    finally{if(valid())setBusy(false)}
  }

  const loadRules=useCallback((signal:AbortSignal)=>source.rules!(key,signal).then(r=>r.rules),[source,key])

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
        // Any option the agent offered, Always allow and Never included (Codync; decided 2026-11-14).
        if(!live.options.some(o=>o.id===option.id&&o.kind===option.kind))throw failure("pa_scope_denied")
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
    setEarlierLoading(true)
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
    finally{reads.current.delete(control);if(collectionBusy.current.get("history")===control)collectionBusy.current.delete("history");if(valid())setEarlierLoading(false)}
  }
  const exportBot=()=>void authorized(source,current,s=>source.export(key,s),new AbortController().signal).then(value=>{const url=URL.createObjectURL(new Blob([JSON.stringify(value,null,2)],{type:"application/json"}));const a=document.createElement("a");a.href=url;a.download="bot-export.json";a.click();URL.revokeObjectURL(url)}).catch(fail)
  const loadExport=async()=>{
    const value=await authorized(source,current,s=>source.export(key,s),new AbortController().signal)
    if(!valid()||!sameConversation({key:value.bot.key,project:value.bot.project,conversation_id:value.conversation.id},scope))throw failure("pa_scope_denied")
    if(memoryShown.current)setMemory(previous=>previous&&previous.memory.revision>value.memory.revision?previous:value)
  }
  // The panel opens at once with placeholders; a failed read closes it onto the chat's error line.
  const loadExportRef=useRef(loadExport);loadExportRef.current=loadExport
  const failRef=useRef(fail);failRef.current=fail
  const memoryPanel=useRef<ReturnType<typeof memoryOpener>|null>(null)
  memoryPanel.current??=memoryOpener(()=>loadExportRef.current(),{
    open:open=>{memoryShown.current=open;if(!open)setMemory(null)},
    loading:setMemoryLoading,
    fail:error=>failRef.current(error)})
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
  // Opening a conversation draws chat-shaped placeholders after 150ms, until its first read answers.
  const chatSkeleton=useShowAfter(!data&&!error)
  const bot=data?.bot??summary.bot,enabled=!recoveryRequired&&!!data&&canWrite(data.source), parts=partitionMessages(messages,turns)
  const activeTurn=turns.find(turn=>turn.id===data?.conversation.active_turn_id&&(turn.state==="running"||turn.state==="waiting_permission"))
  const live=!recoveryRequired&&!error&&source.current?.()!==false&&liveActivity(summary.source)&&liveActivity(data?.source??summary.source)
  const mood=live?(data?chatMood(data.source,activeTurn,head?.pending_permission_count??0):rosterMood(summary)):"idle"
  const queuedIDs=new Set(parts.queued.map(message=>message.id)),chatTimeline=timeline.filter(row=>!row.message||!queuedIDs.has(row.message.id))
  return <section className="pa-chat" aria-label={bot.name}>
    <header className="pa-chat-header"><button className="pa-circle pa-chat-back" onClick={()=>{flush();onBack()}} aria-label={w.back}>‹</button><BotAvatar appearance={bot.appearance} size={36} mood={mood}/><div><h2 tabIndex={-1}>{bot.name}</h2><small className="pa-row-project"><ProjectMark icon={projectIcon} size={12}/><span>{[projectName||w.unnamedProject,bot.agent==="claude"?"Claude Code":"Codex",machineName].filter(Boolean).join(" · ")}</span></small></div><PaMenu label={w.moreActions} items={[
      {id:"library",label:w.library,onSelect:()=>setLibrary(true)},
      {id:"settings",label:w.settings,onSelect:()=>setSettings(true)},
      {id:"routines",label:w.routines,onSelect:()=>setRoutines(true)},
      {id:"memory",label:w.memory,onSelect:()=>void memoryPanel.current?.open()},
      {id:"refresh",label:w.refresh,onSelect:()=>void refresh(true)},
      {id:"export",label:w.export,onSelect:exportBot}]}/></header>
    <div className="pa-chat-scroll" ref={scroll} onScroll={onScroll} onFocus={markVisible} tabIndex={0} aria-label={w.compose+" "+bot.name}>
      <div className="pa-chat-content">{chatSkeleton&&<ChatSkeleton label={w.chatLoading}/>}{data&&!canWrite(data.source)&&<p role="status">{w.history_only} · {w[data.source.state]}</p>}{error&&<p role="alert">{error==="cli_not_installed"?w.setup_cli_not_installed:error==="provider_version_unsupported"?w.setup_version_unsupported:<>{w.unavailable} ({error})</>} <button onClick={()=>void refresh(true)}>{w.refresh}</button></p>}
      {!!more&&<button className="pa-pill" disabled={earlierLoading} aria-busy={earlierLoading||undefined} onClick={()=>void earlier()}>{earlierLoading?w.earlierLoading:w.earlier}</button>}<ChatRows source={source} botKey={key} attachmentURL={id=>source.attachmentURL?.(key,id)??null} timeline={chatTimeline} onOpenWork={onOpenWork} onQuote={m=>{setQuote(m.id);view.quotes.set(draftKey,m.id);scroll.current?.parentElement?.querySelector<HTMLTextAreaElement>(".pa-composer textarea")?.focus()}} w={w} language={language}/>{sending&&<article className="pa-message pa-mine pa-sending" aria-live="polite"><p>{sending}</p><small><span className="pa-send-spinner" aria-hidden="true"/>{w.sending}</small></article>}
      <Trace summaries={traceSummaries} details={details} total={head?.trace_category_count??0} more={!!traceMore} onMore={()=>void collection("traces",traceMore)} onDetail={row=>void detail("trace",row.id,row.detail_cursor)} w={w}/>
      {(head?.pending_permission_count??0)>0&&<section className="pa-permissions"><h3>{w.permission} · {head?.pending_permission_count}</h3>{permissions.map(row=>{const value=details[row.id],offer=value&&"fence" in value?value:undefined;return <PermissionCard key={row.id} summary={row} offer={offer} enabled={enabled&&!unknown} onDetail={()=>void detail("permission",row.id,row.detail_cursor)} onAnswer={answer} w={w}/>})}{permissionMore&&<button className="pa-pill" onClick={()=>void collection("permissions",permissionMore)}>{w.more}</button>}</section>}
      {turns.filter(t=>!["queued","accepted","running","waiting_permission"].includes(t.state)).map(t=><p className="pa-turn" key={t.id}>{w[t.state]}{t.failure_code&&` (${t.failure_code})`}</p>)}
      <WorkingTransition turn={activeTurn} activity={activeTurn?activity[activeTurn.id]??"":""} waiting={mood==="needsInput"} live={live} ready={!!data} w={w}/>
      {routines&&<Routines key={JSON.stringify([accountKey,readerID,source.authorizationRevision,key.machine_id,key.bot_id])} source={source} bot={bot} enabled={enabled} w={w} storageKey={JSON.stringify([accountKey,readerID,key.machine_id,key.bot_id])} onClose={()=>setRoutines(false)}/>}
      {(memory||memoryLoading)&&<MemoryPanel value={memory} pendingText={memorySave.current?.entry.text} pendingSources={memoryForget.current?.source_ids} enabled={enabled} onSave={saveMemory} onForget={forgetMemory} onRefresh={loadExport} onClose={()=>memoryPanel.current?.close()} w={w}/>}
      </div>
      {/* The same floating capsule as an ordinary Session's transcript (session/jump.css): it takes no line of its own. */}
      <div className="tx-jump pa-jump" data-end="bottom" data-shown={following?"off":"on"} data-fresh="off">
        <button type="button" className="tx-jump-go" tabIndex={following?-1:0} aria-hidden={following?true:undefined} title={w.jump} onClick={()=>{followingRef.current=true;setFollowing(true);if(scroll.current)scroll.current.scrollTop=scroll.current.scrollHeight}}>
          <svg viewBox="0 0 16 16" aria-hidden="true" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round"><path d="M8 3v10M3.5 8.5 8 13l4.5-4.5"/></svg>
          <span>{w.jump}</span>
        </button>
      </div>
    </div>
    <div className="pa-chat-footer">{parts.queued.map(m=>{const queuedTurn=turns.find(t=>t.message_id===m.id&&t.state==="queued");return <div className="pa-queued" key={m.id}><span>{w.queued}: {m.text||m.files?.map(f=>f.name).join(" · ")}</span>{queuedTurn&&!m.files?.length&&!m.attachments?.length&&source.canSendNow?.()&&!!data?.conversation.active_turn_id&&<button type="button" className="pa-queued-send" aria-label={w.sendNow} title={w.sendNow} disabled={!enabled||busy} onClick={()=>void sendNow(queuedTurn.id)}>↳</button>}{queuedTurn&&source.canUnqueue?.()&&<button type="button" className="pa-queued-remove" aria-label={w.removeQueued} title={w.removeQueued} disabled={!enabled||busy} onClick={()=>void unqueue(queuedTurn.id)}>×</button>}</div>})}{receipt&&(receipt.state==="failed"||receipt.state==="unknown")&&<p role="status">{w[receipt.state]}</p>}{(unknown||(head?.unknown_turn_count??0)>0)&&<p role="status">{w.unknown}{(head?.unknown_turn_count??0)>0&&` · ${head!.unknown_turn_count}`}</p>}{legacyBlocked()&&<div className="pa-held-send" role="group" aria-label={w.unverifiedDraft}><p>{w.unverifiedExplanation}</p><button type="button" disabled={busy||!enabled} onClick={writeNew}>{w.writeNewMessage}</button></div>}{view.unverifiedDrafts.history(draftKey,scope).map((draft,i)=><div className="pa-held-send pa-held-aside" role="group" aria-label={w.unverifiedDraft} key={i}><p>{w.unverifiedDraft}: <span className="pa-held-text">{draft.text}</span>{draft.quote&&<> · {w.heldQuoted}: {messages.find(m=>m.id===draft.quote)?.text??w.quoteMissing}</>}</p></div>)}{view.unverifiedDrafts.problem(draftKey)&&<p role="alert">{w.unverifiedUnsaved}</p>}{unknown&&view.heldOutcomes.get(draftKey)?.intent.command.operation==="send"&&<div className="pa-held-send" role="group" aria-label={w.unknown}><p>{w.heldSend}</p><HeldAttachments held={view.heldOutcomes.get(draftKey)!} w={w}/><button type="button" disabled={inspection?.state==="checking"||!data} aria-busy={inspection?.state==="checking"||undefined} onClick={()=>void inspect()}>{inspection?.state==="checking"?w.checkingSend:w.checkSend}</button><button type="button" disabled={busy||inspection?.state==="checking"} onClick={writeNew}>{w.writeNewMessage}</button></div>}{view.heldOutcomes.problem(draftKey)==="unreadable"&&<div className="pa-held-send" role="group" aria-label={w.unknown}><p role="alert">{w.heldUnreadable}</p><button type="button" disabled={busy} onClick={writeNew}>{w.writeNewMessage}</button></div>}{view.heldOutcomes.problem(draftKey)==="unsaved"&&(unknown||view.heldOutcomes.aside(draftKey).length>0)&&<p role="alert">{w.heldUnsaved}</p>}{view.heldOutcomes.unreadable(draftKey)&&<p role="status">{w.heldEarlierUnreadable} <button type="button" onClick={()=>{view.heldOutcomes.dismissUnreadable(draftKey);repaintJournal()}}>{w.dismissHeldNote}</button></p>}{view.heldOutcomes.aside(draftKey).map(held=>{const intent=held.intent as W.ProjectAssistantSendRequest,id=intent.command.idempotency_key,check=asideChecks[id];return <div className="pa-held-send pa-held-aside" role="group" aria-label={w.heldEarlier} key={id}><p>{w.heldEarlier}: <span className="pa-held-text">{intent.text||w.heldNoText}</span>{intent.quote_message_id&&<> · {w.heldQuoted}</>}</p><HeldAttachments held={held} w={w}/><button type="button" disabled={check?.state==="checking"||!data} aria-busy={check?.state==="checking"||undefined} onClick={()=>void inspectAside(held)}>{check?.state==="checking"?w.checkingSend:w.checkEarlierSend}</button>{check&&check.state!=="checking"&&<p role="status" className="pa-held-result">{asideResult(check,w)}</p>}</div>})}{Object.entries(asideChecks).filter(([id,c])=>(c.state==="delivered"||c.state==="failed")&&!view.heldOutcomes.aside(draftKey).some(h=>h.intent.command.idempotency_key===id)).map(([id,c])=><p role="status" className="pa-held-result" key={id}>{asideResult(c,w)}</p>)}{inspection&&inspection.state!=="checking"&&<p role="status" className="pa-held-result">{inspection.state==="delivered"?w.checkDelivered:inspection.state==="failed"?w.checkFailed:inspection.state==="unknown"?w.checkUnknown:inspection.state==="missing"?w.checkMissing:w.checkUnavailable.replace("{code}",inspection.code??"unavailable")}</p>}{!draftDurable&&<p role="status">{w.draftUnavailable}</p>}{filesLost&&<p role="alert">{w.filesLost} <button type="button" onClick={()=>{rememberFiles(false);setFilesLost(false)}}>{w.discardFiles}</button></p>}
      {data?.conversation.active_turn_id&&<div className="pa-chat-tools"><button disabled={!enabled||busy} onClick={()=>void stop()}>{w.stop}</button></div>}
      {fileError&&<p role="alert">{fileError}</p>}<Composer key={draftKey} text={text} onText={changeText} onSend={()=>void send()} disabled={!enabled||unknown||legacyBlocked()} sendBlocked={filesLost} busy={busy} quote={quote?messages.find(m=>m.id===quote)?.text??w.quoteMissing:undefined} onClearQuote={()=>{setQuote("");view.quotes.delete(draftKey)}} w={w} images={images} onImages={canAttach?addImages:undefined} onRemoveImage={i=>setImages(old=>old.filter((_,j)=>j!==i))} files={files} onFiles={canInput?addFiles:undefined} onRemoveFile={id=>{const next=files.filter(f=>f.id!==id);setFiles(next);rememberFiles(!!next.length)}} uploading={uploading} onCancelUpload={()=>uploadControl.current?.abort()} onFileError={setFileError}/>
    </div>
    {library&&<OutputLibrary source={source} bot={bot} messages={parts.chat} w={w} onClose={()=>setLibrary(false)}/>}
    {settings&&<BotSettings bot={bot} enabled={enabled} manage={source.canManage?.()&&source.setHidden&&source.deleteBot?<BotManage w={w} enabled={enabled} onHide={async()=>{const r=await mutate((_,s)=>source.setHidden!({command:command(key,"visibility"),hidden:true},s));if(valid()){onBotChanged(r.bot);setSettings(false);onBack()}}} onDelete={async()=>{await mutate((_,s)=>source.deleteBot!({command:command(key,"delete")},s));setSettings(false);onDeleted?.()}}/>:undefined} rules={bot.agent==="claude"&&source.canRules?.()&&source.rules&&source.removeRule?<RuleList w={w} enabled={enabled} load={loadRules} remove={async rule=>{await mutate((_,s)=>source.removeRule!({command:command(key,"rule_remove"),rule},s))}}/>:undefined} reason={recoveryRequired?w.unavailable:readOnlyReason(data?.source,w as unknown as Record<string,string>)} w={w} onClose={()=>setSettings(false)} onSave={async value=>{try{const r=await mutate((_,s)=>source.edit({command:command(key,"edit"),settings:value},s));if(valid()){setData(old=>old?{...old,bot:r.bot}:old);onBotChanged(r.bot);setSettings(false)}}catch(e){fail(e);throw e}}}/>}
  </section>
}

/** What the held send carried that this page may no longer have. */
function HeldAttachments({held,w}:{held:HeldOutcome;w:Words}) {
  const pictures=held.pictures??0,files=held.files??[]
  if(!pictures&&!files.length)return null
  return <p className="pa-held-attachments">{files.length>0&&<>{w.heldFiles}: {files.join(" · ")}</>}{files.length>0&&pictures>0&&" · "}{pictures>0&&(held.restored?w.heldPicturesLost:w.heldPictures).replace("{count}",String(pictures))}</p>
}
function asideResult(check:{state:string;code?:string},w:Words) {
  return check.state==="delivered"?w.checkEarlierDelivered:check.state==="failed"?w.checkEarlierFailed:check.state==="unknown"?w.checkUnknown:check.state==="missing"?w.checkEarlierMissing:w.checkUnavailable.replace("{code}",check.code??"unavailable")
}
