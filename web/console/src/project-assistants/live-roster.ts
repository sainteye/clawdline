import type * as W from "@clawdline/contract"
import type { SourceClient } from "./client"
// @ts-expect-error -- Node's strip-types runner uses source extensions.
import { authorized, errorCode, failure } from "./client.ts"
// @ts-expect-error -- Node's strip-types runner uses source extensions.
import { validateRosterFrame, RosterStreamFailure } from "./roster-stream.ts"
// @ts-expect-error -- Node's strip-types runner uses source extensions.
import { botIdentity, canRead, mergeRoster } from "./view-state.ts"

// bots.roster_client_rows: includes removal revisions, so replay cannot
// resurrect deleted rows and a long-lived connection cannot grow forever.
export const LiveRosterRows = 1000
export interface LiveRoster {
  epoch: string
  cursor: string
  complete: boolean
  hello: boolean
  snapshotting: boolean
  records: ReadonlyMap<string,W.ProjectAssistantRosterChange>
}
export const emptyLiveRoster = ():LiveRoster => ({epoch:"",cursor:"",complete:false,hello:false,snapshotting:false,records:new Map()})

/** Apply atomically. A cursor belongs to these facts, never to hello or EOF. */
export function applyLiveRoster(old:LiveRoster,value:unknown,request:W.ProjectAssistantRosterRequest):LiveRoster {
  const frame=validateRosterFrame(value,request)
  if(frame.type==="resync"||frame.type==="closure")throw new RosterStreamFailure(frame.type,frame.failure)
  if(frame.type==="hello") {
    if(old.hello)throw failure("pa_invalid")
    if(old.cursor&&old.epoch!==frame.source_epoch)throw failure("pa_cursor_expired")
    return {...old,epoch:frame.source_epoch!,hello:true}
  }
  if(!old.hello||old.epoch!==frame.source_epoch)throw failure("pa_cursor_expired")
  const snapshot=frame.type==="snapshot"||frame.type==="snapshot-complete"
  if(!snapshot&&!old.complete)throw failure("pa_cursor_expired")
  const records=new Map<string,W.ProjectAssistantRosterChange>(snapshot&&!old.snapshotting?[]:old.records)
  for(const change of frame.changes??[]) {
    const key=botIdentity(change.key),previous=records.get(key)
    if(previous&&previous.revision>change.revision)continue
    if(!previous&&records.size>=LiveRosterRows)throw new RosterStreamFailure("capacity")
    records.set(key,change)
  }
  return {epoch:old.epoch,cursor:frame.cursor!,complete:frame.type==="snapshot-complete"||!snapshot&&old.complete,hello:true,snapshotting:frame.type==="snapshot",records}
}

export function liveRosterItems(state:LiveRoster):W.ProjectAssistantBotSummary[] {
  return mergeRoster([[...state.records.values()].flatMap(row=>row.summary&&(!row.removed||row.summary.bot.hidden)?[row.summary]:[])])
}

export interface LiveRosterUpdate {
  items?: W.ProjectAssistantBotSummary[]
  statuses?: W.ProjectAssistantSourceStatus[]
  error: string
  pending: boolean
}
function reconnectWait(signal:AbortSignal,ms:number):Promise<void> {
  return new Promise(resolve=>{
    if(signal.aborted){resolve();return}
    const finish=()=>{clearTimeout(timer);signal.removeEventListener("abort",finish);resolve()}
    const timer=setTimeout(finish,ms)
    signal.addEventListener("abort",finish,{once:true})
  })
}

/** One serial subscription per source. Timers reconnect; they never list Bots. */
export async function followLiveRoster(source:SourceClient,current:()=>readonly SourceClient[],signal:AbortSignal,retryMs:number,update:(value:LiveRosterUpdate)=>void,wait=reconnectWait):Promise<void> {
  if(!source.subscribeRoster)return
  const revision=source.authorizationRevision,request={machine_id:source.machineID}
  const held=()=>!signal.aborted&&source.authorizationRevision===revision&&current().some(s=>s===source)
  const fence=()=>{if(!held()||source.current?.()===false)throw failure("pa_revoked")}
  let state=emptyLiveRoster(),statuses:W.ProjectAssistantSourceStatus[]=[]
  update({error:"",pending:true})
  while(held()) {
    try {
      const answer=await authorized(source,current,s=>source.status(s),signal)
      fence()
      statuses=answer.sources.filter(s=>s.machine_id===source.machineID)
      const own=statuses[0]
      if(!own||!canRead(own)) {update({items:[],statuses,error:"",pending:false});return}
      if(!state.complete)state=emptyLiveRoster()
      state={...state,hello:false,snapshotting:false}
      await source.subscribeRoster({...request,cursor:state.complete?state.cursor:""},frame=>{
        fence()
        state=applyLiveRoster(state,frame,request)
        if(state.complete&&frame.type!=="hello") {
          const items=liveRosterItems(state),observed=frame.changes?.find(c=>c.summary)?.summary?.source
          if(observed)statuses=[observed]
          update({items,statuses,error:"",pending:false})
        }
      },signal)
      throw failure("pa_offline")
    }catch(error) {
      if(signal.aborted||!held())return
      const code=errorCode(error)
      const denied=code==="pa_revoked"||code==="pa_scope_denied"||code==="pa_unpaired"
      update({...(denied?{items:[]}:{}),statuses:statuses.map(s=>({...s,fresh:false,writable:false})),error:code,pending:false})
      if(denied||code==="pa_invalid"||code==="pa_unsupported"||code==="pa_disabled")return
      // A fresh snapshot still exceeding the cache budget needs intervention;
      // a replay full of removal markers gets one fresh snapshot to reclaim it.
      if(error instanceof RosterStreamFailure&&error.kind==="capacity"&&!state.complete)return
      if(code==="pa_cursor_expired"||code==="pa_capacity_full"||!state.complete)state=emptyLiveRoster()
      if(source.current?.()===false)return
      await wait(signal,retryMs)
    }
  }
}
