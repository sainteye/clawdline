import type * as W from "@clawdline/contract"
import type {SourceClient} from "./client"
// @ts-expect-error -- Node's strip-types runner uses source extensions.
import {authorized, failure} from "./client.ts"

/** Refresh the loaded page coverage using fresh keyset cursors, then replace atomically. */
export async function readSnapshotRoster(source:SourceClient,current:()=>readonly SourceClient[],query:string,pages:number,signal:AbortSignal) {
  return authorized(source,current,async s=>{
    const status=await source.status(s)
    let cursor="",loadedPages=0
    const items:W.ProjectAssistantBotSummary[]=[]
    do {
      const response=await source.list({machine_id:source.machineID,query,cursor,page_size:100},s)
      if(response.next_cursor&&response.next_cursor===cursor)throw failure("pa_invalid")
      items.push(...response.items);cursor=response.next_cursor;loadedPages++
    }while(cursor&&loadedPages<pages)
    return {status,items,cursor,loadedPages}
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

/** Use the existing conversation cadence only while this roster is foreground. */
export function followSnapshotRoster(load:()=>Promise<void>,signal:AbortSignal,refreshMs:number,document:EventTarget&{visibilityState:string},window:EventTarget) {
  let timer:ReturnType<typeof setTimeout>|undefined,generation=0,paused=document.visibilityState==="hidden"
  const clear=()=>{if(timer)clearTimeout(timer);timer=undefined}
  const refresh=async()=>{
    const attempt=++generation;clear()
    if(signal.aborted||paused)return
    await load()
    if(!signal.aborted&&!paused&&attempt===generation)timer=setTimeout(()=>void refresh(),refreshMs)
  }
  const hide=()=>{paused=true;generation++;clear()}
  const visibility=()=>{paused=document.visibilityState==="hidden";if(paused)hide();else void refresh()}
  const show=()=>visibility()
  const stop=()=>{hide();document.removeEventListener("visibilitychange",visibility);window.removeEventListener("pagehide",hide);window.removeEventListener("pageshow",show)}
  if(signal.aborted)return
  document.addEventListener("visibilitychange",visibility);window.addEventListener("pagehide",hide);window.addEventListener("pageshow",show)
  signal.addEventListener("abort",stop,{once:true})
  void refresh()
}
