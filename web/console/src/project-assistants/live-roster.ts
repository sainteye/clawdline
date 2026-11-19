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
  retryable?: boolean
}
/** Manual retry only wakes the serial reconnect wait; it never replaces an active read. */
export function createRosterReconnect() {
  let wake:(()=>void)|undefined
  return {
    retry:()=>wake?.(),
    wait:(signal:AbortSignal,ms:number,minimumMs=0):Promise<void>=>new Promise(resolve=>{
      if(signal.aborted){resolve();return}
      const earliest=Date.now()+minimumMs
      let deadline=Date.now()+Math.max(ms,minimumMs),timer:ReturnType<typeof setTimeout>
      const finish=()=>{clearTimeout(timer);wake=undefined;signal.removeEventListener("abort",finish);resolve()}
      // Keep each timer within the existing cadence, even for a long server cooldown.
      const schedule=()=>{const left=deadline-Date.now();if(left<=0)finish();else timer=setTimeout(schedule,Math.min(ms,left))}
      wake=()=>{clearTimeout(timer);deadline=earliest;schedule()}
      signal.addEventListener("abort",finish,{once:true})
      schedule()
    }),
  }
}

async function readInitialRosterFallback(source:SourceClient,current:()=>readonly SourceClient[],signal:AbortSignal):Promise<W.ProjectAssistantBotSummary[]> {
  return authorized(source,current,async s=>{
    const pageSize=100,items:W.ProjectAssistantBotSummary[]=[],seen=new Set<string>()
    let cursor=""
    do {
      const answer=await source.list({machine_id:source.machineID,cursor,query:"",page_size:pageSize},s)
      if(answer.contract_version!=="project-assistants.v1"||!Array.isArray(answer.sources)||!Array.isArray(answer.items)||
        answer.items.length>pageSize||typeof answer.next_cursor!=="string")throw failure("pa_invalid")
      const own=answer.sources.find(status=>status.machine_id===source.machineID)
      if(own?.state==="revoked")throw failure("pa_revoked")
      if(own?.state==="unpaired")throw failure("pa_unpaired")
      if(!own||!canRead(own))throw failure("pa_scope_denied")
      if(own.state!=="online"||!own.fresh)throw failure("pa_offline")
      for(const item of answer.items) {
        if(item.bot.key.machine_id!==source.machineID||item.bot.project.machine_id!==source.machineID||
          item.source.machine_id!==source.machineID||!canRead(item.source))throw failure("pa_scope_denied")
        items.push({...item,source:{...own,fresh:false,writable:false}})
      }
      cursor=answer.next_cursor
      if(cursor&&(!answer.items.length||seen.has(cursor)))throw failure("pa_invalid")
      if(cursor&&seen.size+1>=LiveRosterRows/pageSize)throw failure("pa_capacity_full")
      seen.add(cursor)
    }while(cursor&&!s.aborted)
    return mergeRoster([items])
  },signal)
}

const rosterAuthorityLost=(code:string)=>["pa_revoked","pa_scope_denied","pa_unpaired","pa_disabled"].includes(code)

/** One serial subscription per source. A failed initial stream may recover via one List pass. */
export async function followLiveRoster(source:SourceClient,current:()=>readonly SourceClient[],signal:AbortSignal,retryMs:number,update:(value:LiveRosterUpdate)=>void,wait=createRosterReconnect().wait):Promise<void> {
  if(!source.subscribeRoster)return
  const revision=source.authorizationRevision,request={machine_id:source.machineID}
  const held=()=>!signal.aborted&&source.authorizationRevision===revision&&current().some(s=>s===source)
  const fence=()=>{if(!held()||source.current?.()===false)throw failure("pa_revoked")}
  let state=emptyLiveRoster(),statuses:W.ProjectAssistantSourceStatus[]=[],lastError="",initialReadAvailable=true
  while(held()) {
    update({error:lastError,pending:true})
    let readingStream=false
    try {
      const answer=await authorized(source,current,s=>source.status(s),signal)
      fence()
      statuses=answer.sources.filter(s=>s.machine_id===source.machineID)
      const own=statuses[0]
      if(!own||!canRead(own)) {update({items:[],statuses,error:"",pending:false});return}
      if(!state.complete)state=emptyLiveRoster()
      state={...state,hello:false,snapshotting:false}
      readingStream=true
      await source.subscribeRoster({...request,cursor:state.complete?state.cursor:""},frame=>{
        fence()
        state=applyLiveRoster(state,frame,request)
        if(state.complete&&frame.type!=="hello") {
          initialReadAvailable=false
          const items=liveRosterItems(state),observed=frame.changes?.find(c=>c.summary)?.summary?.source
          if(observed)statuses=[observed]
          lastError=""
          update({items,statuses,error:"",pending:false})
        }
      },signal)
      throw failure("pa_offline")
    }catch(error) {
      if(signal.aborted||!held())return
      let code=errorCode(error)
      let fallbackItems:W.ProjectAssistantBotSummary[]|undefined
      if(initialReadAvailable&&readingStream&&(code==="pa_cursor_expired"||code==="pa_offline")) {
        initialReadAvailable=false
        try {
          const items=await readInitialRosterFallback(source,current,signal)
          fence()
          fallbackItems=items
        }catch(fallbackError) {
          if(!held())return
          const refusal=errorCode(fallbackError)
          if(rosterAuthorityLost(refusal))code=refusal
        }
      }
      lastError=code
      const denied=rosterAuthorityLost(code)
      // A fresh oversized snapshot requires intervention; replay tombstones get one reset.
      const stopped=denied||code==="pa_invalid"||code==="pa_unsupported"||error instanceof RosterStreamFailure&&error.kind==="capacity"&&!state.complete||source.current?.()===false
      const items=denied?[]:fallbackItems
      update({items,statuses:denied?[]:statuses.map(s=>({...s,fresh:false,writable:false})),error:code,pending:false,retryable:!stopped})
      if(stopped)return
      if(code==="pa_cursor_expired"||code==="pa_capacity_full"||!state.complete)state=emptyLiveRoster()
      if(source.current?.()===false)return
      const after=(error as {error?:{retry_after_seconds?:number}})?.error?.retry_after_seconds
      const minimumMs=typeof after==="number"&&Number.isFinite(after)&&after>0?after*1000:0
      await wait(signal,retryMs,minimumMs)
    }
  }
}
