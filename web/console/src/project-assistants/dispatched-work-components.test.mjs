import assert from "node:assert/strict"
import { test } from "node:test"
import { createRequire } from "node:module"
import { fileURLToPath } from "node:url"
import { build } from "esbuild"
import React from "react"
import { renderToStaticMarkup } from "react-dom/server"

globalThis.location ??= new URL("http://synthetic.invalid/")
const {outputFiles}=await build({stdin:{contents:'export {BotRow} from "./BotRow";export {DispatchedWorkStrip,openDispatchedSession} from "./DispatchedWorkStrip";export {words} from "./words"',resolveDir:fileURLToPath(new URL(".",import.meta.url)),loader:"ts"},bundle:true,platform:"node",format:"cjs",packages:"external",write:false,jsx:"automatic",loader:{".css":"empty"}})
const compiled={exports:{}}
new Function("require","module","exports",outputFiles[0].text)(createRequire(import.meta.url),compiled,compiled.exports)
const {BotRow,DispatchedWorkStrip,openDispatchedSession,words}=compiled.exports
const w=words("zh-Hant"),render=(c,p)=>renderToStaticMarkup(React.createElement(c,p))
const live={fresh:true,state:"online",readable:true,writable:true,access:"interactive"}
const item=extra=>({bot:{key:{machine_id:"m",bot_id:"atlas"},name:"Atlas",project:{kind:"repository",machine_id:"m",project_id:"p"},appearance:{shape:"blob",color:"#5b8def"},agent:"claude"},status:"idle",activity:"",latest_message:"Last reply",activity_at:"2026-11-23T09:00:00Z",pending_permissions:0,unread_count:0,source:live,...extra})
const rowProps=(summary,showDispatched=true)=>({item:summary,selected:false,onSelect(){},w,language:"zh-Hant",now:Date.parse("2026-11-23T10:00:00Z"),projectName:"clawdline",showDispatched})

test("a roster row shows its clone count under the time only when Sessions are running", () => {
  const busy=render(BotRow,rowProps(item({dispatched_work:{running:2,needs_you:1,delivered:1}})))
  assert.match(busy,/class="pa-clones"[^>]*>.*?3<\/span>/)
  assert.match(busy,/data-status="needsInput"/)
  assert.match(busy,/aria-label="[^"]*3 個分身在跑/)
  const waiting=render(BotRow,rowProps(item({dispatched_work:{running:2,needs_you:0,delivered:0}})))
  assert.match(waiting,/data-status="waitingOnWork"/)
  assert.match(waiting,/data-mood="waitingOnWork"/)
  assert.match(waiting,/等 2 個分身完成/)
  for(const quiet of [item({}),item({dispatched_work:{running:0,needs_you:0,delivered:3}})]){
    assert.doesNotMatch(render(BotRow,rowProps(quiet)),/pa-clones/)
  }
})

test("with the setting off a row renders exactly as it does without dispatched work", () => {
  const plain=render(BotRow,rowProps(item({}),false))
  assert.equal(render(BotRow,rowProps(item({dispatched_work:{running:2,needs_you:1,delivered:1}}),false)),plain)
})

test("the chat strip takes no space without work, keeps the background capsule, and opens the Session", () => {
  const capsule=React.createElement("span",{className:"capsule"},"bg")
  assert.equal(render(DispatchedWorkStrip,{links:[],w,background:capsule}),'<span class="capsule">bg</span>')
  assert.equal(render(DispatchedWorkStrip,{links:[],w}),"")
  const html=render(DispatchedWorkStrip,{links:[{id:"a",task_id:"a",state:"executing",title:"Build",started_at:"2026-11-23T09:00:00Z"},{id:"b",task_id:"b",state:"executing"}],w,background:capsule})
  assert.match(html,/class="capsule"/)
  assert.match(html,/aria-expanded="false"/)
  assert.match(html,/2 個工作在跑/)
  globalThis.location.hash=""
  openDispatchedSession({id:"a",task_id:"a",state:"executing",terminal:"%1103"})
  assert.equal(globalThis.location.hash,"#session=%251103")
  let fallback=null
  openDispatchedSession({id:"b",task_id:"b",state:"executing"},link=>{fallback=link.id})
  assert.equal(fallback,"b")
})
