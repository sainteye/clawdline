import test from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node strip-types tests import source files.
import { localQuantile, readLocalPerformance } from "./api-performance.ts"

test("owner percentiles use merged counts and keep empty and overflow unavailable",()=>{
  const bounds=[1,2,5,10,20,50,100,200,500,1000,2000,5000,10000,30000,60000]
  const bins=Array(16).fill(0);bins[0]=99;bins[15]=1
  assert.equal(localQuantile({bins,count:100,sum_ms:90000,max_ms:80000},bounds,95),1)
  assert.equal(localQuantile({bins,count:100,sum_ms:90000,max_ms:80000},bounds,100),null)
  assert.equal(localQuantile({bins:Array(16).fill(0),count:0,sum_ms:0,max_ms:0},bounds,95),null)
})
test("owner reads use only their explicit machine URL and refuse authorization loss",async()=>{
  let seen="",options:RequestInit|undefined
  const get=(async(url,init)=>{seen=String(url);options=init;return new Response("",{status:403})}) as typeof fetch
  await assert.rejects(readLocalPerformance(get,"/v1/api-performance?hours=1",new AbortController().signal),/api_performance_refused/)
  assert.equal(seen,"/v1/api-performance?hours=1");assert.equal(options?.credentials,"same-origin");assert.equal(options?.cache,"no-store")
})
