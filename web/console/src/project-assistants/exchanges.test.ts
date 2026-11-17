import {test} from "node:test"
import assert from "node:assert/strict"
import {createRequire} from "node:module"
import {fileURLToPath} from "node:url"
import {build} from "esbuild"
import React from "react"
import {renderToStaticMarkup} from "react-dom/server"
import type * as W from "@clawdline/contract"
import type {SourceClient} from "./client"
// @ts-expect-error -- Node loads source extensions.
import {loadExchange,scopedExchange,exchangeOutcome,exchangeDelivery} from "./exchanges.ts"
// @ts-expect-error -- Node loads source extensions.
import {errorCode} from "./client.ts"

const key={machine_id:"machine",bot_id:"requester"},peer={...key,bot_id:"responder"}
const project:W.ProjectAssistantProjectScope={machine_id:key.machine_id,project_id:"project",canonical_id:"canonical",kind:"repository"}
const scope={key,project,conversation_id:"source-chat"}
const exchange:W.ProjectAssistantExchange={id:"exchange",project,source:{key,conversation_id:"source-chat"},target:{key:peer,conversation_id:"target-chat"},source_turn_id:"source-turn",request_turn_id:"request-turn",request:"Review\n<script>untrusted</script>\n"+"long line ".repeat(2000),reply:"Looks complete",detail:"Preserved diagnostic",outcome:"unknown",revision:3,hops:1,delivery:"waiting"}
function fixture(){
  const value={contract_version:"project-assistants.v1",bot:{key,project,conversation_id:scope.conversation_id,name:"Requester"},conversation:{key,id:scope.conversation_id},exchanges:[structuredClone(exchange)]} as W.ProjectAssistantExport
  const status:W.ProjectAssistantSourcesResponse={contract_version:"project-assistants.v1",sources:[{machine_id:key.machine_id,state:"online",access:"history_only",readable:true,writable:false,fresh:true,observed_at:"",capabilities:[]}]}
  const calls:string[]=[],source={machineID:key.machine_id,authorizationRevision:"grant",current:()=>true,async export(k:W.ProjectAssistantBotKey){assert.deepEqual(k,key);calls.push("export");return value},async status(){calls.push("status");return status}} as unknown as SourceClient
  return {value,status,calls,source}
}
const signal=()=>new AbortController().signal

test("exchange detail preserves full request/reply/detail from a captured read-only export without commands",async()=>{
  const f=fixture(),result=await loadExchange(f.source,scope,exchange,signal())
  assert.deepEqual(result,{exchange,botName:"Requester"});assert.deepEqual(f.calls,["export","status"])
  assert.equal(exchangeOutcome(result.exchange.outcome),"unknown");assert.equal(exchangeDelivery(result.exchange.delivery),"waiting")
  assert.equal(scopedExchange(exchange,{key:peer,project,conversation_id:"target-chat"}),exchange)
})

test("exchange scope refuses foreign machine, Project, either Bot and conversation before publishing",async()=>{
  const changes:Array<(f:ReturnType<typeof fixture>)=>void>=[
    f=>{f.value.bot.key={...key,bot_id:"other"}},f=>{f.value.bot.project={...project,project_id:"other"}},
    f=>{f.value.conversation.id="other"},f=>{f.value.conversation.key={...key,machine_id:"other"}},
    f=>{f.value.exchanges![0].project={...project,canonical_id:"other"}},f=>{f.value.exchanges![0].project={...project,project_id:"other"}},
    f=>{f.value.exchanges![0].source.key={...key,machine_id:"other"}},f=>{f.value.exchanges![0].target.key={...peer,bot_id:"other"}},
    f=>{f.value.exchanges![0].target.conversation_id="other"},f=>{f.value.exchanges![0].source_turn_id="other"},
  ]
  for(const change of changes){const f=fixture();change(f);await assert.rejects(loadExchange(f.source,scope,exchange,signal()),e=>errorCode(e)==="pa_scope_denied")}
  const f=fixture();Object.defineProperty(f.source,"machineID",{value:"foreign"});await assert.rejects(loadExchange(f.source,scope,exchange,signal()),e=>errorCode(e)==="pa_scope_denied");assert.deepEqual(f.calls,[])
})

test("missing, duplicated, malformed, stale and revoked exchange reads remain explicit failures",async()=>{
  for(const kind of ["missing","duplicate","malformed","stale","offline","revoked","hidden","unreadable"]){
    const f=fixture()
    if(kind==="missing")f.value.exchanges=[]
    if(kind==="duplicate")f.value.exchanges!.push(structuredClone(exchange))
    if(kind==="malformed")f.value.exchanges![0].reply=42 as unknown as string
    if(kind==="stale")f.value.exchanges![0].revision=1
    if(kind==="offline")f.status.sources[0].fresh=false
    if(kind==="revoked")f.status.sources[0].state="revoked"
    if(kind==="hidden")f.status.sources[0].access="hidden"
    if(kind==="unreadable")f.status.sources[0].readable=false
    await assert.rejects(loadExchange(f.source,scope,exchange,signal()),e=>errorCode(e)===({missing:"pa_not_found",duplicate:"pa_invalid",malformed:"pa_invalid",stale:"pa_stale_version",offline:"pa_offline",revoked:"pa_revoked",hidden:"pa_scope_denied",unreadable:"pa_scope_denied"}[kind]))
  }
})

test("abort, authority replacement and revocation discard late export or status results; explicit retry can recover",async()=>{
  for(const stage of ["export","status"] as const)for(const kind of ["abort","revoke","revision"]){
    const f=fixture(),control=new AbortController(),read=f.source[stage].bind(f.source);let release!:()=>void
    Reflect.set(f.source,stage,async(...args:unknown[])=>{const answer=await (read as (...args:unknown[])=>Promise<unknown>)(...args);await new Promise<void>(resolve=>{release=resolve});return answer})
    const pending=loadExchange(f.source,scope,exchange,control.signal)
    while(!release)await new Promise(resolve=>setImmediate(resolve))
    if(kind==="abort")control.abort();if(kind==="revoke")f.source.current=()=>false;if(kind==="revision")Object.defineProperty(f.source,"authorizationRevision",{value:"replacement"})
    release();await assert.rejects(pending,e=>kind==="abort"?(e as Error).name==="AbortError":errorCode(e)==="pa_revoked")
  }
  const f=fixture();f.value.exchanges=[];await assert.rejects(loadExchange(f.source,scope,exchange,signal()));f.value.exchanges=[exchange];assert.equal((await loadExchange(f.source,scope,exchange,signal())).exchange.id,exchange.id)
})

test("known and future request/delivery states stay distinct regardless of final-looking reply text",()=>{
  for(const state of ["queued","sent","completed","failed","cancelled","unknown"] as const)assert.equal(exchangeOutcome(state),state)
  assert.equal(exchangeOutcome("future"),"unknown")
  for(const state of ["waiting","queued","delivered","dropped"] as const)assert.equal(exchangeDelivery(state),state)
  assert.equal(exchangeDelivery(undefined),"unknown");assert.equal(exchangeDelivery("future"),"unknown")
})

test("rendered chat offers the exchange action only for its captured Bot; panel starts with bilingual loading",async()=>{
  if(typeof document==="undefined")Object.defineProperty(globalThis,"document",{configurable:true,value:{getElementById:()=>null,querySelector:()=>null,querySelectorAll:()=>[],addEventListener:()=>{},createElement:()=>({style:{},setAttribute(){},appendChild(){},addEventListener(){}}),documentElement:{style:{},setAttribute(){},lang:"en"},body:{appendChild(){},classList:{add(){},remove(){},toggle(){},contains:()=>false}}}})
  const {outputFiles}=await build({stdin:{contents:'export {ChatRows} from "./ChatRows";export {ExchangePanel} from "./ExchangePanel";export {words} from "./words"',resolveDir:fileURLToPath(new URL(".",import.meta.url)),loader:"ts"},bundle:true,platform:"node",format:"cjs",packages:"external",write:false,jsx:"automatic",loader:{".css":"empty"}})
  const compiled={exports:{} as {ChatRows:typeof import("./ChatRows").ChatRows;ExchangePanel:typeof import("./ExchangePanel").ExchangePanel;words:typeof import("./words").words}}
  new Function("require","module","exports",outputFiles[0].text)(createRequire(import.meta.url),compiled,compiled.exports)
  const message:W.ProjectAssistantMessage={id:"notice",seq:1,at:"2026-11-17T00:00:00Z",kind:"notice",text:"Review requested",turn_id:"source-turn",quote_message_id:"",fence:{...scope,binding_id:"",generation:1},exchange}
  const source=fixture().source
  const render=(m:W.ProjectAssistantMessage,botKey=key,language="en")=>renderToStaticMarkup(React.createElement(compiled.exports.ChatRows,{timeline:[{id:m.id,kind:"message",message:m,anchor_message_seq:1,observed_event_seq:1}],source,botKey,onQuote:()=>{},w:compiled.exports.words(language),language}))
  assert.match(render(message),/>View Bot exchange<\/button>/)
  assert.match(render(message,key,"zh-Hant-TW"),/>查看 Bot 互審往來<\/button>/)
  assert.doesNotMatch(render({...message,exchange:undefined}),/>View Bot exchange</)
  assert.doesNotMatch(render(message,peer),/>View Bot exchange</)
  for(const [language,label] of [["en","Loading exchange…"],["zh-Hant-TW","正在載入往來詳情…"]]){
    const html=renderToStaticMarkup(React.createElement(compiled.exports.ExchangePanel,{source,scope,reference:exchange,language,onClose:()=>{}}))
    assert.ok(html.includes(label));assert.ok(html.includes('aria-busy="true"'));assert.ok(!html.includes("<script>"));assert.ok(!html.includes(exchange.reply!))
  }
})
