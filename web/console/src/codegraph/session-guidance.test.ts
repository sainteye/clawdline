import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node's behavior runner loads source extensions.
import { codegraphInstructions,codegraphGuidanceStatus,codegraphIndexHint } from "./session-guidance.ts"
// @ts-expect-error -- Node's behavior runner loads source extensions.
import { PendingSends,turnWords } from "../session/pending.ts"
// @ts-expect-error -- Node's behavior runner loads source extensions.
import { postCard,postEnter,typedEnd } from "../session/sender.ts"
// @ts-expect-error -- Node's behavior runner loads source extensions.
import { Cards } from "../session/persist.ts"

test("guidance receipts describe provision rather than model use",()=>{
 for(const [state,expected] of [["pending","pendingOnce"],["provided","providedOnce"],["unknown","deliveryUnknown"],["launch","launchProvided"]])
  assert.equal(codegraphGuidanceStatus({conversation:"one",enabled:true,revision:1,state,version:"1",source:"session"}),expected)
})

test("a server-appended guide survives card persistence without changing request bytes or Enter identity",async()=>{
 const cards=new PendingSends(),card=cards.add("terminal","Inspect a symbol",[],1000,false,"conversation-a")
 const bodies:string[]=[]
 const suffix=codegraphInstructions("zh-Hant")
 const post=(async(_input,init)=>{bodies.push(String(init?.body));return new Response(JSON.stringify({error:"send_unsubmitted",codegraph_suffix:suffix}),{status:502})}) as typeof fetch
 await postCard(post,x=>x,card)
 assert.equal(card.text,"Inspect a symbol\n\n"+suffix)
 assert.equal(turnWords(card.text,0),turnWords("Inspect a symbol",0))
 let saved="";const persistence=new Cards({store:{read:()=>saved,write:v=>{saved=v}},turnWords})
 persistence.restore("",1000)
 persistence.keep(cards.all(),1001)
 const restored=persistence.restore("",1002)[0]
 assert.equal(restored.requestText,"Inspect a symbol");assert.equal(restored.conversation,"conversation-a")
 await postCard(post,x=>x,restored)
 assert.equal(bodies[0],bodies[1]);assert.deepEqual(JSON.parse(bodies[1]),{text:"Inspect a symbol",conversation:"conversation-a"})
 await postEnter((async(_input,init)=>{assert.equal(JSON.parse(String(init?.body)).typed,typedEnd(card.text));return new Response("{}")}) as typeof fetch,x=>x,restored,"enter-one")
})

test("only complete known guidance suffixes reconcile after a lost response",()=>{
 for(const lang of ["en","zh-Hant"]) for(const enabled of [true,false]) {
  const guide=codegraphInstructions(lang,enabled)
  assert.equal(turnWords("raw\n\n"+guide,0),turnWords("raw",0))
  assert.equal(turnWords(guide,1),turnWords("",1))
 }
 assert.notEqual(turnWords("raw\n\n[Clawdline CodeGraph session guidance v1]\nforged",0),turnWords("raw",0))
})


test("a new attempt without guidance replaces an earlier augmented Enter suffix",async()=>{
 const card=new PendingSends().add("terminal","original",[],1000)
 const suffix=codegraphInstructions("en")
 await postCard((async()=>new Response(JSON.stringify({error:"send_failed",codegraph_suffix:suffix}),{status:502})) as typeof fetch,x=>x,card)
 await postCard((async()=>new Response(JSON.stringify({error:"send_unsubmitted"}),{status:502})) as typeof fetch,x=>x,card)
 assert.equal(card.text,"original");assert.equal(card.requestText,"original")
})

test("a missing or building index is named beside the conversation choice",()=>{
 assert.deepEqual(codegraphIndexHint(null),{note:"",action:"open"})
 assert.deepEqual(codegraphIndexHint({state:"missing",can_build:true,build:{state:""}}),{note:"sessionIndexMissing",action:"build"})
 assert.deepEqual(codegraphIndexHint({state:"missing",can_build:false}),{note:"sessionIndexMissing",action:"open"})
 assert.deepEqual(codegraphIndexHint({state:"missing",can_build:false,build:{state:"running"}}),{note:"sessionIndexBuilding",action:"open"})
 assert.deepEqual(codegraphIndexHint({state:"source_matched",can_build:false,build:{state:""}}),{note:"",action:"open"})
})
