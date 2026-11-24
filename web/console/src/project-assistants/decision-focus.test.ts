import {test} from "node:test"
import assert from "node:assert/strict"
import {createRequire} from "node:module"
import {fileURLToPath} from "node:url"
import {build} from "esbuild"
import React from "react"
import {renderToStaticMarkup} from "react-dom/server"
import type * as W from "@clawdline/contract"

// A push about a Bot's question opens its chat at the question's card, and the
// card follows the reply through to the Bot finishing. Scrolling and the
// highlight run in a real browser in tools/project-assistants-decision-card.e2e.mjs.
const project:W.ProjectAssistantProjectScope={machine_id:"machine",project_id:"project",canonical_id:"canonical",kind:"repository"}
const key={machine_id:"machine",bot_id:"beacon"}
const fence:W.ProjectAssistantFence={key,project,conversation_id:"beacon-chat",binding_id:"",generation:0}
const summary=(state:W.ProjectAssistantPermissionState="pending",expires="2099-01-01T00:00:00Z"):W.ProjectAssistantReadPermissionSummary=>
  ({id:"d1",turn_id:"",fence,state,expires_at:expires,detail_cursor:"c1",tool_kind:"decision",title:"Ship the release?"})
const offer=(state:W.ProjectAssistantPermissionState="pending",answer=""):W.ProjectAssistantPermissionOffer=>({id:"d1",native_request_id:"",turn_id:"",fence,state,
  expires_at:"2099-01-01T00:00:00Z",selected_option_id:"",kind:"decision",
  options:[{id:"ship",kind:"allow_once",label:"Ship it today",allow:true,read_only:true}],
  decision:{question:"Ship the release?",...(answer?{answer_text:answer}:{})}} as W.ProjectAssistantPermissionOffer)
const turn=(id:string,state:W.ProjectAssistantTurnState)=>({id,state} as W.ProjectAssistantReadTurnSummary)

async function load() {
  globalThis.document??={getElementById:()=>null,querySelector:()=>null,querySelectorAll:()=>[],addEventListener:()=>{},createElement:()=>({style:{},setAttribute(){},appendChild(){},addEventListener(){}}),documentElement:{style:{},setAttribute(){},lang:"en"},body:{appendChild(){},classList:{add(){},remove(){},toggle(){},contains:()=>false}}} as never
  globalThis.location??=new URL("http://synthetic.invalid/") as never
  const {outputFiles}=await build({stdin:{contents:'export * from "./decision-focus";export {DecisionCard} from "./DecisionCard";export {words} from "./words"',
    resolveDir:fileURLToPath(new URL(".",import.meta.url)),loader:"ts"},bundle:true,platform:"node",format:"cjs",packages:"external",write:false,jsx:"automatic",loader:{".css":"empty"}})
  const compiled={exports:{} as typeof import("./decision-focus")&{DecisionCard:typeof import("./DecisionCard").DecisionCard;words:typeof import("./words").words}}
  new Function("require","module","exports",outputFiles[0].text)(createRequire(import.meta.url),compiled,compiled.exports)
  return compiled.exports
}

test("a push link names the decision to open, and nothing else does",async()=>{
  const {decisionFromFragment}=await load()
  assert.equal(decisionFromFragment("#bot=beacon&conversation=beacon-chat&machine=m&decision=d1"),"d1")
  assert.equal(decisionFromFragment("#bot=beacon&conversation=beacon-chat"),"","an ordinary Bot link focuses no card")
  assert.equal(decisionFromFragment("#decision=d1"),"","a decision without its Bot is not a destination")
  assert.equal(decisionFromFragment("#bot=beacon&conversation=c&decision=d1&decision=d2"),"","two decisions are ambiguous")
})

test("the chat waits for the card, scrolls to an open one and says a closed one is handled",async()=>{
  const {decisionFocus}=await load()
  assert.equal(decisionFocus("d1",[],false),"wait","the permissions have not been read yet")
  assert.equal(decisionFocus("d1",[{summary:summary()}],false),"scroll")
  assert.equal(decisionFocus("d1",[{summary:summary("answered")}],true),"handled")
  assert.equal(decisionFocus("d1",[{summary:summary("pending","2000-01-01T00:00:00Z")}],true),"handled","an expired question is handled, not open")
  assert.equal(decisionFocus("d1",[{summary:{...summary(),id:"other"}}],true),"handled","read and absent: nothing to scroll to")
})

test("after a reply the card follows the Bot's turn: working, then finished, never guessed",async()=>{
  const {decisionFollowUp}=await load()
  assert.equal(decisionFollowUp("",[],undefined),undefined,"no reply from this page: nothing claimed")
  assert.equal(decisionFollowUp("t1",[],undefined),"working","accepted and not yet observed")
  assert.equal(decisionFollowUp("t1",[turn("t1","queued")],"working"),"working")
  assert.equal(decisionFollowUp("t1",[turn("t1","running")],"working"),"working")
  assert.equal(decisionFollowUp("t1",[turn("t1","succeeded")],"working"),"done")
  assert.equal(decisionFollowUp("t1",[],"done"),"done","a finished turn that left the list stays finished")
  assert.equal(decisionFollowUp("t1",[turn("t1","failed")],"working"),undefined,"a failed turn is the interruption's to show, not a finish")
  assert.equal(decisionFollowUp("t1",[turn("t1","cancelled")],"working"),undefined)
})

test("an answered card shows the Bot working, then finished, in both languages",async()=>{
  const {DecisionCard,words}=await load()
  for(const [language,working,done] of [["en","Beacon is working on it…","Beacon finished"],["zh-Hant","Beacon 處理中…","Beacon 已完成"]] as const){
    const render=(followUp?:"working"|"done")=>renderToStaticMarkup(React.createElement(DecisionCard,{summary:summary("answered"),offer:offer("answered","Ship it today"),
      enabled:true,onAnswer:async()=>{},askerName:"Beacon",w:words(language),followUp}))
    const busy=render("working")
    assert.ok(busy.includes(working),language);assert.ok(!busy.includes(done),language)
    const finished=render("done")
    assert.ok(finished.includes(done),language);assert.ok(!finished.includes(working),language)
    assert.ok(finished.includes(language==="en"?"You replied: Ship it today":"你已回覆：Ship it today"),language)
    const quiet=render()
    assert.ok(!quiet.includes(working)&&!quiet.includes(done),"without an observed turn the card claims neither")
  }
})

test("the card a push opened is marked so it can be found and highlighted",async()=>{
  const {DecisionCard,words}=await load()
  const html=renderToStaticMarkup(React.createElement(DecisionCard,{summary:summary(),offer:offer(),enabled:true,onAnswer:async()=>{},askerName:"Beacon",w:words("en"),highlight:true}))
  assert.match(html,/id="pa-decision-d1"/)
  assert.match(html,/data-focused="true"/)
  const plain=renderToStaticMarkup(React.createElement(DecisionCard,{summary:summary(),offer:offer(),enabled:true,onAnswer:async()=>{},askerName:"Beacon",w:words("en")}))
  assert.doesNotMatch(plain,/data-focused/)
})

test("a closed decision opened from a push says it was already handled",async()=>{
  const {words}=await load()
  assert.equal(words("en").decisionHandled,"This decision was already handled.")
  assert.equal(words("zh-Hant").decisionHandled,"這個決定已經處理過了。")
})
