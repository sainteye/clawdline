import type * as W from "@clawdline/contract"
// @ts-expect-error -- Node executes source extensions.
import {validRoutineTrigger} from "./routine-events.ts"

export type RoutineIntent={action:"save"|"enable"|"delete"|"test";request:W.ProjectAssistantRoutineSaveRequest|W.ProjectAssistantRoutineEnabledRequest|W.ProjectAssistantRoutineActionRequest}|{action:"hook";request:W.ProjectAssistantRoutineHookRequest}
type Scope=Pick<W.ProjectAssistantBot,"key"|"project"|"conversation_id">
type ObjectValue=Record<string,unknown>
const object=(value:unknown):value is ObjectValue=>!!value&&typeof value==="object"&&!Array.isArray(value)
const fields=(value:ObjectValue,names:string[])=>Object.keys(value).length===names.length&&names.every(name=>Object.hasOwn(value,name))
const integer=(value:unknown)=>Number.isSafeInteger(value)&&Number(value)>=0

/** Stored bytes identify an uncertain action; they never grant another scope. */
export function readRoutineIntent(storage:Pick<Storage,"getItem">,key:string,scope:Scope):{state:"none"}|{state:"blocked"}|{state:"held";intent:RoutineIntent} {
  try {
    const raw=storage.getItem(key)
    if(raw===null)return {state:"none"}
    const value:unknown=JSON.parse(raw)
    if(!object(value)||!fields(value,["action","request"])||!["save","enable","delete","test","hook"].includes(String(value.action))||!object(value.request))return {state:"blocked"}
    const q=value.request,c=q.command,f=q.fence
    const extra=value.action==="save"?["name","instruction","triggers","timeout_seconds","enabled"]:value.action==="enable"?["enabled"]:value.action==="hook"?["rotate"]:[]
    if(!fields(q,["command","fence","id","expected_revision",...extra])||!object(c)||!fields(c,["key","operation","idempotency_key","content_hash"])||!object(c.key)||!fields(c.key,["machine_id","bot_id"])||c.operation!=="edit"||typeof c.idempotency_key!=="string"||!c.idempotency_key||typeof c.content_hash!=="string")return {state:"blocked"}
    if(c.key.machine_id!==scope.key.machine_id||c.key.bot_id!==scope.key.bot_id||!object(f)||!fields(f,["key","project","conversation_id","binding_id","generation"])||!object(f.key)||!fields(f.key,["machine_id","bot_id"])||f.key.machine_id!==scope.key.machine_id||f.key.bot_id!==scope.key.bot_id||!object(f.project)||!fields(f.project,["machine_id","project_id","canonical_id","kind"])||f.conversation_id!==scope.conversation_id||typeof f.binding_id!=="string"||!integer(f.generation))return {state:"blocked"}
    for(const field of ["machine_id","project_id","canonical_id","kind"] as const)if(f.project[field]!==scope.project[field])return {state:"blocked"}
    if(typeof q.id!=="string"||!integer(q.expected_revision)||(!q.id&&(value.action!=="save"||q.expected_revision!==0))||(!!q.id&&q.expected_revision===0))return {state:"blocked"}
    if(value.action==="enable"&&typeof q.enabled!=="boolean")return {state:"blocked"}
    if(value.action==="hook"&&typeof q.rotate!=="boolean")return {state:"blocked"}
    if(value.action==="save") {
      if(typeof q.name!=="string"||typeof q.instruction!=="string"||typeof q.enabled!=="boolean"||!integer(q.timeout_seconds)||!Array.isArray(q.triggers)||!q.triggers.length)return {state:"blocked"}
      for(const t of q.triggers)if(!validRoutineTrigger(t))return {state:"blocked"}
    }
    return {state:"held",intent:value as unknown as RoutineIntent}
  } catch {return {state:"blocked"}}
}
