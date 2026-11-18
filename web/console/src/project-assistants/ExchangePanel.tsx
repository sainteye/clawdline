import { bindNativeModalBackdrop } from "../modal-backdrop.js"
import {useEffect,useRef,useState} from "react"
import type * as W from "@clawdline/contract"
import type {SourceClient} from "./client"
import {errorCode} from "./client"
import {exchangeDelivery,exchangeOutcome,loadExchange} from "./exchanges"
import type {ExchangeScope} from "./exchanges"
import {exchangeWords} from "./exchange-words"
import "./ExchangePanel.css"

/** A modal owns one captured read. Unmount, scope changes and retry abort old reads. */
export function ExchangePanel({source,scope,reference,language,onClose}:{source:SourceClient;scope:ExchangeScope;reference:W.ProjectAssistantExchange;language:string;onClose:()=>void}) {
  const w=exchangeWords(language),dialog=useRef<HTMLDialogElement>(null),close=useRef<HTMLButtonElement>(null)
  const [attempt,setAttempt]=useState(0),[result,setResult]=useState<{source:SourceClient;identity:string;revision:string;attempt:number;value?:Awaited<ReturnType<typeof loadExchange>>;error?:string}|null>(null)
  const identity=JSON.stringify([scope.key,scope.project,scope.conversation_id,reference.id,reference.revision,reference.source,reference.target,reference.source_turn_id,reference.request_turn_id]),revision=source.authorizationRevision
  useEffect(()=>{
    const previous=document.activeElement,el=dialog.current
    if (el) { bindNativeModalBackdrop(el); el.showModal() }
    close.current?.focus()
    return()=>{el?.close();if(previous instanceof HTMLElement&&previous.isConnected)previous.focus()}
  },[])
  useEffect(()=>{
    const control=new AbortController()
    void loadExchange(source,scope,reference,control.signal).then(value=>{
      if(!control.signal.aborted)setResult({source,identity,revision,attempt,value})
    },error=>{if(!control.signal.aborted)setResult({source,identity,revision,attempt,error:errorCode(error)})})
    // Match the existing output viewer's source-lifetime check, without re-fetching content.
    const timer=setInterval(()=>{if(source.current?.()===false||source.authorizationRevision!==revision){control.abort();setResult({source,identity,revision,attempt,error:"pa_revoked"})}},3000)
    return()=>{control.abort();clearInterval(timer)}
  },[source,identity,attempt,revision])
  const current=result?.source===source&&result.identity===identity&&result.revision===revision&&result.attempt===attempt?result:null
  const valid=source.current?.()!==false&&source.authorizationRevision===revision
  const value=valid?current?.value:undefined,error=valid?current?.error:"pa_revoked",x=value?.exchange
  const who=(p:W.ProjectAssistantExchangeEndpoint)=>`${p.key.bot_id===scope.key.bot_id&&value?.botName?value.botName+" · ":""}${p.key.bot_id}`
  return <dialog data-modal-dismiss="true" ref={dialog} className="pa-exchange-panel" aria-label={w.title} onCancel={event=>{event.preventDefault();onClose()}}>
    <header><h3>{w.title}</h3><button ref={close} type="button" onClick={onClose}>{w.close}</button></header>
    <div className="pa-exchange-scroll" aria-busy={!x&&!error}>
      {!x&&!error&&<p role="status">{w.loading}</p>}
      {error&&<p role="alert">{w.unavailable} ({error}) <button type="button" onClick={()=>setAttempt(n=>n+1)}>{w.retry}</button></p>}
      {x&&<>
        <dl><dt>{w.requester}</dt><dd>{who(x.source)}</dd><dt>{w.responder}</dt><dd>{who(x.target)}</dd><dt>{w.state}</dt><dd>{w[exchangeOutcome(x.outcome)]}</dd><dt>{w.delivery}</dt><dd>{w[`delivery_${exchangeDelivery(x.delivery)}`]}</dd></dl>
        <p className="pa-exchange-note">{w.deliveryNote}</p>
        <section><h4>{w.request}</h4><pre>{x.request}</pre></section>
        <section><h4>{w.reply}</h4>{x.reply?<pre>{x.reply}</pre>:<p>{w.noReply}</p>}</section>
        {x.detail&&<section><h4>{w.detail}</h4><pre>{x.detail}</pre></section>}
        <dl><dt>{w.identity}</dt><dd>{x.id}</dd><dt>{w.revision}</dt><dd>{x.revision}</dd><dt>{w.hops}</dt><dd>{x.hops}</dd></dl>
        <button type="button" onClick={()=>setAttempt(n=>n+1)}>{w.refresh}</button>
      </>}
    </div>
  </dialog>
}
