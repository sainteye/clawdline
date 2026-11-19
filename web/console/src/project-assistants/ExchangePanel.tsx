import { bindNativeModalBackdrop } from "../modal-backdrop.js"
import {useEffect,useLayoutEffect,useRef,useState} from "react"
import type * as W from "@clawdline/contract"
import type {SourceClient} from "./client"
import {errorCode} from "./client"
import {exchangeDelivery,exchangeOutcome,exchangePeer,loadExchange} from "./exchanges"
import type {ExchangeEntry,ExchangeScope} from "./exchanges"
import {exchangeWords} from "./exchange-words"
import {BotAvatar} from "./BotAvatar"
import {BotMessageMarkdown} from "./BotMessageMarkdown"
import {sameBot} from "./view-state"
import {afterSurfaceExit} from "./surface-motion"
import {ChatSeparatorGapMs} from "./chat-items"
import type {Words} from "./words"
import "./ExchangePanel.css"

/** The source owns the recorded outcomes; live notices update an open conversation. */
export function ExchangePanel({source,scope,reference,entries,roster,language,w,onClose}:{
  source:SourceClient;scope:ExchangeScope;reference:W.ProjectAssistantExchange;entries:readonly ExchangeEntry[];
  roster?:readonly W.ProjectAssistantBot[];language:string;w:Words;onClose:()=>void
}) {
  const words=exchangeWords(language),dialog=useRef<HTMLDialogElement>(null),close=useRef<HTMLButtonElement>(null),scroll=useRef<HTMLDivElement>(null)
  const follows=useRef(true)
  const [closing,setClosing]=useState(false),[attempt,setAttempt]=useState(0)
  const [result,setResult]=useState<{source:SourceClient;identity:string;revision:string;attempt:number;value?:Awaited<ReturnType<typeof loadExchange>>;error?:string}|null>(null)
  const peer=exchangePeer(reference,scope.key),identity=JSON.stringify([scope.key,scope.project,scope.conversation_id,peer.key,peer.conversation_id,reference.id,reference.revision])
  const revision=source.authorizationRevision
  useLayoutEffect(()=>{
    const previous=document.activeElement,node=dialog.current
    if(node){bindNativeModalBackdrop(node);node.showModal()}
    close.current?.focus()
    return()=>{node?.close();if(previous instanceof HTMLElement&&previous.isConnected)previous.focus({preventScroll:true})}
  },[])
  useLayoutEffect(()=>{if(closing&&dialog.current)return afterSurfaceExit(dialog.current,onClose)},[closing,onClose])
  useEffect(()=>{
    const control=new AbortController(),current={source,identity,revision,attempt}
    void loadExchange(source,scope,reference,control.signal,true).then(value=>{
      if(!control.signal.aborted)setResult({...current,value})
    },error=>{if(!control.signal.aborted)setResult({...current,error:errorCode(error)})})
    const timer=setInterval(()=>{if(source.current?.()===false||source.authorizationRevision!==revision){control.abort();setResult({...current,error:"pa_revoked"})}},3000)
    return()=>{control.abort();clearInterval(timer)}
  },[source,identity,attempt,revision])
  const current=result?.source===source&&result.identity===identity&&result.revision===revision&&result.attempt===attempt?result:null
  const valid=source.current?.()!==false&&source.authorizationRevision===revision&&current?.error!=="pa_revoked"
  const value=valid?current?.value:undefined,error=valid?current?.error:"pa_revoked"
  const inScope=(entry:ExchangeEntry)=>{
    const participant=exchangePeer(entry.exchange,scope.key)
    return sameBot(participant.key,peer.key)&&participant.conversation_id===peer.conversation_id
  }
  const byID=new Map<string,ExchangeEntry>()
  if(valid)for(const entry of [...value?.history??[],...entries.filter(inScope)]) {
    const prior=byID.get(entry.exchange.id)
    if(!prior||entry.exchange.revision>=prior.exchange.revision)byID.set(entry.exchange.id,entry)
  }
  if(value&&byID.has(value.exchange.id)){
    const entry=byID.get(value.exchange.id)!
    if(value.exchange.revision>=entry.exchange.revision)byID.set(value.exchange.id,{...entry,exchange:value.exchange})
  }
  const rows=[...byID.values()].sort((a,b)=>a.seq-b.seq)
  const participant=(person:W.ProjectAssistantExchangeEndpoint)=>{
    const bot=roster?.find(bot=>sameBot(bot.key,person.key)&&bot.conversation_id===person.conversation_id&&bot.project.canonical_id===scope.project.canonical_id)
    return {bot,name:bot?.name||(sameBot(person.key,scope.key)?value?.botName:"")||person.key.bot_id}
  }
  const nameOf=(person:W.ProjectAssistantExchangeEndpoint)=>participant(person).name
  const badge=(person:W.ProjectAssistantExchangeEndpoint)=>{
    const {bot,name}=participant(person)
    return <span className="pa-exchange-party">{bot&&<BotAvatar appearance={bot.appearance} size={22}/>}<strong>{name}</strong></span>
  }
  const own=sameBot(reference.source.key,scope.key)?reference.source:reference.target
  const signature=JSON.stringify(rows.map(row=>[row.exchange.id,row.exchange.revision]))
  useLayoutEffect(()=>{
    if(follows.current&&scroll.current)scroll.current.scrollTop=scroll.current.scrollHeight
  },[signature])
  const dismiss=()=>setClosing(true)
  return <dialog data-modal-dismiss="true" ref={dialog} className="pa-exchange-panel" data-closing={closing||undefined} inert={closing}
    aria-label={words.title} onCancel={event=>{event.preventDefault();dismiss()}}>
    <header><h3>{badge(own)}<span aria-hidden="true">⇆</span>{badge(peer)}</h3>
      <button className="pa-circle" ref={close} type="button" onClick={dismiss} aria-label={words.close}>×</button></header>
    <div ref={scroll} className="pa-exchange-scroll" aria-busy={!value&&!error} onScroll={event=>{
      const node=event.currentTarget
      follows.current=node.scrollHeight-node.scrollTop-node.clientHeight<8
    }}>
      {!value&&!error&&<p className="pa-exchange-note" role="status">{words.loading}</p>}
      {error&&<p className="pa-exchange-note" role="alert">{words.unavailable} ({error}) <button type="button" onClick={()=>setAttempt(n=>n+1)}>{words.retry}</button></p>}
      {rows.map(({exchange:x,at},index)=>{
        const time=Date.parse(at),then=Date.parse(rows[index-1]?.at??"")
        const outcome=exchangeOutcome(x.outcome),delivery=exchangeDelivery(x.delivery)
        const pending=outcome==="queued"||outcome==="sent"
        return <section key={x.id} className="pa-exchange-record">
          {Number.isFinite(time)&&(!Number.isFinite(then)||time-then>ChatSeparatorGapMs)&&<time dateTime={at}>
            {new Intl.DateTimeFormat(language,{month:"short",day:"numeric",hour:"numeric",minute:"2-digit"}).format(time)}
          </time>}
          <div className="pa-exchange-bubble-wrap">{badge(x.source)}<div className="pa-exchange-bubble"><BotMessageMarkdown text={x.request} w={w}/></div></div>
          {x.reply&&<div className="pa-exchange-bubble-wrap">{badge(x.target)}<div className="pa-exchange-bubble"><BotMessageMarkdown text={x.reply} w={w}/></div></div>}
          {pending?<p className="pa-exchange-note" role="status">{words.waiting.replace("{name}",nameOf(x.target))}</p>:
            outcome!=="completed"?<p className="pa-exchange-note pa-exchange-error">{words[outcome]}{x.detail&&` · ${x.detail}`}</p>:null}
          {delivery==="waiting"||delivery==="dropped"?<p className="pa-exchange-note">{words[`delivery_${delivery}`]}</p>:null}
        </section>
      })}
    </div>
  </dialog>
}
