import { lazy, Suspense, useCallback, useEffect, useMemo, useRef, useState } from "react"
import type * as W from "@clawdline/contract"
import { catalogWord } from "../catalog"
import type { BrowserCall, BrowserPanelTarget } from "../session/browser-call"
import { authorized, type SourceClient } from "./client"

export const botBrowserWord = (key: string) => catalogWord("botBrowser", key)
// Session transport setup is loaded only when the person opens the panel.
const BrowserPanel=lazy(()=>import("../session/BrowserPanel").then(module=>({default:module.BrowserPanel})))

/** The panel's authority is the same immutable source as the chat beside it. */
export function BotBrowser({source,current,bot,writable,reason,onClose,onStale}: {
  source:SourceClient;current:()=>readonly SourceClient[];bot:W.ProjectAssistantBot;
  writable:boolean;reason:string;onClose:()=>void;onStale:()=>Promise<void>;
}) {
  const [stale,setStale]=useState(false)
  const host=useRef<HTMLDivElement>(null)
  const stopped=useRef(false),lifetime=useRef(new AbortController())
  const reload=useRef(onStale);reload.current=onStale
  useEffect(()=>()=>lifetime.current.abort(),[])
  useEffect(()=>{
    const opener=host.current?.closest(".pa-chat")?.querySelector<HTMLElement>('.pa-chat-header button[aria-haspopup="menu"]')
    return()=>{if(opener?.isConnected)opener.focus({preventScroll:true})}
  },[])
  const call:BrowserCall=useCallback(async<T,>(method:"GET"|"POST",path:string,body?:unknown):Promise<T>=>{
    if(stopped.current)throw Object.assign(new Error("pa_stale_version"),{code:"pa_stale_version"})
    if(!source.browser)throw Object.assign(new Error("pa_unsupported"),{code:"pa_unsupported"})
    try {
      return await authorized(source,current,s=>source.browser!<T>(bot.key,bot.conversation_id,method,path,body,s),lifetime.current.signal)
    } catch(error) {
      const code=(error as {code?:string;error?:{code?:string}})?.code??(error as {error?:{code?:string}})?.error?.code
      if(code==="pa_stale_version") {
        stopped.current=true;setStale(true)
        // Reload the Bot's identity, then wait for another explicit panel open.
        void reload.current().catch(()=>{})
      }
      throw error
    }
  },[source,current,bot.key.machine_id,bot.key.bot_id,bot.conversation_id])
  const refusal=useCallback((error:unknown)=>{
    const value=error as {code?:string;error?:{code?:string}}
    const code=value?.code??value?.error?.code??"browser_unavailable"
    if(!code.startsWith("pa_"))return ""
    const key=code==="pa_outcome_unknown"?"unknown":code==="pa_stale_version"?"stale":code==="pa_unsupported"?"unsupported":code==="pa_unpaired"?"unpaired":code==="pa_read_only"?"readOnly":"refused"
    return botBrowserWord(key).replace("{code}",code)
  },[])
  const unavailable=source.browserUnavailable?.()??(!source.browser?"pa_unsupported":"")
  const target:BrowserPanelTarget=useMemo(()=>({kind:"bot",id:JSON.stringify([source.machineID,source.authorizationRevision,bot.key.bot_id]),conversation:bot.conversation_id,name:bot.name,
    remote:source.browserRemote??false,writable:writable&&!unavailable&&!stale,reason:stale?botBrowserWord("stale"):unavailable?refusal({code:unavailable}):reason,
    call,refusal}),[source,bot.key.bot_id,bot.conversation_id,bot.name,writable,unavailable,stale,reason,call,refusal])
  return <div ref={host} className="pa-browser" role="dialog" aria-modal="true" aria-label={bot.name+" · "+botBrowserWord("open")} onKeyDown={event=>{
    if(event.defaultPrevented)return
    if(event.key==="Escape"){event.preventDefault();event.stopPropagation();onClose();return}
    if(event.key!=="Tab")return
    const nodes=[...host.current?.querySelectorAll<HTMLElement>('button:not(:disabled),input:not(:disabled),textarea:not(:disabled),summary,[tabindex="0"]')??[]].filter(node=>node.getClientRects().length>0)
    if(event.shiftKey&&document.activeElement===nodes[0]){event.preventDefault();nodes.at(-1)?.focus()}
    else if(!event.shiftKey&&document.activeElement===nodes.at(-1)){event.preventDefault();nodes[0]?.focus()}
  }}><Suspense fallback={<p role="status">{botBrowserWord("open")}…</p>}><BrowserPanel target={target} open onClose={onClose}/></Suspense></div>
}
