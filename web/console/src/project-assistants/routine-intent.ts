import type * as W from "@clawdline/contract"
// @ts-expect-error -- Node executes source extensions.
import {newRoutineTrigger, validRoutineTrigger} from "./routine-events.ts"

export const routineRepeats = ["hourly", "daily", "weekdays", "weekly", "custom"] as const
export type RoutineRepeat = typeof routineRepeats[number]
export type RoutineCalendarDraft = {
  repeat: RoutineRepeat
  time: string
  weekday: number
  customExpression?: string
}
export type RoutineTriggerDraft = {
  trigger: W.ProjectAssistantRoutineTrigger
  calendar: RoutineCalendarDraft | null
}

const weekdays = ["sun", "mon", "tue", "wed", "thu", "fri", "sat"]
const clockTime = /^(?:[01]\d|2[0-3]):[0-5]\d$/
const clockPart = (value: number) => String(value).padStart(2, "0")

/** Recognize only the shapes the form can express. The original wire values stay attached. */
export function readRoutineCalendar(expression: string): RoutineCalendarDraft {
  const custom: RoutineCalendarDraft = {repeat: "custom", time: "09:00", weekday: 1, customExpression: expression}
  const fields = expression.trim().split(/\s+/)
  if (fields.length !== 5) return custom
  const [minute, hour, dayOfMonth, month, day] = fields
  if (dayOfMonth !== "*" || month !== "*" || !/^\d{1,2}$/.test(minute) || Number(minute) > 59) return custom
  const mm = clockPart(Number(minute))
  if (hour === "*" && day === "*") return {repeat: "hourly", time: `09:${mm}`, weekday: 1}
  if (!/^\d{1,2}$/.test(hour) || Number(hour) > 23) return custom
  const time = `${clockPart(Number(hour))}:${mm}`
  if (day === "*") return {repeat: "daily", time, weekday: 1}
  if (day === "1-5" || day.toLowerCase() === "mon-fri") return {repeat: "weekdays", time, weekday: 1}
  const weekday = /^[0-6]$/.test(day) ? Number(day) : weekdays.indexOf(day.toLowerCase())
  return weekday < 0 ? custom : {repeat: "weekly", time, weekday}
}

export function compileRoutineCalendar(draft: RoutineCalendarDraft): string {
  if (draft.repeat === "custom") return draft.customExpression ?? ""
  if (!clockTime.test(draft.time)) return ""
  const [hour, minute] = draft.time.split(":").map(Number)
  if (draft.repeat === "hourly") return `${minute} * * * *`
  const day = draft.repeat === "weekdays" ? "1-5" : draft.repeat === "weekly" ? draft.weekday : "*"
  return `${minute} ${hour} * * ${day}`
}

export function editableRoutineTrigger(trigger: W.ProjectAssistantRoutineTrigger): boolean {
  if (!["cron", "once", "interval", "event", "webhook"].includes(trigger.type)) return false
  const known = newRoutineTrigger(trigger.type)
  return Object.keys(trigger).every(key => Object.hasOwn(known, key))
    && typeof trigger.expression === "string" && typeof trigger.timeZone === "string"
    && Number.isSafeInteger(trigger.seconds) && Number.isSafeInteger(trigger.at)
    && (trigger.filters === undefined || (Array.isArray(trigger.filters)
      && trigger.filters.every(row => row && typeof row.key === "string" && typeof row.value === "string"
        && Object.keys(row).length === 2)))
}

export function routineTriggerDraft(trigger: W.ProjectAssistantRoutineTrigger): RoutineTriggerDraft {
  const calendar = trigger.type === "cron" && editableRoutineTrigger(trigger)
    ? readRoutineCalendar(trigger.expression)
    : null
  return {trigger, calendar}
}

/** The list omits empty event filters. The save contract requires an array. */
export function routineTriggerForSave({trigger}: RoutineTriggerDraft): W.ProjectAssistantRoutineTrigger {
  return trigger.type === "event" && trigger.filters === undefined ? {...trigger, filters: []} : trigger
}

export function newRoutineTriggerDraft(
  type = "cron", timeZone = Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC",
): RoutineTriggerDraft {
  const trigger = newRoutineTrigger(type)
  if (type === "cron") {
    const calendar: RoutineCalendarDraft = {repeat: "daily", time: "09:00", weekday: 1}
    return {trigger: {...trigger, expression: compileRoutineCalendar(calendar), timeZone}, calendar}
  }
  return {trigger, calendar: null}
}

export function updateRoutineCalendar(draft: RoutineTriggerDraft, change: Partial<RoutineCalendarDraft>): RoutineTriggerDraft {
  if (!draft.calendar) return draft
  const calendar = {...draft.calendar, ...change}
  if (calendar.repeat === "custom" && calendar.customExpression === undefined) {
    calendar.customExpression = draft.trigger.expression
  }
  return {calendar, trigger: {...draft.trigger, expression: compileRoutineCalendar(calendar)}}
}

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
