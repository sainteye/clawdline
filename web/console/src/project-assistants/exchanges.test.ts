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
import {loadExchangePage,loadExchangeBot,scopedExchange,exchangeOutcome,exchangeDelivery,exchangeEntries,mergeExchangeEntries} from "./exchanges.ts"
// @ts-expect-error -- Node loads source extensions.
import {errorCode,failure} from "./client.ts"

const key={machine_id:"machine",bot_id:"requester"},peer={...key,bot_id:"responder"}
const project:W.ProjectAssistantProjectScope={machine_id:key.machine_id,project_id:"project",canonical_id:"canonical",kind:"repository"}
const scope={key,project,conversation_id:"source-chat"}
const exchange:W.ProjectAssistantExchange={id:"exchange",project,source:{key,conversation_id:"source-chat"},target:{key:peer,conversation_id:"target-chat"},source_turn_id:"source-turn",request_turn_id:"request-turn",request:"Review\n<script>untrusted</script>\n"+"long line ".repeat(2000),reply:"Looks complete",detail:"Preserved diagnostic",outcome:"unknown",revision:3,hops:1,delivery:"waiting"}
const at="2026-11-17T00:00:00Z"
const fence:W.ProjectAssistantFence={...scope,binding_id:"",generation:0}
const own:W.ProjectAssistantBot={...scope,name:"Requester",agent:"codex",appearance:{shape:"blob",color:"#e39567"},permission_policy:"ask",instructions:{revision:1,text:"",role_snapshot_ref:""},created_at:at,updated_at:at,version:1}
const targetProject:W.ProjectAssistantProjectScope={...project,project_id:"other",canonical_id:"other-canonical"}
const target:W.ProjectAssistantBot={...own,key:peer,project:targetProject,conversation_id:"new-target-chat",name:"Partner"}
const notice=(x=exchange,seq=1):W.ProjectAssistantMessage=>({id:`notice-${seq}`,seq,at,kind:"notice",text:"Review requested",turn_id:"source-turn",quote_message_id:"",fence,exchange:x})
const row=(message:W.ProjectAssistantMessage):W.ProjectAssistantReadViewItem=>({id:message.id,kind:"message",message,anchor_message_seq:message.seq,observed_event_seq:10})
function fixture() {
  const status:W.ProjectAssistantSourceStatus={machine_id:key.machine_id,state:"online",access:"history_only",readable:true,writable:false,fresh:true,observed_at:"",capabilities:[]}
  const conversation={key,id:scope.conversation_id,binding_id:"",generation:0,active_turn_id:"",last_message_seq:4,memory_revision:1,version:1}
  const value:W.ProjectAssistantReadViewResponse={contract_version:"project-assistants.v1",mode:"snapshot",source_epoch:"epoch",source:status,
    head:{key,project,current_fence:fence,conversation,bot_version:1,memory_revision:1,event_frontier:10,unknown_turn_count:0,pending_permission_count:0,trace_category_count:0},
    items:[row(notice(structuredClone(exchange)))],snapshot:{bot:own,pending_turns:[],queued_messages:[]},has_more:true,next_cursor:"earlier",next_seq:0}
  const calls:W.ProjectAssistantReadViewRequest[]=[]
  const source={machineID:key.machine_id,authorizationRevision:"grant",current:()=>true,
    async export(){throw failure("pa_capacity_full")},
    async view(query:W.ProjectAssistantReadViewRequest){calls.push(query);return value},
    async get(k:W.ProjectAssistantBotKey){assert.deepEqual(k,peer);return {contract_version:value.contract_version,bot:target,source:status,conversation}}
  } as unknown as SourceClient
  return {value,status,calls,source}
}
const signal=()=>new AbortController().signal

test("exchange pages preserve full request, reply and detail without requesting a Bot export",async()=>{
  const f=fixture()
  f.value.items.push(row(notice({...exchange,id:"other-exchange",target:{key:{...peer,bot_id:"other"},conversation_id:"other-chat"}},2)))
  f.value.items.push(row(notice({...exchange,request_turn_id:"changed",revision:4},3)))
  const first=await loadExchangePage(f.source,scope,exchange,signal())
  assert.deepEqual(first.entries,[{exchange,seq:1}])
  assert.equal(first.bot?.name,"Requester")
  assert.deepEqual(first.next,{cursor:"earlier",sourceEpoch:"epoch"})
  Object.assign(f.value,{mode:"history",snapshot:undefined,has_more:false,next_cursor:"",items:[row(notice({...exchange,id:"older",outcome:"completed"},1))]})
  const earlier=await loadExchangePage(f.source,scope,exchange,signal(),first.next)
  assert.equal(earlier.entries[0].exchange.id,"older")
  assert.equal(earlier.next,undefined)
  assert.deepEqual(f.calls.map(({mode,cursor,source_epoch})=>[mode,cursor,source_epoch]),[["snapshot","",""],["history","earlier","epoch"]])
  assert.equal(scopedExchange(exchange,{key:peer,project,conversation_id:"target-chat"}),exchange)
})

test("old notice pages retain evidence of the actual send and reply times",()=>{
  const request={...notice({...exchange,outcome:"queued",revision:1,reply:undefined}),id:"exchange-queued"}
  const replyAt="2026-11-17T01:00:00Z",promotedAt="2026-11-17T02:00:00Z"
  const completion={...notice({...exchange,outcome:"completed",revision:3},2),id:"exchange-result-source",at:replyAt}
  const promotion={...notice({...exchange,outcome:"completed",revision:4,delivery:"queued"},3),id:"exchange-reply-input",at:promotedAt}
  assert.equal(exchangeEntries([promotion],scope)[0].replyAt,undefined)
  const [entry]=mergeExchangeEntries([promotion,request,completion].flatMap(message=>exchangeEntries([message],scope)))
  assert.equal(entry.requestAt,at)
  assert.equal(entry.replyAt,replyAt)
  assert.equal(entry.exchange.revision,4)
})

test("exchange pages reject malformed results, foreign scope and unreadable sources",async()=>{
  for(const [change,code] of [
    [(f:ReturnType<typeof fixture>)=>{f.value.items=[row({...notice(),fence:{...fence,conversation_id:"other"}})]},"pa_scope_denied"],
    [(f:ReturnType<typeof fixture>)=>{f.value.next_cursor=""},"pa_invalid"],
    [(f:ReturnType<typeof fixture>)=>{f.status.fresh=false},"pa_offline"],
    [(f:ReturnType<typeof fixture>)=>{f.status.state="revoked"},"pa_revoked"],
    [(f:ReturnType<typeof fixture>)=>{f.status.access="hidden"},"pa_scope_denied"],
  ] as const){
    const f=fixture();change(f)
    await assert.rejects(loadExchangePage(f.source,scope,exchange,signal()),e=>errorCode(e)===code)
  }
  const f=fixture();Object.defineProperty(f.source,"machineID",{value:"foreign"})
  await assert.rejects(loadExchangePage(f.source,scope,exchange,signal()),e=>errorCode(e)==="pa_scope_denied")
  assert.deepEqual(f.calls,[])
})

test("peer profile lookup accepts a new conversation in the recorded Project, and rejects a different Project",async()=>{
  const f=fixture()
  assert.deepEqual(await loadExchangeBot(f.source,{key:peer,project:targetProject},signal()),target)
  await assert.rejects(loadExchangeBot(f.source,{key:peer,project},signal()),e=>errorCode(e)==="pa_scope_denied")
})

test("closing the panel or changing the grant discards a late page",async()=>{
  for(const kind of ["abort","revoke","revision"]){
    const f=fixture(),control=new AbortController(),read=f.source.view.bind(f.source);let release!:()=>void
    f.source.view=async(...args)=>{const answer=await read(...args);await new Promise<void>(resolve=>{release=resolve});return answer}
    const pending=loadExchangePage(f.source,scope,exchange,control.signal)
    while(!release)await new Promise(resolve=>setImmediate(resolve))
    if(kind==="abort")control.abort()
    if(kind==="revoke")f.source.current=()=>false
    if(kind==="revision")Object.defineProperty(f.source,"authorizationRevision",{value:"replacement"})
    release();await assert.rejects(pending,e=>kind==="abort"?(e as Error).name==="AbortError":errorCode(e)==="pa_revoked")
  }
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
  const message=notice()
  const source=fixture().source
  const render=(m:W.ProjectAssistantMessage,botKey=key,language="en",roster?:W.ProjectAssistantBot[])=>renderToStaticMarkup(React.createElement(compiled.exports.ChatRows,{timeline:[{id:m.id,kind:"message",message:m,anchor_message_seq:1,observed_event_seq:1}],source,botKey,roster,onQuote:()=>{},w:compiled.exports.words(language),language}))
  const crossProject={...message,exchange:{...exchange,target_project:targetProject}}
  assert.match(render(crossProject,key,"zh-Hant-TW",[target]),/aria-label="已傳訊息給 Partner"/)
  assert.match(render(crossProject,key,"en",[{...target,project,name:"Wrong"},target]),/aria-label="Messaged Partner"/)
  const incoming={...crossProject,fence:{...fence,key:peer,conversation_id:"target-chat",project:targetProject}}
  assert.match(render(incoming,peer,"en",[own,target]),/aria-label="Message from Requester"/)
  // The recipient's chat shows what it replied, so a refusal is visible without opening the exchange.
  const refused={...incoming,exchange:{...incoming.exchange,outcome:"completed",reply:"I could not save this to memory: edit_memory was refused (pa_scope_denied).\nPlease tell me directly."}}
  assert.match(render(refused,peer,"en",[own,target]),/class="pa-exchange-summary">I could not save this to memory: edit_memory was refused \(pa_scope_denied\)\. Please tell me directly\.</)
  const long=render({...refused,exchange:{...refused.exchange,reply:"x".repeat(400)}},peer,"en",[own,target])
  assert.match(long,new RegExp(`pa-exchange-summary">${"x".repeat(160)}…<`))
  assert.ok(!render(crossProject,key,"en",[target]).includes("pa-exchange-summary"))
  assert.match(render(message),/aria-label="Messaged Unknown Bot"/)
  assert.match(render(message,key,"zh-Hant-TW"),/aria-label="已傳訊息給 未知的 Bot"/)
  assert.ok(!render(message).includes("Review requested"))
  assert.ok(!render({...message,exchange:undefined}).includes("pa-exchange-entry"))
  assert.ok(!render(message,peer).includes("pa-exchange-entry"))
  const updates=exchangeEntries([message,{...message,id:"updated",seq:2,exchange:{...exchange,revision:exchange.revision+1,outcome:"completed"}}],scope)
  assert.equal(updates.length,1)
  assert.equal(updates[0].seq,1)
  assert.equal(updates[0].exchange.outcome,"completed")
  for(const [language,label] of [["en","Loading exchange…"],["zh-Hant-TW","正在載入往來詳情…"]]){
    const html=renderToStaticMarkup(React.createElement(compiled.exports.ExchangePanel,{source,scope,reference:exchange,entries:[],language,w:compiled.exports.words(language),onClose:()=>{}}))
    assert.ok(html.includes(label));assert.ok(html.includes('aria-busy="true"'));assert.ok(!html.includes("<script>"));assert.ok(!html.includes(exchange.reply!))
  }
})
