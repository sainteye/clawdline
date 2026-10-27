import test from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node's test runner loads TypeScript source.
import { supportCall, SupportFailure } from "./api.ts"
test("a member mutation carries the same caller-owned key and body on an explicit retry",async()=>{
 const calls:{url:string;init:RequestInit}[]=[]
 const get:typeof fetch=async(url,init)=>{calls.push({url:String(url),init:init!});return new Response(JSON.stringify({question:{id:"sq_test",title:"Question",state:"queued",revision:1,reply_locale:"en"}}),{status:201,headers:{"content-type":"application/json"}})}
 const body={title:"Question",body:"Help"}
 await supportCall("https://api.test","/v1/support/questions","POST",body,"same-key",get)
 await supportCall("https://api.test","/v1/support/questions","POST",body,"same-key",get)
 assert.equal(calls.length,2);assert.equal(calls[0]!.init.credentials,"include");assert.equal(calls[0]!.init.body,calls[1]!.init.body)
 assert.equal((calls[0]!.init.headers as Record<string,string>)["Idempotency-Key"],"same-key")
})
test("transport errors and malformed success are ambiguous; a typed 409 is a definite refusal",async()=>{
 const failure=(code:string,definite:boolean)=>(e:unknown)=>e instanceof SupportFailure&&e.code===code&&e.definite===definite
 await assert.rejects(supportCall("https://api.test","/v1/support/questions","POST",{},"k",async()=>{throw new Error("lost")}),failure("support_outcome_unknown",false))
 await assert.rejects(supportCall("https://api.test","/v1/support/questions","POST",{},"k",async()=>new Response("not json")),failure("support_response_unreadable",false))
 await assert.rejects(supportCall("https://api.test","/v1/support/questions","POST",{},"k",async()=>new Response(JSON.stringify({error:{code:"support_revision_moved"}}),{status:409})),failure("support_revision_moved",true))
})
test("a FAQ response cannot substitute another language or malformed answer rows",async()=>{
 const get:typeof fetch=async()=>new Response(JSON.stringify({locale:"en",items:[{locale:"en",title:"English",body:"Answer",sources:[]}]}))
 await assert.rejects(supportCall("https://api.test","/v1/faq?locale=zh-Hant","GET",undefined,undefined,get),e=>e instanceof SupportFailure&&e.code==="support_response_unreadable")
 const bad:typeof fetch=async()=>new Response(JSON.stringify({questions:null}))
 await assert.rejects(supportCall("https://api.test","/v1/support/questions","GET",undefined,undefined,bad))
})

test("Admin reporter identity accepts a missing display name and refuses malformed profiles",async()=>{
 const question={id:"sq_test",title:"Question",state:"queued",revision:1,reply_locale:"en"}
 const read=(reporter:unknown)=>supportCall<{question:{reporter:{id:string;name:string|null}}}>("https://api.test","/su/v1/support/questions/sq_test","GET",undefined,undefined,async()=>new Response(JSON.stringify({question:{...question,reporter}})))
 assert.equal((await read({id:"member-test",name:null})).question.reporter.name,null)
 await assert.rejects(read({id:"member-test",name:{text:"not a name"}}),e=>e instanceof SupportFailure&&e.code==="support_response_unreadable")
})
