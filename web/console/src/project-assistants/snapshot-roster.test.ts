import {test} from "node:test"
import assert from "node:assert/strict"
import type {SourceClient} from "./client"
// @ts-expect-error -- Node executes source extensions.
import {readSnapshotRoster,serialSnapshotRefresh,followSnapshotRoster} from "./snapshot-roster.ts"
// @ts-expect-error -- Node executes source extensions.
import {failure,errorCode} from "./client.ts"

test("snapshot refresh replaces only loaded coverage using fresh query cursors",async()=>{
  const calls:unknown[]=[],control=new AbortController()
  const source={machineID:"m",authorizationRevision:"a",status:async()=>({sources:[]}),list:async(q:{cursor:string;query:string})=>{calls.push(q);return {items:[{id:q.cursor||"replacement"}],next_cursor:q.cursor?"unloaded":"new-second"}}} as unknown as SourceClient
  const result=await readSnapshotRoster(source,()=>[source],"search",2,control.signal)
  assert.deepEqual(result.items,[{id:"replacement"},{id:"new-second"}])
  assert.equal(result.cursor,"unloaded");assert.equal(result.loadedPages,2)
  assert.deepEqual(calls.map((q:any)=>[q.query,q.cursor]),[["search",""],["search","new-second"]])
})
test("deletion shrinks a loaded range without retaining deleted ghosts",async()=>{
  let calls=0
  const source={machineID:"m",authorizationRevision:"a",status:async()=>({sources:[]}),list:async()=>{calls++;return {items:[],next_cursor:""}}} as unknown as SourceClient
  const result=await readSnapshotRoster(source,()=>[source],"",3,new AbortController().signal)
  assert.deepEqual(result.items,[]);assert.equal(calls,1);assert.equal(result.loadedPages,1)
})
test("slow reads are single flight and demand coalesces without aborting progress",async()=>{
  let release!:()=>void,runs=0,active=0,max=0
  const control=new AbortController()
  const load=serialSnapshotRefresh(async()=>{runs++;active++;max=Math.max(max,active);if(runs===1)await new Promise<void>(r=>release=r);active--},control.signal)
  const first=load();const again=load();load();assert.equal(runs,1)
  release();await Promise.all([first,again]);assert.equal(runs,2);assert.equal(max,1)
  control.abort();await load();assert.equal(runs,2)
})
test("late query cancellation and authority replacement reject old snapshots",async()=>{
  for(const revoke of [false,true]) {
    const control=new AbortController();let release!:()=>void,current:SourceClient[]=[]
    const source={machineID:"m",authorizationRevision:"a",status:async()=>({sources:[]}),list:async()=>{await new Promise<void>(r=>release=r);return {items:[{id:"old"}],next_cursor:""}}} as unknown as SourceClient
    current=[source]
    const pending=readSnapshotRoster(source,()=>current,"old-query",1,control.signal)
    await Promise.resolve();await Promise.resolve()
    if(revoke)current=[];else control.abort()
    release()
    await assert.rejects(pending,e=>revoke?errorCode(e)==="pa_revoked":(e as Error).name==="AbortError")
  }
})
test("read failures are surfaced without a manufactured empty success",async()=>{
  const source={machineID:"m",authorizationRevision:"a",status:async()=>{throw failure("pa_revoked")}} as unknown as SourceClient
  await assert.rejects(readSnapshotRoster(source,()=>[source],"",1,new AbortController().signal),e=>errorCode(e)==="pa_revoked")
})

test("foreground return catches up immediately, pagehide pauses and unmount stops",async()=>{
  const document=Object.assign(new EventTarget(),{visibilityState:"hidden"}),window=new EventTarget(),control=new AbortController()
  let runs=0
  followSnapshotRoster(async()=>{runs++},control.signal,5,document,window)
  assert.equal(runs,0)
  document.visibilityState="visible";document.dispatchEvent(new Event("visibilitychange"));assert.equal(runs,1)
  await Promise.resolve();window.dispatchEvent(new Event("pagehide"))
  await new Promise(r=>setTimeout(r,15));assert.equal(runs,1)
  window.dispatchEvent(new Event("pageshow"));assert.equal(runs,2)
  control.abort();await new Promise(r=>setTimeout(r,15));assert.equal(runs,2)
  document.dispatchEvent(new Event("visibilitychange"));assert.equal(runs,2)
})

test("an explicit roster entry reads once while hidden and keeps periodic reads paused",async()=>{
  const document=Object.assign(new EventTarget(),{visibilityState:"hidden"}),window=new EventTarget(),control=new AbortController()
  const phases:string[]=[]
  const source={machineID:"m",authorizationRevision:"a",status:async()=>{phases.push("status");return {sources:[]}},list:async()=>{phases.push("list");return {items:[{id:"stored-bot"}],next_cursor:""}}} as unknown as SourceClient
  let rows:unknown[]=[]
  followSnapshotRoster(async()=>{rows=(await readSnapshotRoster(source,()=>[source],"",1,control.signal)).items},control.signal,5,document,window,{readOnEntry:true})
  try {
    await new Promise(resolve=>setImmediate(resolve))
    assert.deepEqual(phases,["status","list"])
    assert.deepEqual(rows,[{id:"stored-bot"}])
    await new Promise(resolve=>setTimeout(resolve,15))
    assert.deepEqual(phases,["status","list"],"a hidden page must not start a periodic poll")
  } finally {control.abort()}
})

test("a hidden entry surfaces a typed read refusal instead of waiting for visibility",async()=>{
  const document=Object.assign(new EventTarget(),{visibilityState:"hidden"}),window=new EventTarget(),control=new AbortController()
  let phase="waiting",lists=0
  const source={machineID:"m",authorizationRevision:"a",status:async()=>{throw failure("pa_offline")},list:async()=>{lists++;return {items:[],next_cursor:""}}} as unknown as SourceClient
  followSnapshotRoster(async()=>{try{await readSnapshotRoster(source,()=>[source],"",1,control.signal);phase="ready"}catch(error){phase=errorCode(error)}},control.signal,5,document,window,{readOnEntry:true})
  try {
    await new Promise(resolve=>setImmediate(resolve))
    assert.equal(phase,"pa_offline")
    assert.equal(lists,0)
  } finally {control.abort()}
})
