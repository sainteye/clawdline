import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react"
import type * as W from "@clawdline/contract"
import type { SourceClient } from "./client"
import { authorized, command, errorCode, failure } from "./client"
import { botIdentity, canRead, canWrite, draftIdentity, sameBot, sameFence, type ViewState } from "./view-state"
import { mergeMessages, partitionMessages } from "./chat-items"
import { BotAvatar } from "./BotAvatar"
import { BotSettings } from "./BotSettings"
import { ChatRows } from "./ChatRows"
import { Composer } from "./Composer"
import { PermissionCard } from "./PermissionCard"
import { Trace } from "./Trace"
import { WorkResultCard } from "./WorkResultCard"
import type { Words } from "./words"

export const EventRefreshMs = 3000
export interface ChatProps {
  summary: W.ProjectAssistantBotSummary; source:SourceClient; current:()=>readonly SourceClient[];
  accountKey:string;readerID?:string;view:ViewState;w:Words;language:string;onBack:()=>void;
  onBotChanged:(bot:W.ProjectAssistantBot)=>void;onOpenWork?:(work:W.ProjectAssistantWorkLink)=>void
}
export function Chat({summary,source,current,accountKey,readerID,view,w,language,onBack,onBotChanged,onOpenWork}:ChatProps) {
  const key=summary.bot.key,identity=botIdentity(key),draftKey=draftIdentity(accountKey,key)
  const grant=useRef(source.authorizationRevision).current
  const [data,setData]=useState<W.ProjectAssistantGetResponse|null>(null),[messages,setMessages]=useState<W.ProjectAssistantMessage[]>([])
  const [permissions,setPermissions]=useState<W.ProjectAssistantPermissionOffer[]>([]),[turns,setTurns]=useState<W.ProjectAssistantTurn[]>([])
  const [traces,setTraces]=useState<W.ProjectAssistantTrace[]>([]),[works,setWorks]=useState<W.ProjectAssistantWorkLink[]>([])
  const [text,setText]=useState(()=>view.draft(draftKey)),[quote,setQuote]=useState(view.quotes.get(draftKey)??"")
  const [error,setError]=useState(""),[receipt,setReceipt]=useState<W.ProjectAssistantReceipt|null>(null),[busy,setBusy]=useState(false)
  const [unknown,setUnknown]=useState(false),[settings,setSettings]=useState(false),[memory,setMemory]=useState<W.ProjectAssistantExport|null>(null)
  const [sourceIDs,setSourceIDs]=useState(""),[more,setMore]=useState(0),[following,setFollowing]=useState(true),[draftDurable,setDraftDurable]=useState(true)
  const scroll=useRef<HTMLDivElement>(null),alive=useRef(true),currentData=useRef(data),lastSeq=useRef(0),seen=useRef(new Set<string>())
  const refreshBusy=useRef(false),timer=useRef<ReturnType<typeof setTimeout>|null>(null),eventControl=useRef<AbortController|null>(null),mutationControl=useRef<AbortController|null>(null)
  const lastRead=useRef(0),restored=useRef(false),followingRef=useRef(true)
  const receiptRef=useRef(receipt);receiptRef.current=receipt
  currentData.current=data
  const fence=useCallback((d:W.ProjectAssistantGetResponse):W.ProjectAssistantFence=>({key:d.bot.key,project:d.bot.project,conversation_id:d.conversation.id,binding_id:d.conversation.binding_id,generation:d.conversation.generation}),[])
  const valid=useCallback(()=>alive.current&&current().some(s=>s===source&&s.authorizationRevision===grant),[source,current,grant])
  const fail=useCallback((e:unknown)=>{if(valid())setError(errorCode(e))},[valid])
  const changeText=(value:string)=>{view.stage(draftKey,value);setText(value)}
  const flush=()=>setDraftDurable(view.flush(draftKey))
  const recoverSnapshot=useCallback(async(signal:AbortSignal)=>{
    const {snapshot,fresh}=await authorized(source,current,async s=>{
      const status=await source.status(s),own=status.sources.find(row=>row.machine_id===key.machine_id)
      if(!own||!canRead(own))throw failure("pa_revoked")
      if(own.state!=="online")throw failure("pa_offline")
      const snapshot=await source.export(key,s)
      if(!sameBot(snapshot.bot.key,key)||!sameBot(snapshot.conversation.key,key))throw failure("pa_scope_denied")
      // The transactional export is the execution authority. Do not replace it with a later GET.
      const fresh:W.ProjectAssistantGetResponse={contract_version:snapshot.contract_version,bot:snapshot.bot,conversation:snapshot.conversation,source:own}
      return {snapshot,fresh}
    },signal)
    if(!valid()||signal.aborted)return
    const currentFence=fence(fresh)
    mutationControl.current?.abort()
    setPermissions(snapshot.permissions.filter(p=>sameFence(p.fence,currentFence)))
    setTurns(snapshot.turns.filter(t=>sameFence(t.fence,currentFence)))
    setWorks(snapshot.work_links.filter(work=>sameFence(work.fence,currentFence)))
    setTraces(snapshot.events.filter(e=>sameFence(e.fence,currentFence)&&e.trace).reduce<W.ProjectAssistantTrace[]>((rows,event)=>[...rows.filter(t=>!(t.turn_id===event.trace!.turn_id&&t.category===event.trace!.category)),event.trace!],[]))
    // Export must read facts and the retained event frontier in one source transaction.
    lastSeq.current=snapshot.events.reduce((seq,event)=>Math.max(seq,event.seq),0)
    seen.current=new Set(snapshot.events.map(event=>event.id))
    setMessages(snapshot.messages.filter(m=>sameBot(m.fence.key,key)&&m.fence.conversation_id===fresh.conversation.id))
    setMore(0);lastRead.current=0;setMemory(old=>old?snapshot:null)
    currentData.current=fresh;setData(fresh);setError("")
    const ownReceipts=snapshot.receipts.filter(r=>sameBot(r.command.key,key))
    const pending=ownReceipts.find(r=>r.id===receiptRef.current?.id)
    if(pending){setReceipt(pending);setUnknown(pending.state==="unknown")}
    return fresh
  },[source,current,identity,valid,fence])

  const refresh=useCallback(async()=>{
    if(refreshBusy.current||!valid()||document.visibilityState==="hidden")return
    if(timer.current){clearTimeout(timer.current);timer.current=null}
    refreshBusy.current=true
    const control=new AbortController();eventControl.current=control
    try {
      const snapshot=await authorized(source,current,s=>source.get(key,s),control.signal)
      if(!canRead(snapshot.source))throw failure("pa_revoked")
      if(!valid()||control.signal.aborted)return
      const before=currentData.current
      currentData.current=snapshot;setData(snapshot)
      if(before&&!sameFence(fence(before),fence(snapshot))) {
        mutationControl.current?.abort();setPermissions([]);setTurns([]);setTraces([])
      }
      if(snapshot.source.state!=="online")return
      let moreEvents=true
      while(moreEvents&&!control.signal.aborted&&valid()) {
        const page=await authorized(source,current,s=>source.events({key,after_seq:lastSeq.current,page_size:256},s),control.signal)
        if(!valid()||control.signal.aborted)return
        for(const event of page.items) {
          if(seen.current.has(event.id)||event.seq<=lastSeq.current||!sameBot(event.fence.key,key))continue
          seen.current.add(event.id)
          // Stale binding events cannot update a current permission, turn or chat.
          if(!sameFence(event.fence,fence(snapshot)))continue
          if(event.message)setMessages(old=>mergeMessages([old,[event.message!]]))
          if(event.permission)setPermissions(old=>[...old.filter(p=>p.id!==event.permission!.id),event.permission!])
          if(event.turn)setTurns(old=>[...old.filter(t=>t.id!==event.turn!.id),event.turn!])
          if(event.trace)setTraces(old=>[...old.filter(t=>!(t.turn_id===event.trace!.turn_id&&t.category===event.trace!.category)),event.trace!])
          if(event.work)setWorks(old=>[...old.filter(t=>t.id!==event.work!.id),event.work!])
          if(event.kind==="feature_disabled"){setData({...snapshot,source:{...snapshot.source,access:"history_only",writable:false}});mutationControl.current?.abort()}
          if(event.source){setData({...snapshot,source:event.source});if(!canWrite(event.source))mutationControl.current?.abort()}
        }
        if(page.has_more&&page.next_seq<=lastSeq.current)throw failure("pa_cursor_expired")
        lastSeq.current=page.next_seq;moreEvents=page.has_more
      }
      setError("")
      if(canWrite(snapshot.source)&&valid()&&!control.signal.aborted)timer.current=setTimeout(()=>void refresh(),EventRefreshMs)
    }catch(e){if(!control.signal.aborted){
      if(errorCode(e)==="pa_cursor_expired") {
        mutationControl.current?.abort()
        if(currentData.current){const stale={...currentData.current,source:{...currentData.current.source,fresh:false,writable:false}};currentData.current=stale;setData(stale)}
        try{const recovered=await recoverSnapshot(control.signal);if(recovered&&canWrite(recovered.source)&&valid()&&!control.signal.aborted)timer.current=setTimeout(()=>void refresh(),EventRefreshMs)}catch(snapshotError){fail(snapshotError);if(valid())setData(old=>old?{...old,source:{...old.source,fresh:false,writable:false}}:old)}
      }else{fail(e);if(valid())setData(old=>old?{...old,source:{...old.source,fresh:false,writable:false}}:old)}
    }}finally{refreshBusy.current=false}
  },[source,current,identity,valid,fence,fail,recoverSnapshot])

  useEffect(()=>{
    alive.current=true;const control=new AbortController()
    void authorized(source,current,async s=>{
      const snapshot=await source.get(key,s)
      if(!canRead(snapshot.source))throw failure("pa_revoked")
      const page=await source.history({key,conversation_id:snapshot.conversation.id,before_seq:0,page_size:100},s)
      if(!control.signal.aborted&&valid()) {setData(snapshot);currentData.current=snapshot;setMessages(page.items.filter(m=>sameBot(m.fence.key,key)&&m.fence.conversation_id===snapshot.conversation.id));setMore(page.has_more?page.next_before_seq:0)}
    },control.signal).then(()=>{if(!control.signal.aborted)void refresh()}).catch(e=>{if(!control.signal.aborted)fail(e)})
    const visibility=()=>{if(document.visibilityState==="hidden"){eventControl.current?.abort();if(timer.current)clearTimeout(timer.current);view.flush(draftKey)}else void refresh()}
    document.addEventListener("visibilitychange",visibility);window.addEventListener("pagehide",visibility)
    return()=>{alive.current=false;control.abort();eventControl.current?.abort();mutationControl.current?.abort();if(timer.current)clearTimeout(timer.current);view.flush(draftKey);document.removeEventListener("visibilitychange",visibility);window.removeEventListener("pagehide",visibility)}
  },[identity,source,current,refresh,valid,fail,draftKey,view])

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
    const captured=currentData.current;if(!captured||!canWrite(captured.source)||!valid())throw failure("pa_read_only")
    const control=new AbortController();mutationControl.current=control
    return authorized(source,current,async s=>{
      const fresh=await source.get(key,s)
      if(!canWrite(fresh.source))throw failure("pa_read_only")
      if(!sameFence(fence(captured),fence(fresh)))throw failure("pa_stale_generation")
      return operation(fresh,s)
    },control.signal)
  }
  const send=async()=>{
    if(busy||unknown||!text.trim())return
    const submitted=text;setBusy(true)
    try{const result=await mutate((d,s)=>source.send({command:command(key,"send"),fence:fence(d),text:submitted,quote_message_id:quote},s));if(!valid())return;setReceipt(result.receipt);if(result.receipt.state==="unknown"){setUnknown(true)}else if(result.receipt.state!=="failed"){if(view.draft(draftKey)===submitted)changeText("");setQuote("");view.quotes.delete(draftKey);flush()}void refresh()}
    catch(e){if(valid()){fail(e);if(errorCode(e)==="pa_outcome_unknown"||errorCode(e)==="execution_effect_unknown"||errorCode(e)==="unavailable")setUnknown(true)}}finally{if(valid())setBusy(false)}
  }
  const answer=async(offer:W.ProjectAssistantPermissionOffer,option:W.ProjectAssistantPermissionOption)=>{
    try{const result=await mutate((d,s)=>{
      const live=permissions.find(p=>p.id===offer.id)
      if(!live||live.state!=="pending"||Date.parse(live.expires_at)<=Date.now()||!sameFence(offer.fence,fence(d)))throw failure("pa_stale_generation")
      return source.answer({command:command(key,"answer"),fence:offer.fence,offer_id:offer.id,turn_id:offer.turn_id,option_id:option.id},s)
    });if(valid()){setReceipt(result.receipt);setPermissions(old=>old.map(p=>p.id===offer.id?{...p,state:result.receipt.state==="unknown"?"unknown":"answered",selected_option_id:option.id}:p))}}
    catch(e){fail(e)}
  }
  const earlier=async()=>{
    if(!data||!more)return
    const el=scroll.current,oldHeight=el?.scrollHeight??0,oldTop=el?.scrollTop??0
    try{const page=await authorized(source,current,s=>source.history({key,conversation_id:data.conversation.id,before_seq:more,page_size:100},s),new AbortController().signal);setMessages(old=>mergeMessages([page.items,old]));setMore(page.has_more?page.next_before_seq:0);requestAnimationFrame(()=>{if(el&&valid())el.scrollTop=oldTop+el.scrollHeight-oldHeight})}catch(e){fail(e)}
  }
  const loadExport=async()=>{
    try {const value=await authorized(source,current,s=>source.export(key,s),new AbortController().signal);if(valid())setMemory(value)}catch(e){fail(e)}
  }
  const bot=data?.bot??summary.bot,enabled=!!data&&canWrite(data.source), parts=partitionMessages(messages,turns)
  return <section className="pa-chat" aria-label={bot.name}>
    <header className="pa-chat-header"><button className="pa-circle pa-chat-back" onClick={()=>{flush();onBack()}} aria-label={w.back}>‹</button><BotAvatar appearance={bot.appearance} size={36}/><div><h2 tabIndex={-1}>{bot.name}</h2><small>{bot.project.project_id} · {bot.agent==="claude"?"Claude Code":"Codex"} · {key.machine_id}</small></div><button className="pa-circle" aria-label={w.settings} onClick={()=>setSettings(true)}>···</button></header>
    <div className="pa-chat-scroll" ref={scroll} onScroll={onScroll} onFocus={markVisible} tabIndex={0} aria-label={w.compose+" "+bot.name}>
      <div className="pa-chat-content">{!data&&!error&&<p role="status">{w.loading}</p>}{data&&!canWrite(data.source)&&<p role="status">{w.history_only} · {w[data.source.state]}</p>}{error&&<p role="alert">{w.unavailable} ({error}) <button onClick={()=>void refresh()}>{w.refresh}</button></p>}
      {more>0&&<button className="pa-pill" onClick={()=>void earlier()}>{w.earlier}</button>}<ChatRows messages={parts.chat} onQuote={m=>{setQuote(m.id);view.quotes.set(draftKey,m.id);scroll.current?.parentElement?.querySelector("textarea")?.focus()}} w={w} language={language}/>
      {works.map(work=><WorkResultCard key={work.id} work={work} w={w} onOpen={onOpenWork}/>)}<Trace items={traces} w={w}/>
      {permissions.map(offer=><PermissionCard key={offer.id} offer={offer} enabled={enabled} onAnswer={answer} w={w}/>)}
      {turns.filter(t=>t.state!=="queued"&&t.state!=="accepted").map(t=><p className="pa-turn" key={t.id}>{w[t.state]}{t.failure&&` (${t.failure.code})`}</p>)}
      {memory&&<section className="pa-card"><h3>{w.memoryRevision}: {memory.memory.revision}</h3><p>{w.forgetScope}</p><p>{w.forgotten}: {memory.memory.forgotten_source_ids.join(", ")||"—"}</p>{memory.memory_entries.filter(e=>!e.forgotten).map(e=><p key={e.id}>{e.id}: {e.text}</p>)}<label>{w.sourceIDs}<textarea value={sourceIDs} onChange={e=>setSourceIDs(e.target.value)}/></label><button disabled={!enabled||!sourceIDs.trim()} onClick={()=>void mutate((d,s)=>source.forget({command:command(key,"forget"),project:d.bot.project,expected_revision:memory.memory.revision,source_ids:sourceIDs.split("\n").map(v=>v.trim()).filter(Boolean)},s)).then(()=>loadExport()).catch(fail)}>{w.forget}</button></section>}
      </div>
    </div>
    <div className="pa-chat-footer">{parts.queued.map(m=><div className="pa-queued" key={m.id}>{w.queued}: {m.text}</div>)}{receipt&&<p role="status">{w[receipt.state]} · {receipt.id}</p>}{unknown&&<p role="status">{w.unknown}</p>}{!draftDurable&&<p role="status">{w.draftUnavailable}</p>}
      {!following&&<button className="pa-pill" onClick={()=>{followingRef.current=true;setFollowing(true);if(scroll.current)scroll.current.scrollTop=scroll.current.scrollHeight}}>{w.jump}</button>}
      <div className="pa-chat-tools"><button onClick={()=>void refresh()}>{w.refresh}</button><button onClick={()=>void loadExport()}>{w.memory}</button><button onClick={()=>void authorized(source,current,s=>source.export(key,s),new AbortController().signal).then(value=>{const url=URL.createObjectURL(new Blob([JSON.stringify(value,null,2)],{type:"application/json"}));const a=document.createElement("a");a.href=url;a.download="bot-export.json";a.click();URL.revokeObjectURL(url)}).catch(fail)}>{w.export}</button>{data?.conversation.active_turn_id&&<button disabled={!enabled||busy} onClick={()=>void mutate((d,s)=>source.stop({command:command(key,"stop"),fence:fence(d),turn_id:d.conversation.active_turn_id},s)).then(r=>{if(valid())setReceipt(r.receipt);void refresh()}).catch(fail)}>{w.stop}</button>}</div>
      <Composer key={draftKey} text={text} onText={changeText} onSend={()=>void send()} disabled={!enabled||unknown} busy={busy} quote={quote?messages.find(m=>m.id===quote)?.text??w.quoteMissing:undefined} onClearQuote={()=>{setQuote("");view.quotes.delete(draftKey)}} w={w}/>
    </div>
    {settings&&<BotSettings bot={bot} enabled={enabled} w={w} onClose={()=>setSettings(false)} onSave={async value=>{try{const r=await mutate((_,s)=>source.edit({command:command(key,"edit"),settings:value},s));if(valid()){setData(old=>old?{...old,bot:r.bot}:old);onBotChanged(r.bot);setSettings(false)}}catch(e){fail(e);throw e}}}/>}
  </section>
}
