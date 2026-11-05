import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node's behavior runner loads source extensions.
import { codegraphGuidanceEnabled, codegraphGuidanceKey, setCodegraphGuidance, messageWithCodegraphGuidance } from "./session-guidance.ts"

test("a CodeGraph preference survives a reload but never crosses machine or conversation identity", () => {
 const saved = new Map<string,string>()
 const old = Object.getOwnPropertyDescriptor(globalThis,"localStorage")
 Object.defineProperty(globalThis,"localStorage",{configurable:true,value:{getItem:(key:string)=>saved.get(key)??null,setItem:(key:string,value:string)=>saved.set(key,value),removeItem:(key:string)=>saved.delete(key)}})
 try {
  const first={id:"terminal",machine:"machine-a",sessionId:"conversation-a",execution_generation:"first"}
  assert.equal(codegraphGuidanceEnabled(first),false)
  setCodegraphGuidance(first,true)
  const resumed={...first,execution_generation:"resumed"},otherMachine={...first,machine:"machine-b"}
  assert.equal(codegraphGuidanceEnabled(resumed),true)
  assert.equal(codegraphGuidanceEnabled(otherMachine),false)
  assert.equal(codegraphGuidanceEnabled({...first,sessionId:"conversation-b"}),false)
  assert.equal(codegraphGuidanceKey({...first,sessionId:undefined}),"")
  setCodegraphGuidance(first,false)
  assert.equal(saved.size,0)
 } finally { if(old)Object.defineProperty(globalThis,"localStorage",old);else Reflect.deleteProperty(globalThis,"localStorage") }
})

test("automatic CodeGraph guidance preserves provider commands and freezes the selected message bytes", () => {
 const guide="Synthetic CodeGraph guidance"
 for(const command of ["/quit","/exit","/model example","/resume","/unknown-command argument","  /compact\n"])
  assert.equal(messageWithCodegraphGuidance(command,guide,true),command)
 const captured=messageWithCodegraphGuidance("Inspect this function",guide,true)
 assert.match(captured,/^Inspect this function\n\nSynthetic CodeGraph guidance$/)
 assert.equal(messageWithCodegraphGuidance("A later message",guide,false),"A later message")
 assert.ok(captured.endsWith(guide),"unchecking cannot change a previously captured queued/retry payload")
 assert.equal(messageWithCodegraphGuidance("",guide,true),guide,"a validated image-only message may carry guidance")
})
