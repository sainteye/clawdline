import {test} from "node:test"
import assert from "node:assert/strict"
import {createRequire} from "node:module"
import {fileURLToPath} from "node:url"
import {build} from "esbuild"
import React from "react"
import {renderToStaticMarkup} from "react-dom/server"
import type * as W from "@clawdline/contract"

const project:W.ProjectAssistantProjectScope={machine_id:"machine",project_id:"project",canonical_id:"canonical",kind:"repository"}
const atlas={machine_id:"machine",bot_id:"atlas"},beacon={machine_id:"machine",bot_id:"beacon"}
const at="2026-11-23T00:00:00Z"
const endpoint=(key:W.ProjectAssistantBotKey)=>({key,conversation_id:`${key.bot_id}-chat`})
const exchange=(id:string,from:W.ProjectAssistantBotKey,to:W.ProjectAssistantBotKey,request:string,reply?:string):W.ProjectAssistantExchange=>
  ({id,project,source:endpoint(from),target:endpoint(to),source_turn_id:`${id}-s`,request_turn_id:`${id}-r`,request,reply,outcome:"completed",revision:1,hops:1})
const bot=(key:W.ProjectAssistantBotKey,name:string):W.ProjectAssistantBot=>({key,project,conversation_id:`${key.bot_id}-chat`,name,agent:"claude",
  appearance:{shape:"blob",color:"#e39567"},permission_policy:"ask",instructions:{revision:1,text:"",role_snapshot_ref:""},created_at:at,updated_at:at,version:1})

async function panel() {
  if(typeof document==="undefined")Object.defineProperty(globalThis,"document",{configurable:true,value:{getElementById:()=>null,querySelector:()=>null,querySelectorAll:()=>[],addEventListener:()=>{},createElement:()=>({style:{},setAttribute(){},appendChild(){},addEventListener(){}}),documentElement:{style:{},setAttribute(){},lang:"en"},body:{appendChild(){},classList:{add(){},remove(){},toggle(){},contains:()=>false}}}})
  const {outputFiles}=await build({stdin:{contents:'export {ExchangePanel} from "./ExchangePanel";export {words} from "./words"',resolveDir:fileURLToPath(new URL(".",import.meta.url)),loader:"ts"},bundle:true,platform:"node",format:"cjs",packages:"external",write:false,jsx:"automatic",loader:{".css":"empty"}})
  const compiled={exports:{} as {ExchangePanel:typeof import("./ExchangePanel").ExchangePanel;words:typeof import("./words").words}}
  new Function("require","module","exports",outputFiles[0].text)(createRequire(import.meta.url),compiled,compiled.exports)
  return compiled.exports
}

/** Each message's speaker and side, in reading order. */
function speakers(html:string) {
  return [...html.matchAll(/data-side="(left|right)"[^>]*>.*?<strong>([^<]*)<\/strong>/g)].map(match=>`${match[2]}:${match[1]}`)
}

test("two Bots keep their own sides in the exchange panel, whoever starts each exchange",async()=>{
  const {ExchangePanel,words}=await panel()
  const first=exchange("one",atlas,beacon,"Is CI green?","All green")
  const entries=[first,exchange("two",beacon,atlas,"Which version is deployed?","7388204fb"),exchange("three",atlas,beacon,"Run smoke test")]
    .map((x,index)=>({exchange:x,seq:index+1}))
  const render=(roster:W.ProjectAssistantBot[])=>renderToStaticMarkup(React.createElement(ExchangePanel,{source:{authorizationRevision:"grant"} as never,
    scope:{key:atlas,project,conversation_id:"atlas-chat"},reference:first,entries,roster,language:"en",w:words("en"),onClose:()=>{}}))
  const html=render([bot(atlas,"Atlas"),bot(beacon,"Beacon")])
  // The heading reads "Atlas ⇆ Beacon": Atlas speaks from the left, Beacon from the right.
  assert.ok(/<h3>.*Atlas.*⇆.*Beacon.*<\/h3>/.test(html))
  assert.deepEqual(speakers(html),["Atlas:left","Beacon:right","Beacon:right","Atlas:left","Atlas:left"])
  // A Bot nobody can identify still takes its side and renders.
  const unknown="Unknown Bot"
  assert.deepEqual(speakers(render([bot(atlas,"Atlas")])),["Atlas:left",`${unknown}:right`,`${unknown}:right`,"Atlas:left","Atlas:left"])
})

test("the exchange shows a delayed reply's own time but skips markers during a burst",async()=>{
  const {ExchangePanel,words}=await panel()
  const requestAt="2026-11-23T00:03:00Z",replyAt="2026-11-23T00:38:00Z"
  const first={...exchange("one",atlas,beacon,"Please review","Reviewed"),request_at:requestAt,reply_at:replyAt}
  const next={...exchange("two",atlas,beacon,"Anything else?","All clear"),request_at:"2026-11-23T00:39:00Z",reply_at:"2026-11-23T00:40:00Z"}
  const entries=[{exchange:first,seq:1},{exchange:next,seq:2}]
  const html=renderToStaticMarkup(React.createElement(ExchangePanel,{source:{authorizationRevision:"grant"} as never,
    scope:{key:atlas,project,conversation_id:"atlas-chat"},reference:first,entries,language:"zh-Hant-TW",w:words("zh-Hant-TW"),onClose:()=>{}}))
  assert.deepEqual([...html.matchAll(/<time[^>]* datetime="([^"]+)"/gi)].map(match=>match[1]),[requestAt,replyAt])
  assert.ok(html.indexOf("Please review")<html.indexOf(replyAt)&&html.indexOf(replyAt)<html.indexOf("Reviewed"))
})
