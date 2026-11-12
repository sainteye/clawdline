/** Synthetic source fixture. No HTTP, provider, recorder, real Session or model dependency. */
import React from "react"
import { createRoot } from "react-dom/client"
import type * as W from "@clawdline/contract"
import type { SourceClient } from "./client"
import { failure } from "./client"
import { Bots } from "./Bots"
import { BotOrb } from "./BotOrb"
import { BotAvatar } from "./BotAvatar"
import { avatarPaths, palette } from "./avatar-paths"
import { createViewState, sameBot } from "./view-state"
export const contractVersion="project-assistants.v1"
export interface FixtureLog {method:string;machine:string;request:unknown}
export function fixtureSource(machineID="studio",log:FixtureLog[]=[]): SourceClient & {setState:(state:W.ProjectAssistantSourceState,access?:W.ProjectAssistantAccess)=>void;push:(event:W.ProjectAssistantEvent)=>void;seedHistory:(count:number)=>void;holdGet:()=>void;releaseGet:()=>void;expireCursor:(mode?:"ok"|"pa_offline"|"pa_revoked"|"pa_capacity_full")=>void;holdEvents:()=>void;releaseEvents:()=>void;holdExport:()=>void;releaseExport:()=>void;completePreviousWork:()=>void;emitMessage:(text:string)=>void;log:FixtureLog[];bots:W.ProjectAssistantBot[]} {
  const project:W.ProjectAssistantProjectScope={machine_id:machineID,project_id:"project",canonical_id:"canonical",kind:"repository"}
  const at="2026-11-12T06:20:00Z"
  const names=["Project assistant","Code review","Interface design","Documentation","Research","Testing"]
  const shapes=Object.keys(avatarPaths) as W.ProjectAssistantShape[]
  const bots:W.ProjectAssistantBot[]=names.map((name,i)=>({key:{machine_id:machineID,bot_id:`bot-${i}`},project,name,appearance:{shape:shapes[i],color:palette[i]},agent:i%2?"codex":"claude",conversation_id:`chat-${i}`,created_at:at,updated_at:at,version:1,permission_policy:"ask",instructions:{text:"Plan work and delegate changes through isolated tasks.",revision:1,role_snapshot_ref:""}}))
  let state:W.ProjectAssistantSourceState="online",access:W.ProjectAssistantAccess="interactive",seq=0
  const events:W.ProjectAssistantEvent[]=[],messages=new Map<string,W.ProjectAssistantMessage[]>(),receipts=new Map<string,W.ProjectAssistantReceipt>()
  let getGate:Promise<void>|null=null,releaseGet:(()=>void)|null=null
  let generation=1,expiredCursor=false,exportFailure:"ok"|"pa_offline"|"pa_revoked"|"pa_capacity_full"="ok"
  let eventGate:Promise<void>|null=null,exportGate:Promise<void>|null=null,releaseEvents:(()=>void)|null=null,releaseExport:(()=>void)|null=null
  const conversation=(bot:W.ProjectAssistantBot):W.ProjectAssistantConversation=>({id:bot.conversation_id,key:bot.key,binding_id:"fixture-binding",generation,active_turn_id:"",last_message_seq:messages.get(bot.key.bot_id)?.length??0,memory_revision:1,version:1})
  const fence=(bot:W.ProjectAssistantBot):W.ProjectAssistantFence=>({key:bot.key,project,conversation_id:bot.conversation_id,binding_id:"fixture-binding",generation})
  for(const bot of bots)messages.set(bot.key.bot_id,[{id:`${bot.key.bot_id}-user`,seq:1,kind:"user",text:"Help me plan the next tasks.",at,fence:fence(bot),turn_id:"turn-1",quote_message_id:""},{id:`${bot.key.bot_id}-reply`,seq:2,kind:"assistant",text:"I split the work into five tasks. First freeze the shared contract, then work in parallel.\nYou can give me the next task.",at,fence:fence(bot),turn_id:"turn-1",quote_message_id:""}])
  const capabilities=(bot:W.ProjectAssistantBot):W.ProjectAssistantCapabilities=>({key:bot.key,project,agent:bot.agent,cli_version:"scripted",adapter_version:"fixture",read_only_enforced:true,resume:false,send_message:true,native_memory:"isolated",native_memory_verified:true,evidence:"scripted peer only"})
  const status=():W.ProjectAssistantSourceStatus=>({machine_id:machineID,state,access,readable:state!=="revoked"&&state!=="unpaired",writable:state==="online"&&access==="interactive",fresh:state==="online",observed_at:at,capabilities:bots.map(capabilities)})
  const check=(method:string,request:unknown,signal:AbortSignal,write=false)=>{
    log.push({method,machine:machineID,request})
    if(signal.aborted)throw new DOMException("Aborted","AbortError")
    if(state!=="online")throw failure(state==="revoked"?"pa_revoked":state==="unsupported"?"pa_unsupported":state==="unpaired"?"pa_unpaired":"pa_offline")
    if(write&&access!=="interactive")throw failure("pa_read_only")
  }
  const find=(key:W.ProjectAssistantBotKey)=>{const bot=bots.find(b=>sameBot(b.key,key));if(!bot)throw failure("pa_not_found");return bot}
  const receive=(cmd:W.ProjectAssistantCommand):W.ProjectAssistantReceipt=>{let r=receipts.get(cmd.idempotency_key);if(!r){r={id:crypto.randomUUID(),command:cmd,state:"accepted",event_seq:seq,resource_id:cmd.key.bot_id};receipts.set(cmd.idempotency_key,r)}return r}
  const summary=(bot:W.ProjectAssistantBot):W.ProjectAssistantBotSummary=>({bot,source:status(),activity:bot.key.bot_id==="bot-2"?"Working through the interface flow…":"",activity_at:at,latest_message:messages.get(bot.key.bot_id)?.at(-1)?.text??"",unread_count:bot.key.bot_id==="bot-0"?2:0,pending_permissions:bot.key.bot_id==="bot-1"?1:0,read_cursor:{key:bot.key,conversation_id:bot.conversation_id,reader_id:"fixture",message_seq:0},version:bot.version})
  const push=(event:W.ProjectAssistantEvent)=>{events.push({...event,seq:++seq,id:event.id||crypto.randomUUID()})}
  const permissionBot=bots[1]
  push({id:"offer-event",seq:0,at,version:contractVersion,kind:"permission_offered",fence:fence(permissionBot),permission:{id:"permission-1",native_request_id:"request-1",turn_id:"turn-1",fence:fence(permissionBot),state:"pending",expires_at:"2099-01-01T00:00:00Z",selected_option_id:"",options:[{id:"yes",kind:"allow_once",label:"Allow once",allow:true,read_only:true},{id:"no",kind:"reject_once",label:"Deny",allow:false,read_only:true},{id:"always",kind:"allow_always",label:"Always",allow:true,read_only:true}]}})
  push({id:"work-event",seq:0,at,version:contractVersion,kind:"work_updated",fence:fence(bots[0]),work:{id:"work-1",fence:fence(bots[0]),turn_id:"turn-1",board_item_id:"PA-01",task_id:"task-1",state:"accepted",landing_commit:"",result_message_id:""}})
  push({id:"trace-event",seq:0,at,version:contractVersion,kind:"trace_updated",fence:fence(bots[0]),trace:{turn_id:"turn-1",category:"read",complete:true,text:"Reviewed the shared contract and split independent changes."}})
  const memory:W.ProjectAssistantMemoryRevision={key:bots[0].key,project,revision:1,forgotten_through:0,forgotten_source_ids:[]}
  const api:SourceClient={machineID,authorizationRevision:`${machineID}-grant-1`,
    async status(signal){log.push({method:"status",machine:machineID,request:null});if(signal.aborted)throw new DOMException("Aborted","AbortError");return{contract_version:contractVersion,sources:[status()]}},
    async projects(signal){check("projects",null,signal);return {at:0,catalog:{available:true,enabled:true,mode:"fixture",projects:[{id:"project",name:"Clawdline",label:"Clawdline",activeItemCount:0,itemCount:0,isStartPoint:false,summaryCoverage:"complete"}],readState:{},revision:1,schemaVersion:1,source:{}}} as W.ProjectCatalogAnswer},
    async list(r,s){check("list",r,s);const matches=bots.filter(b=>!r.query||`${b.name} Clawdline ${summary(b).latest_message}`.toLowerCase().includes(r.query.toLowerCase()));const offset=Number(r.cursor||0);const items=matches.slice(offset,offset+3);return{contract_version:contractVersion,items:items.map(summary),sources:[status()],next_cursor:offset+3<matches.length?String(offset+3):""}},
    async get(r,s){if(getGate)await getGate;check("get",r,s);const bot=find(r);return{contract_version:contractVersion,bot,conversation:conversation(bot),source:status()}},
    async create(r,s){check("create",r,s,true);let bot=bots.find(b=>sameBot(b.key,r.command.key));if(!bot){bot={...bots[0],key:r.command.key,name:r.settings.name,appearance:r.settings.appearance,agent:r.settings.agent,permission_policy:r.settings.permission_policy,conversation_id:crypto.randomUUID(),instructions:{text:r.settings.instructions,revision:1,role_snapshot_ref:r.settings.role_snapshot_ref}};bots.push(bot);messages.set(bot.key.bot_id,[])}return{contract_version:contractVersion,bot,receipt:receive(r.command)}},
    async edit(r,s){check("edit",r,s,true);const bot=find(r.command.key);if(bot.version!==r.settings.expected_version)throw failure("pa_stale_version");Object.assign(bot,{name:r.settings.name,appearance:r.settings.appearance,instructions:{...bot.instructions,text:r.settings.instructions,revision:bot.instructions.revision+1},version:bot.version+1});return{contract_version:contractVersion,bot,receipt:receive(r.command)}},
    async history(r,s){check("history",r,s);const bot=find(r.key),all=(messages.get(r.key.bot_id)??[]).filter(m=>!r.before_seq||m.seq<r.before_seq),items=all.slice(-r.page_size);return{contract_version:contractVersion,conversation:conversation(bot),items,has_more:all.length>items.length,next_before_seq:items[0]?.seq??0}},
    async send(r,s){check("send",r,s,true);const bot=find(r.command.key);const receipt=receive(r.command);if(r.text==="unknown"){receipt.state="unknown";return{contract_version:contractVersion,receipt}}
      const rows=messages.get(bot.key.bot_id)??[];const message:W.ProjectAssistantMessage={id:receipt.id,seq:rows.length+1,at,fence:fence(bot),kind:"user",text:r.text,quote_message_id:r.quote_message_id,turn_id:receipt.id};if(!rows.some(m=>m.id===message.id)){messages.set(bot.key.bot_id,[...rows,message]);push({id:crypto.randomUUID(),seq:0,at,version:contractVersion,kind:"message_created",fence:fence(bot),message})}return{contract_version:contractVersion,receipt}},
    async stop(r,s){check("stop",r,s,true);return{contract_version:contractVersion,receipt:receive(r.command)}},
    async answer(r,s){check("answer",r,s,true);return{contract_version:contractVersion,receipt:receive(r.command)}},
    async markRead(r,s){check("markRead",r,s);return{contract_version:contractVersion,cursor:r.cursor}},
    async observe(r,s){check("observe",r,s);return{contract_version:contractVersion,receipt:receive(r.command)}},
    async events(r,s){if(eventGate&&sameBot(r.key,bots[0].key))await eventGate;check("events",r,s);if(expiredCursor&&sameBot(r.key,bots[0].key))throw failure("pa_cursor_expired");return{contract_version:contractVersion,items:events.filter(e=>sameBot(e.fence.key,r.key)&&e.seq>r.after_seq),has_more:false,next_seq:seq}},
    async forget(r,s){check("forget",r,s,true);memory.revision++;memory.forgotten_source_ids.push(...r.source_ids);memory.forgotten_through=memory.revision;return{contract_version:contractVersion,memory:{...memory,key:r.command.key}}},
    async saveMemory(r,s){check("saveMemory",r,s,true);return{contract_version:contractVersion,memory:{...memory,key:r.command.key}}},
    async export(r,s){if(exportGate)await exportGate;check("export",r,s);if(exportFailure!=="ok"&&sameBot(r,bots[0].key))throw failure(exportFailure);if(sameBot(r,bots[0].key))expiredCursor=false;const bot=find(r),revision={...memory,key:r},ownEvents=events.filter(e=>sameBot(e.fence.key,r));return{contract_version:contractVersion,schema_version:1,bot,conversation:conversation(bot),messages:messages.get(r.bot_id)??[],memory:revision,memory_entries:[],context:{memory:revision,provenance_complete:true,sources:[],text:""},bindings:[],permissions:ownEvents.flatMap(e=>e.permission?[e.permission]:[]),read_cursors:[],receipts:[...receipts.values()].filter(receipt=>sameBot(receipt.command.key,r)),turns:ownEvents.flatMap(e=>e.turn?[e.turn]:[]),work_links:[...new Map(ownEvents.flatMap(e=>e.work?[[e.work.id,e.work] as const]:[])).values()],events:ownEvents}},
  }
  return Object.assign(api,{setState(next:W.ProjectAssistantSourceState,nextAccess=access){state=next;access=nextAccess},push,log,bots,
    expireCursor(mode:"ok"|"pa_offline"|"pa_revoked"|"pa_capacity_full"="ok"){
      expiredCursor=true;exportFailure=mode
      const bot=bots[0],oldFence=fence(bot),options=events.find(e=>e.permission)?.permission?.options??[]
      push({id:crypto.randomUUID(),seq:0,at,version:contractVersion,kind:"permission_offered",fence:oldFence,permission:{id:"old-generation-offer",fence:oldFence,turn_id:"old-turn",native_request_id:"old-request",state:"pending",expires_at:"2099-01-01T00:00:00Z",selected_option_id:"",options}})
      generation++
      push({id:crypto.randomUUID(),seq:0,at,version:contractVersion,kind:"permission_offered",fence:fence(bot),permission:{id:"new-generation-offer",fence:fence(bot),turn_id:"new-turn",native_request_id:"new-request",state:"pending",expires_at:"2099-01-01T00:00:00Z",selected_option_id:"",options}})
    },
    emitMessage(text:string){const bot=bots[0],rows=messages.get(bot.key.bot_id)??[];const message:W.ProjectAssistantMessage={id:crypto.randomUUID(),seq:rows.length+1,at,fence:fence(bot),kind:"assistant",text,quote_message_id:"",turn_id:"after-snapshot"};messages.set(bot.key.bot_id,[...rows,message]);push({id:crypto.randomUUID(),seq:0,at,version:contractVersion,kind:"message_created",fence:fence(bot),message})},
    completePreviousWork(){
      const bot=bots[0],oldFence=fence(bot),rows=messages.get(bot.key.bot_id)??[]
      generation++
      for(const kind of ["user","assistant","work"] as const){const message:W.ProjectAssistantMessage={id:crypto.randomUUID(),seq:rows.length+1,at,fence:oldFence,kind,text:`Saved previous execution ${kind}`,turn_id:"previous-turn",quote_message_id:""};rows.push(message);push({id:crypto.randomUUID(),seq:0,at,version:contractVersion,kind:"message_created",fence:oldFence,message})}
      messages.set(bot.key.bot_id,rows)
      push({id:crypto.randomUUID(),seq:0,at,version:contractVersion,kind:"work_updated",fence:oldFence,work:{id:"work-1",fence:oldFence,turn_id:"turn-1",board_item_id:"PA-01",task_id:"task-1",state:"landed",landing_commit:"synthetic-landing",result_message_id:rows.at(-1)!.id}})
    },
    holdEvents(){eventGate=new Promise(resolve=>{releaseEvents=resolve})},releaseEvents(){releaseEvents?.();eventGate=null;releaseEvents=null},
    holdExport(){exportGate=new Promise(resolve=>{releaseExport=resolve})},releaseExport(){releaseExport?.();exportGate=null;releaseExport=null},
    holdGet(){getGate=new Promise(resolve=>{releaseGet=resolve})},releaseGet(){releaseGet?.();getGate=null;releaseGet=null},
    seedHistory(count:number){const bot=bots[0];messages.set(bot.key.bot_id,Array.from({length:count},(_,i)=>({id:`history-${i+1}`,seq:i+1,kind:i%2?"assistant":"user",text:`History message ${i+1}`,at,fence:fence(bot),turn_id:"history",quote_message_id:""})))},
  })
}

export function mountFixture() {
  const log:FixtureLog[]=[],a=fixtureSource("studio",log),b=fixtureSource("notebook",log)
  let sourceList:SourceClient[]=[a,b]
  const root=createRoot(document.getElementById("fixture")!),language=new URLSearchParams(location.search).get("lang")??"zh-Hant"
  const view=createViewState(window.localStorage)
  let hostGeneration=0,opened=false,opener:HTMLElement|null=null
  const paint=()=>root.render(React.createElement(React.Fragment,null,
    React.createElement("div",{className:"fixture-sessions",hidden:opened},React.createElement("h1",null,"clawdline"),React.createElement("h2",null,"Sessions"),React.createElement("p",null,"Synthetic Session list · existing voice owner remains mounted"),React.createElement("button",{id:"session-opener"},"Original Session"),React.createElement(BotOrb,{onOpen:(element?:HTMLElement)=>{opener=element??null;opened=true;paint()},language,unread:4,pendingPermissions:2})),
    opened?React.createElement(Bots,{key:hostGeneration,viewState:view,sources:sourceList,onClose:()=>{opened=false;paint()},language,accountKey:"fixture-account",readerID:"fixture-reader",manageHistory:true,returnFocus:opener}):null,
    React.createElement("button",{id:"voice-owner",onClick:()=>{},"aria-label":"Existing voice stop control"},"Voice stop")))
  const controls={log,a,b,remount(){hostGeneration++;paint()},open(){opened=true;paint()},revoke(){sourceList=[b];paint()},offline(){a.setState("offline");paint()},readOnly(){a.setState("online","history_only");sourceList=[Object.assign(Object.create(a),{authorizationRevision:"readonly-grant"}),b];paint()},shapes(){root.render(React.createElement("div",{className:"project-assistants fixture-shapes"},Object.keys(avatarPaths).map((shape,i)=>React.createElement("figure",{key:shape},React.createElement(BotAvatar,{appearance:{shape:shape as W.ProjectAssistantShape,color:palette[i]},size:92}),React.createElement("figcaption",null,shape))),React.createElement(BotOrb,{onOpen:()=>{},language})))}}
  Object.assign(window,{botsFixture:controls});paint()
}
