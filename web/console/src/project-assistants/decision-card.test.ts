import {test} from "node:test"
import assert from "node:assert/strict"
import {createRequire} from "node:module"
import {fileURLToPath} from "node:url"
import {build} from "esbuild"
import React from "react"
import {renderToStaticMarkup} from "react-dom/server"
import type * as W from "@clawdline/contract"

// A Bot asked by another Bot to get the person's decision: the asked Bot (Beacon)
// shows the question under the exchange's capsule, and the asking Bot (Atlas)
// points at Beacon until the person answers. Click behaviour runs in a real
// browser in tools/project-assistants-decision-card.e2e.mjs.
const project:W.ProjectAssistantProjectScope={machine_id:"machine",project_id:"project",canonical_id:"canonical",kind:"repository"}
const atlas={machine_id:"machine",bot_id:"atlas"},beacon={machine_id:"machine",bot_id:"beacon"}
const at="2026-11-23T00:00:00Z"
const fence=(key:W.ProjectAssistantBotKey):W.ProjectAssistantFence=>({key,project,conversation_id:`${key.bot_id}-chat`,binding_id:"",generation:0})
const bot=(key:W.ProjectAssistantBotKey,name:string):W.ProjectAssistantBot=>({key,project,conversation_id:`${key.bot_id}-chat`,name,agent:"claude",
  appearance:{shape:"blob",color:"#e39567"},permission_policy:"ask",instructions:{revision:1,text:"",role_snapshot_ref:""},created_at:at,updated_at:at,version:1})
const exchange=(outcome:W.ProjectAssistantExchange["outcome"]):W.ProjectAssistantExchange=>({id:"x1",project,
  source:{key:atlas,conversation_id:"atlas-chat"},target:{key:beacon,conversation_id:"beacon-chat"},
  source_turn_id:"s",request_turn_id:"r",request:"Ship the release?",outcome,revision:1,hops:1})
const notice=(owner:W.ProjectAssistantBotKey,x:W.ProjectAssistantExchange):W.ProjectAssistantMessage=>
  ({id:`notice-${owner.bot_id}`,seq:1,at,kind:"notice",text:"",turn_id:"t",quote_message_id:"",fence:fence(owner),exchange:x})
const timeline=(...messages:W.ProjectAssistantMessage[]):W.ProjectAssistantReadViewItem[]=>messages.map(message=>({id:message.id,kind:"message",message,anchor_message_seq:message.seq,observed_event_seq:1}))
const summary=(state:W.ProjectAssistantPermissionState="pending"):W.ProjectAssistantReadPermissionSummary=>
  ({id:"d1",turn_id:"t",fence:fence(beacon),state,expires_at:"2099-01-01T00:00:00Z",detail_cursor:"c1",tool_kind:"decision",title:"Ship the release?"})
const offer=(state:W.ProjectAssistantPermissionState="pending",answer=""):W.ProjectAssistantPermissionOffer=>({id:"d1",native_request_id:"",turn_id:"t",fence:fence(beacon),state,
  expires_at:"2099-01-01T00:00:00Z",selected_option_id:"",kind:"decision",
  options:[{id:"ship",kind:"allow_once",label:"Ship it today",allow:true,read_only:true},{id:"wait",kind:"reject_once",label:"Wait for QA",allow:false,read_only:true}],
  decision:{question:"Ship the release?",exchange_id:"x1",source_bot_id:"atlas",request:"Atlas asks whether to ship",...(answer?{answer_text:answer}:{})}} as W.ProjectAssistantPermissionOffer)

async function load() {
  globalThis.document??={getElementById:()=>null,querySelector:()=>null,querySelectorAll:()=>[],addEventListener:()=>{},createElement:()=>({style:{},setAttribute(){},appendChild(){},addEventListener(){}}),documentElement:{style:{},setAttribute(){},lang:"en"},body:{appendChild(){},classList:{add(){},remove(){},toggle(){},contains:()=>false}}} as never
  globalThis.location??=new URL("http://synthetic.invalid/") as never
  const {outputFiles}=await build({stdin:{contents:'export {ChatRows} from "./ChatRows";export {DecisionCard,DecisionPointer,decisionsUnderCapsules} from "./DecisionCard";export {words} from "./words"',
    resolveDir:fileURLToPath(new URL(".",import.meta.url)),loader:"ts"},bundle:true,platform:"node",format:"cjs",packages:"external",write:false,jsx:"automatic",loader:{".css":"empty"}})
  const compiled={exports:{} as {ChatRows:typeof import("./ChatRows").ChatRows;DecisionCard:typeof import("./DecisionCard").DecisionCard;
    DecisionPointer:typeof import("./DecisionCard").DecisionPointer;decisionsUnderCapsules:typeof import("./DecisionCard").decisionsUnderCapsules;words:typeof import("./words").words}}
  new Function("require","module","exports",outputFiles[0].text)(createRequire(import.meta.url),compiled,compiled.exports)
  return compiled.exports
}
const source={machineID:"machine",authorizationRevision:"grant",current:()=>true} as never

test("the asked Bot's chat shows the decision directly under the exchange capsule it came from",async()=>{
  const {ChatRows,DecisionCard,words}=await load(),w=words("en")
  const html=renderToStaticMarkup(React.createElement(ChatRows,{timeline:timeline(notice(beacon,exchange("needs_person"))),w,language:"en",source,botKey:beacon,
    roster:[bot(atlas,"Atlas"),bot(beacon,"Beacon")],
    renderExchangeDecision:(id:string)=>id==="x1"?React.createElement(DecisionCard,{summary:summary(),offer:offer(),enabled:true,onAnswer:async()=>{},askerName:"Beacon",requesterName:"Atlas",w}):null}))
  const capsule=html.indexOf('class="pa-exchange-attribution"'),entry=html.indexOf("pa-exchange-entry"),card=html.indexOf("pa-decision")
  assert.ok(capsule>=0&&entry>capsule&&card>entry,"the card follows the capsule button")
  assert.ok(html.slice(capsule,card).includes("Atlas"),"the capsule names the asking Bot")
  assert.match(html,/Needs your decision/)
  assert.match(html,/Ship it today/);assert.match(html,/Wait for QA/);assert.match(html,/Reply yourself/)
  assert.doesNotMatch(html,/You replied/)
})

test("the asking Bot shows a waiting pointer, not a finished exchange, until the person decides",async()=>{
  const {ChatRows,words}=await load()
  for(const [language,waiting,go] of [["en","Beacon is waiting for your decision","Go to Beacon"],["zh-Hant","Beacon 在等你決定","前往 Beacon"]] as const){
    const render=(outcome:W.ProjectAssistantExchange["outcome"])=>renderToStaticMarkup(React.createElement(ChatRows,{timeline:timeline(notice(atlas,exchange(outcome))),
      w:words(language),language,source,botKey:atlas,roster:[bot(atlas,"Atlas"),bot(beacon,"Beacon")],onOpenBot:()=>{}}))
    const html=render("needs_person")
    assert.ok(html.includes(waiting),language);assert.ok(html.includes(go),language)
    assert.ok(html.indexOf("pa-decision-pointer")>html.indexOf("pa-exchange-entry"),"pointer under the capsule")
    const done=render("completed")
    assert.equal(done.includes(waiting),false,"a completed exchange no longer points at the person")
  }
})

test("the go-to shortcut opens the Bot the question is waiting in",async()=>{
  const {DecisionPointer,words}=await load()
  const opened:string[]=[]
  // DecisionPointer holds no state, so its element tree can be read directly and its button pressed.
  const tree=DecisionPointer({name:"Beacon",w:words("en"),onOpen:()=>opened.push("beacon")}) as React.ReactElement<{children:React.ReactNode}>
  const button=React.Children.toArray(tree.props.children).find((child):child is React.ReactElement<{onClick:()=>void}>=>React.isValidElement(child)&&child.type==="button")
  assert.ok(button);button.props.onClick()
  assert.deepEqual(opened,["beacon"])
  const none=DecisionPointer({name:"Beacon",w:words("en")}) as React.ReactElement<{children:React.ReactNode}>
  assert.equal(React.Children.toArray(none.props.children).some(child=>React.isValidElement(child)&&child.type==="button"),false,"no shortcut when the Bot cannot be opened")
})

test("an answered decision shows the person's reply and no longer offers choices",async()=>{
  const {DecisionCard,words}=await load()
  const html=renderToStaticMarkup(React.createElement(DecisionCard,{summary:summary("answered"),offer:offer("answered","Wait for QA"),enabled:true,onAnswer:async()=>assert.fail("rendering answered"),askerName:"Beacon",w:words("en")}))
  assert.match(html,/You replied: Wait for QA/)
  assert.doesNotMatch(html,/Ship it today|Reply yourself/)
})

test("memory proposals use localized controls and show the confirmed detail while the summary catches up",async()=>{
  const {DecisionCard,words}=await load()
  const fact="The maintenance window starts on Wednesday."
  const memorySummary={...summary(),title:"Save this to memory?"}
  const memoryOffer={...offer(),decision:{question:memorySummary.title,memory:fact},
    options:[{...offer().options[0],id:"reply-memory-save",label:"Save"},{...offer().options[1],id:"reply-memory-skip",label:"Don't save"}]}
  const saved={...memoryOffer,state:"answered" as const,selected_option_id:"reply-memory-save"}
  const skipped={...saved,selected_option_id:"reply-memory-skip"}
  for(const [language,title,save,skip,stored,declined] of [
    ["zh-Hant","要記下這條嗎？","記下","不要","已記下","沒有記下"],
    ["en","Save to memory?","Save","Don't save","Saved to memory","Not saved"],
  ]){
    const w=words(language)
    const render=(s:W.ProjectAssistantReadPermissionSummary,o?:W.ProjectAssistantPermissionOffer)=>
      renderToStaticMarkup(React.createElement(DecisionCard,{summary:s,offer:o,enabled:true,onAnswer:async()=>assert.fail("rendering only"),askerName:"Beacon",w}))
    const loading=render(memorySummary)
    assert.ok(loading.includes(title),"the roster can focus the localized card while its detail loads")
    assert.ok(!loading.includes(memorySummary.title))
    assert.doesNotMatch(loading,/<button/)
    const pending=render(memorySummary,memoryOffer)
    for(const value of [title,fact,`<span>${save}</span>`,`<span>${skip.replace("'", "&#x27;")}</span>`])assert.ok(pending.includes(value),value)
    assert.doesNotMatch(pending,/pa-decision-own|pa-decision-dismiss/)
    const answeredSummary={...memorySummary,state:"answered" as const}
    assert.ok(!render(answeredSummary,memoryOffer).includes(declined),"a stale detail does not imply a skip")
    const expiredSummary={...memorySummary,state:"expired" as const}
    for(const cardSummary of [memorySummary,answeredSummary,expiredSummary]){
      for(const [answer,label] of [[saved,stored],[skipped,declined]] as const){
        const answered=render(cardSummary,answer)
        assert.ok(answered.includes(label),`${label} with a ${cardSummary.state} summary`)
        assert.ok(answered.includes(fact))
        assert.doesNotMatch(answered,/<button|aria-busy|pa-decision-spinner/)
      }
    }
  }
})

test("only a decision whose exchange capsule is loaded moves under it; others stay in the permission section",async()=>{
  const {decisionsUnderCapsules}=await load()
  const loaded={summary:summary(),offer:offer()},unread={summary:{...summary(),id:"d2"}},native={summary:{...summary(),id:"p1",tool_kind:"execute"},offer:{...offer(),id:"p1"}}
  const capsule=notice(beacon,exchange("needs_person"))
  assert.deepEqual([...decisionsUnderCapsules([loaded,unread,native],[capsule],new Set()).entries()].map(([id,card])=>[id,card.summary.id]),[["x1","d1"]])
  assert.equal(decisionsUnderCapsules([loaded],[],new Set()).size,0,"no capsule in the loaded page")
  assert.equal(decisionsUnderCapsules([loaded],[capsule],new Set(["d1"])).size,0,"an inline team card keeps its own place")
})

test("a pending question offers No need, which the person can press to close it",async()=>{
  const {DecisionCard,words}=await load()
  for(const language of ["en","zh-Hant"]){
    const w=words(language)
    const html=renderToStaticMarkup(React.createElement(DecisionCard,{summary:summary(),offer:offer(),enabled:true,onAnswer:async()=>{},askerName:"Beacon",w}))
    assert.match(html,new RegExp(`<button type="button" class="pa-decision-own pa-decision-dismiss" title="${w.decisionDismissText}"><span>${w.decisionDismiss}</span></button>`))
    const answered=renderToStaticMarkup(React.createElement(DecisionCard,{summary:summary("answered"),offer:offer("answered",w.decisionDismissText),enabled:true,onAnswer:async()=>{},askerName:"Beacon",w}))
    assert.ok(!answered.includes("pa-decision-dismiss"),"an answered question has nothing to close")
  }
})
