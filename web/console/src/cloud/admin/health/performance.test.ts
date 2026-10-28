import test from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node strip-types tests import source files.
import { parsePerformance, readPerformance, PERFORMANCE_LIMITS } from "./performance.ts"
// @ts-expect-error -- Node strip-types tests import source files.
import { performanceFixture } from "./performance-fixture.ts"
// @ts-expect-error -- Node strip-types tests import source files.
import { healthRoute, healthHref } from "./route.ts"

test("service performance preserves missing, no traffic and stale evidence",()=>{
  const now=Date.now(),normal=parsePerformance(performanceFixture(now))
  assert.equal(normal.status,"reporting")
  const empty=parsePerformance(performanceFixture(now,1,"empty"))
  assert.equal(empty.status,"unknown");assert.equal(empty.current.p95_ms,null)
  const stale=parsePerformance(performanceFixture(now,1,"stale"));assert.equal(stale.status,"unknown");assert.equal(stale.host_fresh,false)
  const noTraffic=performanceFixture(now);noTraffic.current.count=0;noTraffic.current.errors=0;noTraffic.current.cancelled=0;noTraffic.current.slow=0;noTraffic.current.p50_ms=null;noTraffic.current.p95_ms=null;noTraffic.current.p99_ms=null;noTraffic.current.error_pct=null
  assert.equal(parsePerformance(noTraffic).current.count,0)
  const wrong=structuredClone(stale);wrong.status="reporting";assert.throws(()=>parsePerformance(wrong),/monitor_contract/)
})
test("oversized lists and impossible denominators are refused, never clipped",()=>{
  const b=performanceFixture(Date.now());b.current.errors=b.current.count+1;assert.throws(()=>parsePerformance(b))
  const huge=performanceFixture(Date.now());huge.history=Array(PERFORMANCE_LIMITS.points+1).fill(huge.history[0]);assert.throws(()=>parsePerformance(huge))
})
test("the service route retains its hours and pause without requesting owner scope",async()=>{
  const route=healthRoute("/admin/health/services","?view=performance&hours=168&until=2026-01-01T00:00:00.000Z")!
  assert.equal(route.screen,"M03");assert.match(healthHref(route,route.scope),/hours=168/)
  let seen="",init:RequestInit|undefined
  const get=(async(url,options)=>{seen=String(url);init=options;return new Response(JSON.stringify(performanceFixture(Date.now())))}) as typeof fetch
  await readPerformance(get,"https://api.example.test",6,null,new AbortController().signal)
  assert.equal(seen,"https://api.example.test/su/v1/health/performance?hours=6");assert.equal(init?.cache,"no-store");assert.equal(init?.credentials,"include")
})
test("permission refusal has no successful cached result and abort reaches the fetch",async()=>{
  await assert.rejects(readPerformance((async()=>new Response("",{status:404})) as typeof fetch,"",1,null,new AbortController().signal),/monitor_refused/)
  const ctl=new AbortController()
  const get=((_:unknown,init?:RequestInit)=>new Promise<Response>((_resolve,reject)=>{init?.signal?.addEventListener("abort",()=>reject(new Error("aborted")),{once:true})})) as typeof fetch
  const p=readPerformance(get,"",1,null,ctl.signal);ctl.abort();await assert.rejects(p,/aborted/)
})
