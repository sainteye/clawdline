import {test} from "node:test"
import assert from "node:assert/strict"
import {
  compileRoutineCalendar, editableRoutineTrigger, newRoutineTriggerDraft, readRoutineCalendar,
  readRoutineIntent, routineTriggerDraft, routineTriggerForSave, updateRoutineCalendar, type RoutineRepeat,
// @ts-expect-error -- Node executes source extensions.
} from "./routine-intent.ts"

const key={machine_id:"synthetic-source",bot_id:"synthetic-bot"}
const project={machine_id:key.machine_id,project_id:"project",canonical_id:"canonical",kind:"repository" as const}
const scope={key,project,conversation_id:"chat"}
const original={action:"test",request:{command:{key,operation:"edit",idempotency_key:"original-action",content_hash:""},fence:{key,project,conversation_id:"chat",binding_id:"old-binding",generation:2},id:"routine",expected_revision:1}}

test("a held routine action retains its exact identity and historical fence",()=>{
  const raw=JSON.stringify(original)
  const storage={getItem:()=>raw}
  assert.deepEqual(readRoutineIntent(storage,"journal",scope),{state:"held",intent:original})
  assert.equal(storage.getItem(),raw)
  assert.deepEqual(readRoutineIntent({getItem:()=>null},"journal",scope),{state:"none"})
})

test("event saves survive reload with only closed typed filters",()=>{
  const value={action:"save",request:{...original.request,name:"Synthetic event",instruction:"Inspect it",timeout_seconds:3600,enabled:true,triggers:[{type:"event",seconds:0,at:0,expression:"",timeZone:"",source:"github",event:"issues.opened",filters:[{key:"messageContains",value:"synthetic"}]}]}}
  assert.deepEqual(readRoutineIntent({getItem:()=>JSON.stringify(value)},"journal",scope),{state:"held",intent:value})
  const polluted=structuredClone(value)
  polluted.request.triggers[0].filters.push({key:"messageContains",value:"duplicate"})
  assert.deepEqual(readRoutineIntent({getItem:()=>JSON.stringify(polluted)},"journal",scope),{state:"blocked"})
})

test("unreadable, unsupported and foreign routine commands stay blocked without changing storage",()=>{
  const variants=["",'{"action":"test","request":',"null","[]",JSON.stringify({...original,action:"future-action"}),JSON.stringify({...original,extra:"future-fact"})]
  for(const change of [
    (v:typeof original)=>{v.request.command.key={...key,bot_id:"other"}},
    (v:typeof original)=>{v.request.fence.conversation_id="other"},
    (v:typeof original)=>{v.request.fence.project={...project,project_id:"other"}},
    (v:typeof original)=>{v.request.fence.project={...project,machine_id:"other"}},
  ]) {const value=structuredClone(original);change(value);variants.push(JSON.stringify(value))}
  for(const raw of variants){
    let bytes=raw,writes=0
    const storage={getItem:()=>bytes,setItem:(_key:string,value:string)=>{writes++;bytes=value},removeItem:()=>{writes++;bytes=""}}
    assert.deepEqual(readRoutineIntent(storage,"journal",scope),{state:"blocked"})
    assert.equal(bytes,raw);assert.equal(writes,0)
  }
  assert.deepEqual(readRoutineIntent({getItem:()=>{throw Error("storage unavailable")}},"journal",scope),{state:"blocked"})
})

test("calendar presets write only standard wall-clock cron and the selected zone", () => {
  const initial = newRoutineTriggerDraft("cron", "America/New_York")
  assert.deepEqual(initial.trigger, {type:"cron", seconds:0, at:0, expression:"0 9 * * *", timeZone:"America/New_York"})
  const cases: [RoutineRepeat, string][] = [
    ["hourly", "5 * * * *"], ["daily", "5 18 * * *"], ["weekdays", "5 18 * * 1-5"], ["weekly", "5 18 * * 0"],
  ]
  for (const [repeat, expression] of cases) {
    const draft = updateRoutineCalendar(initial, {repeat, time:"18:05", weekday:0})
    assert.equal(draft.trigger.expression, expression)
    assert.equal(draft.trigger.timeZone, "America/New_York")
    assert.equal(readRoutineCalendar(expression).repeat, repeat)
  }
  for (const time of ["", "24:00", "09:60", "9:01"]) {
    assert.equal(updateRoutineCalendar(initial, {time}).trigger.expression, "")
  }
})

test("only representable existing schedules open as presets and preserve the original wire values", () => {
  const base = newRoutineTriggerDraft("cron", "US/Eastern").trigger
  for (const [expression, repeat, weekday] of [
    ["  00 09 * * *  ", "daily", 1],
    ["15 09 * * MON-FRI", "weekdays", 1],
    ["35 21 * * Sun", "weekly", 0],
    ["05 * * * *", "hourly", 1],
  ] as const) {
    const original = {...base, expression}
    const draft = routineTriggerDraft(original)
    assert.equal(draft.calendar?.repeat, repeat)
    assert.equal(draft.calendar?.weekday, weekday)
    assert.deepEqual(draft.trigger, original)
  }
  for (const expression of ["@daily", " 7,42 18 * * 1,3 ", "0 12 1 * *", "65 9 * * *", "00 24 * * *", "0 9 * * 8"]) {
    const draft = routineTriggerDraft({...base, expression})
    assert.equal(draft.calendar?.repeat, "custom")
    assert.equal(compileRoutineCalendar(draft.calendar!), expression)
    assert.equal(draft.trigger.expression, expression)
  }
  const weekdays = routineTriggerDraft({...base, expression:" 00 09 * * MON-FRI "})
  const custom = updateRoutineCalendar(weekdays, {repeat:"custom"})
  assert.equal(custom.trigger.expression, weekdays.trigger.expression)
  const edited = updateRoutineCalendar(custom, {customExpression:" 5 18 * * 1,3 "})
  const weekly = updateRoutineCalendar(edited, {repeat:"weekly", weekday:6})
  assert.equal(weekly.trigger.expression, "0 9 * * 6")
  assert.equal(updateRoutineCalendar(weekly, {repeat:"custom"}).trigger.expression, " 5 18 * * 1,3 ")
})

test("known trigger lists round-trip as exact commands and future trigger data remains untouched", () => {
  const triggers = [
    {...newRoutineTriggerDraft("once").trigger, at:1_800_000_000_345},
    {...newRoutineTriggerDraft("interval").trigger, seconds:95},
    {...newRoutineTriggerDraft("cron", "Etc/UTC").trigger, expression:" 2,8 9 * * 0,6 "},
    {...newRoutineTriggerDraft("event").trigger, filters:[{key:"messageContains", value:"synthetic"}]},
    newRoutineTriggerDraft("webhook").trigger,
  ]
  const wire = triggers.map(trigger => routineTriggerForSave(routineTriggerDraft(trigger)))
  assert.deepEqual(wire, triggers)
  const {filters: _filters, ...unfilteredEvent} = triggers[3]
  const unfiltered = routineTriggerDraft(unfilteredEvent)
  assert.deepEqual(unfiltered.trigger, unfilteredEvent)
  assert.deepEqual(routineTriggerForSave(unfiltered), {...unfilteredEvent, filters:[]})
  const value = {action:"save", request:{...original.request, name:"Synthetic", instruction:"Inspect", timeout_seconds:120, enabled:true, triggers:wire}}
  assert.deepEqual(readRoutineIntent({getItem:()=>JSON.stringify(value)}, "journal", scope), {state:"held", intent:value})

  for (const unknown of [
    {...triggers[0], type:"future-mode", future_schedule:{minimum:15}},
    {...triggers[2], future_schedule:{minimum:15}},
    {...triggers[3], filters:[{key:"messageContains", value:"synthetic", operator:"prefix"}]},
  ]) {
    assert.equal(editableRoutineTrigger(unknown), false)
    assert.deepEqual(routineTriggerDraft(unknown), {trigger:unknown, calendar:null})
    const raw = JSON.stringify({...value, request:{...value.request, triggers:[unknown]}})
    assert.deepEqual(readRoutineIntent({getItem:()=>raw}, "journal", scope), {state:"blocked"})
  }
})
