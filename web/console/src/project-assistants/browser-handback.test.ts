import {test} from "node:test"
import assert from "node:assert/strict"
import type {BrowserCall} from "../session/browser-call"
// @ts-expect-error Node executes source extensions.
import {createBrowserHandback} from "./browser-handback.ts"

function fixture() {
  const tabs=[{tab:9,conversation:"owner",browser_mode:"user_chrome",person:true,current:true},{tab:9,conversation:"owner",browser_mode:"isolated",person:true,current:false},{tab:10,conversation:"foreign",browser_mode:"user_chrome",person:true,current:false}]
  const calls:{method:string;path:string;body?:any}[]=[]
  let fail=false
  const call:BrowserCall=async(method,path,body)=>{
    calls.push({method,path,body})
    if(method==="GET")return {owner:"owner",mode:"user_chrome",status:{tabs:tabs.map(t=>({...t}))}} as never
    const value=body as {tab:number;browser_mode:string;person:boolean}
    const tab=tabs.find(t=>t.tab===value.tab&&t.browser_mode===value.browser_mode)!;tab.person=value.person
    if(fail){fail=false;throw Object.assign(new Error("unknown"),{code:"pa_outcome_unknown"})}
    return {ok:true} as never
  }
  return {tabs,calls,call,unknown(){fail=true}}
}

test("exit hands only this Bot's human-controlled tabs back in their exact Chrome profiles",async()=>{
  const f=fixture(),handoff=createBrowserHandback(f.call)
  await handoff.exit();await handoff.exit()
  assert.deepEqual(f.tabs.map(t=>t.person),[false,false,true])
  const posts=f.calls.filter(c=>c.method==="POST")
  assert.deepEqual(posts.map(c=>c.body),[{tab:9,person:false,browser_mode:"user_chrome"},{tab:9,person:false,browser_mode:"isolated"}])
  assert.equal(f.calls.filter(c=>c.method==="GET").length,2,"fresh status before and observed handback after")
})

test("leaving during takeover waits for it and shares one handback across close and unmount",async()=>{
  const f=fixture(),handoff=createBrowserHandback(f.call)
  let release!:()=>void
  const takeover=handoff.track("POST","/v1/browser/control",()=>new Promise<void>(resolve=>{release=resolve}))
  const first=handoff.exit(),second=handoff.exit()
  assert.equal(first,second);assert.equal(f.calls.length,0)
  await assert.rejects(handoff.track("POST","/v1/browser/open",async()=>{}),{code:"pa_stale_version"})
  release();await takeover;await first
  assert.equal(f.calls.filter(c=>c.method==="POST").length,2)
})

test("unknown handback is not replayed; another explicit exit first observes the source",async()=>{
  const f=fixture(),handoff=createBrowserHandback(f.call)
  f.tabs[1].person=false;f.unknown()
  await assert.rejects(handoff.exit(),{code:"pa_outcome_unknown"})
  assert.equal(handoff.closing(),false)
  await handoff.exit()
  assert.equal(f.calls.filter(c=>c.method==="POST").length,1,"the source already applied it; do not send again")
})

test("unreadable ownership never releases an unrelated tab",async()=>{
  let posts=0
  const handoff=createBrowserHandback(async(method)=>{if(method==="POST")posts++;return {status:{tabs:[{tab:1,person:true}]}} as never})
  await assert.rejects(handoff.exit(),{code:"pa_invalid"});assert.equal(posts,0)
})
