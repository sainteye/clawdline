import type * as W from "@clawdline/contract"
import type {SourceClient} from "./client"
// @ts-expect-error -- Node's strip-types runner loads source extensions.
import {failure} from "./client.ts"
// @ts-expect-error -- Node's strip-types runner loads source extensions.
import {sameBot,sameConversation} from "./view-state.ts"

// @ts-expect-error -- Node's strip-types runner loads source extensions.
import {readBotMetadata,readSource,readView} from "./read-views.ts"

export type ExchangeEntry = {exchange:W.ProjectAssistantExchange;seq:number;at:string}
export type ExchangeScope = Pick<W.ProjectAssistantFence,"key"|"project"|"conversation_id">
const text=(v:unknown):v is string=>typeof v==="string"&&v.length>0
const sameProject=(a:W.ProjectAssistantProjectScope,b:W.ProjectAssistantProjectScope)=>!!a&&!!b&&a.machine_id===b.machine_id&&a.project_id===b.project_id&&a.canonical_id===b.canonical_id&&a.kind===b.kind
const projectScope=(v:W.ProjectAssistantProjectScope)=>!!v&&text(v.machine_id)&&text(v.project_id)&&text(v.canonical_id)&&["repository","place","personal"].includes(v.kind)
const endpoint=(v:W.ProjectAssistantExchangeEndpoint)=>!!v&&!!v.key&&text(v.key.machine_id)&&text(v.key.bot_id)&&text(v.conversation_id)

/** Validate both participants before any request, reply or diagnostic can be published. */
export function scopedExchange(x:W.ProjectAssistantExchange,scope:ExchangeScope):W.ProjectAssistantExchange {
  if(!x||!text(x.id)||!endpoint(x.source)||!endpoint(x.target)||!text(x.source_turn_id)||!text(x.request_turn_id)||typeof x.request!=="string"||typeof x.outcome!=="string"||!Number.isSafeInteger(x.revision)||x.revision<1||!Number.isSafeInteger(x.hops)||x.hops<1||x.hops>8||[x.reply,x.detail,x.delivery,x.reply_turn_id].some(v=>v!==undefined&&typeof v!=="string"))throw failure("pa_invalid")
  const destination = x.target_project ?? x.project
  if(!projectScope(x.project)||!projectScope(destination)||x.project.machine_id!==scope.key.machine_id||destination.machine_id!==scope.key.machine_id||x.source.key.machine_id!==scope.key.machine_id||x.target.key.machine_id!==scope.key.machine_id||sameBot(x.source.key,x.target.key)||
    !(sameConversation({...x.source,project:x.project},scope)||sameConversation({...x.target,project:destination},scope)))throw failure("pa_scope_denied")
  return x
}

export function exchangeParticipant(exchange:W.ProjectAssistantExchange,side:"source"|"target"):ExchangeScope {
  return {...exchange[side],project:side==="source"?exchange.project:exchange.target_project??exchange.project}
}

export function exchangePeer(exchange:W.ProjectAssistantExchange,key:W.ProjectAssistantBotKey):ExchangeScope {
  return exchangeParticipant(exchange,sameBot(exchange.source.key,key)?"target":"source")
}

// A profile belongs to the Bot's Project and survives a fresh conversation.
export function exchangeBot(roster:readonly W.ProjectAssistantBot[]|undefined,party:Pick<ExchangeScope,"key"|"project">) {
  return roster?.find(bot=>sameBot(bot.key,party.key)&&sameProject(bot.project,party.project))
}

export async function loadExchangeBot(source:SourceClient,party:Pick<ExchangeScope,"key"|"project">,signal:AbortSignal) {
  const revision=source.authorizationRevision
  checkRead(source,party,revision,signal)
  const reply=await source.get(party.key,signal)
  checkRead(source,party,revision,signal)
  if(reply?.contract_version!=="project-assistants.v1"||!reply.bot)throw failure("pa_invalid")
  requireExchangeSource(reply.source,party.key.machine_id)
  readBotMetadata(reply.bot,{...party,conversation_id:reply.bot.conversation_id})
  return reply.bot
}

function sameExchange(a:W.ProjectAssistantExchange,b:W.ProjectAssistantExchange) {
  return sameConversation(exchangeParticipant(a,"source"),exchangeParticipant(b,"source"))&&
    sameConversation(exchangeParticipant(a,"target"),exchangeParticipant(b,"target"))&&
    a.source_turn_id===b.source_turn_id&&a.request_turn_id===b.request_turn_id
}

export function mergeExchangeEntries(entries:readonly ExchangeEntry[]):ExchangeEntry[] {
  const merged=new Map<string,ExchangeEntry>()
  for(const entry of entries) {
    const old=merged.get(entry.exchange.id)
    if(!old){merged.set(entry.exchange.id,entry);continue}
    if(!sameExchange(old.exchange,entry.exchange))continue
    const first=entry.seq<old.seq?entry:old
    merged.set(entry.exchange.id,{...first,exchange:entry.exchange.revision>=old.exchange.revision?entry.exchange:old.exchange})
  }
  return [...merged.values()].sort((a,b)=>a.seq-b.seq)
}

export function exchangeEntries(messages:readonly W.ProjectAssistantMessage[],scope:ExchangeScope):ExchangeEntry[] {
  const entries:ExchangeEntry[]=[]
  for(const message of messages) {
    if(!message.exchange||!sameConversation(message.fence,scope))continue
    try{entries.push({exchange:scopedExchange(message.exchange,scope),seq:message.seq,at:message.at})}catch{/* Ignore a malformed notice. */}
  }
  return mergeExchangeEntries(entries)
}

function checkRead(source:SourceClient,scope:Pick<ExchangeScope,"key">,revision:string,signal:AbortSignal) {
  if(signal.aborted)throw new DOMException("Aborted","AbortError")
  if(source.current?.()===false||source.authorizationRevision!==revision)throw failure("pa_revoked")
  if(source.machineID!==scope.key.machine_id)throw failure("pa_scope_denied")
}

function requireExchangeSource(status:W.ProjectAssistantSourceStatus,machine:string) {
  const s=readSource(status,machine)
  if(s.state==="revoked"||s.state==="unpaired")throw failure("pa_revoked")
  if(!s.fresh||s.state!=="online")throw failure("pa_offline")
  if(!s.readable||s.access==="hidden")throw failure("pa_scope_denied")
}

export type ExchangePageCursor={cursor:string;sourceEpoch:string}

/** Read the captured chat's paginated view through its existing byte budget. */
export async function loadExchangePage(source:SourceClient,scope:ExchangeScope,reference:W.ProjectAssistantExchange,signal:AbortSignal,next?:ExchangePageCursor) {
  const revision=source.authorizationRevision
  checkRead(source,scope,revision,signal)
  scopedExchange(reference,scope)
  const query:W.ProjectAssistantReadViewRequest={
    key:scope.key,conversation_id:scope.conversation_id,mode:next?"history":"snapshot",
    cursor:next?.cursor??"",source_epoch:next?.sourceEpoch??"",after_seq:0,page_size:100
  }
  const answer=await source.view(query,signal)
  checkRead(source,scope,revision,signal)
  requireExchangeSource(answer?.source,scope.key.machine_id)
  const page=readView(answer,query,scope)
  const peer=exchangePeer(reference,scope.key)
  const entries=exchangeEntries(page.items.flatMap(row=>row.message?[row.message]:[]),scope)
    .filter(entry=>sameConversation(exchangePeer(entry.exchange,scope.key),peer)&&
      (entry.exchange.id!==reference.id||sameExchange(reference,entry.exchange)))
  return {entries,bot:page.snapshot?.bot,next:page.has_more?{cursor:page.next_cursor,sourceEpoch:page.source_epoch}:undefined}
}

/** Text content never determines completion or delivery. Unknown future states stay unknown. */
export function exchangeOutcome(value:string):"queued"|"sent"|"completed"|"failed"|"cancelled"|"unknown" {
  return value==="queued"||value==="sent"||value==="completed"||value==="failed"||value==="cancelled"?value:"unknown"
}
export function exchangeDelivery(value:string|undefined):"waiting"|"queued"|"delivered"|"dropped"|"unknown" {
  return value==="waiting"||value==="queued"||value==="delivered"||value==="dropped"?value:"unknown"
}
