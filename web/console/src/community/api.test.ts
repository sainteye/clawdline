import test from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node loads TypeScript source.
import { communityCall } from "./api.ts"
// @ts-expect-error -- Node loads TypeScript source.
import { SupportFailure } from "../support/api.ts"
const topic={id:"sq_"+"a".repeat(40),title:"A usage question",request_type:"question",topic:"other",state:"answered",revision:3,source_language:"en",reply_locale:"en",author:{display_name:"Member",avatar_url:null},progress:null,reply_count:1,created_at:"2026-01-01T00:00:00Z",updated_at:"2026-01-01T01:00:00Z",messages:[{id:"sm_one",kind:"member",author:{display_name:"Member",avatar_url:null},body:"Question",at:"2026-01-01T00:00:00Z",locale:"en",sources:[],images:[]}]}
const response=(value:unknown)=>async()=>new Response(JSON.stringify(value),{status:200,headers:{"content-type":"application/json"}})
test("a public response requires actual topics and complete per-message identities",async()=>{
 assert.deepEqual(await communityCall("https://api.example.test","/v1/community/topics","GET",undefined,undefined,response({topics:[topic],next_cursor:null})),{topics:[topic],next_cursor:null})
 for(const value of [{},{topics:[]},{topics:[{...topic,author:{display_name:"Member",avatar_url:null,email:"private@example.test"}}],next_cursor:null},{topics:[{...topic,messages:[{...topic.messages[0],author:undefined}]}],next_cursor:null}]) {
  await assert.rejects(communityCall("https://api.example.test","/v1/community/topics","GET",undefined,undefined,response(value)),(e:unknown)=>e instanceof SupportFailure&&e.code==="support_response_unreadable")
 }
})
test("an unreadable successful write stays uncertain instead of authorizing a duplicate submission",async()=>{
 await assert.rejects(communityCall("https://api.example.test","/v1/community/topics","POST",{public_consent:true},"same-key",response({})),(e:unknown)=>e instanceof SupportFailure&&!e.definite)
})
