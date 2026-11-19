import type * as W from "@clawdline/contract"
import type {SourceClient} from "./client"
// @ts-expect-error -- Node's strip-types runner loads source extensions.
import {failure} from "./client.ts"
// @ts-expect-error -- Node's strip-types runner loads source extensions.
import {sameBot,sameConversation} from "./view-state.ts"

export type ExchangeEntry = {exchange:W.ProjectAssistantExchange;seq:number;at:string}
export type ExchangeScope = Pick<W.ProjectAssistantFence,"key"|"project"|"conversation_id">
const text=(v:unknown):v is string=>typeof v==="string"&&v.length>0
const sameProject=(a:W.ProjectAssistantProjectScope,b:W.ProjectAssistantProjectScope)=>!!a&&!!b&&a.machine_id===b.machine_id&&a.project_id===b.project_id&&a.canonical_id===b.canonical_id&&a.kind===b.kind
const endpoint=(v:W.ProjectAssistantExchangeEndpoint)=>!!v&&!!v.key&&text(v.key.machine_id)&&text(v.key.bot_id)&&text(v.conversation_id)

/** Validate both participants before any request, reply or diagnostic can be published. */
export function scopedExchange(x:W.ProjectAssistantExchange,scope:ExchangeScope):W.ProjectAssistantExchange {
  if(!x||!text(x.id)||!endpoint(x.source)||!endpoint(x.target)||!text(x.source_turn_id)||!text(x.request_turn_id)||typeof x.request!=="string"||typeof x.outcome!=="string"||!Number.isSafeInteger(x.revision)||x.revision<1||!Number.isSafeInteger(x.hops)||x.hops<1||x.hops>8||[x.reply,x.detail,x.delivery,x.reply_turn_id].some(v=>v!==undefined&&typeof v!=="string"))throw failure("pa_invalid")
  if(!sameProject(x.project,scope.project)||x.project.machine_id!==scope.key.machine_id||x.source.key.machine_id!==scope.key.machine_id||x.target.key.machine_id!==scope.key.machine_id||sameBot(x.source.key,x.target.key)||![x.source,x.target].some(p=>sameConversation({...p,project:x.project},scope)))throw failure("pa_scope_denied")
  return x
}

export function exchangePeer(exchange:W.ProjectAssistantExchange,key:W.ProjectAssistantBotKey):W.ProjectAssistantExchangeEndpoint {
  return sameBot(exchange.source.key,key)?exchange.target:exchange.source
}

export function exchangeEntries(messages:readonly W.ProjectAssistantMessage[],scope:ExchangeScope):ExchangeEntry[] {
  const entries=new Map<string,ExchangeEntry>()
  for(const message of messages) {
    if(!message.exchange||!sameConversation(message.fence,scope))continue
    let exchange:W.ProjectAssistantExchange
    try{exchange=scopedExchange(message.exchange,scope)}catch{continue}
    const existing=entries.get(exchange.id)
    if(!existing)entries.set(exchange.id,{exchange,seq:message.seq,at:message.at})
    else if(exchange.revision>=existing.exchange.revision)entries.set(exchange.id,{...existing,exchange})
  }
  return [...entries.values()].sort((a,b)=>a.seq-b.seq)
}

/** Captured export is the existing bounded authenticated read route, never a Bot command. */
export async function loadExchange(source:SourceClient,scope:ExchangeScope,reference:W.ProjectAssistantExchange,signal:AbortSignal,includePeerHistory=false):Promise<{exchange:W.ProjectAssistantExchange;botName:string;history?:ExchangeEntry[]}> {
  const revision=source.authorizationRevision
  const check=()=>{
    if(signal.aborted)throw new DOMException("Aborted","AbortError")
    if(source.current?.()===false||source.authorizationRevision!==revision)throw failure("pa_revoked")
    if(source.machineID!==scope.key.machine_id)throw failure("pa_scope_denied")
  }
  check();scopedExchange(reference,scope)
  const value=await source.export(scope.key,signal);check()
  if(!value||value.contract_version!=="project-assistants.v1"||!value.bot?.key||!value.conversation?.key||!sameConversation({key:value.bot.key,project:value.bot.project,conversation_id:value.bot.conversation_id},scope)||!sameBot(value.conversation.key,scope.key)||value.conversation.id!==scope.conversation_id)throw failure("pa_scope_denied")
  if(value.exchanges!==undefined&&!Array.isArray(value.exchanges))throw failure("pa_invalid")
  const found=(value.exchanges??[]).filter(x=>x?.id===reference.id)
  if(found.length!==1)throw failure(found.length?"pa_invalid":"pa_not_found")
  const x=scopedExchange(found[0],scope)
  if(!sameBot(x.source.key,reference.source.key)||!sameBot(x.target.key,reference.target.key)||x.source.conversation_id!==reference.source.conversation_id||x.target.conversation_id!==reference.target.conversation_id||x.source_turn_id!==reference.source_turn_id||x.request_turn_id!==reference.request_turn_id)throw failure("pa_scope_denied")
  if(x.revision<reference.revision)throw failure("pa_stale_version")
  // Export has no source envelope; the paired status supplies freshness/access.
  const status=await source.status(signal);check()
  const states=status?.sources?.filter(s=>s.machine_id===scope.key.machine_id)??[]
  if(status?.contract_version!=="project-assistants.v1"||states.length!==1)throw failure("pa_scope_denied")
  const s=states[0]
  if(s.state==="revoked"||s.state==="unpaired")throw failure("pa_revoked")
  if(!s.fresh||s.state!=="online")throw failure("pa_offline")
  if(!s.readable||s.access==="hidden")throw failure("pa_scope_denied")
  check()
  const view={exchange:x,botName:typeof value.bot.name==="string"?value.bot.name:""}
  if(!includePeerHistory)return view
  const peer=exchangePeer(x,scope.key),authoritative=new Map<string,W.ProjectAssistantExchange>()
  for(const entry of value.exchanges??[]){
    // The export can contain exchanges with other peers in this chat.
    try{
      const item=scopedExchange(entry,scope)
      if(sameBot(exchangePeer(item,scope.key).key,peer.key)&&exchangePeer(item,scope.key).conversation_id===peer.conversation_id)
        authoritative.set(item.id,item)
    }catch{/* No foreign or malformed record is eligible for this sheet. */}
  }
  return {...view,history:exchangeEntries(value.messages??[],scope)
    .filter(entry=>authoritative.has(entry.exchange.id))
    .map(entry=>({...entry,exchange:authoritative.get(entry.exchange.id)!}))}
}

/** Text content never determines completion or delivery. Unknown future states stay unknown. */
export function exchangeOutcome(value:string):"queued"|"sent"|"completed"|"failed"|"cancelled"|"unknown" {
  return value==="queued"||value==="sent"||value==="completed"||value==="failed"||value==="cancelled"?value:"unknown"
}
export function exchangeDelivery(value:string|undefined):"waiting"|"queued"|"delivered"|"dropped"|"unknown" {
  return value==="waiting"||value==="queued"||value==="delivered"||value==="dropped"?value:"unknown"
}
