import {test} from "node:test"
import assert from "node:assert/strict"
import {readFileSync} from "node:fs"
import type * as W from "@clawdline/contract"
import type {SourceClient} from "./client"
// @ts-expect-error -- Node executes source extensions.
import {emptyLiveRoster,applyLiveRoster,liveRosterItems,followLiveRoster,createRosterReconnect,type LiveRosterUpdate} from "./live-roster.ts"
// @ts-expect-error -- Node executes source extensions.
import {RosterStreamFailure} from "./roster-stream.ts"
// @ts-expect-error -- Node executes source extensions.
import {errorCode} from "./client.ts"

const request={machine_id:"machine"}
const status:W.ProjectAssistantSourceStatus={machine_id:"machine",state:"online",access:"history_only",readable:true,writable:false,fresh:true,observed_at:"",capabilities:[]}
const item=(id="bot"):W.ProjectAssistantBotSummary=>({
  bot:{key:{machine_id:"machine",bot_id:id},project:{machine_id:"machine",project_id:"project",canonical_id:"canonical",kind:"repository"},conversation_id:`chat/${id}`,name:`Bot ${id}`,agent:"codex",permission_policy:"ask",appearance:{shape:"blob",color:"#123456"},instructions:{text:"",revision:1,role_snapshot_ref:""},version:1,created_at:"2026-11-15T00:00:00Z",updated_at:"2026-11-15T00:00:00Z"},
  source:status,activity:"Read notes.md",activity_at:"2026-11-15T00:00:00Z",latest_message:"hello",unread_count:3,pending_permissions:0,version:1,status:"working",
  read_cursor:{key:{machine_id:"machine",bot_id:id},conversation_id:`chat/${id}`,reader_id:"source-reader",message_seq:0},
})
const hello:W.ProjectAssistantRosterFrame={type:"hello",source_epoch:"epoch"}
const row=(revision:number,summary=item()):W.ProjectAssistantRosterChange=>({revision,key:summary.bot.key,removed:false,summary})
const frame=(type:"snapshot"|"snapshot-complete"|"changes",cursor:string,changes:W.ProjectAssistantRosterChange[]=[]):W.ProjectAssistantRosterFrame=>({type,source_epoch:"epoch",cursor,changes})
const begin=()=>applyLiveRoster(emptyLiveRoster(),hello,request)

test("live roster replaces absolute facts and preserves hidden management while deleting tombstones",()=>{
  let state=applyLiveRoster(begin(),frame("snapshot","partial",[row(1)]),request)
  assert.equal(state.complete,false)
  state=applyLiveRoster(state,frame("snapshot-complete","ready"),request)
  assert.equal(state.complete,true)
  const waiting={...item(),status:"needsInput" as const,pending_permissions:1,activity:"Needs approval: Run unit tests",unread_count:4}
  state=applyLiveRoster(state,frame("changes","wait",[row(3,waiting),row(2)]),request)
  assert.equal(liveRosterItems(state)[0].activity,waiting.activity)
  state=applyLiveRoster(state,frame("changes","duplicate",[row(3,waiting)]),request)
  assert.equal(liveRosterItems(state)[0].unread_count,4)
  const hidden={...waiting,bot:{...waiting.bot,hidden:true}}
  state=applyLiveRoster(state,frame("changes","hidden",[{...row(4,hidden),removed:true}]),request)
  assert.equal(liveRosterItems(state)[0].bot.hidden,true)
  state=applyLiveRoster(state,frame("changes","deleted",[{key:item().bot.key,revision:5,removed:true}]),request)
  assert.deepEqual(liveRosterItems(state),[])
  state=applyLiveRoster(state,frame("changes","old-replay",[row(3,waiting)]),request)
  assert.deepEqual(liveRosterItems(state),[])
  state=applyLiveRoster(state,frame("changes","recreated",[row(6)]),request)
  assert.equal(liveRosterItems(state).length,1)
})

test("hello and terminal controls never establish completeness or advance an applied cursor",()=>{
  const state=begin()
  assert.equal(state.cursor,"")
  assert.equal(state.complete,false)
  assert.throws(()=>applyLiveRoster(state,frame("changes","invalid",[row(1)]),request),e=>errorCode(e)==="pa_cursor_expired")
  const complete=applyLiveRoster(state,frame("snapshot-complete","old",[row(1)]),request)
  assert.throws(()=>applyLiveRoster({...complete,hello:false},{...hello,source_epoch:"restart"},request),e=>errorCode(e)==="pa_cursor_expired")
  for(const type of ["closure","resync"] as const)assert.throws(()=>applyLiveRoster(complete,{type,failure:{code:"pa_cursor_expired",field:"stream",retry_after_seconds:0}},request),e=>e instanceof RosterStreamFailure&&e.kind===type)
  assert.equal(complete.cursor,"old")
  const foreign=row(2,item("foreign"));foreign.key={...foreign.key,machine_id:"other"}
  assert.throws(()=>applyLiveRoster(complete,frame("changes","bad",[row(2),foreign]),request))
  assert.equal(complete.cursor,"old")
  assert.equal(liveRosterItems(complete)[0].unread_count,3)
})

test("live cache bounds removal history and a fresh complete snapshot reclaims it",()=>{
  const registered=JSON.parse(readFileSync(new URL("../../../../internal/domain/capacity/testdata/register.golden.json",import.meta.url),"utf8"))
  const limit=registered.find((row:{Name:string})=>row.Name==="bots.roster_client_rows").Limit as number
  let state=applyLiveRoster(begin(),frame("snapshot-complete","empty"),request)
  for(let n=0;n<limit;n+=100)state=applyLiveRoster(state,frame("changes",`batch-${n}`,Array.from({length:Math.min(100,limit-n)},(_,i)=>({revision:n+i,key:{machine_id:"machine",bot_id:`deleted-${n+i}`},removed:true}))),request)
  assert.equal(state.records.size,limit)
  const cursor=state.cursor
  assert.throws(()=>applyLiveRoster(state,frame("changes","overflow",[{revision:limit+1,key:{machine_id:"machine",bot_id:"extra"},removed:true}]),request),e=>e instanceof RosterStreamFailure&&e.kind==="capacity")
  assert.equal(state.cursor,cursor)
  state=applyLiveRoster(state,frame("snapshot-complete","new-empty"),request)
  assert.equal(state.records.size,0)
})

test("follower resumes only its applied complete cursor and never polls List",async()=>{
  const control=new AbortController(),updates:LiveRosterUpdate[]=[],cursors:string[]=[],source={machineID:"machine",authorizationRevision:"grant",current:()=>true,
    status:async()=>({contract_version:"project-assistants.v1",sources:[status]}),
    list:()=>{assert.fail("live roster must not poll List")},
    subscribeRoster:async(q:W.ProjectAssistantRosterRequest,receive:(f:W.ProjectAssistantRosterFrame)=>void)=>{
      cursors.push(q.cursor??"");receive(hello)
      if(cursors.length===1){receive(frame("snapshot-complete","applied",[row(1)]));throw new RosterStreamFailure("eof")}
      receive(frame("changes","caught-up",[row(2,{...item(),status:"idle",activity:"",unread_count:0})]));control.abort()
    },
  } as unknown as SourceClient
  await followLiveRoster(source,()=>[source],control.signal,3000,value=>updates.push(value),async()=>{})
  assert.deepEqual(cursors,["","applied"])
  assert.equal(updates.filter(u=>u.items).length,2)
  assert.equal(updates.at(-1)!.items![0].status,"idle")
  assert.equal(updates.at(-1)!.items![0].unread_count,0)
  assert.ok(updates.some(u=>u.error==="pa_offline"&&u.statuses?.[0].fresh===false))
})

test("an incomplete snapshot restarts fresh and revocation clears its rows without retry",async()=>{
  const control=new AbortController(),updates:LiveRosterUpdate[]=[],cursors:string[]=[],source={machineID:"machine",authorizationRevision:"grant",current:()=>true,
    status:async()=>({contract_version:"project-assistants.v1",sources:[status]}),
    subscribeRoster:async(q:W.ProjectAssistantRosterRequest,receive:(f:W.ProjectAssistantRosterFrame)=>void)=>{
      cursors.push(q.cursor??"");receive(hello)
      if(cursors.length===1){receive(frame("snapshot","partial",[row(1)]));throw new RosterStreamFailure("eof")}
      receive(frame("snapshot-complete","ready",[row(2)]))
      throw {contract_version:"project-assistants.v1",error:{code:"pa_revoked",field:"grant",retry_after_seconds:0}}
    },
  } as unknown as SourceClient
  await followLiveRoster(source,()=>[source],control.signal,3000,value=>updates.push(value),async()=>{})
  assert.deepEqual(cursors,["",""])
  assert.equal(updates.filter(u=>u.items?.length).length,1)
  assert.deepEqual(updates.at(-1)!.items,[])
  assert.equal(updates.at(-1)!.error,"pa_revoked")
  assert.equal(updates.at(-1)!.retryable,false)
})


test("manual reconnect wakes one existing wait and cannot start parallel work",async t=>{
  t.mock.timers.enable({apis:["setTimeout","Date"]})
  const reconnect=createRosterReconnect(),control=new AbortController()
  reconnect.retry() // No read or stream is created while there is no wait.
  let resumed=0
  const waiting=reconnect.wait(control.signal,3000).then(()=>resumed++)
  reconnect.retry();reconnect.retry();await waiting
  assert.equal(resumed,1)
  t.mock.timers.tick(3000);await Promise.resolve();assert.equal(resumed,1)
  reconnect.retry();assert.equal(resumed,1)
  const cancelled=reconnect.wait(control.signal,3000).then(()=>resumed++)
  control.abort();await cancelled;assert.equal(resumed,2)
})

test("manual reconnect respects the server cooldown and timers cannot overflow",async t=>{
  t.mock.timers.enable({apis:["setTimeout","Date"]})
  const reconnect=createRosterReconnect(),control=new AbortController()
  let finished=false
  const waiting=reconnect.wait(control.signal,3000,5000).then(()=>{finished=true})
  reconnect.retry();t.mock.timers.tick(4999);await Promise.resolve();assert.equal(finished,false)
  t.mock.timers.tick(1);await waiting;assert.equal(finished,true)
  finished=false
  const long=reconnect.wait(control.signal,3000,Number.MAX_SAFE_INTEGER).then(()=>{finished=true})
  reconnect.retry();t.mock.timers.tick(3000);await Promise.resolve();assert.equal(finished,false)
  control.abort();await long
})

test("a transient status failure retries serially and reports pending until recovery",async()=>{
  const control=new AbortController(),updates:LiveRosterUpdate[]=[],waits:number[][]=[]
  let attempts=0
  const source={machineID:"machine",authorizationRevision:"grant",current:()=>true,
    status:async()=>{if(++attempts===1)throw {error:{code:"pa_offline",retry_after_seconds:5}};return {sources:[status]}},
    subscribeRoster:async(_q:W.ProjectAssistantRosterRequest,receive:(f:W.ProjectAssistantRosterFrame)=>void)=>{
      receive(hello);receive(frame("snapshot-complete","ready",[row(1)]));control.abort()
    },
  } as unknown as SourceClient
  await followLiveRoster(source,()=>[source],control.signal,3000,value=>updates.push(value),async(_signal,ms,minimum)=>{waits.push([ms,minimum??0])})
  assert.deepEqual(waits,[[3000,5000]])
  assert.deepEqual(updates.map(u=>[u.error,u.pending]),[["",true],["pa_offline",false],["pa_offline",true],["",false]])
  assert.equal(attempts,2)
})

test("an oversized fresh snapshot stops and does not promise automatic retry",async()=>{
  const control=new AbortController(),updates:LiveRosterUpdate[]=[]
  const source={machineID:"machine",authorizationRevision:"grant",status:async()=>({sources:[status]}),
    subscribeRoster:async()=>{throw new RosterStreamFailure("capacity")},
  } as unknown as SourceClient
  await followLiveRoster(source,()=>[source],control.signal,3000,value=>updates.push(value),async()=>assert.fail("permanent capacity refusal cannot reconnect"))
  assert.equal(updates.at(-1)!.error,"pa_capacity_full")
  assert.equal(updates.at(-1)!.retryable,false)
})

test("a cold resync loads one paged roster without trusting partial rows or inventing a stream cursor",async()=>{
  const control=new AbortController(),updates:LiveRosterUpdate[]=[],streamCursors:string[]=[],listCursors:string[]=[]
  const source={machineID:"machine",authorizationRevision:"grant",current:()=>true,
    status:async()=>({sources:[status]}),
    list:async(q:W.ProjectAssistantListRequest)=>{
      assert.equal(q.machine_id,"machine")
      assert.equal(q.query,"")
      listCursors.push(q.cursor)
      return {contract_version:"project-assistants.v1",sources:[status],items:[item(q.cursor?"second":"first")],next_cursor:q.cursor?"":"second-page"}
    },
    subscribeRoster:async(q:W.ProjectAssistantRosterRequest,receive:(f:W.ProjectAssistantRosterFrame)=>void)=>{
      streamCursors.push(q.cursor??"");receive(hello)
      if(streamCursors.length<3){
        receive(frame("snapshot","partial",[row(1,item("stream-partial"))]))
        receive({type:"resync",failure:{code:"pa_cursor_expired",field:"roster_stream",retry_after_seconds:0}})
      }
      receive(frame("snapshot-complete","live",[row(2,item("live"))]));control.abort()
    },
  } as unknown as SourceClient
  await followLiveRoster(source,()=>[source],control.signal,3000,value=>updates.push(value),async()=>{})
  assert.deepEqual(streamCursors,["","",""])
  assert.deepEqual(listCursors,["","second-page"])
  const recovered=updates.filter(u=>u.items?.length)
  assert.deepEqual(recovered.map(u=>u.items!.map(i=>i.bot.key.bot_id).sort()),[["first","second"],["live"]])
  assert.ok(recovered[0].items?.every(i=>!i.source.fresh&&!i.source.writable))
  assert.equal(recovered[0].error,"pa_cursor_expired")
  assert.equal(recovered[1].items?.[0].source.fresh,true)
})

test("a typed refusal during cold roster recovery ends the read and clears authority",async()=>{
  const updates:LiveRosterUpdate[]=[]
  let lists=0
  const source={machineID:"machine",authorizationRevision:"grant",current:()=>true,
    status:async()=>({sources:[status]}),
    list:async()=>{
      if(++lists===1)return {contract_version:"project-assistants.v1",sources:[status],items:[item()],next_cursor:"second-page"}
      throw new RosterStreamFailure("closure",{code:"pa_revoked",field:"grant",retry_after_seconds:0})
    },
    subscribeRoster:async()=>{throw new RosterStreamFailure("eof")},
  } as unknown as SourceClient
  await followLiveRoster(source,()=>[source],new AbortController().signal,3000,value=>updates.push(value),async()=>assert.fail("revoked roster cannot reconnect"))
  assert.equal(lists,2)
  assert.deepEqual(updates.filter(u=>u.items).map(u=>u.items),[[]])
  assert.deepEqual(updates.at(-1)?.statuses,[])
  assert.equal(updates.at(-1)?.error,"pa_revoked")
  assert.equal(updates.at(-1)?.retryable,false)
})
