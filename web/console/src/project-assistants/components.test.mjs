import assert from "node:assert/strict"
import { test } from "node:test"
import { createRequire } from "node:module"
import { fileURLToPath } from "node:url"
import { build } from "esbuild"
import React from "react"
import { renderToStaticMarkup } from "react-dom/server"
const {outputFiles}=await build({stdin:{contents:'export {BotOrb} from "./BotOrb";export {PermissionCard} from "./PermissionCard";export {withPermissionRecovery} from "./Chat";export {Trace} from "./Trace";export {readView} from "./read-views";export {SourceStatusPanel} from "./SourceStatusPanel";export {sourceHealth} from "./source-status";export {ChatRows} from "./ChatRows";export {fixtureSource} from "./fixtures";export {words} from "./words"',resolveDir:fileURLToPath(new URL(".",import.meta.url)),loader:"ts"},bundle:true,platform:"node",format:"cjs",packages:"external",write:false,jsx:"automatic",loader:{".css":"empty"}})
const compiled={exports:{}}
new Function("require","module","exports",outputFiles[0].text)(createRequire(import.meta.url),compiled,compiled.exports)
const {BotOrb,PermissionCard,withPermissionRecovery,Trace,readView,SourceStatusPanel,sourceHealth,ChatRows,fixtureSource,words}=compiled.exports
const render=(component,props)=>renderToStaticMarkup(React.createElement(component,props))
test("fixed global entry is bilingual and has no microphone or model side effect",()=>{
  let opened=0
  for(const language of ["en","zh-Hant"]){const html=render(BotOrb,{onOpen:()=>opened++,language,unread:2,pendingPermissions:1});assert.match(html,/aria-label=/);assert.match(html,/#e39567/);assert.match(html,/>3</);assert.doesNotMatch(html,/microphone|mic|audio/)}
  assert.equal(opened,0)
})
test("permission options keep the persistent grants and disable them in history",async()=>{
  const s=fixtureSource(),page=await s.events({key:s.bots[1].key,after_seq:0,page_size:256},new AbortController().signal),offer=page.items[0].permission
  const html=render(PermissionCard,{offer,enabled:false,onAnswer:()=>assert.fail("render answered"),w:words("en")})
  assert.equal((html.match(/disabled=""/g)||[]).length,3)
  assert.match(html,/Always allow/)
})
test("an offline machine shows as a problem at the top of the list, with its fix",async()=>{
  const s=fixtureSource();s.setState("offline")
  const value=await s.status(new AbortController().signal),w=words("zh-Hant")
  const html=render(SourceStatusPanel,{health:sourceHealth(value.sources,new Map(),[],w),w})
  assert.match(html,/這台機器：離線/);assert.match(html,/確認那台機器開著/);assert.match(html,/aria-expanded="false"/)
})
test("reading, selecting and metadata creation do not start a scripted model",async()=>{
  const log=[],s=fixtureSource("one",log),signal=new AbortController().signal
  await s.status(signal);await s.projects({machine_id:"one",cursor:"",page_size:100},signal);const page=await s.list({machine_id:"one",query:"",cursor:"",page_size:100},signal)
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
test("bounded fixture snapshots begin after a genuine600-event machine frontier and never call Export",async()=>{
  const source=fixtureSource(),signal=new AbortController().signal,key=source.bots[0].key
  source.seedHistory(600);source.configureViews({forbidLegacy:true})
  const request={key,conversation_id:source.bots[0].conversation_id,mode:"snapshot",source_epoch:"",cursor:"",after_seq:0,page_size:100}
  const page=readView(await source.view(request,signal),request,{key,project:source.bots[0].project,conversation_id:request.conversation_id})
  assert.equal(page.items.length,100);assert.equal(page.head.event_frontier,602)
  source.emitMessage("603");source.emitMessage("604")
  const next=await source.view({...request,mode:"events",source_epoch:page.source_epoch,after_seq:602,page_size:256},signal)
  assert.deepEqual(next.items.map(row=>row.event.seq),[603,604])
  assert.equal(next.next_seq,604)
  assert.equal(source.log.filter(row=>["export","get","history","events"].includes(row.method)).length,0)
})
test("101 permission and trace summaries remain separate from complete detail and honest totals",async()=>{
  const source=fixtureSource(),signal=new AbortController().signal,key=source.bots[0].key
  source.seedCollections(101)
  const base={key,conversation_id:source.bots[0].conversation_id,mode:"snapshot",source_epoch:"",cursor:"",after_seq:0,page_size:100}
  const snapshot=await source.view(base,signal)
  assert.equal(snapshot.head.pending_permission_count,101);assert.equal(snapshot.head.trace_category_count,101)
  const first=await source.view({...base,mode:"permissions",source_epoch:snapshot.source_epoch},signal)
  assert.equal(first.items.length,100);assert.equal(first.has_more,true)
  const summary=first.items[0].permission_summary
  const html=render(PermissionCard,{summary,enabled:true,onDetail:()=>assert.fail("render loaded detail"),onAnswer:()=>assert.fail("summary answered"),w:words("en")})
  assert.match(html,/View details/);assert.doesNotMatch(html,/Allow once/)
  const last=await source.view({...base,mode:"permissions",source_epoch:snapshot.source_epoch,cursor:first.next_cursor},signal)
  assert.equal(last.items.length,1);assert.equal(last.has_more,false)
  const detail=await source.view({...base,mode:"detail",source_epoch:snapshot.source_epoch,cursor:summary.detail_cursor,page_size:1},signal)
  assert.equal(detail.items[0].permission.options.length,101)
  const traces=await source.view({...base,mode:"traces",source_epoch:snapshot.source_epoch},signal)
  const traceHTML=render(Trace,{summaries:traces.items.map(row=>row.trace_summary),total:snapshot.head.trace_category_count,more:true,w:words("zh-Hant")})
  assert.match(traceHTML,/101/);assert.match(traceHTML,/查看詳細內容/);assert.doesNotMatch(traceHTML,/Complete trace/)
})
test("a late second permission preflight refusal latches only its current scope and never enters the answer effect",async()=>{
  for(const code of ["pa_cursor_expired","pa_stale_generation","pa_capacity_full"])for(const current of [true,false]){
    const refusal={contract_version:"project-assistants.v1",error:{code,field:"detail",retry_after_seconds:0}}
    let rejectDetail,locks=0,effects=0,attempts=0
    const detail=new Promise((_,reject)=>{rejectDetail=reject})
    const attempt=withPermissionRecovery(async()=>{attempts++;await detail;effects++;return "answered"},()=>current,()=>{locks++})
    rejectDetail(refusal)
    await assert.rejects(attempt,error=>error===refusal)
    assert.equal(locks,current&&code!=="pa_capacity_full"?1:0,`${code} current=${current}`)
    assert.equal(effects,0);assert.equal(attempts,1,"no automatic permission replay")
  }
})
// As in Codync (decided 2026-11-14), Always allow and Never are offered.
test("an enabled permission card offers allowing once and always",()=>{
  const offer={id:"offer",fence:{key:{machine_id:"m",bot_id:"b"},project:{machine_id:"m",project_id:"p",canonical_id:"c",kind:"repository"},conversation_id:"c",binding_id:"x",generation:1},turn_id:"t",native_request_id:"1",state:"pending",expires_at:"2099-01-01T00:00:00Z",selected_option_id:"",
    options:[{id:"once",kind:"allow_once",allow:true,read_only:false,label:"Yes"},{id:"always",kind:"allow_always",allow:true,read_only:false,label:"Always"},{id:"no",kind:"reject_once",allow:false,read_only:false,label:"No"}]}
  const html=render(PermissionCard,{offer,enabled:true,onAnswer:async()=>{},w:words("en")})
  const buttons=[...html.matchAll(/<button[^>]*>([^<]*)<\/button>/g)].map(m=>[m[1],/disabled=""/.test(m[0])])
  assert.deepEqual(buttons,[["Allow once",false],["Always allow",false],["Reject once",false]])
})
test("a permission card says what the tool will do before it is answered",()=>{
  const fence={key:{machine_id:"m",bot_id:"b"},project:{machine_id:"m",project_id:"p",canonical_id:"c",kind:"repository"},conversation_id:"c",binding_id:"x",generation:1}
  const offer={id:"offer",fence,turn_id:"t",native_request_id:"1",state:"pending",expires_at:"2099-01-01T00:00:00Z",selected_option_id:"",options:[{id:"once",kind:"allow_once",allow:true,read_only:false,label:"Yes"}],
    action:{title:"rm -rf /tmp/x",tool_kind:"execute",detail:"rm -rf /tmp/x",diffs:[],shortened:false}}
  const run=render(PermissionCard,{offer,enabled:true,onAnswer:async()=>{},w:words("zh-Hant")})
  assert.match(run,/想執行指令/);assert.match(run,/rm -rf \/tmp\/x/)
  const edit={...offer,action:{title:"Write notes.md",tool_kind:"edit",detail:"/repo/notes.md",diffs:[{path:"/repo/notes.md",old_text:"old line",new_text:"new line"}],shortened:true}}
  const changed=render(PermissionCard,{offer:edit,enabled:true,onAnswer:async()=>{},w:words("en")})
  assert.match(changed,/Wants to change a file/);assert.match(changed,/old line/);assert.match(changed,/new line/);assert.match(changed,/Shortened/)
  const summaryOnly=render(PermissionCard,{summary:{id:"offer",fence,turn_id:"t",state:"pending",expires_at:"2099-01-01T00:00:00Z",detail_cursor:"d",title:"curl example.com",tool_kind:"fetch"},enabled:true,onAnswer:async()=>{},w:words("en")})
  assert.match(summaryOnly,/Wants to use the network/);assert.match(summaryOnly,/curl example.com/)
})
