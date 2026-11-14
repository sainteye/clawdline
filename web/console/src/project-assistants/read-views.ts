import type * as W from "@clawdline/contract"
// @ts-expect-error -- Runtime enums use the generated source extension for Node's strip-types runner.
import { ProjectAssistantAccessValues, ProjectAssistantAgentValues, ProjectAssistantShapeValues, ProjectAssistantPermissionPolicyValues, ProjectAssistantSourceStateValues, ProjectAssistantNativeMemoryModeValues, ProjectAssistantTurnStateValues, ProjectAssistantPermissionStateValues, ProjectAssistantMessageKindValues, ProjectAssistantWorkStateValues, ProjectAssistantEventKindValues, ProjectAssistantErrorCodeValues, ProjectAssistantReadViewModeValues } from "../../../contract/src/generated.ts"
// @ts-expect-error -- Node's strip-types runner loads the source extension.
import { failure } from "./client.ts"
// @ts-expect-error -- Node's strip-types runner loads the source extension.
import { sameConversation, sameFence, canRead } from "./view-state.ts"

export type ReadScope = Pick<W.ProjectAssistantFence,"key"|"project"|"conversation_id">
const integer=(n:unknown,min=0):n is number=>Number.isSafeInteger(n)&&Number(n)>=min
const text=(s:unknown):s is string=>typeof s==="string"&&s.length>0
const object=(v:unknown):v is Record<string,unknown>=>typeof v==="object"&&v!==null&&!Array.isArray(v)
const oneOf=<T extends string>(v:unknown,choices:readonly T[]):v is T=>typeof v==="string"&&choices.includes(v as T)
const date=(v:unknown)=>typeof v==="string"&&Number.isFinite(Date.parse(v))
const key=(v:unknown):v is W.ProjectAssistantBotKey=>object(v)&&text(v.machine_id)&&text(v.bot_id)
const project=(v:unknown):v is W.ProjectAssistantProjectScope=>object(v)&&text(v.machine_id)&&text(v.project_id)&&text(v.canonical_id)&&(v.kind==="repository"||v.kind==="place")
const fence=(v:unknown):v is W.ProjectAssistantFence=>object(v)&&key(v.key)&&project(v.project)&&text(v.conversation_id)&&typeof v.binding_id==="string"&&integer(v.generation)
const cursor=(v:unknown):v is string=>typeof v==="string"&&new TextEncoder().encode(v).length<=4096
const states=ProjectAssistantTurnStateValues
const permissionStates=ProjectAssistantPermissionStateValues
function own(value:unknown,scope:ReadScope,current?:W.ProjectAssistantFence):asserts value is W.ProjectAssistantFence {
  if(!fence(value))throw failure("pa_invalid")
  if(!sameConversation(value,scope))throw failure("pa_scope_denied")
  if(current&&!sameFence(value,current))throw failure("pa_stale_generation")
}
function sourceFailure(f:W.ProjectAssistantFailure) {
  if(!object(f)||!oneOf(f.code,ProjectAssistantErrorCodeValues)||typeof f.field!=="string"||!integer(f.retry_after_seconds))throw failure("pa_invalid")
}
/** Validate the raw source before writable/grant composition can mask malformed flags. */
export function readSource(value:unknown,machineID:string):W.ProjectAssistantSourceStatus {
  const v=value as W.ProjectAssistantSourceStatus
  if(!object(v)||!text(v.machine_id)||!oneOf(v.state,ProjectAssistantSourceStateValues)||!oneOf(v.access,ProjectAssistantAccessValues)||typeof v.readable!=="boolean"||typeof v.writable!=="boolean"||typeof v.fresh!=="boolean"||typeof v.observed_at!=="string"||!Array.isArray(v.capabilities))throw failure("pa_invalid")
  if(v.machine_id!==machineID)throw failure("pa_scope_denied")
  for(const c of v.capabilities){
    if(!object(c)||!key(c.key)||!project(c.project)||!oneOf(c.agent,ProjectAssistantAgentValues)||typeof c.cli_version!=="string"||typeof c.adapter_version!=="string"||typeof c.evidence!=="string"||!oneOf(c.native_memory,ProjectAssistantNativeMemoryModeValues)||typeof c.native_memory_verified!=="boolean"||typeof c.read_only_enforced!=="boolean"||typeof c.resume!=="boolean"||typeof c.send_message!=="boolean")throw failure("pa_invalid")
    if(c.key.machine_id!==machineID||c.project.machine_id!==machineID)throw failure("pa_scope_denied")
    if(c.unavailable!==undefined)sourceFailure(c.unavailable)
  }
  return v
}
function metadata(b:W.ProjectAssistantBot,scope:ReadScope) {
  if(!object(b)||!key(b.key)||!project(b.project)||!text(b.conversation_id)||!text(b.name)||!integer(b.version,1)||!oneOf(b.agent,ProjectAssistantAgentValues)||!oneOf(b.permission_policy,ProjectAssistantPermissionPolicyValues)||!object(b.appearance)||!oneOf(b.appearance.shape,ProjectAssistantShapeValues)||typeof b.appearance.color!=="string"||!object(b.instructions)||!integer(b.instructions.revision,1)||typeof b.instructions.text!=="string"||typeof b.instructions.role_snapshot_ref!=="string"||!date(b.created_at)||!date(b.updated_at))throw failure("pa_invalid")
  if(!sameConversation({key:b.key,project:b.project,conversation_id:b.conversation_id},scope))throw failure("pa_scope_denied")
}
function work(w:W.ProjectAssistantWorkLink,scope:ReadScope) {
  if(!object(w)||!text(w.id)||!text(w.turn_id)||!oneOf(w.state,ProjectAssistantWorkStateValues)||typeof w.board_item_id!=="string"||typeof w.task_id!=="string"||typeof w.landing_commit!=="string"||typeof w.result_message_id!=="string")throw failure("pa_invalid")
  own(w.fence,scope)
}

function message(m:W.ProjectAssistantMessage,scope:ReadScope) {
  if(!m||!text(m.id)||!integer(m.seq,1)||typeof m.text!=="string"||typeof m.turn_id!=="string"||typeof m.quote_message_id!=="string"||!date(m.at)||!oneOf(m.kind,ProjectAssistantMessageKindValues))throw failure("pa_invalid")
  own(m.fence,scope)
}
function turn(t:W.ProjectAssistantReadTurnSummary,scope:ReadScope,current?:W.ProjectAssistantFence) {
  if(!t||!text(t.id)||!text(t.message_id)||!oneOf(t.state,states)||typeof t.failure_code!=="string")throw failure("pa_invalid")
  own(t.fence,scope,current)
}
function offer(p:W.ProjectAssistantPermissionOffer,scope:ReadScope,current?:W.ProjectAssistantFence) {
  if(!p||!text(p.id)||!text(p.turn_id)||!oneOf(p.state,permissionStates)||!date(p.expires_at)||!Array.isArray(p.options)||typeof p.native_request_id!=="string"||typeof p.selected_option_id!=="string")throw failure("pa_invalid")
  own(p.fence,scope,current)
  // Display text from the adapter; an older daemon omits it.
  const a=p.action
  if(a!==undefined&&(!a||typeof a.title!=="string"||typeof a.tool_kind!=="string"||typeof a.detail!=="string"||typeof a.shortened!=="boolean"||!Array.isArray(a.diffs)||a.diffs.some(d=>!d||typeof d.path!=="string"||typeof d.old_text!=="string"||typeof d.new_text!=="string")))throw failure("pa_invalid")
  const ids=new Set<string>()
  for(const option of p.options){if(!option||!text(option.id)||ids.has(option.id)||typeof option.label!=="string"||typeof option.allow!=="boolean"||typeof option.read_only!=="boolean"||!["allow_once","allow_always","reject_once","reject_always"].includes(option.kind))throw failure("pa_invalid");ids.add(option.id)}
}
function trace(t:W.ProjectAssistantTrace) {if(!t||!text(t.turn_id)||!text(t.category)||typeof t.text!=="string"||typeof t.complete!=="boolean")throw failure("pa_invalid")}

/** The existing wire limits are shared with the source: 100/256 rows, 16 turns, 4096 cursor bytes. */
export function readViewRequest(q:W.ProjectAssistantReadViewRequest):W.ProjectAssistantReadViewRequest {
  if(!q||!key(q.key)||!text(q.conversation_id)||!oneOf(q.mode,ProjectAssistantReadViewModeValues)||typeof q.source_epoch!=="string"||!cursor(q.cursor)||!integer(q.after_seq)||!integer(q.page_size,1)||q.page_size>(q.mode==="events"?256:100))throw failure("pa_invalid")
  if(q.mode==="snapshot") {if(q.source_epoch||q.cursor||q.after_seq)throw failure("pa_invalid")}
  else if(!q.source_epoch||q.mode!=="events"&&q.after_seq||q.mode==="events"&&q.cursor||(q.mode==="history"||q.mode==="detail")&&!q.cursor||q.mode==="detail"&&q.page_size!==1)throw failure("pa_invalid")
  return q
}

/** Refuse malformed or foreign successful envelopes before they can release a recovery latch. */
export function readView(answer:W.ProjectAssistantReadViewResponse,q:W.ProjectAssistantReadViewRequest,scope:ReadScope):W.ProjectAssistantReadViewResponse {
  readViewRequest(q)
  if(q.key.machine_id!==scope.key.machine_id||q.key.bot_id!==scope.key.bot_id||q.conversation_id!==scope.conversation_id)throw failure("pa_scope_denied")
  if(!answer||answer.contract_version!=="project-assistants.v1"||answer.mode!==q.mode||!text(answer.source_epoch)||!answer.source||!answer.head||!Array.isArray(answer.items)||answer.items.length>q.page_size||typeof answer.has_more!=="boolean"||!cursor(answer.next_cursor)||!integer(answer.next_seq))throw failure("pa_invalid")
  if(q.mode!=="snapshot"&&q.source_epoch!==answer.source_epoch)throw failure("pa_cursor_expired")
  readSource(answer.source,q.key.machine_id)
  if(!canRead(answer.source))throw failure(answer.source.state==="revoked"?"pa_revoked":"pa_scope_denied")
  const h=answer.head,c=h.conversation
  if(!key(h.key)||!project(h.project)||!c||!key(c.key)||!text(c.id)||!integer(c.generation)||typeof c.binding_id!=="string"||!integer(c.last_message_seq)||!integer(c.memory_revision)||!integer(c.version,1)||typeof c.active_turn_id!=="string"||!integer(h.bot_version,1)||!integer(h.memory_revision,1)||!integer(h.event_frontier)||!integer(h.unknown_turn_count)||!integer(h.pending_permission_count)||!integer(h.trace_category_count))throw failure("pa_invalid")
  own(h.current_fence,scope)
  if(!sameConversation({key:h.key,project:h.project,conversation_id:c.id},scope)||h.project.machine_id!==h.key.machine_id||c.key.machine_id!==h.key.machine_id||c.key.bot_id!==h.key.bot_id)throw failure("pa_scope_denied")
  if(c.binding_id!==h.current_fence.binding_id||c.generation!==h.current_fence.generation||c.memory_revision>h.memory_revision)throw failure("pa_invalid")
  if(q.mode==="snapshot") {
    const s=answer.snapshot,b=s?.bot
    if(!s||!b||!Array.isArray(s.pending_turns)||!Array.isArray(s.queued_messages)||s.pending_turns.length>16||s.queued_messages.length>16||b.version!==h.bot_version)throw failure("pa_invalid")
    metadata(b,scope)
    const ids=new Set<string>(),queued=new Set<string>(),pending=new Map<string,W.ProjectAssistantReadTurnSummary>(),included=new Set<string>()
    for(const t of s.pending_turns){turn(t,scope);if(ids.has(t.id)||pending.has(t.message_id)||!["accepted","queued","running","waiting_permission"].includes(t.state))throw failure("pa_invalid");ids.add(t.id);pending.set(t.message_id,t);if(t.state==="accepted"||t.state==="queued")queued.add(t.message_id)}
    for(const m of s.queued_messages){message(m,scope);const t=pending.get(m.id);if(m.kind!=="user"||!t||m.turn_id!==t.id||included.has(m.id))throw failure("pa_invalid");included.add(m.id);queued.delete(m.id)}
    if(queued.size)throw failure("pa_invalid")
    if(s.last_turn){turn(s.last_turn,scope,h.current_fence);if(!["succeeded","failed","cancelled","unknown"].includes(s.last_turn.state))throw failure("pa_invalid")}
  }else if(answer.snapshot!==undefined)throw failure("pa_invalid")
  const allowed=q.mode==="snapshot"||q.mode==="history"?["message","work"]:q.mode==="events"?["event"]:q.mode==="permissions"?["permission_summary"]:q.mode==="traces"?["trace_summary"]:["permission","trace"]
  const ids=new Set<string>(),identities=new Set<string>();let seq=q.after_seq
  for(const row of answer.items) {
    if(!row||!text(row.id)||!allowed.includes(row.kind)||!integer(row.anchor_message_seq)||!integer(row.observed_event_seq)||row.observed_event_seq>h.event_frontier)throw failure("pa_invalid")
    const rowIdentity=JSON.stringify([row.kind,row.id])
    if(ids.has(rowIdentity))throw failure("pa_invalid")
    ids.add(rowIdentity)
    const kinds=["message","work","event","permission_summary","trace_summary","permission","trace"] as const
    if(kinds.filter(kind=>row[kind]!==undefined).length!==1||!row[row.kind])throw failure("pa_invalid")
    const payloadID=row.message?.id??row.work?.id??row.permission_summary?.id??row.trace_summary?.id??row.permission?.id??(row.trace?JSON.stringify([row.trace.turn_id,row.trace.category]):row.event?.id)
    const identity=JSON.stringify([row.kind,payloadID]);if(identities.has(identity))throw failure("pa_invalid");identities.add(identity)
    if(row.message){message(row.message,scope);if(row.anchor_message_seq!==row.message.seq)throw failure("pa_invalid")}
    if(row.work)work(row.work,scope)
    if(row.permission_summary){const p=row.permission_summary;if(!text(p.id)||!text(p.turn_id)||!oneOf(p.state,permissionStates)||!date(p.expires_at)||!cursor(p.detail_cursor)||!p.detail_cursor||(p.title!==undefined&&typeof p.title!=="string")||(p.tool_kind!==undefined&&typeof p.tool_kind!=="string"))throw failure("pa_invalid");own(p.fence,scope,h.current_fence)}
    if(row.trace_summary){const t=row.trace_summary;if(!text(t.id)||!text(t.turn_id)||!text(t.category)||typeof t.complete!=="boolean"||!cursor(t.detail_cursor)||!t.detail_cursor)throw failure("pa_invalid")}
    if(row.permission)offer(row.permission,scope,h.current_fence)
    if(row.trace)trace(row.trace)
    if(row.event){
      const e=row.event
      if(!text(e.id)||!integer(e.seq,1)||e.seq<=seq||e.seq!==row.observed_event_seq||e.version!=="project-assistants.v1"||!date(e.at)||!oneOf(e.kind,ProjectAssistantEventKindValues))throw failure("pa_invalid")
      const payloads=["bot","message","work","turn","permission","trace","source","memory","read"] as const
      const expected={bot_created:"bot",bot_updated:"bot",message_created:"message",work_updated:"work",turn_updated:"turn",permission_offered:"permission",permission_answered:"permission",trace_updated:"trace",source_updated:"source",memory_changed:"memory",read_advanced:"read",feature_disabled:""}[e.kind]
      if(payloads.filter(k=>e[k]!==undefined).length!==(expected?1:0)||expected&&!(e as unknown as Record<string,unknown>)[expected])throw failure("pa_invalid")
      seq=e.seq;own(e.fence,scope)
      if(e.message)message(e.message,scope)
      if(e.work)work(e.work,scope)
      if(e.turn){const t=e.turn;if(!object(t)||!text(t.id)||!text(t.message_id)||!oneOf(t.state,states))throw failure("pa_invalid");own(t.fence,scope);if(t.failure!==undefined)sourceFailure(t.failure)}
      if(e.permission)offer(e.permission,scope)
      if(e.trace)trace(e.trace)
      if(e.source)readSource(e.source,q.key.machine_id)
      if(e.bot){const b=e.bot;metadata(b.bot,scope);readSource(b.source,q.key.machine_id);if(!integer(b.unread_count)||!integer(b.pending_permissions)||!integer(b.version,1)||typeof b.activity!=="string"||typeof b.activity_at!=="string"||typeof b.latest_message!=="string"||!b.read_cursor)throw failure("pa_invalid");const r=b.read_cursor;if(!key(r.key)||!text(r.conversation_id)||!integer(r.message_seq)||typeof r.reader_id!=="string")throw failure("pa_invalid");if(r.key.machine_id!==scope.key.machine_id||r.key.bot_id!==scope.key.bot_id||r.conversation_id!==scope.conversation_id)throw failure("pa_scope_denied")}
      if(e.memory){const m=e.memory;if(!key(m.key)||!project(m.project)||!integer(m.revision,1)||!integer(m.forgotten_through)||!Array.isArray(m.forgotten_source_ids)||m.forgotten_source_ids.some(id=>typeof id!=="string"))throw failure("pa_invalid");if(!sameConversation({key:m.key,project:m.project,conversation_id:scope.conversation_id},scope))throw failure("pa_scope_denied")}
      if(e.read){const r=e.read;if(!key(r.key)||!text(r.conversation_id)||!integer(r.message_seq)||typeof r.reader_id!=="string")throw failure("pa_invalid");if(r.key.machine_id!==scope.key.machine_id||r.key.bot_id!==scope.key.bot_id||r.conversation_id!==scope.conversation_id)throw failure("pa_scope_denied")}
    }
  }
  if(q.mode==="events") {if(answer.next_cursor||answer.next_seq!==seq||seq>h.event_frontier||answer.has_more&&seq===q.after_seq)throw failure("pa_invalid")}
  else if(answer.next_seq!==0||answer.has_more!==!!answer.next_cursor||answer.has_more&&(!answer.items.length||answer.next_cursor===q.cursor))throw failure("pa_invalid")
  if(q.mode==="detail"&&(answer.items.length!==1||answer.has_more))throw failure("pa_invalid")
  return answer
}

/** Source row IDs and tuple anchors stay intact; a stale live page cannot downgrade an event. */
export function mergeTimeline(pages:readonly (readonly W.ProjectAssistantReadViewItem[])[]):W.ProjectAssistantReadViewItem[] {
  const rows=new Map<string,W.ProjectAssistantReadViewItem>()
  for(const row of pages.flat()) {
    if(row.kind!=="message"&&row.kind!=="work")continue
    const id=JSON.stringify([row.kind,row.work?.id??row.message?.id??row.id]),old=rows.get(id)
    if(!old||row.observed_event_seq>=old.observed_event_seq)rows.set(id,row)
    else if(row.work&&old.anchor_message_seq===0&&row.anchor_message_seq>0)rows.set(id,{...old,id:row.id,anchor_message_seq:row.anchor_message_seq})
  }
  return [...rows.values()].sort((a,b)=>a.anchor_message_seq-b.anchor_message_seq||(a.kind<b.kind?-1:a.kind>b.kind?1:0)||(a.id<b.id?-1:a.id>b.id?1:0))
}

/** Event work has no invented admission anchor: retain a source anchor when already known. */
export function eventTimeline(rows:readonly W.ProjectAssistantReadViewItem[],events:readonly W.ProjectAssistantReadViewItem[]):W.ProjectAssistantReadViewItem[] {
  const added:W.ProjectAssistantReadViewItem[]=[]
  for(const row of events) {
    const e=row.event;if(!e)continue
    if(e.message)added.push({id:e.message.id,kind:"message",anchor_message_seq:e.message.seq,observed_event_seq:e.seq,message:e.message})
    if(e.work){const previous=rows.find(r=>r.work?.id===e.work!.id);added.push({id:previous?.id??e.work.id,kind:"work",anchor_message_seq:previous?.anchor_message_seq??0,observed_event_seq:e.seq,work:e.work})}
  }
  return mergeTimeline([rows,added])
}

export function turnSummary(t:W.ProjectAssistantTurn):W.ProjectAssistantReadTurnSummary {return{id:t.id,fence:t.fence,message_id:t.message_id,state:t.state,failure_code:t.failure?.code??""}}
