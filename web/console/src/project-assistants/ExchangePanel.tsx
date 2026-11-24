import { bindNativeModalBackdrop } from "../modal-backdrop.js"
import {useEffect,useLayoutEffect,useRef,useState} from "react"
import type * as W from "@clawdline/contract"
import type {SourceClient} from "./client"
import {errorCode} from "./client"
import {exchangeBot,exchangeDelivery,exchangeOutcome,exchangeParticipant,exchangePeer,loadExchangePage,mergeExchangeEntries} from "./exchanges"
import type {ExchangeEntry,ExchangePageCursor,ExchangeScope} from "./exchanges"
import {exchangeWords} from "./exchange-words"
import {BotAvatar} from "./BotAvatar"
import {BotMessageMarkdown} from "./BotMessageMarkdown"
import {sameConversation} from "./view-state"
import {afterSurfaceExit} from "./surface-motion"
import {chatTimeBreak,chatTimeFormat} from "./chat-time"
import type {Words} from "./words"
import "./ExchangePanel.css"

/** The source owns the recorded outcomes; live notices update an open conversation. */
export function ExchangePanel({source,scope,reference,entries,roster,language,w,onClose}:{
  source:SourceClient;scope:ExchangeScope;reference:W.ProjectAssistantExchange;entries:readonly ExchangeEntry[];
  roster?:readonly W.ProjectAssistantBot[];language:string;w:Words;onClose:()=>void
}) {
  const words=exchangeWords(language),dialog=useRef<HTMLDialogElement>(null),close=useRef<HTMLButtonElement>(null),scroll=useRef<HTMLDivElement>(null)
  const follows=useRef(true)
  const [closing,setClosing]=useState(false),[request,setRequest]=useState<{next?:ExchangePageCursor}>({})
  const [pages,setPages]=useState<{entries:ExchangeEntry[];bot?:W.ProjectAssistantBot;next?:ExchangePageCursor}>({entries:[]})
  const [result,setResult]=useState<{request:typeof request;error?:string}|null>(null)
  const [revoked,setRevoked]=useState(false)
  const peer=exchangePeer(reference,scope.key),revision=useRef(source.authorizationRevision).current
  useLayoutEffect(()=>{
    const previous=document.activeElement,node=dialog.current
    if(node){bindNativeModalBackdrop(node);node.showModal()}
    close.current?.focus()
    return()=>{node?.close();if(previous instanceof HTMLElement&&previous.isConnected)previous.focus({preventScroll:true})}
  },[])
  useLayoutEffect(()=>{if(closing&&dialog.current)return afterSurfaceExit(dialog.current,onClose)},[closing,onClose])
  useEffect(()=>{
    const control=new AbortController()
    void loadExchangePage(source,scope,reference,control.signal,request.next).then(page=>{
      if(control.signal.aborted)return
      setPages(previous=>({...page,bot:page.bot??previous.bot,entries:mergeExchangeEntries([...previous.entries,...page.entries])}))
      setResult({request})
    },error=>{if(!control.signal.aborted)setResult({request,error:errorCode(error)})})
    return()=>control.abort()
  },[source,request])
  useEffect(()=>{
    const timer=setInterval(()=>{if(source.current?.()===false||source.authorizationRevision!==revision)setRevoked(true)},3000)
    return()=>clearInterval(timer)
  },[source,revision])
  const error=revoked||source.current?.()===false||source.authorizationRevision!==revision?"pa_revoked":result?.request===request?result.error:undefined
  const readable=error!=="pa_revoked"&&error!=="pa_scope_denied"
  const loading=readable&&result?.request!==request
  const rows=readable?mergeExchangeEntries([...entries,...pages.entries].filter(entry=>sameConversation(exchangePeer(entry.exchange,scope.key),peer))):[]
  const participant=(party:ExchangeScope)=>readable?exchangeBot(roster,party)??exchangeBot(pages.bot?[pages.bot]:undefined,party):undefined
  const nameOf=(party:ExchangeScope)=>participant(party)?.name||words.unknownBot
  const badge=(party:ExchangeScope)=>{
    const bot=participant(party)
    return <span className="pa-exchange-party">{bot&&<BotAvatar appearance={bot.appearance} size={22}/>}<strong>{bot?.name||words.unknownBot}</strong></span>
  }
  // Sides follow the heading "scope ⇆ peer": the peer Bot speaks from the right, whoever started an exchange.
  const message=(party:ExchangeScope,text:string)=><div className="pa-exchange-bubble-wrap" data-side={sameConversation(party,peer)?"right":"left"}>
    {badge(party)}<div className="pa-exchange-bubble"><BotMessageMarkdown text={text} w={w}/></div></div>
  const signature=JSON.stringify(rows.map(row=>[row.exchange.id,row.exchange.revision]))
  useLayoutEffect(()=>{
    if(follows.current&&scroll.current)scroll.current.scrollTop=scroll.current.scrollHeight
  },[signature])
  const timeFormat=chatTimeFormat(language)
  let previousAt:string|undefined
  const marker=(at:string|undefined)=>{
    const show=chatTimeBreak(at,previousAt)
    previousAt=at
    return show&&at?<time dateTime={at}>{timeFormat.format(Date.parse(at))}</time>:null
  }
  const dismiss=()=>setClosing(true)
  return <dialog data-modal-dismiss="true" ref={dialog} className="pa-exchange-panel" data-closing={closing||undefined} inert={closing}
    aria-label={words.title} onCancel={event=>{event.preventDefault();dismiss()}}>
    <header><h3>{badge(scope)}<span aria-hidden="true">⇆</span>{badge(peer)}</h3>
      <button className="pa-circle" ref={close} type="button" onClick={dismiss} aria-label={words.close}>×</button></header>
    <div ref={scroll} className="pa-exchange-scroll" aria-busy={loading} onScroll={event=>{
      const node=event.currentTarget
      follows.current=node.scrollHeight-node.scrollTop-node.clientHeight<8
    }}>
      {loading&&<p className="pa-exchange-note" role="status">{request.next?w.earlierLoading:words.loading}</p>}
      {readable&&!loading&&!error&&pages.next&&<button className="pa-pill" type="button" onClick={()=>{follows.current=false;setRequest({next:pages.next})}}>{w.earlier}</button>}
      {error&&<p className="pa-exchange-note" role="alert">{words.unavailable} ({error}) <button type="button" onClick={()=>setRequest(error==="pa_cursor_expired"?{}:{...request})}>{words.retry}</button></p>}
      {rows.map(({exchange:x,requestAt,replyAt})=>{
        const outcome=exchangeOutcome(x.outcome),delivery=exchangeDelivery(x.delivery)
        const pending=outcome==="queued"||outcome==="sent"
        const restarted=outcome==="unknown"&&x.detail==="execution_effect_unknown:restart"
        const showDelivery=delivery!=="unknown"&&(outcome!=="completed"||delivery!=="delivered")
        return <section key={x.id} className="pa-exchange-record">
          {marker(x.request_at??requestAt)}
          {message(exchangeParticipant(x,"source"),x.request)}
          {x.reply&&<>{marker(x.reply_at??replyAt)}{message(exchangeParticipant(x,"target"),x.reply)}</>}
          {pending?<p className="pa-exchange-note" role="status">{words.waiting.replace("{name}",nameOf(exchangeParticipant(x,"target")))}</p>:
            x.outcome==="needs_person"?<p className="pa-exchange-note" role="status">{w.decisionWaiting.replace("{name}",nameOf(exchangeParticipant(x,"target")))}</p>:
            outcome!=="completed"?<p className="pa-exchange-note pa-exchange-error">{words.state} · {words[outcome]}{restarted&&` · ${w.failRestart}`}</p>:null}
          {showDelivery&&<p className="pa-exchange-note">{words.delivery} · {words[`delivery_${delivery}`]}</p>}
          {x.detail&&<details className="pa-exchange-note"><summary>{words.detail}</summary><code>{x.detail}</code></details>}
        </section>
      })}
    </div>
  </dialog>
}
