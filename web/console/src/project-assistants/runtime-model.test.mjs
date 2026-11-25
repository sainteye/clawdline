import assert from "node:assert/strict"
import { test } from "node:test"
import { createRequire } from "node:module"
import { fileURLToPath } from "node:url"
import { build } from "esbuild"
import React from "react"
import { renderToStaticMarkup } from "react-dom/server"
// The console's catalog and modal modules look up a few page objects when they load.
globalThis.document??={getElementById:()=>null,querySelector:()=>null,querySelectorAll:()=>[],addEventListener:()=>{},removeEventListener:()=>{},createElement:()=>({style:{},setAttribute(){},appendChild(){},addEventListener(){}}),documentElement:{style:{},setAttribute(){},lang:"en"},body:{appendChild(){},classList:{add(){},remove(){},toggle(){},contains:()=>false}}}
globalThis.location??=new URL("http://synthetic.invalid/")
globalThis.window??={document:globalThis.document,location:globalThis.location,navigator:globalThis.navigator,matchMedia:()=>({matches:false,addEventListener(){},removeEventListener(){}}),addEventListener(){},removeEventListener(){}}
const {outputFiles}=await build({stdin:{contents:'export {CatalogModelSelect} from "./AgentModelPicker";export {ChatModel} from "./ChatModel";export {words} from "./words"',resolveDir:fileURLToPath(new URL(".",import.meta.url)),loader:"ts"},bundle:true,platform:"node",format:"cjs",packages:"external",write:false,jsx:"automatic",loader:{".css":"empty"}})
const compiled={exports:{}}
new Function("require","module","exports",outputFiles[0].text)(createRequire(import.meta.url),compiled,compiled.exports)
const {CatalogModelSelect,ChatModel,words}=compiled.exports
const w=words("en")
const render=(component,props)=>renderToStaticMarkup(React.createElement(component,props))
const codex={agent:"codex",models:[{value:"gpt-5.5",name:"GPT-5.5"},{value:"gpt-5.4-mini",name:"GPT-5.4 mini"}],default_name:"GPT-5.5",efforts:["high","xhigh"],source:"agent",observed_at:"2026-11-25T06:00:00Z",refreshing:false}
const shown=html=>[...html.matchAll(/class="pa-select-value[^"]*"[^>]*>([^<]*)</g)].map(m=>m[1])

test("a saved Codex ACP value shows as its catalog model and effort",()=>{
  const html=render(CatalogModelSelect,{agent:"codex",models:{status:"ready",catalog:codex,waiting:false},retry(){},value:"gpt-5.5/high",onChange(){},w})
  assert.deepEqual(shown(html),["GPT-5.5","high"])
})

test("a saved Claude default shows as the Agent default, with no effort drop-down",()=>{
  const claude={...codex,agent:"claude",models:[{value:"sonnet",name:"Sonnet 5"}],default_name:"Opus 5.5",efforts:[]}
  const html=render(CatalogModelSelect,{agent:"claude",models:{status:"ready",catalog:claude,waiting:false},retry(){},value:"default",onChange(){},w})
  assert.equal(shown(html).length,1)
  assert.match(shown(html)[0],/Opus 5\.5/)
})

test("while the catalog loads the drop-down is busy and keeps the saved model",()=>{
  const html=render(CatalogModelSelect,{agent:"claude",models:{status:"loading"},retry(){},value:"sonnet",onChange(){},w})
  assert.match(html,/aria-busy="true"/)
  assert.deepEqual(shown(html),["sonnet"])
})

test("an older machine offers only the Agent default",()=>{
  const html=render(CatalogModelSelect,{agent:"codex",models:{status:"unsupported"},retry(){},value:"",onChange(){},w})
  assert.equal(shown(html).length,1)
  assert.doesNotMatch(html,/aria-busy/)
})

test("the chat header names a saved choice without reading any list",()=>{
  const bot={agent:"codex",runtime_settings:{model:"gpt-5.5/high",options:{}}}
  const source={canMemorySettings:()=>true}
  assert.match(render(ChatModel,{bot,source,enabled:true,activeTurn:false,save:async()=>{},w}),/gpt-5\.5 · high/)
  assert.match(render(ChatModel,{bot:{...bot,runtime_settings:{model:"default",options:{}}},source,enabled:true,activeTurn:false,save:async()=>{},w}),new RegExp(w.nativeDefault))
  // A catalog Agent is editable without the per-Bot discovery route; the daemon checks the choice on save.
  assert.match(render(ChatModel,{bot,source,enabled:true,activeTurn:false,save:async()=>{},w}),/aria-haspopup="dialog"/)
  assert.doesNotMatch(render(ChatModel,{bot:{...bot,agent:"antigravity"},source,enabled:true,activeTurn:false,save:async()=>{},w}),/aria-haspopup="dialog"/)
})
