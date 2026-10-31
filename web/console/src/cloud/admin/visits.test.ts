import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node runs source tests directly.
import { demoVisits, parseVisits, readVisits, visitPath } from "./visits.ts"
test("accepts the separate browser/request units and preserves unknown comparisons",()=>{
 const data=demoVisits(30,1,new Date("2026-10-31T10:00:00Z"));data.previous_views=null
 const s=parseVisits(data);assert.equal(s.previous_views,null);assert.equal(s.views,s.daily.reduce((n:number,r:{views:number})=>n+r.views,0));assert.equal(s.members[0].path,"/docs/install")
})
test("refuses private paths, forged links, oversized member pages and invalid units",()=>{
 for(const mutate of [(s:ReturnType<typeof demoVisits>)=>{s.members[0].path="/sessions/private"},(s:ReturnType<typeof demoVisits>)=>{s.pages[0].key="https://evil.test"},(s:ReturnType<typeof demoVisits>)=>{s.members=Array(21).fill(s.members[0])},(s:ReturnType<typeof demoVisits>)=>{s.members[0].active_ms=1800001},(s:ReturnType<typeof demoVisits>)=>{s.member_accounts=s.views+1}]){
  const s=demoVisits();mutate(s);assert.throws(()=>parseVisits(s),/invalid_analytics_report/)
 }
 for(const path of ["/admin","/docs/private/a","//evil.test","/docs/install?key=x","/_docs-locales/zh-TW/docs"] )assert.equal(visitPath(path),false)
})
test("uses cookie credentials, binds the requested pages and never turns an API refusal into zero",async()=>{
 const signal=new AbortController().signal
 const data=demoVisits(7);data.pages_page=2
 let calls=0
 const get=(async(url:RequestInfo|URL,init?:RequestInit)=>{calls++;assert.match(String(url),/days=7&page=1&pages_page=2$/);assert.equal(init?.credentials,"include");assert.equal(init?.cache,"no-store");return new Response(JSON.stringify(data))}) as typeof fetch
 assert.equal((await readVisits(get,"https://api.example",7,1,signal,2)).days,7);assert.equal(calls,1)
 for(const status of [401,404,429,503])await assert.rejects(readVisits((async()=>new Response("{}",{status})) as typeof fetch,"x",7,1,signal),status===404?/analytics_not_available/:new RegExp(`http_${status}`))
 await assert.rejects(readVisits((async()=>new Response(JSON.stringify(demoVisits(30)))) as typeof fetch,"x",7,1,signal),/query_mismatch/)
})

test("keeps server refusal codes and normalizes unreadable or unreachable reports",async()=>{
 const signal=new AbortController().signal
 await assert.rejects(readVisits((async()=>new Response(JSON.stringify({error:{code:"analytics_busy",message:"internal detail"}}),{status:503})) as typeof fetch,"x",7,1,signal),/analytics_busy/)
 await assert.rejects(readVisits((async()=>{throw new Error("private network detail")}) as typeof fetch,"x",7,1,signal),/analytics_network_error/)
 await assert.rejects(readVisits((async()=>new Response("broken JSON")) as typeof fetch,"x",7,1,signal),/invalid_analytics_report/)
})
