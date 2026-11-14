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
import { createViewState, sameBot, sameFence } from "./view-state"
export const contractVersion="project-assistants.v1"
export interface FixtureLog {method:string;machine:string;request:unknown}
export function fixtureSource(machineID="studio",log:FixtureLog[]=[]) {
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
  const fence=(bot:W.ProjectAssistantBot):W.ProjectAssistantFence=>({key:bot.key,project:bot.project,conversation_id:bot.conversation_id,binding_id:"fixture-binding",generation})
  for(const bot of bots)messages.set(bot.key.bot_id,[{id:`${bot.key.bot_id}-user`,seq:1,kind:"user",text:"Help me plan the next tasks.",at,fence:fence(bot),turn_id:"turn-1",quote_message_id:""},{id:`${bot.key.bot_id}-reply`,seq:2,kind:"assistant",text:"I split the work into five tasks. First freeze the shared contract, then work in parallel.\nYou can give me the next task.",at,fence:fence(bot),turn_id:"turn-1",quote_message_id:""}])
  let projectCount=1,projectRevision=1,projectFailure:W.ProjectAssistantErrorCode|null=null,projectInvalid:string|null=null,unknownCreate=false
  let projectGate:Promise<void>|null=null,releaseProjects:(()=>void)|null=null
  let createGate:Promise<void>|null=null,releaseCreate:(()=>void)|null=null
  let statusGate:Promise<void>|null=null,releaseStatus:(()=>void)|null=null
  const created=new Set<string>()
  const capabilities=(bot:W.ProjectAssistantBot):W.ProjectAssistantCapabilities=>({key:bot.key,project,agent:bot.agent,cli_version:"scripted",adapter_version:"fixture",read_only_enforced:false,resume:false,send_message:false,native_memory:"unknown",native_memory_verified:false,evidence:"synthetic metadata; no native provider admitted",unavailable:failure("adapter_not_found").error})
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
  let detailFailure:W.ProjectAssistantErrorCode|null=null,detailFenceChange=false
  let snapshotHolds:{release:()=>void;signal:AbortSignal}[]|null=null
  let viewFailure:W.ProjectAssistantErrorCode|null=null,viewInvalid:string|null=null,viewUnsupported=false,legacyForbidden=false,viewOversized=false
  let viewGate:Promise<void>|null=null,releaseView:(()=>void)|null=null,holdMode:W.ProjectAssistantReadViewMode|null=null
  let historyGate:Promise<void>|null=null,releaseHistory:(()=>void)|null=null
  const readEpoch=`${machineID}-epoch-1`
  const latest=<T,>(rows:readonly T[],identity:(row:T)=>string):T[]=>[...new Map(rows.map(row=>[identity(row),row])).values()]
  const readPage=(r:W.ProjectAssistantReadViewRequest):W.ProjectAssistantReadViewResponse=>{
    const bot=find(r.key),f=fence(bot),own=events.filter(e=>sameBot(e.fence.key,r.key)),current=own.filter(e=>sameFence(e.fence,f))
    const offers=latest(current.flatMap(e=>e.permission?[e.permission]:[]),p=>p.id).filter(p=>p.state==="pending")
    const traces=latest(current.flatMap(e=>e.trace?[e.trace]:[]),t=>JSON.stringify([t.turn_id,t.category]))
    const turns=latest(own.flatMap(e=>e.turn?[e.turn]:[]),t=>t.id)
    const pending=turns.filter(t=>["accepted","queued","running","waiting_permission"].includes(t.state))
    const summarize=(t:W.ProjectAssistantTurn):W.ProjectAssistantReadTurnSummary=>({id:t.id,fence:t.fence,message_id:t.message_id,state:t.state,failure_code:t.failure?.code??"",started_at:t.started_at??""})
    const head:W.ProjectAssistantReadViewHead={key:bot.key,project:bot.project,conversation:conversation(bot),current_fence:f,bot_version:bot.version,memory_revision:memory.revision,event_frontier:seq,unknown_turn_count:turns.filter(t=>t.state==="unknown").length,pending_permission_count:offers.length,trace_category_count:traces.length}
    head.conversation.memory_revision=memory.revision
    const response:W.ProjectAssistantReadViewResponse={contract_version:contractVersion,mode:r.mode,source:status(),source_epoch:readEpoch,head,items:[],has_more:false,next_cursor:"",next_seq:0}
    const work=latest(own.filter(e=>e.work),e=>e.work!.id)
    const timeline:W.ProjectAssistantReadViewItem[]=[...(messages.get(bot.key.bot_id)??[]).map(message=>({id:message.id,kind:"message" as const,anchor_message_seq:message.seq,observed_event_seq:seq,message})),...work.map(event=>({id:event.work!.id,kind:"work" as const,anchor_message_seq:messages.get(bot.key.bot_id)?.find(m=>m.turn_id===event.work!.turn_id)?.seq??0,observed_event_seq:seq,work:event.work}))].sort((a,b)=>a.anchor_message_seq-b.anchor_message_seq||(a.kind<b.kind?-1:a.kind>b.kind?1:0)||(a.id<b.id?-1:a.id>b.id?1:0))
    const ref=(kind:string,id:string)=>`${readEpoch}:${generation}:${kind}:${encodeURIComponent(id)}`
    const paginate=(rows:W.ProjectAssistantReadViewItem[],mode:string)=>{const offset=r.cursor?Number(r.cursor.split(":").at(-1)):0;response.items=rows.slice(offset,offset+r.page_size);response.has_more=offset+response.items.length<rows.length;response.next_cursor=response.has_more?`${mode}:${offset+response.items.length}`:""}
    if(r.mode==="snapshot"||r.mode==="history"){
      const count=r.mode==="snapshot"?timeline.length:Number(r.cursor.split(":").at(-1)),start=Math.max(0,count-r.page_size)
      response.items=timeline.slice(start,count);response.has_more=start>0;response.next_cursor=start?`history:${start}`:""
      if(r.mode==="snapshot"){
        const queued=new Set(pending.filter(t=>t.state==="queued"||t.state==="accepted").map(t=>t.message_id)),last=turns.filter(t=>sameFence(t.fence,f)&&!["accepted","queued","running","waiting_permission"].includes(t.state)).at(-1)
        response.snapshot={bot,pending_turns:pending.map(summarize),queued_messages:(messages.get(bot.key.bot_id)??[]).filter(m=>queued.has(m.id)),...(last?{last_turn:summarize(last)}:{})}
      }
    }else if(r.mode==="events"){
      const matching=own.filter(e=>e.seq>r.after_seq);response.items=matching.slice(0,r.page_size).map(event=>({id:event.id,kind:"event",anchor_message_seq:0,observed_event_seq:event.seq,event}));response.has_more=matching.length>response.items.length;response.next_seq=response.items.at(-1)?.event?.seq??r.after_seq
    }else if(r.mode==="permissions")paginate(offers.map(permission=>({id:permission.id,kind:"permission_summary",anchor_message_seq:0,observed_event_seq:seq,permission_summary:{id:permission.id,fence:permission.fence,turn_id:permission.turn_id,state:permission.state,expires_at:permission.expires_at,detail_cursor:ref("permission",permission.id)}})),"permissions")
    else if(r.mode==="traces")paginate(traces.map((trace,i)=>({id:`trace-${trace.turn_id}-${trace.category}`,kind:"trace_summary",anchor_message_seq:0,observed_event_seq:seq,trace_summary:{id:`trace-${trace.turn_id}-${trace.category}`,turn_id:trace.turn_id,category:trace.category,complete:trace.complete,detail_cursor:ref("trace",String(i))}})),"traces")
    else{
      const prefix=`${readEpoch}:${generation}:`,parts=r.cursor.slice(prefix.length).split(":")
      if(!r.cursor.startsWith(prefix))throw failure("pa_cursor_expired")
      if(parts[0]==="permission"){const permission=offers.find(p=>p.id===decodeURIComponent(parts[1]));if(!permission)throw failure("pa_not_found");response.items=[{id:permission.id,kind:"permission",anchor_message_seq:0,observed_event_seq:seq,permission}]}
      else{const trace=traces[Number(parts[1])];if(!trace)throw failure("pa_not_found");response.items=[{id:r.cursor,kind:"trace",anchor_message_seq:0,observed_event_seq:seq,trace}]}
    }
    return structuredClone(response)
  }
  const api:SourceClient={machineID,authorizationRevision:`${machineID}-grant-1`,
    async status(signal){log.push({method:"status",machine:machineID,request:null});if(signal.aborted)throw new DOMException("Aborted","AbortError");const answer={contract_version:contractVersion,sources:[status()]};if(statusGate)await statusGate;return answer},
    async projects(r,signal){
      check("projects",r,signal)
      if(r.machine_id!==machineID)throw failure("pa_scope_denied")
      if(projectFailure)throw failure(projectFailure)
      const offset=r.cursor?Number(r.cursor.split(":")[1]):0
      if(r.cursor&&!r.cursor.startsWith(`${projectRevision}:`))throw failure("pa_cursor_expired")
      const items=Array.from({length:projectCount},(_,i)=>({target:{machine_id:machineID,project_id:i?`project-${i}`:"project"},label:i<2?"Clawdline":`Project ${i+1}`})).slice(offset,offset+r.page_size)
      const more=offset+items.length<projectCount
      const answer:W.ProjectAssistantProjectsResponse={contract_version:contractVersion,source:status(),agents:["claude","codex"],revision:String(projectRevision),items,next_cursor:more?`${projectRevision}:${offset+items.length}`:"",has_more:more}
      if(projectInvalid==="foreign"&&items[0])items[0].target.machine_id="foreign"
      if(projectInvalid==="duplicate"&&items[0])items.push(items[0])
      if(projectInvalid==="cursor"){answer.next_cursor=r.cursor;answer.has_more=true}
      if(projectInvalid==="revision"&&r.cursor)answer.revision="different"
      // Deliberately ignore cancellation after capture to test the publication fence.
      if(projectGate)await projectGate
      return answer
    },
    async list(r,s){check("list",r,s);const matches=bots.filter(b=>!r.query||`${b.name} Clawdline ${summary(b).latest_message}`.toLowerCase().includes(r.query.toLowerCase()));const offset=Number(r.cursor||0);const items=matches.slice(offset,offset+3);return{contract_version:contractVersion,items:items.map(summary),sources:[status()],next_cursor:offset+3<matches.length?String(offset+3):""}},
    async get(r,s){if(legacyForbidden)throw new Error("legacy GET forbidden");if(getGate)await getGate;check("get",r,s);const bot=find(r);return{contract_version:contractVersion,bot,conversation:conversation(bot),source:status()}},
    async create(r,s){check("create",r,s,true);if(createGate)await createGate;if(unknownCreate)throw failure("pa_outcome_unknown");let bot=bots.find(b=>sameBot(b.key,r.command.key));if(!bot){bot={...bots[0],key:r.command.key,name:r.settings.name,appearance:r.settings.appearance,agent:r.settings.agent,permission_policy:r.settings.permission_policy,conversation_id:crypto.randomUUID(),instructions:{text:r.settings.instructions,revision:1,role_snapshot_ref:r.settings.role_snapshot_ref}};bot.project={...project,...r.settings.target};bots.push(bot);created.add(bot.key.bot_id);messages.set(bot.key.bot_id,[])}return{contract_version:contractVersion,bot,receipt:receive(r.command)}},
    async edit(r,s){check("edit",r,s,true);const bot=find(r.command.key);if(bot.version!==r.settings.expected_version)throw failure("pa_stale_version");Object.assign(bot,{name:r.settings.name,appearance:r.settings.appearance,instructions:{...bot.instructions,text:r.settings.instructions,revision:bot.instructions.revision+1},version:bot.version+1});return{contract_version:contractVersion,bot,receipt:receive(r.command)}},
    async history(r,s){if(legacyForbidden)throw new Error("legacy history forbidden");check("history",r,s);const bot=find(r.key),all=(messages.get(r.key.bot_id)??[]).filter(m=>!r.before_seq||m.seq<r.before_seq),items=all.slice(-r.page_size);return{contract_version:contractVersion,conversation:conversation(bot),items,has_more:all.length>items.length,next_before_seq:items[0]?.seq??0}},
    async view(r,s){
      check("view",r,s)
      if(viewUnsupported)throw failure("pa_unsupported")
      if(r.mode!=="snapshot"&&r.source_epoch!==readEpoch)throw failure("pa_cursor_expired")
      if(r.mode==="events"&&expiredCursor&&sameBot(r.key,bots[0].key))throw failure("pa_cursor_expired")
      if(r.mode==="snapshot"&&sameBot(r.key,bots[0].key)){
        if(viewFailure||exportFailure!=="ok")throw failure(viewFailure??exportFailure as W.ProjectAssistantErrorCode)
        expiredCursor=false
      }
      if(viewOversized&&r.mode==="detail")throw failure("pa_capacity_full")
      if(detailFailure&&r.mode==="detail")throw failure(detailFailure)
      const answer=readPage(r)
      if(detailFenceChange&&r.mode==="detail"){
        answer.head.current_fence.generation++;answer.head.conversation.generation++
        if(answer.items[0].permission)answer.items[0].permission.fence.generation=answer.head.current_fence.generation
      }
      if(snapshotHolds&&r.mode==="snapshot")await new Promise<void>(resolve=>snapshotHolds!.push({release:resolve,signal:s}))
      if(viewInvalid==="mode")answer.mode="detail"
      if(viewInvalid==="scope")answer.head.project={...answer.head.project,canonical_id:"foreign"}
      if(viewInvalid==="cursor"){answer.has_more=true;answer.next_cursor=r.cursor}
      // Ignore cancellation after capture, as an adversarial transport can do.
      if(viewGate&&(!holdMode||holdMode===r.mode))await viewGate
      if(historyGate&&r.mode==="history")await historyGate
      if(eventGate&&r.mode==="events"&&sameBot(r.key,bots[0].key))await eventGate
      return answer
    },
    async send(r,s){check("send",r,s,true);if(created.has(r.command.key.bot_id))throw failure("adapter_not_found");const bot=find(r.command.key);const receipt=receive(r.command);if(r.text==="unknown"){receipt.state="unknown";return{contract_version:contractVersion,receipt}}
      const rows=messages.get(bot.key.bot_id)??[];const message:W.ProjectAssistantMessage={id:receipt.id,seq:rows.length+1,at,fence:fence(bot),kind:"user",text:r.text,quote_message_id:r.quote_message_id,turn_id:receipt.id};if(!rows.some(m=>m.id===message.id)){messages.set(bot.key.bot_id,[...rows,message]);push({id:crypto.randomUUID(),seq:0,at,version:contractVersion,kind:"message_created",fence:fence(bot),message})}return{contract_version:contractVersion,receipt}},
    async stop(r,s){check("stop",r,s,true);return{contract_version:contractVersion,receipt:receive(r.command)}},
    async answer(r,s){check("answer",r,s,true);return{contract_version:contractVersion,receipt:receive(r.command)}},
    async markRead(r,s){check("markRead",r,s);return{contract_version:contractVersion,cursor:r.cursor}},
    async observe(r,s){check("observe",r,s);return{contract_version:contractVersion,receipt:receive(r.command)}},
    async events(r,s){if(legacyForbidden)throw new Error("legacy events forbidden");if(eventGate&&sameBot(r.key,bots[0].key))await eventGate;check("events",r,s);if(expiredCursor&&sameBot(r.key,bots[0].key))throw failure("pa_cursor_expired");return{contract_version:contractVersion,items:events.filter(e=>sameBot(e.fence.key,r.key)&&e.seq>r.after_seq),has_more:false,next_seq:seq}},
    async forget(r,s){check("forget",r,s,true);memory.revision++;memory.forgotten_source_ids.push(...r.source_ids);memory.forgotten_through=memory.revision;return{contract_version:contractVersion,memory:{...memory,key:r.command.key}}},
    async saveMemory(r,s){check("saveMemory",r,s,true);return{contract_version:contractVersion,memory:{...memory,key:r.command.key}}},
    async export(r,s){if(legacyForbidden)throw new Error("huge legacy Export forbidden");if(exportGate)await exportGate;check("export",r,s);if(exportFailure!=="ok"&&sameBot(r,bots[0].key))throw failure(exportFailure);if(sameBot(r,bots[0].key))expiredCursor=false;const bot=find(r),revision={...memory,key:r},ownEvents=events.filter(e=>sameBot(e.fence.key,r));return{contract_version:contractVersion,schema_version:1,bot,conversation:conversation(bot),messages:messages.get(r.bot_id)??[],memory:revision,memory_entries:[],context:{memory:revision,provenance_complete:true,sources:[],text:""},bindings:[],permissions:ownEvents.flatMap(e=>e.permission?[e.permission]:[]),read_cursors:[],receipts:[...receipts.values()].filter(receipt=>sameBot(receipt.command.key,r)),turns:ownEvents.flatMap(e=>e.turn?[e.turn]:[]),work_links:[...new Map(ownEvents.flatMap(e=>e.work?[[e.work.id,e.work] as const]:[])).values()],events:ownEvents}},
  }
  return Object.assign(api,{configureProjects(options:{count?:number;failure?:W.ProjectAssistantErrorCode|null;invalid?:"duplicate"|"foreign"|"cursor"|"revision"|null}){if(options.count!==undefined){projectCount=options.count;projectRevision++}if("failure" in options)projectFailure=options.failure??null;if("invalid" in options)projectInvalid=options.invalid??null},
    holdProjects(){projectGate=new Promise(resolve=>{releaseProjects=resolve})},releaseProjects(){releaseProjects?.();projectGate=null;releaseProjects=null},unknownCreate(value:boolean){unknownCreate=value},
    holdStatus(){statusGate=new Promise(resolve=>{releaseStatus=resolve})},releaseStatus(){releaseStatus?.();statusGate=null;releaseStatus=null},
    holdCreate(){createGate=new Promise(resolve=>{releaseCreate=resolve})},releaseCreate(){releaseCreate?.();createGate=null;releaseCreate=null},
    setState(next:W.ProjectAssistantSourceState,nextAccess=access){state=next;access=nextAccess},push,log,bots,
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
    configureViews(options:{failure?:W.ProjectAssistantErrorCode|null;invalid?:string|null;unsupported?:boolean;forbidLegacy?:boolean;oversizedDetail?:boolean;detailFailure?:W.ProjectAssistantErrorCode|null;detailFenceChange?:boolean}){if("failure" in options)viewFailure=options.failure??null;if("invalid" in options)viewInvalid=options.invalid??null;if(options.unsupported!==undefined)viewUnsupported=options.unsupported;if(options.forbidLegacy!==undefined)legacyForbidden=options.forbidLegacy;if(options.oversizedDetail!==undefined)viewOversized=options.oversizedDetail;if("detailFailure" in options)detailFailure=options.detailFailure??null;if(options.detailFenceChange!==undefined)detailFenceChange=options.detailFenceChange},
    holdSnapshots(){snapshotHolds=[]},releaseSnapshot(index:number){snapshotHolds?.[index]?.release()},heldSnapshotSignals(){return snapshotHolds?.map(row=>row.signal.aborted)??[]},
    expireSameFence(){expiredCursor=true},
    holdView(mode?:W.ProjectAssistantReadViewMode){holdMode=mode??null;viewGate=new Promise(resolve=>{releaseView=resolve})},releaseView(){releaseView?.();viewGate=null;releaseView=null;holdMode=null},
    holdHistory(){historyGate=new Promise(resolve=>{releaseHistory=resolve})},releaseHistory(){releaseHistory?.();historyGate=null;releaseHistory=null},
    seedHistory(count:number){
      const bot=bots[0],rows:W.ProjectAssistantMessage[]=Array.from({length:count},(_,i)=>({id:`history-${i+1}`,seq:i+1,kind:i%2?"assistant":"user",text:`History message ${i+1}`,at,fence:fence(bot),turn_id:`history-turn-${i+1}`,quote_message_id:""}));messages.set(bot.key.bot_id,rows)
      events.length=0;seq=0
      for(const message of rows)push({id:`journal-${message.id}`,seq:0,at,version:contractVersion,kind:"message_created",fence:message.fence,message})
      for(const message of messages.get(bots[2].key.bot_id)??[])push({id:`other-${message.id}`,seq:0,at,version:contractVersion,kind:"message_created",fence:message.fence,message})
    },
    seedCollections(count:number){
      const bot=bots[0],f=fence(bot)
      for(let i=events.length-1;i>=0;i--)if(events[i].trace&&sameBot(events[i].fence.key,bot.key))events.splice(i,1)
      for(let i=0;i<count;i++){
        push({id:`offer-${i}`,seq:0,at,version:contractVersion,kind:"permission_offered",fence:f,permission:{id:`offer-${i}`,fence:f,turn_id:`collection-turn-${i}`,native_request_id:`request-${i}`,state:"pending",expires_at:"2099-01-01T00:00:00Z",selected_option_id:"",options:Array.from({length:101},(_,j)=>({id:`option-${j}`,kind:j?"reject_once":"allow_once",label:`stored ${j}`,allow:j===0,read_only:true}))}})
        push({id:`trace-${i}`,seq:0,at,version:contractVersion,kind:"trace_updated",fence:f,trace:{turn_id:`collection-turn-${i}`,category:"read",complete:true,text:`Complete trace ${i}`}})
      }
    },
    seedWork(state:W.ProjectAssistantWorkState){const bot=bots[0],f={...fence(bot),generation:0};push({id:crypto.randomUUID(),seq:0,at,version:contractVersion,kind:"work_updated",fence:f,work:{id:"late-work",fence:f,turn_id:"history-turn-1",board_item_id:"item",task_id:"task",state,landing_commit:state==="landed"?"newer-work-observation":"",result_message_id:""}})},
    seedPending(queuedCount:number,unknownCount:number){
      const bot=bots[0],rows=messages.get(bot.key.bot_id)??[]
      for(let i=0;i<queuedCount+unknownCount;i++){
        const id=`pending-${i}`,state=i<queuedCount?"queued":"unknown",f=i<queuedCount?fence(bot):{...fence(bot),generation:0}
        const message:W.ProjectAssistantMessage={id:`message-${id}`,seq:rows.length+1,at,fence:f,kind:"user",text:`Pending message ${i}`,turn_id:id,quote_message_id:""};rows.push(message)
        push({id:`event-${id}`,seq:0,at,version:contractVersion,kind:"turn_updated",fence:f,turn:{id,fence:f,message_id:message.id,state,started_at:at,completed_at:"",instructions:{bot_revision:1,role_snapshot_ref:"",text:"not copied into summary",memory:{...memory,key:bot.key},sources:[]}} as unknown as W.ProjectAssistantTurn})
      }
      messages.set(bot.key.bot_id,rows)
    },

  })
}

export function mountFixture() {
  const log:FixtureLog[]=[],a=fixtureSource("studio",log),b=fixtureSource("notebook",log)
  let sourceList:SourceClient[]=[a,b]
  const root=createRoot(document.getElementById("fixture")!),language=new URLSearchParams(location.search).get("lang")??"zh-Hant"
  const view=createViewState(window.localStorage)
  let accountKey="fixture-account",hostGeneration=0,opened=false,opener:HTMLElement|null=null
  const paint=()=>root.render(React.createElement(React.Fragment,null,
    React.createElement("div",{className:"fixture-sessions",hidden:opened},React.createElement("h1",null,"clawdline"),React.createElement("h2",null,"Sessions"),React.createElement("p",null,"Synthetic Session list · existing voice owner remains mounted"),React.createElement("button",{id:"session-opener"},"Original Session"),React.createElement(BotOrb,{onOpen:(element?:HTMLElement)=>{opener=element??null;opened=true;paint()},language,unread:4,pendingPermissions:2})),
    opened?React.createElement(Bots,{key:hostGeneration,viewState:view,sources:sourceList,onClose:()=>{opened=false;paint()},language,accountKey,readerID:"fixture-reader",manageHistory:true,returnFocus:opener}):null,
    React.createElement("button",{id:"voice-owner",onClick:()=>{},"aria-label":"Existing voice stop control"},"Voice stop")))
  const controls={log,a,b,
    // Update authority before the host renders: selected ref and abort stay unchanged.
    revokeBeforeRender(){sourceList.splice(sourceList.indexOf(a),1)},
    replaceGrantBeforeRender(){sourceList.splice(sourceList.indexOf(a),1,Object.assign(Object.create(a),{authorizationRevision:"replacement-grant"}))},
    account(next:string){accountKey=next;hostGeneration++;paint()},
    remount(){hostGeneration++;paint()},open(){opened=true;paint()},revoke(){sourceList=[b];paint()},offline(){a.setState("offline");paint()},readOnly(){a.setState("online","history_only");sourceList=[Object.assign(Object.create(a),{authorizationRevision:"readonly-grant"}),b];paint()},shapes(){root.render(React.createElement("div",{className:"project-assistants fixture-shapes"},Object.keys(avatarPaths).map((shape,i)=>React.createElement("figure",{key:shape},React.createElement(BotAvatar,{appearance:{shape:shape as W.ProjectAssistantShape,color:palette[i]},size:92}),React.createElement("figcaption",null,shape))),React.createElement(BotOrb,{onOpen:()=>{},language})))}}
  Object.assign(window,{botsFixture:controls});paint()
}
