import assert from "node:assert/strict"
import { test } from "node:test"
import { createRequire } from "node:module"
import { fileURLToPath } from "node:url"
import { build } from "esbuild"
import React from "react"
import { renderToStaticMarkup } from "react-dom/server"
const {outputFiles}=await build({stdin:{contents:'export {BotOrb} from "./BotOrb";export {PermissionCard} from "./PermissionCard";export {BotStatus} from "./BotStatus";export {ChatRows} from "./ChatRows";export {fixtureSource} from "./fixtures";export {words} from "./words"',resolveDir:fileURLToPath(new URL(".",import.meta.url)),loader:"ts"},bundle:true,platform:"node",format:"cjs",packages:"external",write:false,jsx:"automatic",loader:{".css":"empty"}})
const compiled={exports:{}}
new Function("require","module","exports",outputFiles[0].text)(createRequire(import.meta.url),compiled,compiled.exports)
const {BotOrb,PermissionCard,BotStatus,ChatRows,fixtureSource,words}=compiled.exports
const render=(component,props)=>renderToStaticMarkup(React.createElement(component,props))
test("fixed global entry is bilingual and has no microphone or model side effect",()=>{
  let opened=0
  for(const language of ["en","zh-Hant"]){const html=render(BotOrb,{onOpen:()=>opened++,language,unread:2,pendingPermissions:1});assert.match(html,/aria-label=/);assert.match(html,/#e39567/);assert.match(html,/>3</);assert.doesNotMatch(html,/microphone|mic|audio/)}
  assert.equal(opened,0)
})
test("permission options preserve unavailable persistent grants and disabled history",async()=>{
  const s=fixtureSource(),page=await s.events({key:s.bots[1].key,after_seq:0,page_size:256},new AbortController().signal),offer=page.items[0].permission
  const html=render(PermissionCard,{offer,enabled:false,onAnswer:()=>assert.fail("render answered"),w:words("en")})
  assert.equal((html.match(/disabled=""/g)||[]).length,3)
  assert.match(html,/Always allow \(unavailable\)/)
})
test("offline status remains distinct from an empty source",async()=>{
  const s=fixtureSource();s.setState("offline")
  const value=await s.status(new AbortController().signal),html=render(BotStatus,{sources:value.sources,w:words("zh-Hant")})
  assert.match(html,/離線/);assert.match(html,/尚未確認是否最新/)
})
test("reading, selecting and metadata creation do not start a scripted model",async()=>{
  const log=[],s=fixtureSource("one",log),signal=new AbortController().signal
  await s.status(signal);await s.projects(signal);const page=await s.list({machine_id:"one",query:"",cursor:"",page_size:100},signal)
  const bot=page.items[0].bot;await s.get(bot.key,signal);const history=await s.history({key:bot.key,conversation_id:bot.conversation_id,before_seq:0,page_size:100},signal)
  const html=render(ChatRows,{messages:history.items,w:words("en"),language:"en",onQuote:()=>{}})
  assert.match(html,/Help me plan/);assert.equal(log.filter(x=>x.method==="send"||x.method==="stop"||x.method==="answer").length,0)
})
test("expired event cursor exports a current-generation snapshot then resumes after its own frontier",async()=>{
  const source=fixtureSource(),signal=new AbortController().signal,key=source.bots[0].key
  source.expireCursor()
  await assert.rejects(source.events({key,after_seq:1,page_size:256},signal),e=>e.error.code==="pa_cursor_expired")
  const snapshot=await source.export(key,signal)
  assert.equal(snapshot.conversation.generation,2)
  assert.equal(snapshot.permissions.some(p=>p.fence.generation===1),true)
  assert.equal(snapshot.permissions.some(p=>p.fence.generation===2),true)
  const frontier=Math.max(...snapshot.events.map(e=>e.seq))
  source.emitMessage("after snapshot")
  const events=await source.events({key,after_seq:frontier,page_size:256},signal)
  assert.deepEqual(events.items.map(e=>e.message?.text),["after snapshot"])
  assert.equal(events.items[0].fence.generation,2)
})
test("durable messages and work retain their original execution in the source snapshot",async()=>{
  const source=fixtureSource(),signal=new AbortController().signal,key=source.bots[0].key
  source.completePreviousWork()
  const snapshot=await source.export(key,signal)
  assert.equal(snapshot.conversation.generation,2)
  assert.deepEqual(snapshot.messages.slice(-3).map(m=>[m.kind,m.fence.generation]),[["user",1],["assistant",1],["work",1]])
  assert.equal(snapshot.work_links.length,1)
  assert.equal(snapshot.work_links[0].state,"landed")
  assert.equal(snapshot.work_links[0].fence.generation,1)
})
