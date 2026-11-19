import assert from "node:assert/strict"
import { after, test } from "node:test"
import { createRequire } from "node:module"
import { fileURLToPath } from "node:url"
import { build } from "esbuild"
import React from "react"
import { renderToStaticMarkup } from "react-dom/server"
// ChatRows renders replies with the console's legacy Markdown reader, whose
// module chain looks up a few elements when it loads; a page always has a
// document, this test environment needs a stand-in.
globalThis.document??={getElementById:()=>null,querySelector:()=>null,querySelectorAll:()=>[],addEventListener:()=>{},createElement:()=>({style:{},setAttribute(){},appendChild(){},addEventListener(){}}),documentElement:{style:{},setAttribute(){},lang:"en"},body:{appendChild(){},classList:{add(){},remove(){},toggle(){},contains:()=>false}}}
// The composer shares the browser recorder; rendering does not open it.
globalThis.location??=new URL("http://synthetic.invalid/")
globalThis.window??={document:globalThis.document,location:globalThis.location,navigator:globalThis.navigator,matchMedia:()=>({matches:false,addEventListener(){},removeEventListener(){}}),addEventListener(){},removeEventListener(){}}
const {outputFiles}=await build({stdin:{contents:'export {BotOrb} from "./BotOrb";export {DevServers,ServerRow,DevServerTitle,serverTitle,serverState,serverActions,serverControlRequest,serverAddresses,serverPortSummary,localServerSource,serverCopyFailure} from "./DevServers";export {BotRow} from "./BotRow";export {PaSelect} from "./PaSelect";export {PermissionCard} from "./PermissionCard";export {pendingPermissionView,withPermissionRecovery} from "./Chat";export {Trace} from "./Trace";export {readView} from "./read-views";export {SourceStatusPanel} from "./SourceStatusPanel";export {sourceHealth} from "./source-status";export {ChatRows} from "./ChatRows";export {fixtureSource} from "./fixtures";export {words} from "./words";export {BotRoster} from "./BotRoster";export {MemoryPanel} from "./MemoryPanel";export {ChatSkeleton} from "./PaLoading";export {assistantBadge} from "../cloud/project-assistants"',resolveDir:fileURLToPath(new URL(".",import.meta.url)),loader:"ts"},bundle:true,platform:"node",format:"cjs",packages:"external",write:false,jsx:"automatic",loader:{".css":"empty"}})
const compiled={exports:{}}
// The browser bridge owns singleton clocks. Keep their lifetime inside this SSR fixture.
const importedTimers=[],nativeTimers={setTimeout:globalThis.setTimeout,setInterval:globalThis.setInterval}
for(const kind of Object.keys(nativeTimers))globalThis[kind]=(...args)=>{const timer=nativeTimers[kind](...args);importedTimers.push(timer);return timer}
try{new Function("require","module","exports",outputFiles[0].text)(createRequire(import.meta.url),compiled,compiled.exports)}
finally{Object.assign(globalThis,nativeTimers)}
after(()=>{for(const timer of importedTimers)clearTimeout(timer)})
const {BotRow,PaSelect,BotOrb,PermissionCard,pendingPermissionView,withPermissionRecovery,Trace,readView,SourceStatusPanel,sourceHealth,ChatRows,fixtureSource,words,BotRoster,MemoryPanel,ChatSkeleton,assistantBadge}=compiled.exports
const render=(component,props)=>renderToStaticMarkup(React.createElement(component,props))
const timeline=messages=>messages.map(message=>({id:message.id,kind:"message",anchor_message_seq:message.seq,observed_event_seq:0,message}))
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
test("an offline machine shows in the collapsed connection summary, with its fix",async()=>{
  const s=fixtureSource();s.setState("offline")
  const value=await s.status(new AbortController().signal),w=words("zh-Hant")
  const html=render(SourceStatusPanel,{health:sourceHealth(value.sources,new Map(),[],w),w})
  assert.match(html,/這台機器：離線/);assert.match(html,/確認那台機器開著/);assert.match(html,/aria-expanded="false"/)
})
test("reading, selecting and metadata creation do not start a scripted model",async()=>{
  const log=[],s=fixtureSource("one",log),signal=new AbortController().signal
  await s.status(signal);await s.projects({machine_id:"one",cursor:"",page_size:100},signal);const page=await s.list({machine_id:"one",query:"",cursor:"",page_size:100},signal)
  const bot=page.items[0].bot;await s.get(bot.key,signal);const history=await s.history({key:bot.key,conversation_id:bot.conversation_id,before_seq:0,page_size:100},signal)
  const html=render(ChatRows,{timeline:timeline(history.items),w:words("en"),language:"en",onQuote:()=>{}})
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
  assert.match(html,/<details\b[^>]*\bopen=""/)
  assert.match(html,/What it will do/);assert.doesNotMatch(html,/Allow once/)
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
  const buttons=[...html.matchAll(/<button\b([^>]*)>([\s\S]*?)<\/button>/g)].map(m=>({text:m[2].replace(/<[^>]*>/g,""),disabled:/disabled=""/.test(m[1])}))
  assert.equal(buttons.length,3)
  for(const [index,label,native] of [[0,"Allow once","Yes"],[1,"Always allow","Always"],[2,"Reject once","No"]]){
    assert.equal(buttons[index].disabled,false)
    assert.ok(buttons[index].text.includes(label))
    assert.ok(buttons[index].text.includes(native))
  }
})
test("a permission card says what the tool will do before it is answered",()=>{
  const fence={key:{machine_id:"m",bot_id:"b"},project:{machine_id:"m",project_id:"p",canonical_id:"c",kind:"repository"},conversation_id:"c",binding_id:"x",generation:1}
  const offer={id:"offer",fence,turn_id:"t",native_request_id:"1",state:"pending",expires_at:"2099-01-01T00:00:00Z",selected_option_id:"",options:[{id:"once",kind:"allow_once",allow:true,read_only:false,label:"Yes"}],
    action:{title:"rm -rf /tmp/x",tool_kind:"execute",detail:"rm -rf /tmp/x",diffs:[],shortened:false}}
  const run=render(PermissionCard,{offer,enabled:true,onAnswer:async()=>{},w:words("zh-Hant")})
  assert.match(run,/想執行指令/);assert.match(run,/rm -rf \/tmp\/x/)
  const source={...offer,action:{...offer.action,tool_kind:"source_command"}}
  for(const [locale,authority] of [["en",/outside the Agent&#x27;s terminal sandbox/],["zh-Hant",/在 Agent 的終端沙箱之外執行/]]){
    const card=render(PermissionCard,{offer:source,enabled:true,onAnswer:async()=>{},w:words(locale)})
    assert.match(card,authority);assert.match(card,/rm -rf \/tmp\/x/)
  }
  assert.doesNotMatch(run,/終端沙箱之外/)

  const edit={...offer,action:{title:"Write notes.md",tool_kind:"edit",detail:"/repo/notes.md",diffs:[{path:"/repo/notes.md",old_text:"old line",new_text:"new line"}],shortened:true}}
  const changed=render(PermissionCard,{offer:edit,enabled:true,onAnswer:async()=>{},w:words("en")})
  assert.match(changed,/Wants to change a file/);assert.match(changed,/old line/);assert.match(changed,/new line/);assert.match(changed,/Shortened/)
  const summaryOnly=render(PermissionCard,{summary:{id:"offer",fence,turn_id:"t",state:"pending",expires_at:"2099-01-01T00:00:00Z",detail_cursor:"d",title:"curl example.com",tool_kind:"fetch"},enabled:true,onAnswer:async()=>{},w:words("en")})
  assert.match(summaryOnly,/Wants to use the network/);assert.match(summaryOnly,/curl example.com/)
})

test("a stale cached permission detail cannot supply controls for the current turn",()=>{
  const fence={key:{machine_id:"m",bot_id:"b"},project:{machine_id:"m",project_id:"p",canonical_id:"c",kind:"repository"},conversation_id:"c",binding_id:"current",generation:2}
  const summary={id:"offer",fence,turn_id:"t",state:"pending",expires_at:"2000-01-01T00:00:00Z",detail_cursor:"d"}
  const offer={...summary,fence:{...fence,binding_id:"previous",generation:1},expires_at:"2099-01-01T00:00:00Z",native_request_id:"1",selected_option_id:"",options:[{id:"once",kind:"allow_once",allow:true,read_only:false,label:"Yes"}]}
  const html=render(PermissionCard,{summary,offer,enabled:true,onAnswer:()=>assert.fail("stale permission answered"),w:words("en")})
  assert.match(html,/data-state="expired"/)
  assert.doesNotMatch(html,/data-option-kind/)
})

test("the waiting indicator retires known expired permissions even while the head is unchanged",async()=>{
  const source=fixtureSource(),signal=new AbortController().signal,bot=source.bots[1]
  const base={key:bot.key,conversation_id:bot.conversation_id,mode:"snapshot",source_epoch:"",cursor:"",after_seq:0,page_size:100}
  const snapshot=await source.view(base,signal)
  const response=await source.view({...base,mode:"permissions",source_epoch:snapshot.source_epoch},signal)
  const summary=response.items[0].permission_summary,deadline=Date.parse(summary.expires_at)
  const head={...response.head,conversation:{...response.head.conversation,active_turn_id:summary.turn_id}}
  assert.deepEqual(pendingPermissionView(head,[{summary}],deadline-1),{count:1,nextDeadline:deadline})
  assert.deepEqual(pendingPermissionView(head,[{summary}],deadline),{count:0,nextDeadline:Infinity})
  assert.deepEqual(pendingPermissionView({...head,pending_permission_count:2},[{summary}],deadline),{count:1,nextDeadline:Infinity})
  assert.equal(pendingPermissionView({...head,conversation:{...head.conversation,active_turn_id:"new-turn"}},[{summary}],deadline-1).count,0)
  const detail=await source.view({...base,mode:"detail",source_epoch:snapshot.source_epoch,cursor:summary.detail_cursor,page_size:1},signal)
  const offer={...detail.items[0].permission,expires_at:new Date(deadline-2).toISOString()}
  assert.equal(pendingPermissionView(head,[{summary,offer}],deadline-1).count,0)
})

test("a Bot row names its Project with its mark and says nothing internal",async()=>{
  const s=fixtureSource(),page=await s.list({machine_id:s.bots[0].key.machine_id,query:"",cursor:"",page_size:100},new AbortController().signal),first=page.items[0]
  const item={...first,bot:{...first.bot,project:{...first.bot.project,project_id:"0f45f5f1db4a5785"}}}
  const icon={accent:"#e39567",cells:[["#e39567",null],[null,"#e39567"]]}
  const html=render(BotRow,{item:{...item,unread_count:2,latest_message:"- **改程式與檔案**：在[這裡](https://example.com)看"},selected:false,onSelect:()=>{},w:words("zh-Hant"),language:"zh-Hant",projectName:"Team site",projectIcon:icon}).replace(/ data-bot-key="[^"]*"/,"")
  assert.match(html,/Team site/);assert.match(html,/<svg class="pa-project-mark"/);assert.match(html,/未讀 2/);assert.match(html,/改程式與檔案：在這裡看/);assert.doesNotMatch(html,/\*\*|example\.com/)
  assert.ok(!html.includes(item.bot.key.machine_id),"the machine ID stays out of a one-machine row")
  assert.ok(!html.includes(item.bot.project.project_id),"the Project ID stays out of the row and its label")
})
test("the process trace says what each step is, never its turn ID or provider category",()=>{
  const summaries=[{id:"t1",turn_id:"5ba44472905d8854d481db30b022c77f",category:"agent_message_chunk",complete:true,detail_cursor:"c"},{id:"t2",turn_id:"5ba44472905d8854d481db30b022c77f",category:"something_new",complete:true,detail_cursor:"c"}]
  const html=render(Trace,{summaries,total:2,w:words("zh-Hant")})
  assert.match(html,/回覆內容/);assert.match(html,/處理步驟/)
  assert.doesNotMatch(html,/5ba44472|agent_message_chunk|something_new/)
})
test("a Bots drop-down reads as one, with its value and a chevron, and no platform select",()=>{
  const html=render(PaSelect,{label:"Project",value:"",placeholder:"選擇 Project",options:[{value:"a",label:"clawdline"}],onChange:()=>{}})
  assert.match(html,/aria-haspopup="listbox"/);assert.match(html,/aria-expanded="false"/);assert.match(html,/pa-select-chevron/);assert.match(html,/選擇 Project/)
  assert.doesNotMatch(html,/<select/)
})
// A Bot's reply reads as Markdown; the person's own text stays exactly as typed,
// and markup a reply carries is escaped, never run.
test("a Bot reply renders Markdown while the person's message stays literal",()=>{
  const fence={key:{machine_id:"m",bot_id:"b"},project:{machine_id:"m",project_id:"p",canonical_id:"c",kind:"repository"},conversation_id:"c",binding_id:"x",generation:1}
  const message=(id,kind,text,seq)=>({id,kind,text,seq,fence,turn_id:"t",quote_message_id:"",at:"2026-11-14T00:00:00Z"})
  const items=[{id:"u",kind:"message",anchor_message_seq:1,observed_event_seq:1,message:message("u","user","**not bold**",1)},{id:"a",kind:"message",anchor_message_seq:2,observed_event_seq:1,message:message("a","assistant","**bold**\n\n- one\n- two\n\n<script>x()</script>",2)}]
  const html=render(ChatRows,{timeline:items,w:words("en"),language:"en",onQuote:()=>{}})
  assert.match(html,/<strong>bold<\/strong>/);assert.match(html,/<li>one<\/li>/)
  assert.match(html,/\*\*not bold\*\*/)
  assert.ok(!html.includes("<script>"),"a reply's markup was not escaped")
})
test("a message offers a closed action menu without inline Reply",()=>{
  const fence={key:{machine_id:"m",bot_id:"b"},project:{machine_id:"m",project_id:"p",canonical_id:"c",kind:"repository"},conversation_id:"c",binding_id:"x",generation:1}
  const messages=[{id:"u",kind:"user",text:"Hi",seq:1,fence,turn_id:"t",quote_message_id:"",at:"2026-11-14T00:00:00Z"}]
  const html=render(ChatRows,{timeline:timeline(messages),w:words("en"),language:"en",onQuote:()=>{}})
  assert.ok(!html.includes(">Reply<"),"a Reply button was shown on an untouched message")
  assert.match(html,/aria-haspopup="menu"/)
})
test("a Bot reply has one action menu without duplicate inline controls",()=>{
  const fence={key:{machine_id:"m",bot_id:"b"},project:{machine_id:"m",project_id:"p",canonical_id:"c",kind:"repository"},conversation_id:"c",binding_id:"x",generation:1}
  const messages=[{id:"a",kind:"assistant",text:"A long answer",seq:1,fence,turn_id:"t",quote_message_id:"",at:"2026-11-14T00:00:00Z"}]
  const html=render(ChatRows,{timeline:timeline(messages),w:words("en"),language:"en",onQuote:()=>{}})
  assert.equal((html.match(/aria-label="Message actions"/g)??[]).length,1)
  assert.match(html,/aria-haspopup="menu"/)
  assert.doesNotMatch(html,/>Reply<\/button>/,"Reply stays in the unopened menu")
})
test("the roster's first read draws nothing inside its first 150ms, not even the empty line",()=>{
  const w=words("en"),html=render(BotRoster,{items:[],machines:["one"],selected:null,query:"",onQuery:()=>{},machine:"",onMachine:()=>{},onSelect:()=>{},onClose:()=>{},w,language:"en",loading:true,errors:[],projectNames:new Map()})
  assert.doesNotMatch(html,/loading-skeleton|role="status"/);assert.ok(!html.includes(w.empty))
})
test("conversation placeholders say what is loading and show no readable message or control",()=>{
  for(const language of ["en","zh-Hant"]){
    const w=words(language),html=render(ChatSkeleton,{label:w.chatLoading})
    assert.ok(html.includes(w.chatLoading));assert.notEqual(w.chatLoading,w.loading)
    assert.match(html,/role="status"/);assert.match(html,/aria-hidden="true"/);assert.doesNotMatch(html,/<(button|input|textarea|a|p)\b/)
    assert.equal((html.match(/pa-skeleton-bubble/g)||[]).length,4);assert.equal((html.match(/pa-skeleton-mine/g)||[]).length,2)
  }
})
test("memory opens with placeholders and a close button before its read answers",()=>{
  for(const language of ["en","zh-Hant"]){
    const w=words(language),html=render(MemoryPanel,{value:null,enabled:true,w,onSave:async()=>{},onForget:async()=>{},onRefresh:async()=>{},onClose:()=>{}})
    assert.ok(html.includes(w.memoryLoading));assert.ok(html.includes(`>${w.close}</button>`));assert.match(html,/aria-busy="true"/)
    assert.doesNotMatch(html,/<(textarea|input|fieldset)\b/);assert.ok(!html.includes(w.emptyMemory))
  }
})

test("the entry prioritizes live input over work without treating unread as activity",async()=>{
  const source=fixtureSource(),signal=new AbortController().signal
  const {items}=await source.list({machine_id:source.machineID,cursor:"",query:"",page_size:100},signal)
  const idle={...items[0],unread_count:5,pending_permissions:0,status:"idle"}
  const running={...items[1],unread_count:0,pending_permissions:0,status:"working"}
  const waiting={...items[2],unread_count:0,pending_permissions:1,status:"needsInput"}
  assert.equal(assistantBadge([idle],[source]).mood,"idle")
  assert.equal(assistantBadge([idle,running],[source]).mood,"working")
  for(const rows of [[running,waiting],[waiting,running]])assert.equal(assistantBadge(rows,[source]).mood,"needsInput")
  for(const row of [{...waiting,bot:{...waiting.bot,hidden:true}},{...waiting,source:{...waiting.source,fresh:false}},{...waiting,source:{...waiting.source,state:"offline"}}]){
    assert.equal(assistantBadge([running,row],[source]).mood,"working")
  }
  assert.equal(assistantBadge([waiting],[]).mood,"idle")
  assert.equal(assistantBadge([waiting],[{...source,current:()=>false}]).mood,"idle")
  assert.equal(assistantBadge([idle],[{...source,current:()=>false}]).unread,5)
  assert.equal(assistantBadge([idle,running,waiting],[source]).unread,5)
})


test("server controls never carry the displayed executable text or environment",()=>{
  const key={machine_id:"source",bot_id:"bot"},server={id:"known",name:"watch",command:"synthetic-command",folder:"/synthetic/project",attempt:"attempt-one",revision:4,fingerprint:"f".repeat(64),state:"running",environment:{SECRET:"must-not-cross"},pid:123}
  const request=compiled.exports.serverControlRequest(key,server,"human-key")
  assert.deepEqual(request,{key,id:"known",fingerprint:server.fingerprint,attempt:"attempt-one",revision:4,idempotency_key:"human-key"})
  assert.doesNotMatch(JSON.stringify(request),/synthetic-command|synthetic\/project|SECRET|must-not-cross|pid/)
})
test("uncertain or stopping servers offer no new mutation while terminal rows can start or be removed",()=>{
  const actions=compiled.exports.serverActions
  for(const state of ["unknown","stopping","unrecognized"]){assert.deepEqual(actions({state}),[])}
  assert.equal(compiled.exports.serverState({state:"unrecognized"},words("en")),"Status unconfirmed")
  assert.deepEqual(actions({state:"accepted"}),["stop"])
  assert.deepEqual(actions({state:"running"}),["restart","stop"])
  for(const state of ["stopped","failed"]){assert.deepEqual(actions({state}),["start","remove"])}
})
test("sources without server support render no empty details section",()=>{
  const html=render(compiled.exports.DevServers,{source:{canDevServers:()=>false},bot:{key:{machine_id:"source",bot_id:"bot"}},enabled:false,w:words("en")})
  assert.equal(html,"")
})

test("server addresses use source candidates and default to remote-safe selection",()=>{
  const server={state:"running",ports_known:true,ports:[{port:43123,bind:"0.0.0.0",local:"http://127.0.0.1:43123",remote:["http://192.0.2.23:43123","http://192.0.2.23:43123"]},{port:43124,bind:"::1",local:"http://[::1]:43124",remote:[]}]}
  assert.deepEqual(compiled.exports.serverAddresses(server),["http://192.0.2.23:43123"])
  assert.deepEqual(compiled.exports.serverAddresses(server,true),["http://127.0.0.1:43123","http://192.0.2.23:43123","http://[::1]:43124"])
  assert.equal(compiled.exports.serverPortSummary(server),":43123 +1")
  for(const changed of [{ports_known:false},{state:"stopped"},{state:"unknown"},{ports:null},{ports:[null,{},"malformed"]}]){
    assert.deepEqual(compiled.exports.serverAddresses({...server,...changed},true),[])
    assert.equal(compiled.exports.serverPortSummary({...server,...changed}),"")
  }
})
test("server candidates never become script, credentials, DNS or remote loopback targets",()=>{
  const candidates=["javascript:alert(1)","https://192.0.2.23:43123","http://user:synthetic-fixture-password@192.0.2.23:43123","http://supplied.invalid:43123","http://192.0.2.23:43123/path","http://192.0.2.23:43123?secret=synthetic","http://192.0.2.23:43123#fragment","http://192.0.2.23:43124","http://127.0.0.1:43123","http://127.23.4.5:43123","http://[::1]:43123","http://[::ffff:127.0.0.1]:43123","http://0.0.0.0:43123","http://[::]:43123","http://[fe80::123]:43123"]
  for(const candidate of candidates){
    const server={state:"running",ports_known:true,ports:[{port:43123,local:candidate,remote:[candidate]}]}
    assert.deepEqual(compiled.exports.serverAddresses(server),[],candidate)
  }
  const noGuess={state:"running",ports_known:true,ports:[],command:"visit http://192.0.2.23:43123",log:"http://192.0.2.23:43123"}
  assert.deepEqual(compiled.exports.serverAddresses(noGuess,true),[])
})
test("server rows keep one quiet primary action and show the known port summary",()=>{
  const server={name:"Preview",command:"synthetic",state:"running",ports_known:true,ports:[{port:43123,local:"http://127.0.0.1:43123",remote:[]}]}
  for(const language of ["en","zh-Hant"]){
    const w=words(language),props={server,w,active:true,addressesAvailable:true,disabled:false,canRead:()=>true,onLogs:()=>assert.fail("render read logs"),onControl:()=>assert.fail("render mutated")}
    const local=render(compiled.exports.ServerRow,{...props,localSource:true}),remote=render(compiled.exports.ServerRow,{...props,localSource:false})
    assert.ok(local.includes(`title="${w.serverOpen}"`));assert.ok(remote.includes(`title="${w.serverLogs}"`))
    for(const html of [local,remote]){assert.match(html,/:43123/);assert.equal((html.match(/<button/g)||[]).length,2);assert.doesNotMatch(html,/http:\/\//)}
  }
})

test("local transport alone does not authorize loopback on a LAN or Cloud browser",()=>{
  for(const origin of ["http://127.0.0.1:7727/","http://localhost:7727/","http://[::1]:7727/"]){
    assert.equal(compiled.exports.localServerSource({browserRemote:false},origin),true)
    for(const source of [{browserRemote:true},{}])assert.equal(compiled.exports.localServerSource(source,origin),false)
  }
  for(const origin of ["http://192.0.2.23:7727/","https://app.clawdline.com/","https://source.invalid/","file:///synthetic/index.html","malformed"]){
    assert.equal(compiled.exports.localServerSource({browserRemote:false},origin),false)
  }
})

test("clipboard refusals retain their category without exposing arbitrary messages",()=>{
  for(const language of ["en","zh-Hant"]){
    const w=words(language),format=compiled.exports.serverCopyFailure
    assert.equal(format(new DOMException("synthetic sensitive detail","NotAllowedError"),w),w.serverCopyFailed+" (NotAllowedError)")
    assert.equal(format(new DOMException("synthetic unavailable detail","NotSupportedError"),w),w.serverCopyFailed+" (NotSupportedError)")
    assert.equal(format({code:"clipboard_denied",message:"synthetic sensitive detail"},w),w.serverCopyFailed+" (clipboard_denied)")
    for(const error of [null,{message:"synthetic sensitive detail"},{name:"unexpected words with details"}])assert.equal(format(error,w),w.serverCopyFailed)
  }
})

test("the title counts running servers and prefers a confirmed port over a watch name",()=>{
  const watch={id:"watch",name:"Watch build",state:"running",ports_known:true,ports:[]}
  const port={id:"port",name:"Web",state:"running",ports_known:true,ports:[{port:43123},{port:43124}]}
  const later={...watch,id:"later",name:"Another build"}
  for(const language of ["en","zh-Hant"]){
    const w=words(language),state={rows:[watch,port,later],pending:false,fresh:true,inspect:false,error:""},title=compiled.exports.serverTitle
    assert.equal(title(state,w),":43123 +2")
    assert.equal(title({...state,rows:[watch]},w),"Watch build")
    assert.equal(title({...state,rows:[{...port,ports_known:false}]},w),"Web")
    assert.equal(title({...state,rows:[{state:"accepted"}]},w),w.serverStarting)
    assert.equal(title({...state,rows:[{state:"unknown"}]},w),w.serverUnknown)
    assert.equal(title({...state,rows:[{state:"future-active"}]},w),w.serverUnknown)
    assert.equal(title({...state,rows:[{state:"failed"},{state:"stopped"}]},w),w.serverFailed)
    assert.equal(title({...state,rows:[{state:"stopped"}]},w),"")
    for(const mask of [{fresh:false},{inspect:true},{error:"pa_offline"}])assert.equal(title({...state,...mask},w),"")
    assert.equal(title({...state,pending:true},w),":43123 +2","a pending read does not flicker a previously fresh title")
  }
})
