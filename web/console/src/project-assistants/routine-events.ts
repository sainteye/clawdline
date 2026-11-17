import type * as W from "@clawdline/contract"

export const routineEventSources = ["slack", "github", "origin", "microsoftTeams", "linear", "sentry", "pagerduty", "email"] as const
// Existing domain/capacity bounds, measured in UTF-8 bytes rather than input characters.
export const routineEventLimits = {name:200, filters:20, key:200, value:4000} as const
const text=(v:unknown,max:number,empty=false):v is string=>typeof v==="string"&&(empty||v.length>0)&&new TextEncoder().encode(v).length<=max&&!/[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/u.test(v)
export const isHookTrigger=(t:Pick<W.ProjectAssistantRoutineTrigger,"type">)=>t.type==="event"||t.type==="webhook"

/** A type change starts from a closed shape; incompatible fields cannot survive. */
export function newRoutineTrigger(type="interval"):W.ProjectAssistantRoutineTrigger {
  const base={type,seconds:type==="interval"?3600:0,at:0,expression:"",timeZone:""}
  return type==="event"?{...base,source:"github",event:"*",filters:[]}:base
}

export function validRoutineTrigger(value:unknown,events=true):value is W.ProjectAssistantRoutineTrigger {
  if(!value||typeof value!=="object"||Array.isArray(value))return false
  const t=value as Record<string,unknown>,base=["type","seconds","at","expression","timeZone"]
  const allowed=t.type==="event"?[...base,"source","event","filters"]:base
  if(Object.keys(t).some(k=>!allowed.includes(k))||base.some(k=>!Object.hasOwn(t,k))||!Number.isSafeInteger(t.seconds)||!Number.isSafeInteger(t.at)||Number(t.seconds)<0||Number(t.at)<0||typeof t.expression!=="string"||typeof t.timeZone!=="string")return false
  if(t.type==="interval")return Number(t.seconds)>=1&&Number(t.seconds)<=31536000&&t.at===0&&t.expression===""&&t.timeZone===""
  if(t.type==="once")return Number(t.at)>0&&t.seconds===0&&t.expression===""&&t.timeZone===""&&Number(t.at)<=8640000000000000
  if(t.type==="cron")return t.seconds===0&&t.at===0&&!!t.expression&&!!t.timeZone
  if(!events||!isHookTrigger({type:String(t.type)})||t.seconds!==0||t.at!==0||t.expression!==""||t.timeZone!=="")return false
  if(t.type==="webhook")return true
  if(!routineEventSources.includes(t.source as typeof routineEventSources[number])||!text(t.event,routineEventLimits.name)||!Array.isArray(t.filters)||t.filters.length>routineEventLimits.filters)return false
  const seen=new Set<string>()
  return t.filters.every(row=>{
    if(!row||typeof row!=="object"||Array.isArray(row)||Object.keys(row).length!==2||!text(row.key,routineEventLimits.key)||!text(row.value,routineEventLimits.value,true)||seen.has(row.key))return false
    seen.add(row.key);return true
  })
}

/** Copy only known wire fields, never spread an untrusted trigger onto a command. */
export function cleanRoutineTrigger(t:W.ProjectAssistantRoutineTrigger):W.ProjectAssistantRoutineTrigger {
  const base={type:t.type,seconds:t.seconds,at:t.at,expression:t.expression,timeZone:t.timeZone}
  return t.type==="event"?{...base,source:t.source,event:t.event,filters:(t.filters??[]).map(row=>({key:row.key,value:row.value}))}:base
}
