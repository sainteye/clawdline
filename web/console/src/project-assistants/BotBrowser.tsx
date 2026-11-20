import { lazy, Suspense, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react"
import type * as W from "@clawdline/contract"
import {createBrowserHandback} from "../session/browser-handback"
import {toast} from "../overlays/toast"
import { catalogWord } from "../catalog"
import type { BrowserCall, BrowserPanelTarget } from "../session/browser-call"
import { authorized, type SourceClient } from "./client"
import { botBrowserSupported, botOwnedBrowserTab, type BotBrowserStatus } from "./bot-browser-entry"

export const botBrowserWord = (key: string) => catalogWord("botBrowser", key)
// Load Session browser code only for an owned-tab entry or an explicit panel open.
const BrowserPanel=lazy(()=>import("../session/BrowserPanel").then(module=>({default:module.BrowserPanel})))
const BrowserChip=lazy(()=>import("../session/BrowserPanel").then(module=>({default:module.BrowserChip})))

/** The Session chip, with reads captured to this Bot's current source and conversation. */
export function BotBrowserChip({source,current,bot,hidden,paused,onOpen}:{source:SourceClient;current:()=>readonly SourceClient[];bot:W.ProjectAssistantBot;hidden:boolean;paused:boolean;onOpen:(opener:HTMLButtonElement)=>void}) {
  const [owned,setOwned]=useState<{source:SourceClient;conversation:string;tab:number}|null>(null)
  const chip=useRef<HTMLSpanElement>(null)
  const suspended=useRef(paused);suspended.current=paused
  useEffect(()=>{
    setOwned(null)
    if(hidden||!botBrowserSupported(source))return
    const controller=new AbortController();let inFlight=false
    const read=async()=>{
      if(document.hidden||suspended.current||inFlight)return
      inFlight=true
      try {
        const status=await authorized(source,current,signal=>source.browser!<BotBrowserStatus>(bot.key,bot.conversation_id,"GET","/v1/browser/status",undefined,signal),controller.signal)
        if(!controller.signal.aborted){const tab=botOwnedBrowserTab(status);setOwned(tab===null?null:{source,conversation:bot.conversation_id,tab})}
      }catch{if(!controller.signal.aborted)setOwned(null)}
      finally{inFlight=false}
    }
    void read()
    const timer=window.setInterval(()=>void read(),15_000)
    document.addEventListener("visibilitychange",read)
    return()=>{controller.abort();window.clearInterval(timer);document.removeEventListener("visibilitychange",read)}
  },[source,current,bot.key.machine_id,bot.key.bot_id,bot.conversation_id,hidden])
  if(hidden||owned?.source!==source||owned?.conversation!==bot.conversation_id||!botBrowserSupported(source))return null
  return <span ref={chip}><Suspense fallback={null}><BrowserChip onOpen={()=>{
    const opener=chip.current?.querySelector("button")
    if(opener)onOpen(opener)
  }}/></Suspense></span>
}

/** The panel's authority is the same immutable source as the chat beside it. */
export function BotBrowser({source,current,bot,writable,reason,onClose,onStale}: {
  source:SourceClient;current:()=>readonly SourceClient[];bot:W.ProjectAssistantBot;
  writable:boolean;reason:string;onClose:()=>void;onStale:()=>Promise<void>;
}) {
  const [stale,setStale]=useState(false),[closing,setClosing]=useState(false),[exitError,setExitError]=useState("")
  const host=useRef<HTMLDivElement>(null)
  useLayoutEffect(()=>{host.current?.focus({preventScroll:true})},[])
  const stopped=useRef(false)
  const lifetime=useMemo(()=>new AbortController(),[source,current,bot.key.machine_id,bot.key.bot_id,bot.conversation_id])
  const handback=useMemo(()=>createBrowserHandback(<T,>(method:"GET"|"POST",path:string,body?:unknown)=>authorized(source,current,signal=>source.browser!<T>(bot.key,bot.conversation_id,method,path,body,signal),new AbortController().signal)),[source,current,bot.key.machine_id,bot.key.bot_id,bot.conversation_id])
  const currentHandback=useRef(handback);currentHandback.current=handback
  const exitFailure=(error:unknown)=>botBrowserWord("handbackFailed").replace("{code}",(error as {code?:string;error?:{code?:string}})?.code??(error as {error?:{code?:string}})?.error?.code??"browser_unavailable")
  const close=async()=>{
    if(closing)return
    if(!botBrowserSupported(source)){onClose();return}
    setClosing(true);setExitError("")
    try{await handback.exit();onClose()}catch(error){setExitError(exitFailure(error));setClosing(false)}
  }
  const reload=useRef(onStale);reload.current=onStale
  useEffect(()=>()=>{
    // StrictMode replays effects while this same panel is still connected.
    queueMicrotask(()=>{
      if(host.current?.isConnected&&currentHandback.current===handback)return
      if(!botBrowserSupported(source)){lifetime.abort();return}
      void handback.exit().catch(error=>toast(exitFailure(error),true)).finally(()=>lifetime.abort())
    })
  },[handback,lifetime])
  const call:BrowserCall=useCallback(async<T,>(method:"GET"|"POST",path:string,body?:unknown):Promise<T>=>{
    if(stopped.current||handback.closing())throw Object.assign(new Error("pa_stale_version"),{code:"pa_stale_version"})
    if(!source.browser)throw Object.assign(new Error("pa_unsupported"),{code:"pa_unsupported"})
    try {
      return await handback.track(method,path,()=>authorized(source,current,s=>source.browser!<T>(bot.key,bot.conversation_id,method,path,body,s),lifetime.signal))
    } catch(error) {
      const code=(error as {code?:string;error?:{code?:string}})?.code??(error as {error?:{code?:string}})?.error?.code
      if(code==="pa_stale_version") {
        stopped.current=true;setStale(true)
        // Reload the Bot's identity, then wait for another explicit panel open.
        void reload.current().catch(()=>{})
      }
      throw error
    }
  },[source,current,bot.key.machine_id,bot.key.bot_id,bot.conversation_id,handback,lifetime])
  const refusal=useCallback((error:unknown)=>{
    const value=error as {code?:string;error?:{code?:string}}
    const code=value?.code??value?.error?.code??"browser_unavailable"
    if(!code.startsWith("pa_"))return ""
    const key=code==="pa_outcome_unknown"?"unknown":code==="pa_stale_version"?"stale":code==="pa_unsupported"?"unsupported":code==="pa_unpaired"?"unpaired":code==="pa_read_only"?"readOnly":"refused"
    return botBrowserWord(key).replace("{code}",code)
  },[])
  const unavailable=source.browserUnavailable?.()??(!source.browser?"pa_unsupported":"")
  const target:BrowserPanelTarget=useMemo(()=>({kind:"bot",id:JSON.stringify([source.machineID,source.authorizationRevision,bot.key.bot_id]),conversation:bot.conversation_id,name:bot.name,
    remote:source.browserRemote??false,writable:writable&&!unavailable&&!stale&&!closing,reason:closing?botBrowserWord("handingBack"):stale?botBrowserWord("stale"):unavailable?refusal({code:unavailable}):reason,
    call,refusal}),[source,bot.key.bot_id,bot.conversation_id,bot.name,writable,unavailable,stale,closing,reason,call,refusal])
  return <div ref={host} className="pa-browser" role="dialog" aria-modal="true" tabIndex={-1} aria-label={bot.name+" · "+botBrowserWord("open")} onKeyDown={event=>{
    if(event.defaultPrevented)return
    if(event.key==="Escape"){event.preventDefault();event.stopPropagation();void close();return}
    if(event.key!=="Tab")return
    const nodes=[...host.current?.querySelectorAll<HTMLElement>('button:not(:disabled),input:not(:disabled),textarea:not(:disabled),summary,[tabindex="0"]')??[]].filter(node=>node.getClientRects().length>0)
    if(event.shiftKey&&document.activeElement===nodes[0]){event.preventDefault();nodes.at(-1)?.focus()}
    else if(!event.shiftKey&&document.activeElement===nodes.at(-1)){event.preventDefault();nodes[0]?.focus()}
  }}>{exitError&&<p className="pa-browser-exit-error" role="alert">{exitError}</p>}<Suspense fallback={<p role="status">{botBrowserWord("open")}…</p>}><BrowserPanel target={target} open onClose={()=>void close()}/></Suspense></div>
}
