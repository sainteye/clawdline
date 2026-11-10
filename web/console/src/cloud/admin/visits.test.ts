import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node runs source tests directly.
import { demoVisits, parseVisits, readVisits, visitDailyAccounts, visitPath, visitHourlyValues } from "./visits.ts"
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

test("hourly values keep per-hour distinct accounts and divide active time by that hour's page views",()=>{
 const hours=[{hour:"2026-10-31T06:00:00Z",views:2,requests:8,member_accounts:1,active_ms:60000},{hour:"2026-10-31T07:00:00Z",views:0,requests:3,member_accounts:0,active_ms:0}]
 assert.deepEqual(visitHourlyValues(hours,"duration").map(p=>p.value),[30,null])
 assert.deepEqual(visitHourlyValues(hours,"member_accounts").map(p=>p.value),[1,0])
 assert.deepEqual(visitHourlyValues(hours,"requests").map(p=>p.value),[8,3])
})
test("refuses malformed hourly sequences, future hours, forged counts, and inconsistent totals",()=>{
 for(const mutate of [
  (s:ReturnType<typeof demoVisits>)=>{s.hourly![1].hour=s.hourly![0].hour},
  (s:ReturnType<typeof demoVisits>)=>{s.hourly![1].hour="2026-02-31T00:00:00Z"},
  (s:ReturnType<typeof demoVisits>)=>{s.hourly![0].member_accounts=s.hourly![0].views+1},
  (s:ReturnType<typeof demoVisits>)=>{s.hourly![0].active_ms=-1},
  (s:ReturnType<typeof demoVisits>)=>{s.hourly![0].views++},
  (s:ReturnType<typeof demoVisits>)=>{s.hourly=Array(2161).fill(s.hourly![0])},
  (s:ReturnType<typeof demoVisits>)=>{s.hourly![s.hourly!.length-1].hour="2026-11-01T00:00:00Z"},
 ]){const s=demoVisits(30,1,new Date("2026-10-31T10:00:00Z"));mutate(s);assert.throws(()=>parseVisits(s),/invalid_analytics_report/)}
 const older=demoVisits();delete older.hourly;assert.equal(parseVisits(older).hourly,undefined)
})

test("longer periods chart accounts distinct within each UTC day, and an earlier API keeps the hourly series",()=>{
 const data=demoVisits(7,1,new Date("2026-11-03T10:00:00Z"))
 for(const d of data.daily)d.member_accounts=0
 data.daily[data.daily.length-1].member_accounts=2
 const points=visitDailyAccounts(parseVisits(data))!
 assert.equal(points.length,7);assert.deepEqual(points.at(-1),{at:"2026-11-03T00:00:00Z",value:2})
 assert.equal(visitDailyAccounts(parseVisits(demoVisits(1,1,new Date("2026-11-03T23:00:00Z")))),null)
 const older=demoVisits(7);for(const d of older.daily)delete d.member_accounts
 assert.equal(visitDailyAccounts(parseVisits(older)),null)
 for(const mutate of [(s:ReturnType<typeof demoVisits>)=>{s.daily[0].member_accounts=s.daily[0].views+1},(s:ReturnType<typeof demoVisits>)=>{s.daily.at(-2)!.member_accounts=s.member_accounts+1},(s:ReturnType<typeof demoVisits>)=>{s.daily[0].member_accounts=-1}]){
  const s=demoVisits(7,1,new Date("2026-11-03T10:00:00Z"));mutate(s);assert.throws(()=>parseVisits(s),/invalid_analytics_report/)
 }
})
