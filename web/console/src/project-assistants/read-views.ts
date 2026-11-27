// @ts-expect-error -- Node loads the source extension.
import {inputMetadata} from "./inputs.ts"
// @ts-expect-error -- Node strip-types source extension.
import {outputMetadata} from "./outputs.ts"
import type * as W from "@clawdline/contract"
// @ts-expect-error -- Node's strip-types runner loads the source extension.
import { catalogWord } from "../catalog.ts"
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
const project=(v:unknown):v is W.ProjectAssistantProjectScope=>object(v)&&text(v.machine_id)&&text(v.project_id)&&text(v.canonical_id)&&(v.kind==="repository"||v.kind==="place"||v.kind==="personal")
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
  if(b.reply_input!==undefined&&b.reply_input!=="queue"&&b.reply_input!=="steer")throw failure("pa_invalid")
  if(!sameConversation({key:b.key,project:b.project,conversation_id:b.conversation_id},scope))throw failure("pa_scope_denied")
}
export function readBotMetadata(b:W.ProjectAssistantBot,scope:ReadScope) { metadata(b,scope) }
function work(w:W.ProjectAssistantWorkLink,scope:ReadScope) {
  if(!object(w)||!text(w.id)||!text(w.turn_id)||!oneOf(w.state,ProjectAssistantWorkStateValues)||typeof w.board_item_id!=="string"||typeof w.task_id!=="string"||typeof w.landing_commit!=="string"||typeof w.result_message_id!=="string")throw failure("pa_invalid")
  own(w.fence,scope)
}
function eventWork(w:W.ProjectAssistantWorkLink|undefined,scope:ReadScope,anchor:number,lastMessageSeq:number) {
  if(!w)return
  work(w,scope)
  if(anchor>lastMessageSeq)throw failure("pa_invalid")
}

function message(m:W.ProjectAssistantMessage,scope:ReadScope) {
  if(!m||!text(m.id)||!integer(m.seq,1)||typeof m.text!=="string"||typeof m.turn_id!=="string"||typeof m.quote_message_id!=="string"||!date(m.at)||!oneOf(m.kind,ProjectAssistantMessageKindValues)||(m.attachments!==undefined&&(!Array.isArray(m.attachments)||m.attachments.some(a=>!a||!/^[0-9a-f]{64}$/.test(a.id)||typeof a.media_type!=="string"||!integer(a.bytes,1)))))throw failure("pa_invalid")
  if(m.reactions!==undefined){if(!Array.isArray(m.reactions))throw failure("pa_invalid");const actors=new Set<string>();for(const a of m.reactions){if(!a||!text(a.id)||actors.has(a.id)||!["human","bot"].includes(a.kind)||typeof a.name!=="string"||!integer(a.revision,1)||!Array.isArray(a.emojis)||a.emojis.some(e=>typeof e!=="string"||!e||new TextEncoder().encode(e).length>32)||a.own!==undefined&&typeof a.own!=="boolean"||a.kind==="bot"&&(!key(a.bot)||a.bot.machine_id!==scope.key.machine_id||a.emojis.length!==1)||a.kind==="human"&&(a.bot!==undefined||a.emojis.length>8))throw failure("pa_invalid");if(a.appearance!==undefined&&(!object(a.appearance)||typeof a.appearance.color!=="string"||!oneOf(a.appearance.shape,ProjectAssistantShapeValues)))throw failure("pa_invalid");actors.add(a.id)}}
  if(m.files!==undefined){if(!Array.isArray(m.files)||m.kind!=="user")throw failure("pa_invalid");m.files.forEach(inputMetadata)}
  if(m.output!==undefined){outputMetadata(m.output);if(m.kind!=="assistant")throw failure("pa_invalid")}
  const notice=m.notice
  if(notice?.key==="peer_dispatch"||notice?.key==="peer_dispatch_failed"){
    if(m.kind!=="notice"||!text(notice.cause)||
      [notice.requester,notice.task_title,notice.failure_field].some(value=>value!==undefined&&typeof value!=="string")||
      (notice.key==="peer_dispatch_failed"&&!text(notice.failure_code))||
      (notice.requester_appearance!==undefined&&(!object(notice.requester_appearance)||typeof notice.requester_appearance.color!=="string"||
        !oneOf(notice.requester_appearance.shape,ProjectAssistantShapeValues))))throw failure("pa_invalid")
  }
  own(m.fence,scope)
}
function turn(t:W.ProjectAssistantReadTurnSummary,scope:ReadScope,current?:W.ProjectAssistantFence) {
  // started_at arrived with API level 63; an older daemon omits it.
  if(!t||!text(t.id)||!text(t.message_id)||!oneOf(t.state,states)||typeof t.failure_code!=="string"||t.failure_field!==undefined&&typeof t.failure_field!=="string"||t.started_at!==undefined&&typeof t.started_at!=="string")throw failure("pa_invalid")
  own(t.fence,scope,current)
  if(t.queue_edit_id!==undefined&&(!text(t.queue_edit_id)||t.state!=="queued"))throw failure("pa_invalid")
  if(t.origin!==undefined&&typeof t.origin!=="string"||t.released_restart!==undefined&&typeof t.released_restart!=="boolean")throw failure("pa_invalid")
  if(t.attempts!==undefined)validateAttempts(t.attempts,scope)
  if(t.state==="queued"&&t.steer_fallback!==undefined&&typeof t.steer_fallback!=="string")throw failure("pa_invalid")
}
function permissionSummary(p:W.ProjectAssistantReadPermissionSummary,scope:ReadScope,fence:W.ProjectAssistantFence) {
  if(!text(p.id)||!oneOf(p.state,permissionStates)||!date(p.expires_at)||!cursor(p.detail_cursor)||!p.detail_cursor||(p.title!==undefined&&typeof p.title!=="string")||(p.tool_kind!==undefined&&typeof p.tool_kind!=="string"))throw failure("pa_invalid")
  if(p.tool_kind==="decision"?p.turn_id!=="":!text(p.turn_id))throw failure("pa_invalid")
  if(p.decision_kind!==undefined&&(p.tool_kind!=="decision"||!["question","memory_save"].includes(p.decision_kind)))throw failure("pa_invalid")
  if(p.memory_exchange_id!==undefined&&(p.decision_kind!=="memory_save"||!text(p.memory_exchange_id)))throw failure("pa_invalid")
  if(p.selected_option_id!==undefined&&(p.decision_kind!=="memory_save"||p.state!=="answered"||!text(p.selected_option_id)))throw failure("pa_invalid")
  own(p.fence,scope,fence)
}
function offer(p:W.ProjectAssistantPermissionOffer,scope:ReadScope,current?:W.ProjectAssistantFence) {
  if(!p||!text(p.id)||!oneOf(p.state,permissionStates)||!date(p.expires_at)||!Array.isArray(p.options)||typeof p.native_request_id!=="string"||typeof p.selected_option_id!=="string")throw failure("pa_invalid")
  if(p.kind==="decision"?p.turn_id!==""||!object(p.decision)||!text(p.decision.question):!text(p.turn_id))throw failure("pa_invalid")
  if(p.decision?.memory_exchange_id!==undefined&&(!text(p.decision.memory)||!text(p.decision.memory_exchange_id)))throw failure("pa_invalid")
  own(p.fence,scope,current)
  // Display text from the adapter; an older daemon omits it.
  const a=p.action
  if(a!==undefined&&(!a||typeof a.title!=="string"||typeof a.tool_kind!=="string"||typeof a.detail!=="string"||typeof a.shortened!=="boolean"||!Array.isArray(a.diffs)||a.diffs.some(d=>!d||typeof d.path!=="string"||typeof d.old_text!=="string"||typeof d.new_text!=="string")))throw failure("pa_invalid")
  const ids=new Set<string>()
  for(const option of p.options){if(!option||!text(option.id)||ids.has(option.id)||typeof option.label!=="string"||typeof option.allow!=="boolean"||typeof option.read_only!=="boolean"||!["allow_once","allow_always","reject_once","reject_always"].includes(option.kind))throw failure("pa_invalid");ids.add(option.id)}
}
function trace(t:W.ProjectAssistantTrace) {if(t?.id!==undefined&&typeof t.id!=="string")throw failure("pa_invalid");if(!t||!text(t.turn_id)||!text(t.category)||typeof t.text!=="string"||typeof t.complete!=="boolean")throw failure("pa_invalid")}

/** The existing wire limits are shared with the source: 100/256 rows and 4096 cursor bytes. */
export function teamPermission(p:W.ProjectAssistantPermissionOffer) {
  return p.id.startsWith("clawdline-source-")&&(p.action?.tool_kind==="team_create"||p.action?.tool_kind==="team_update")
}

export function readViewRequest(q:W.ProjectAssistantReadViewRequest):W.ProjectAssistantReadViewRequest {
  if(!q||!key(q.key)||!text(q.conversation_id)||!oneOf(q.mode,ProjectAssistantReadViewModeValues)||typeof q.source_epoch!=="string"||!cursor(q.cursor)||!integer(q.after_seq)||!integer(q.page_size,1)||q.page_size>(q.mode==="events"?256:100))throw failure("pa_invalid")
  if(q.team_cards!==undefined&&q.team_cards!==true||q.defer_trace_count!==undefined&&q.defer_trace_count!==true)throw failure("pa_invalid")
  if(q.exchange_excerpts!==undefined&&(q.exchange_excerpts!==true||q.mode!=="snapshot"&&q.mode!=="history"))throw failure("pa_invalid")
  if(q.mode==="snapshot") {if(q.source_epoch||q.cursor||q.after_seq)throw failure("pa_invalid")}
  else if(!q.source_epoch||q.mode!=="events"&&q.after_seq||q.mode==="events"&&q.cursor||(q.mode==="history"||q.mode==="detail")&&!q.cursor||q.mode==="detail"&&q.page_size!==1)throw failure("pa_invalid")
  return q
}

/** Refuse malformed or foreign successful envelopes before they can release a recovery latch. */
function validateTimelineMessage(row:W.ProjectAssistantReadViewItem,scope:ReadScope) {
  const m=row.message
  if(row.joined_turn_id!==undefined&&(!text(row.joined_turn_id)||m?.kind!=="user"||row.joined_turn_id===m.turn_id))throw failure("pa_invalid")
  if(m){message(m,scope);if(row.anchor_message_seq!==m.seq)throw failure("pa_invalid")}
}

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
  const context=h.context_usage
  if(context!==undefined&&(!object(context)||!text(context.turn_id)||typeof context.live!=="boolean"||
    (context.live?context.turn_id!==c.active_turn_id:!!c.active_turn_id)||!integer(context.used_tokens)||!integer(context.window_tokens,1)))throw failure("pa_invalid")
  const model=h.active_model
  if(model!==undefined&&(!object(model)||!c.active_turn_id||model.turn_id!==c.active_turn_id||!text(model.name)))throw failure("pa_invalid")
  if(h.queued_turn_count!==undefined&&!integer(h.queued_turn_count))throw failure("pa_invalid")
  if(h.background_tasks!==undefined){
    if(!Array.isArray(h.background_tasks)||h.background_tasks.length>100)throw failure("pa_invalid")
    const ids=new Set<string>()
    for(const task of h.background_tasks){
      if(!task||!text(task.id)||ids.has(task.id)||!text(task.title)||!text(task.origin_message_id)||
        !["waiting","active","waiting_permission","succeeded","failed","cancelled","unknown"].includes(task.state)||
        task.activity!==undefined&&typeof task.activity!=="string"||task.started_at!==undefined&&!date(task.started_at)||task.ended_at!==undefined&&!date(task.ended_at))throw failure("pa_invalid")
      if(task.worker!==undefined&&(!key(task.worker)||task.worker.machine_id!==h.key.machine_id||task.worker.bot_id!=="thread-"+task.id||task.worker.bot_id===h.key.bot_id))throw failure("pa_scope_denied")
      ids.add(task.id)
    }
  }
  if(q.mode==="snapshot") {
    const s=answer.snapshot,b=s?.bot
    if(!s||!b||!Array.isArray(s.pending_turns)||!Array.isArray(s.queued_messages)||b.version!==h.bot_version)throw failure("pa_invalid")
    metadata(b,scope)
    const ids=new Set<string>(),queued=new Set<string>(),pending=new Map<string,W.ProjectAssistantReadTurnSummary>(),included=new Set<string>()
    for(const t of s.pending_turns){turn(t,scope);if(ids.has(t.id)||pending.has(t.message_id)||!["accepted","queued","running","waiting_permission"].includes(t.state))throw failure("pa_invalid");ids.add(t.id);pending.set(t.message_id,t);if(t.state==="accepted"||t.state==="queued")queued.add(t.message_id)}
    for(const m of s.queued_messages){message(m,scope);const t=pending.get(m.id);if((m.kind!=="user"&&m.kind!=="notice")||!t||m.turn_id!==t.id||included.has(m.id))throw failure("pa_invalid");included.add(m.id);queued.delete(m.id)}
    if(queued.size)throw failure("pa_invalid")
    if(s.last_turn){turn(s.last_turn,scope);if(!["succeeded","failed","cancelled","unknown"].includes(s.last_turn.state)||s.last_turn.fence.generation>h.current_fence.generation)throw failure("pa_invalid")}
  }else if(answer.snapshot!==undefined)throw failure("pa_invalid")
  const allowed=q.mode==="snapshot"||q.mode==="history"?(q.team_cards?["message","work","permission"]:["message","work"]):q.mode==="events"?["event"]:q.mode==="permissions"?["permission_summary"]:q.mode==="traces"?["trace_summary"]:q.mode==="progress"?["trace"]:["permission","trace"]
  const ids=new Set<string>(),identities=new Set<string>();let seq=q.after_seq
  for(const row of answer.items) {
    if(!row||!text(row.id)||!allowed.includes(row.kind)||!integer(row.anchor_message_seq)||!integer(row.observed_event_seq)||row.observed_event_seq>h.event_frontier)throw failure("pa_invalid")
    const rowIdentity=JSON.stringify([row.kind,row.id])
    if(ids.has(rowIdentity))throw failure("pa_invalid")
    ids.add(rowIdentity)
    const kinds=["message","work","event","permission_summary","trace_summary","permission","trace"] as const
    if(kinds.filter(kind=>row[kind]!==undefined).length!==1||!row[row.kind])throw failure("pa_invalid")
    const payloadID=row.message?.id??row.work?.id??row.permission_summary?.id??row.trace_summary?.id??row.permission?.id??(row.trace?JSON.stringify([row.trace.turn_id,row.trace.id||row.trace.category]):row.event?.id)
    const identity=JSON.stringify([row.kind,payloadID]);if(identities.has(identity))throw failure("pa_invalid");identities.add(identity)
    validateTimelineMessage(row,scope)
    if(row.work)work(row.work,scope)
    if(row.permission_summary)permissionSummary(row.permission_summary,scope,h.current_fence)
    if(row.trace_summary){const t=row.trace_summary;if(!text(t.id)||!text(t.turn_id)||!text(t.category)||typeof t.complete!=="boolean"||!cursor(t.detail_cursor)||!t.detail_cursor)throw failure("pa_invalid")}
    if(row.permission){
      if(q.mode==="detail")offer(row.permission,scope,h.current_fence)
      else{
        offer(row.permission,scope)
        if(!teamPermission(row.permission)||row.anchor_message_seq<1||row.anchor_message_seq>h.conversation.last_message_seq)throw failure("pa_invalid")
      }
    }
    if(row.trace)trace(row.trace)
    if(q.mode==="progress"){
      if(!row.trace||row.trace.category!=="agent_message_chunk"||!date(row.occurred_at)||
        row.id!==JSON.stringify([row.trace.turn_id,row.trace.id||row.trace.category]))throw failure("pa_invalid")
    }else if(row.occurred_at!==undefined)throw failure("pa_invalid")
    if(row.event){
      const e=row.event
      if(!text(e.id)||!integer(e.seq,1)||e.seq<=seq||e.seq!==row.observed_event_seq||e.version!=="project-assistants.v1"||!date(e.at)||!oneOf(e.kind,ProjectAssistantEventKindValues))throw failure("pa_invalid")
      const payloads=["bot","message","work","turn","permission","trace","source","memory","read"] as const
      const expected={bot_created:"bot",bot_updated:"bot",message_created:"message",queued_message_edited:"message",artifact_renamed:"message",reactions_changed:"message",work_updated:"work",turn_updated:"turn",permission_offered:"permission",permission_answered:"permission",trace_updated:"trace",source_updated:"source",memory_changed:"memory",read_advanced:"read",feature_disabled:""}[e.kind]
      if(payloads.filter(k=>e[k]!==undefined).length!==(expected?1:0)||expected&&!(e as unknown as Record<string,unknown>)[expected])throw failure("pa_invalid")
      seq=e.seq;own(e.fence,scope)
      if(e.message)message(e.message,scope)
      eventWork(e.work,scope,row.anchor_message_seq,h.conversation.last_message_seq)
      if(e.turn){const t=e.turn;if(!object(t)||!text(t.id)||!text(t.message_id)||!oneOf(t.state,states))throw failure("pa_invalid");own(t.fence,scope);if(t.failure!==undefined)sourceFailure(t.failure);if(t.attempts!==undefined)validateAttempts(t.attempts,scope)}
      if(e.permission){
        offer(e.permission,scope)
        if(row.anchor_message_seq&&(!q.team_cards||!teamPermission(e.permission)||row.anchor_message_seq>h.conversation.last_message_seq))throw failure("pa_invalid")
      }
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
    if(row.kind!=="message"&&row.kind!=="work"&&row.kind!=="permission")continue
    const id=JSON.stringify([row.kind,row.work?.id??row.message?.id??row.id]),old=rows.get(id)
    if(!old||row.observed_event_seq>=old.observed_event_seq)rows.set(id,row.work&&old?.anchor_message_seq&&!row.anchor_message_seq?{...row,id:old.id,anchor_message_seq:old.anchor_message_seq}:row)
    else if(row.work&&old.anchor_message_seq===0&&row.anchor_message_seq>0)rows.set(id,{...old,id:row.id,anchor_message_seq:row.anchor_message_seq})
  }
  const rank={message:0,work:1,permission:2,event:3,permission_summary:4,trace_summary:5,trace:6}
  const turnStarts=new Map<string,number>()
  for(const {message} of rows.values()){
    if(message?.kind==="user"||message?.kind==="notice"){
      const old=turnStarts.get(message.turn_id)
      if(old===undefined||message.seq<old)turnStarts.set(message.turn_id,message.seq)
    }
  }
  const positioned=[...rows.values()].map(row=>{
    const anchor=row.work&&!row.anchor_message_seq?turnStarts.get(row.work.turn_id):undefined
    return anchor?{...row,anchor_message_seq:anchor}:row
  })
  const position=(row:W.ProjectAssistantReadViewItem)=>row.work&&!row.anchor_message_seq?Number.MAX_SAFE_INTEGER:row.anchor_message_seq
  return positioned.sort((a,b)=>position(a)-position(b)||rank[a.kind]-rank[b.kind]||(a.id<b.id?-1:a.id>b.id?1:0))
}

/** Older sources send anchor zero; mergeTimeline can recover it from the loaded turn or history. */
export function eventTimeline(rows:readonly W.ProjectAssistantReadViewItem[],events:readonly W.ProjectAssistantReadViewItem[]):W.ProjectAssistantReadViewItem[] {
  let next=[...rows]
  for(const row of events){
    const e=row.event;if(!e)continue
    if(e.turn?.state==="cancelled"&&!e.turn.failure&&!e.turn.started_at){
      next=next.filter(r=>r.message?.id!==e.turn!.message_id)
    }
    if(e.turn?.state==="succeeded"&&e.turn.joined_turn_id){
      const turn=e.turn
      next=next.map(r=>r.message?.kind==="user"&&r.message.id===turn.message_id&&r.message.turn_id===turn.id&&e.seq>=r.observed_event_seq
        ?{...r,observed_event_seq:e.seq,joined_turn_id:turn.joined_turn_id}:r)
    }
    if(e.kind==="reactions_changed"&&e.message){
      // A metadata event cannot introduce unloaded content or overwrite a title.
      next=next.map(r=>r.message?.id===e.message!.id&&e.seq>=r.observed_event_seq?{...r,observed_event_seq:e.seq,message:{...r.message,reactions:e.message!.reactions}}:r)
      continue
    }
    if(e.kind==="artifact_renamed"&&e.message?.output){
      const output=e.message.output
      next=next.map(r=>r.message?.output?.artifact_id===output.artifact_id&&e.seq>=r.observed_event_seq?{...r,observed_event_seq:e.seq,message:{...r.message,output:{...r.message.output,name:output.name,title_version:output.title_version}}}:r)
      continue
    }
    if(e.message)next=mergeTimeline([next,[{id:e.message.id,kind:"message",anchor_message_seq:e.message.seq,observed_event_seq:e.seq,message:e.message}]])
    if(e.permission&&row.anchor_message_seq>0&&teamPermission(e.permission))next=mergeTimeline([next,[{id:e.permission.id,kind:"permission",anchor_message_seq:row.anchor_message_seq,observed_event_seq:e.seq,permission:e.permission}]])
    if(e.work){const previous=next.find(r=>r.work?.id===e.work!.id);next=mergeTimeline([next,[{id:previous?.id??e.work.id,kind:"work",anchor_message_seq:row.anchor_message_seq||previous?.anchor_message_seq||0,observed_event_seq:e.seq,work:e.work}]])}
  }
  return next
}

export function validateAttempts(attempts:W.ProjectAssistantAttemptFact[],scope:ReadScope) {
 if(!Array.isArray(attempts))throw failure("pa_invalid")
 for(const f of attempts){
  if(!object(f)||!integer(f.ordinal,1)||!oneOf(f.agent,ProjectAssistantAgentValues)||typeof f.stage!=="string"||typeof f.send_started!=="boolean"||typeof f.effect!=="string"||typeof f.cleanup_confirmed!=="boolean")throw failure("pa_invalid")
  own(f.fence,scope)
  for(const k of ["opened","send_returned","native_activity","tool_activity","answer_observed"] as const)if(f[k]!==undefined&&typeof f[k]!=="boolean")throw failure("pa_invalid")
  for(const k of ["reason","resume_binding_id","carry_kind"] as const)if(f[k]!==undefined&&typeof f[k]!=="string")throw failure("pa_invalid")
  if(f.carry_after_seq!==undefined&&!integer(f.carry_after_seq))throw failure("pa_invalid")
 }
}
export function turnSummary(t:W.ProjectAssistantTurn):W.ProjectAssistantReadTurnSummary {return{steer_fallback:steerFallback(t)||undefined,id:t.id,fence:t.fence,message_id:t.message_id,state:t.state,failure_code:t.failure?.code??"",failure_field:t.failure?.field,started_at:t.started_at??"",queue_edit_id:t.queue_edit_id,attempts:t.attempts,...(t.origin?{origin:t.origin}:{}),...(t.released_restart?{released_restart:true}:{})}}

/** A Bot steers unless the person explicitly chose to queue follow-ups. */
export function replyInputPreference(bot:Pick<W.ProjectAssistantBot,"reply_input">):"queue"|"steer" {
  return bot.reply_input==="queue"?"queue":"steer"
}
/** Why the source queued this turn instead of steering it; empty when it did not say. */
export function steerFallback(turn:Pick<W.ProjectAssistantReadTurnSummary,"state"|"steer_fallback">|undefined):string {
  return turn?.state==="queued"&&typeof turn.steer_fallback==="string"?turn.steer_fallback:""
}
export function queuedSendNow(source:{canSendNow?:()=>boolean},turn:W.ProjectAssistantReadTurnSummary|undefined,active:W.ProjectAssistantReadTurnSummary|undefined):boolean {
  return turn?.state==="queued"&&active?.state==="running"&&source.canSendNow?.()===true
}
type FallbackWords={steerFallbackAgent:string;steerFallbackImages:string;steerFallbackPermission:string;steerFallbackTurnEnded:string;steerFallbackScope:string;steerFallbackQueued:string}
/** The short reason shown under a message the source put back in the queue. */
export function steerFallbackReason(code:string,w:FallbackWords):string {
  switch(code){
    case "":return ""
    case "agent_unsupported":return w.steerFallbackAgent
    case "images_unsupported":return w.steerFallbackImages
    case "permission_pending":return w.steerFallbackPermission
    case "turn_ended":return w.steerFallbackTurnEnded
    case "scope":return w.steerFallbackScope
    case "steer_busy":return catalogWord("projectAssistants", "steerFallbackBusy")
    case "not_sent":return catalogWord("projectAssistants", "steerFallbackNotSent")
    default:return w.steerFallbackQueued
  }
}
/** A save carries reply_input only when the switch differs from the Bot's effective choice. */
export function replyInputPatch(bot:Pick<W.ProjectAssistantBot,"reply_input">,chosen:"queue"|"steer"):{reply_input?:"queue"|"steer"} {
  return chosen===replyInputPreference(bot)?{}:{reply_input:chosen}
}
