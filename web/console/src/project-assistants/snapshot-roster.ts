import type * as W from "@clawdline/contract"
import type {RosterFailure, SourceClient} from "./client"
// @ts-expect-error -- Node's strip-types runner uses source extensions.
import {authorized, failure} from "./client.ts"

/** Refresh the loaded page coverage using fresh keyset cursors, then replace atomically. */
export async function readSnapshotRoster(source:SourceClient,current:()=>readonly SourceClient[],query:string,pages:number,signal:AbortSignal,cursor="",onFailure?:(evidence:RosterFailure)=>void) {
  return authorized(source,current,async s=>{
    let sources:W.ProjectAssistantSourceStatus[]=[],loadedPages=0
    const items:W.ProjectAssistantBotSummary[]=[]
    do {
      const response=await source.list({machine_id:source.machineID,query,cursor,page_size:100},s,onFailure)
      sources=response.sources
      if(response.next_cursor&&response.next_cursor===cursor)throw failure("pa_invalid")
      items.push(...response.items);cursor=response.next_cursor;loadedPages++
    }while(cursor&&loadedPages<pages)
    return {sources,items,cursor,loadedPages}
  },signal)
}

/** Coalesce demand during a slow read; a timer never aborts an earlier read. */
export function serialSnapshotRefresh(run:(append:boolean)=>Promise<void>,signal:AbortSignal) {
  let active:Promise<void>|undefined,pending=false,more=false
  return (append=false):Promise<void>=>{
    if(signal.aborted)return Promise.resolve()
    pending=true;more ||= append
    if(!active)active=(async()=>{
      while(pending&&!signal.aborted){const append=more;pending=false;more=false;await run(append)}
    })().finally(()=>{active=undefined})
    return active
  }
}

/** Idle reads follow visibility. An accepted operation may retain observation. */
export function followSnapshotRoster(load:()=>Promise<void>,signal:AbortSignal,refreshMs:number,document:EventTarget&{visibilityState:string},window:EventTarget,{readOnEntry=false,keepReading}:{readOnEntry?:boolean;keepReading?:()=>boolean}={}) {
  let timer:ReturnType<typeof setTimeout>|undefined,generation=0,paused=document.visibilityState==="hidden",suspended=false
  const clear=()=>{if(timer)clearTimeout(timer);timer=undefined}
  const eligible=()=>!suspended&&(!paused||keepReading?.()===true)
  const refresh=async(force=false)=>{
    const attempt=++generation;clear()
    if(signal.aborted||suspended||!force&&!eligible())return
    await load()
    if(!signal.aborted&&eligible()&&attempt===generation)timer=setTimeout(()=>void refresh(),refreshMs)
  }
  const hide=()=>{suspended=true;generation++;clear()}
  const visibility=()=>{paused=document.visibilityState==="hidden";generation++;clear();if(eligible())void refresh()}
  const show=()=>{suspended=false;visibility()}
  const stop=()=>{hide();document.removeEventListener("visibilitychange",visibility);window.removeEventListener("pagehide",hide);window.removeEventListener("pageshow",show)}
  if(signal.aborted)return
  document.addEventListener("visibilitychange",visibility);window.addEventListener("pagehide",hide);window.addEventListener("pageshow",show)
  signal.addEventListener("abort",stop,{once:true})
  // Explicit entry/action reads once even in a remote browser's hidden tab.
  // Only an operation still awaiting observation may continue hidden reads.
  void refresh(readOnEntry)
  return ()=>refresh(true)
}
