import { useEffect, useId, useMemo, useRef, useState, type FormEvent } from "react"
import { BotMentionText, useBotMentions, type BotMentionCandidate } from "./BotMentionField"
import { botMentionSegments, botMentionToken } from "./bot-mentions"
import type * as W from "@clawdline/contract"
import type { SourceClient } from "./client"
import { command, errorCode, failure, provablyNotSent } from "./client"
import type { Words } from "./words"

import {
  editableRoutineTrigger, newRoutineTriggerDraft, readRoutineCalendar, readRoutineIntent,
  routineRepeats, routineTriggerDraft, routineTriggerForSave, updateRoutineCalendar,
  type RoutineCalendarDraft, type RoutineIntent as Intent, type RoutineTriggerDraft,
} from "./routine-intent"
import {isHookTrigger,routineEventLimits,routineEventSources,validRoutineTrigger} from "./routine-events"
import {performRoutineHook,routineScopeIdentity} from "./routine-hook-intent"
import {RoutineHook} from "./RoutineHook"
import {afterSurfaceExit} from "./surface-motion"
import {routineWord as tr} from "./routine-words"
import {currentCatalogTag} from "../catalog"
import {macKeyboard} from "../shortcuts"
import "./routine-schedule.css"
type Props={source:SourceClient;bot:W.ProjectAssistantBot;enabled:boolean;w:Words;storageKey:string;mentions?:readonly BotMentionCandidate[]}

export function Routines(props:Props) {
  const capture=useRef({source:props.source,epoch:0})
  if(capture.current.source!==props.source)capture.current={source:props.source,epoch:capture.current.epoch+1}
  return <RoutinePanel key={routineScopeIdentity(props.source,props.bot)+":"+capture.current.epoch} {...props}/>
}

const deviceZone=()=>Intl.DateTimeFormat().resolvedOptions().timeZone||"UTC"
const shortTime=(at:string|number)=>new Intl.DateTimeFormat(currentCatalogTag(),{dateStyle:"medium",timeStyle:"short"}).format(new Date(at))

/** Several fixed hours on the same days ("0 9,18 * * *") read as times, not as raw Cron. */
function cronTimes(expression:string):{repeat:"daily"|"weekdays"|"weekly";times:string;weekday:number}|null {
  const m=/^(\d{1,2}) (\d{1,2}(?:,\d{1,2})+) \* \* (\*|1-5|[0-6])$/u.exec(expression.trim())
  if(!m||Number(m[1])>59)return null
  const hours=m[2].split(",").map(Number)
  if(hours.some(h=>h>23))return null
  const times=hours.map(h=>`${String(h).padStart(2,"0")}:${m[1].padStart(2,"0")}`)
  const list=new Intl.ListFormat(currentCatalogTag(),{style:"long",type:"conjunction"}).format(times)
  return {repeat:m[3]==="*"?"daily":m[3]==="1-5"?"weekdays":"weekly",times:list,weekday:Number(m[3])||0}
}

/** One trigger as a short sentence for the routine's row. The zone shows only when it is not this device's. */
function triggerLine(t:W.ProjectAssistantRoutineTrigger,w:Words):string {
  if(!editableRoutineTrigger(t))return t.type
  if(t.type==="event")return `${tr("event")} · ${t.source} · ${t.event}`
  if(t.type==="webhook")return tr("webhook")
  if(t.type==="once")return `${w.routineOnce} · ${shortTime(t.at)}`
  if(t.type==="interval"){
    const [unit,amount]=t.seconds%3600===0?["hour",t.seconds/3600]:t.seconds%60===0?["minute",t.seconds/60]:["second",t.seconds]
    return tr("everyInterval").replace("{time}",new Intl.NumberFormat(currentCatalogTag(),{style:"unit",unit,unitDisplay:"long"}).format(amount))
  }
  const zone=t.timeZone&&t.timeZone!=="host"&&t.timeZone!==deviceZone()?t.timeZone:""
  const calendar=readRoutineCalendar(t.expression)
  const several=calendar.repeat==="custom"?cronTimes(t.expression):null
  if(calendar.repeat==="custom"&&!several)return zone?`${t.expression} · ${zone}`:t.expression
  const values={minute:calendar.time.slice(3),time:several?.times??calendar.time,day:routineWeekday(several?.weekday??calendar.weekday)}
  const text=tr(`${several?.repeat??calendar.repeat}Line` as "dailyLine").replace(/\{(minute|time|day)\}/gu,(_,key:keyof typeof values)=>values[key])
  return zone?`${text} · ${zone}`:text
}

function ClockIcon(){return <svg viewBox="0 0 20 20" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="1.6" aria-hidden="true"><circle cx="10" cy="10" r="7.5"/><path d="M10 5.5V10H6.5" strokeLinecap="round"/></svg>}
function TrashIcon(){return <svg viewBox="0 0 20 20" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="M3.5 5.5h13M8 5.5V4h4v1.5M5.5 5.5l.8 10.5h7.4l.8-10.5M8.5 8.5v5M11.5 8.5v5"/></svg>}
function PlayIcon(){return <svg viewBox="0 0 20 20" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="1.5" aria-hidden="true"><circle cx="10" cy="10" r="7.5"/><path d="M8.5 7v6l5-3z" fill="currentColor" strokeLinejoin="round"/></svg>}
function CheckIcon(){return <svg viewBox="0 0 20 20" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="M5 10.5l3.2 3L15 6.5"/></svg>}

const routineMinutes = Array.from({length: 60}, (_, minute) => String(minute).padStart(2, "0"))
const routineWeekdays = [1, 2, 3, 4, 5, 6, 0]
const routineZones = [...new Set(["UTC", ...(Intl.supportedValuesOf?.("timeZone") ?? [])])]

function routineWeekday(day: number) {
  return new Intl.DateTimeFormat(currentCatalogTag(), {weekday: "long", timeZone: "UTC"}).format(Date.UTC(2024, 0, 7 + day))
}

function RoutineCalendarSummary({trigger, calendar}: {trigger: W.ProjectAssistantRoutineTrigger; calendar: RoutineCalendarDraft}) {
  const {expression, timeZone} = trigger
  if (!expression.trim() || !timeZone.trim()) return null
  if (calendar.repeat === "custom") return <><code>{expression}</code> · {timeZone}</>
  const text = tr(`${calendar.repeat}Preview`)
  const values = {zone: timeZone, minute: calendar.time.slice(3), time: calendar.time, day: routineWeekday(calendar.weekday)}
  return text.replace(/\{(zone|minute|time|day)\}/gu, (_, key: keyof typeof values) => values[key])
}

function nextCalendarTime(calendar:RoutineCalendarDraft,zone:string,now=Date.now()) {
  if(calendar.repeat==="custom"||!/^([01]\d|2[0-3]):[0-5]\d$/.test(calendar.time))return null
  try {
    const local=new Intl.DateTimeFormat("en-GB",{timeZone:zone,weekday:"short",hour:"2-digit",minute:"2-digit",hourCycle:"h23"})
    const days=["Sun","Mon","Tue","Wed","Thu","Fri","Sat"]
    const start=Math.floor(now/60000)*60000+60000
    // A weekly time that falls in a skipped local hour can next occur two weeks away.
    const end=start+14*24*60*60000
    for(let instant=start;instant<end;instant+=60000){
      const [day,time]=local.format(instant).split(/[, ]+/)
      if(calendar.repeat==="hourly"?time?.slice(3)===calendar.time.slice(3):
        time===calendar.time&&(calendar.repeat==="daily"||calendar.repeat==="weekly"&&day===days[calendar.weekday]||calendar.repeat==="weekdays"&&day!=="Sat"&&day!=="Sun")){
        return new Intl.DateTimeFormat(currentCatalogTag(),{timeZone:zone,dateStyle:"medium",timeStyle:"short"}).format(instant)
      }
    }
  }catch{ /* An incomplete or invalid time zone has no preview. */ }
  return null
}

type SchedulePreviewScope={source:SourceClient;bot:W.ProjectAssistantBot}
type SchedulePreviewResult={inputs:string;next?:string;error?:string;invalid?:boolean}

function useCronPreview({source,bot}:SchedulePreviewScope, enabled:boolean, expression:string, zone:string) {
  const {machine_id,bot_id}=bot.key
  const {project_id,canonical_id,kind}=bot.project
  const conversation_id=bot.conversation_id
  const inputs=JSON.stringify([expression,zone])
  const [result,setResult]=useState<SchedulePreviewResult>()
  useEffect(()=>{
    if(!enabled||!expression.trim()||!zone.trim()||!source.canRoutinePreview?.()||!source.routinePreview)return
    const controller=new AbortController()
    const timer=window.setTimeout(()=>{
      void source.routinePreview!({
        key:{machine_id,bot_id},project:{machine_id,project_id,canonical_id,kind},conversation_id,
        expression,time_zone:zone,
      },controller.signal).then(answer=>{
        if(controller.signal.aborted)return
        const next=new Intl.DateTimeFormat(currentCatalogTag(),{...(zone==="host"?{}:{timeZone:zone}),dateStyle:"medium",timeStyle:"short"}).format(Date.parse(answer.next_due))
        setResult({inputs,next})
      }).catch(error=>{
        if(controller.signal.aborted)return
        const field=(error as Partial<W.ProjectAssistantErrorResponse>|null)?.error?.field
        const invalid=errorCode(error)==="pa_invalid"&&["routine_time_zone","routine_cron","routine_cron_future","routine_preview_input"].includes(field??"")
        const key=invalid?(field==="routine_time_zone"?"invalidTimeZone":field==="routine_cron_future"?"cronNoNext":"invalidCron"):"previewUnavailable"
        setResult({inputs,invalid,error:tr(key)})
      })
    },220)
    return ()=>{window.clearTimeout(timer);controller.abort()}
  },[source,enabled,machine_id,bot_id,project_id,canonical_id,kind,conversation_id,inputs,expression,zone])
  return enabled&&result?.inputs===inputs?result:undefined
}

function RoutineScheduleFields({trigger, calendar, onCalendar, onZone, w, previewScope}: {
  trigger: W.ProjectAssistantRoutineTrigger
  calendar: RoutineCalendarDraft
  previewScope: SchedulePreviewScope
  onCalendar: (change: Partial<RoutineCalendarDraft>) => void
  onZone: (timeZone: string) => void
  w: Words
}) {
  const {repeat, time, weekday} = calendar
  const customPreview=useCronPreview(previewScope,repeat==="custom"||trigger.timeZone==="host",trigger.expression,trigger.timeZone)
  const presetNext=useMemo(()=>nextCalendarTime(calendar,trigger.timeZone),[repeat,time,weekday,trigger.timeZone])
  const next=repeat==="custom"?customPreview?.next:presetNext
  const cronInput=useRef<HTMLInputElement>(null)
  useEffect(()=>{cronInput.current?.setCustomValidity(customPreview?.invalid?customPreview.error??"":"")},[customPreview])
  const zoneList = useId()
  const cronHelp = useId()
  const previewNotice = useId()
  const weekdayLabel = useId()
  const minuteLabel = useId()
  return <div className="pa-routine-calendar">
    <div className="pa-routine-repeat" role="group" aria-label={tr("repeat")}>
      <span className="pa-routine-field-label" aria-hidden="true">{tr("repeat")}</span>
      <div className="pa-routine-choices">
        {routineRepeats.map(choice => <button key={choice} type="button" aria-pressed={choice === repeat}
          onClick={() => { if (choice !== repeat) onCalendar({repeat: choice}) }}>{tr(choice)}</button>)}
      </div>
    </div>
    <div className="pa-routine-fields">
      {repeat === "weekly" && <label><span id={weekdayLabel}>{tr("weekday")}</span>
        <select aria-labelledby={weekdayLabel} value={weekday} onChange={event => onCalendar({weekday: Number(event.target.value)})}>
          {routineWeekdays.map(day => <option key={day} value={day}>{routineWeekday(day)}</option>)}
        </select>
      </label>}
      {repeat === "custom" ? <label>{w.routineExpression}
        <input ref={cronInput} value={trigger.expression} placeholder="0 9 * * 1-5" spellCheck={false} autoCapitalize="off" aria-invalid={customPreview?.invalid||undefined} aria-describedby={customPreview?.error?cronHelp+" "+previewNotice:cronHelp}
          onChange={event => onCalendar({customExpression: event.target.value})} required/>
      </label> : repeat === "hourly" ? <label><span id={minuteLabel}>{tr("minute")}</span>
        <select aria-labelledby={minuteLabel} value={time.slice(3)} onChange={event => onCalendar({time: `${time.slice(0, 2) || "09"}:${event.target.value}`})} required>
          {!time && <option value="" disabled>—</option>}
          {routineMinutes.map(minute => <option key={minute} value={minute}>:{minute}</option>)}
        </select>
      </label> : <label>{tr("scheduleTime")}
        <input type="time" value={time} onChange={event => onCalendar({time: event.target.value})} required/>
      </label>}
      <label>{tr("timeZone")}
        <input list={zoneList} value={trigger.timeZone} placeholder="Asia/Taipei" autoCapitalize="off" spellCheck={false}
          onChange={event => onZone(event.target.value)} required/>
      </label>
      <datalist id={zoneList}>{routineZones.map(zone => <option key={zone} value={zone}/>)}</datalist>
    </div>
    {repeat === "custom" && <small id={cronHelp}>{tr("cronFields")}</small>}
    <div className="pa-routine-preview" aria-live="polite"><strong><RoutineCalendarSummary trigger={trigger} calendar={calendar}/></strong>{next&&<small>{tr("nextPreview").replace("{time}",next)}</small>}{customPreview?.error&&<small id={previewNotice}>{customPreview.error}</small>}</div>
  </div>
}

function RoutineTriggerEditor({draft, index, count, events, w, onChange, onRemove, previewScope}: {
  draft: RoutineTriggerDraft
  previewScope: SchedulePreviewScope
  index: number
  count: number
  events: boolean
  w: Words
  onChange: (draft: RoutineTriggerDraft) => void
  onRemove: () => void
}) {
  const t = draft.trigger
  const editable = editableRoutineTrigger(t)
  const change = (fields: Partial<W.ProjectAssistantRoutineTrigger>) => onChange({...draft, trigger: {...t, ...fields}})
  return <section className="pa-routine-trigger">
    {count > 1 && <h5>{w.routineTrigger} {index + 1}</h5>}
    <label>{w.routineType}
      <select value={editable ? t.type : ""} onChange={event => onChange(newRoutineTriggerDraft(event.target.value))}>
        {!editable && <option value="" disabled>{t.type}</option>}
        <option value="cron">{w.routineCron}</option>
        <option value="interval">{w.routineInterval}</option>
        <option value="once">{w.routineOnce}</option>
        {events && <><option value="event">{tr("event")}</option><option value="webhook">{tr("webhook")}</option></>}
      </select>
    </label>
    {!editable ? <div className="pa-routine-original">
      <p>{tr("unsupportedTrigger")}</p>
      <details><summary>{tr("originalTrigger")}</summary><pre>{JSON.stringify(t, null, 2)}</pre></details>
    </div> : <>
      {draft.calendar && <RoutineScheduleFields previewScope={previewScope} trigger={t} calendar={draft.calendar} w={w}
        onCalendar={value => onChange(updateRoutineCalendar(draft, value))} onZone={timeZone => change({timeZone})}/>}
      {t.type === "interval" && <label>{w.routineSeconds}
        <input type="number" min={1} max={31536000} value={t.seconds} onChange={event => change({seconds: Number(event.target.value)})} required/>
      </label>}
      {t.type === "once" && <label>{w.routineAt}
        <input type="datetime-local" step="0.001" value={t.at > 0 && t.at <= 8640000000000000
          ? new Date(t.at - new Date(t.at).getTimezoneOffset() * 60000).toISOString().slice(0, -1) : ""}
          onChange={event => change({at: event.target.value ? new Date(event.target.value).getTime() : 0})} required/>
      </label>}
      {t.type === "event" && <>
        <p>{tr("eventHint")}</p>
        <label>{tr("source")}<select value={t.source} onChange={event => change({source: event.target.value})}>
          {t.source !== undefined && !routineEventSources.some(source => source === t.source) && <option value={t.source}>{t.source}</option>}
          {routineEventSources.map(source => <option key={source} value={source}>{source}</option>)}
        </select></label>
        <label>{tr("eventName")}<input required value={t.event ?? ""} onChange={event => change({event: event.target.value})}/></label>
        <div className="pa-routine-filters" role="group" aria-label={tr("filters")}>
          <p>{tr("filters")}</p>
          {t.filters?.map((row, n) => <div className="pa-routine-fields" key={n}>
            <label>{tr("filterKey")}<input required value={row.key} onChange={event => change({filters: t.filters!.map((r, k) => k === n ? {key: event.target.value, value: r.value} : r)})}/></label>
            <label>{tr("filterValue")}<input value={row.value} onChange={event => change({filters: t.filters!.map((r, k) => k === n ? {key: r.key, value: event.target.value} : r)})}/></label>
            <button type="button" onClick={() => change({filters: t.filters!.filter((_, k) => k !== n)})}>{tr("removeFilter")}</button>
          </div>)}
          <button type="button" disabled={(t.filters?.length ?? 0) >= routineEventLimits.filters}
            onClick={() => change({filters: [...(t.filters ?? []), {key: "", value: ""}]})}>{tr("addFilter")}</button>
        </div>
      </>}
    </>}
    {count > 1 && <button type="button" className="pa-routine-remove" onClick={onRemove}>{w.routineRemoveTrigger}</button>}
  </section>
}

function RoutinePanel({source,bot,enabled,w,storageKey,mentions}:Props) {
  const [page,setPage]=useState<W.ProjectAssistantRoutineListResponse|null>(null),[editing,setEditing]=useState<W.ProjectAssistantRoutineDefinition|null|undefined>(undefined)
  const [closing,setClosing]=useState(false)
  const [mode,setMode]=useState<"schedule"|"webhook"|"advanced">("schedule")
  const [schedule,setSchedule]=useState(()=>newRoutineTriggerDraft())
  const [name,setName]=useState(""),[instruction,setInstruction]=useState(""),[triggers,setTriggers]=useState<RoutineTriggerDraft[]>([]),[timeout,setTimeout]=useState(3600),[active,setActive]=useState(true)
  const instructionEditor=useRef<HTMLTextAreaElement>(null),instructionMention=useBotMentions({text:instruction,onText:setInstruction,candidates:mentions,editor:instructionEditor,w})
  const [busy,setBusy]=useState(false),[error,setError]=useState(""),[history,setHistory]=useState<{id:string;page:W.ProjectAssistantRoutineListResponse}|null>(null),[confirm,setConfirm]=useState<string|null>(null),[held,setHeld]=useState<Intent|null>(null),[journalBlocked,setJournalBlocked]=useState(false)
  const [hookID,setHookID]=useState<string|null>(null)
  // A switch pressed here shows what it is doing until the source answers and
  // the list is read again; the row never keeps claiming the state it is leaving.
  const [toggling,setToggling]=useState<{id:string;enabled:boolean}|null>(null)
  const [started,setStarted]=useState(false),[historyOpen,setHistoryOpen]=useState(false)
  const live=useRef(enabled);live.current=enabled
  const pending=useRef(false),control=useRef(new AbortController()),mounted=useRef(true)
  const editorID=useId()
  const dialog=useRef<HTMLDialogElement>(null),editorTitle=useRef<HTMLHeadingElement>(null), editorOpener=useRef<HTMLButtonElement>(null)
  const durableKey="clawdline:routine-command:"+storageKey
  const supported=!!source.canRoutines?.()&&!!source.routines&&!!source.saveRoutine&&!!source.enableRoutine&&!!source.deleteRoutine&&!!source.testRoutine
  const events=!!source.canRoutineEvents?.()&&!!source.routineHook
  const authority=enabled&&source.current?.()!==false&&!!page?.source.writable&&page.source.fresh&&page.source.access==="interactive"
  const writable=authority&&!held&&!journalBlocked
  const query=(id="",cursor=""):W.ProjectAssistantRoutineListRequest=>({key:bot.key,project:bot.project,conversation_id:bot.conversation_id,routine_id:id,cursor,page_size:100})
  const load=async(id="",cursor="")=>{
    if(!supported)throw failure("pa_unsupported")
    const answer=await source.routines!(query(id,cursor),control.current.signal)
    if(!mounted.current)return
    if(id)setHistory(old=>({id,page:{...answer,runs:cursor&&old?.id===id?[...old.page.runs,...answer.runs]:answer.runs}}))
    else setPage(old=>({...answer,definitions:cursor&&old?[...old.definitions,...answer.definitions]:answer.definitions}))
  }
  useEffect(()=>{
    mounted.current=true
    const stored=readRoutineIntent({getItem:key=>sessionStorage.getItem(key)},durableKey,bot)
    if(stored.state==="held"){setHeld(stored.intent);if(stored.intent.action==="hook")setHookID(stored.intent.request.id)}
    if(stored.state==="blocked"){setJournalBlocked(true);setError("pa_outcome_unknown")}
    if(supported)void load().catch(e=>{if(mounted.current)setError(errorCode(e))})
    return ()=>{mounted.current=false;control.current.abort()}
  },[])
  useEffect(()=>{
    const node=dialog.current
    if(!node||editing===undefined)return
    node.showModal()
    editorTitle.current?.focus({preventScroll:true})
    return()=>{node.close();editorOpener.current?.focus({preventScroll:true})}
  },[editing])
  useEffect(()=>{
    if(!closing||!dialog.current)return
    return afterSurfaceExit(dialog.current,()=>{setEditing(undefined);setClosing(false)})
  },[closing])
  const edit=(d:W.ProjectAssistantRoutineDefinition|null, opener:HTMLButtonElement)=>{
    // An editor opened while this panel's own mutation is in flight would
    // capture the revision that mutation is about to replace.
    if(pending.current)return
    const original=d?.triggers.map(routineTriggerDraft)??[newRoutineTriggerDraft()]
    const first=original[0]
    const single=original.length===1
    editorOpener.current=opener;setClosing(false);setEditing(d);setName(d?.name??"");setInstruction(d?.instruction??"")
    setTriggers(original)
    setSchedule(single&&first?.calendar?first:newRoutineTriggerDraft())
    setMode(d===null||single&&first?.calendar?"schedule":single&&first?.trigger.type==="webhook"?"webhook":"advanced")
    setTimeout(d?.timeout_seconds??3600);setActive(d?.enabled??true);setError("")
    setStarted(false);setHistoryOpen(false);setConfirm(null)
  }
  const cancelEdit=()=>{if(!busy&&!closing){setClosing(true);setError("")}}
  const selectedTriggers=mode==="schedule"?[schedule]:mode==="webhook"?[newRoutineTriggerDraft("webhook")]:triggers
  const run=async(intent:Intent,replay=false):Promise<W.ProjectAssistantRoutineHookResponse|null>=>{
    if(pending.current||!live.current||!authority||!supported||journalBlocked||(!replay&&held)||source.current?.()===false)return null
    if(intent.action==="hook"&&!events)return null
    pending.current=true;setBusy(true);setError("")
    // Persist the exact command before crossing the transport. A reload or
    // uncertain response can only expose an explicit replay of these bytes.
    try {
      if(!replay) {
        const current=await source.routines!({...query(),page_size:1},control.current.signal)
        intent=intent.action==="hook"?{action:"hook",request:{...intent.request,fence:current.fence}}:{...intent,request:{...intent.request,fence:current.fence}}
      }
      if(!mounted.current||!live.current||control.current.signal.aborted||source.current?.()===false)return null
      try {sessionStorage.setItem(durableKey,JSON.stringify(intent))} catch(error) {setJournalBlocked(true);throw error}
      setHeld(intent)
      const signal=control.current.signal
      let saved:string|undefined
      if(intent.action==="save"){
        const request=intent.request as W.ProjectAssistantRoutineSaveRequest
        const answer=await source.saveRoutine!(request,signal)
        if(!request.id&&request.triggers.some(isHookTrigger))saved=answer.receipt.resource_id
      }
      if(intent.action==="enable")await source.enableRoutine!(intent.request as W.ProjectAssistantRoutineEnabledRequest,signal)
      if(intent.action==="delete")await source.deleteRoutine!(intent.request,signal)
      if(intent.action==="test")await source.testRoutine!(intent.request,signal)
      let hook:W.ProjectAssistantRoutineHookResponse|null=null
      if(intent.action==="hook") {
        hook=await performRoutineHook(intent.request,{
          current:()=>mounted.current&&live.current&&!signal.aborted&&source.current?.()!==false,
          persist:value=>sessionStorage.setItem(durableKey,JSON.stringify(value)),
          send:request=>source.routineHook!(request,signal),
        })
        if(!hook)throw failure("pa_outcome_unknown")
      }
      sessionStorage.removeItem(durableKey)
      if(!mounted.current||!live.current||source.current?.()===false)return null
      // A test run keeps its editor open on the run it started, as in Codync.
      setHeld(null);if(editing!==undefined&&(intent.action==="save"||intent.action==="delete"))setClosing(true);setConfirm(null)
      if(intent.action==="hook")return hook
      if(intent.action==="test"){setStarted(true);setHistoryOpen(true)}
      await load();if(intent.action==="test")await load(intent.request.id)
      if(saved)setHookID(saved)
    } catch(e) {
      if(!mounted.current)return null
      const code=errorCode(e),unknown=!provablyNotSent(e)&&["pa_outcome_unknown","execution_effect_unknown","pa_revoked","unavailable"].includes(code)
      if(!unknown) { try {sessionStorage.removeItem(durableKey);setHeld(null)} catch { /* Keep the durable command held. */ } }
      setError(code)
    } finally {pending.current=false;if(mounted.current){setBusy(false);setToggling(null)}}
    return null
  }
  const action=(d:W.ProjectAssistantRoutineDefinition,kind:"enable"|"delete"|"test")=>{
    if(!page)return
    const request={command:command(bot.key,"edit"),fence:page.fence,id:d.id,expected_revision:d.version,...(kind==="enable"?{enabled:!d.enabled}:{})}
    if(kind==="enable"&&!pending.current&&writable)setToggling({id:d.id,enabled:!d.enabled})
    void run({action:kind,request})
  }
  const save=(event:FormEvent<HTMLFormElement>)=>{
    event.preventDefault()
    if(!page||!writable||busy||closing||editing===undefined)return
    const wire=selectedTriggers.map(routineTriggerForSave)
    if(!wire.length||!wire.every(trigger=>editableRoutineTrigger(trigger)&&validRoutineTrigger(trigger,events))) {
      setError("routine_event_fields")
      return
    }
    void run({action:"save",request:{command:command(bot.key,"edit"),fence:page.fence,id:editing?.id??"",expected_revision:editing?.version??0,name,instruction,triggers:wire,timeout_seconds:timeout,enabled:active}})
  }
  const refresh=async()=>{setBusy(true);try{await load();if(history)await load(history.id);setError("")}catch(e){setError(errorCode(e))}finally{setBusy(false)}}
  const editError=error&&!journalBlocked?error==="routine_event_fields"?tr("invalidEvent"):error==="resume_unsupported"?w.routineResumeUnsupported:error==="pa_outcome_unknown"?w.unknown:w.unavailable:""
  const editorValid=!!name.trim()&&!!instruction.trim()&&selectedTriggers.every(({trigger})=>editableRoutineTrigger(trigger)&&validRoutineTrigger(trigger,events))
  const routines=page?.definitions.filter(d=>!d.follow_up)??[]
  const followUps=page?.definitions.filter(d=>d.follow_up&&d.enabled&&!!d.next_due)??[]
  // The row's second line, as in Codync: what is happening now, else when it runs.
  const summary=(d:W.ProjectAssistantRoutineDefinition):{text:string;tone?:"danger"|"active"}=>{
    if(toggling?.id===d.id)return {text:toggling.enabled?w.routineEnabling:w.routinePausing,tone:"active"}
    const latest=page?.runs.filter(r=>r.routine_id===d.id).sort((a,b)=>b.created_at.localeCompare(a.created_at))[0]
    if(latest&&["accepted","queued","running","waiting_permission"].includes(latest.state))return {text:w[latest.state as keyof Words]??latest.state,tone:"active"}
    if(latest?.state==="failed")return {text:tr("lastRunFailed"),tone:"danger"}
    const schedule=d.triggers.map(t=>triggerLine(t,w)).join(" · ")
    if(!d.enabled)return {text:`${w.routinePaused} · ${schedule}`}
    if(d.next_due)return {text:`${schedule} · ${tr("nextPreview").replace("{time}",shortTime(d.next_due))}`}
    return {text:d.triggers.some(isHookTrigger)?`${schedule} · ${tr("waitingEvent")}`:`${schedule} · ${w.routineSpent}`}
  }
  const hasHook=!!editing&&editing.triggers.some(isHookTrigger)
  const hookPanel=hookID&&events&&<RoutineHook key={hookID} id={hookID} allowed={!!authority} busy={busy} held={held} blocked={journalBlocked} w={w} onClose={()=>setHookID(null)} onRun={async(rotate,replay)=>{
    if(replay)return held?.action==="hook"&&held.request.id===hookID?run(held,true):null
    const d=page?.definitions.find(d=>d.id===hookID)
    if(!d||!page||!writable)return null
    return run({action:"hook",request:{command:command(bot.key,"edit"),fence:page.fence,id:d.id,expected_revision:d.version,rotate}})
  }}/>
  const editorHistory=editing&&history?.id===editing.id?history.page:null
  return <section className="pa-routines pa-routines-embedded" aria-label={w.routines}>
    {followUps.length>0&&<div className="pa-routine-group" aria-label={tr("later")} role="group">
      <h3 className="pa-routine-group-title">{tr("later")}</h3>
      <ul className="pa-routine-list">{followUps.map(d=><li key={d.id} className="pa-routine-card pa-routine-follow">
        <span className="pa-routine-follow-icon"><ClockIcon/></span>
        <div className="pa-routine-follow-text"><p>{d.instruction?<BotMentionText text={d.instruction} candidates={mentions}/>:d.name}</p><small>{Date.parse(d.next_due)<=Date.now()?tr("followUpDue"):tr("followUpAt").replace("{time}",shortTime(d.next_due))}</small></div>
        <button type="button" className="pa-routine-icon-button" disabled={!writable||busy} aria-label={`${tr("cancelFollowUp")}: ${d.name}`} title={tr("cancelFollowUp")} onClick={()=>action(d,"delete")}>×</button>
      </li>)}</ul>
    </div>}
    <header className="pa-routine-section-header"><h3>{w.routines}</h3><button type="button" className="pa-routine-plus" disabled={!writable||busy} aria-label={tr("setUp")} title={tr("setUp")} onClick={event=>edit(null,event.currentTarget)}>+</button></header>
    {!supported?<p className="pa-routines-empty" role="status">{w.routinesUnsupported}</p>:<>
      {!page&&!error&&<p className="pa-routines-empty" role="status">{w.loading}</p>}
      {editing!==undefined&&<dialog ref={dialog} className="pa-routine-dialog" data-closing={closing||undefined} aria-labelledby={editorID}
        onCancel={event=>{event.preventDefault();cancelEdit()}}
        onKeyDown={event=>{
          if(event.key==="Escape")event.stopPropagation()
          if((event.metaKey||event.ctrlKey)&&event.key.toLowerCase()==="s"&&!event.nativeEvent.isComposing){
            event.preventDefault()
            if(editorValid)event.currentTarget.querySelector("form")?.requestSubmit()
          }
        }}>
        <form className="pa-routine-editor" inert={closing} onSubmit={save}>
          <header className="pa-routine-modal-header"><span className="pa-routine-icon" aria-hidden="true"><svg viewBox="0 0 20 20" width="20" height="20" fill="none" stroke="currentColor" strokeWidth="1.5"><circle cx="10" cy="10" r="7.5"/><path d="M10 5.5V10H6.5" strokeLinecap="round"/></svg></span>
            <h4 ref={editorTitle} id={editorID} tabIndex={-1}>{editing?w.routineEdit:tr("setUp")}</h4>
            <button type="button" aria-label={w.close} disabled={busy} onClick={cancelEdit}>×</button>
          </header>
          <fieldset disabled={!writable||busy} aria-labelledby={editorID}>
            <label>{w.name}<input value={name} onChange={event=>setName(event.target.value)} placeholder={tr("nameExample")} required/></label>
            <label>{w.routineInstruction}<span className="pa-routine-instruction">{instructionMention.picker}<textarea ref={instructionEditor} {...instructionMention.textareaProps} onKeyDown={event=>{instructionMention.keyDown(event)}} placeholder={tr("instructionExample")} required/></span>{botMentionSegments(instruction).some(part=>typeof part!=="string")&&<span className="pa-routine-mentions"><BotMentionText text={botMentionSegments(instruction).filter(part=>typeof part!=="string").map(botMentionToken).join(" ")} candidates={mentions}/></span>}</label>
            <div className="pa-routine-when"><span>{tr("when")}</span><div className="pa-routine-modes" role="group" aria-label={tr("when")}>
              <button type="button" aria-pressed={mode==="schedule"} onClick={()=>setMode("schedule")}>{tr("schedule")}</button>
              {events&&<button type="button" aria-pressed={mode==="webhook"} onClick={()=>setMode("webhook")}>{tr("webhook")}</button>}
              {mode==="advanced"&&<button type="button" aria-pressed={true}>{tr("advancedTriggers")}</button>}
            </div></div>
            {mode==="schedule"&&schedule.calendar&&<RoutineScheduleFields previewScope={{source,bot}} trigger={schedule.trigger} calendar={schedule.calendar} w={w}
              onCalendar={change=>setSchedule(old=>updateRoutineCalendar(old,change))} onZone={timeZone=>setSchedule(old=>({...old,trigger:{...old.trigger,timeZone}}))}/>}
            {mode==="advanced"&&<>
              {triggers.length>1&&<p className="pa-routine-or">{w.routineOr}</p>}
              {triggers.map((draft,i)=><RoutineTriggerEditor key={i} previewScope={{source,bot}} draft={draft} index={i} count={triggers.length} events={events} w={w}
                onChange={value=>setTriggers(old=>old.map((row,n)=>n===i?value:row))}
                onRemove={()=>setTriggers(old=>old.filter((_,n)=>n!==i))}/>)}
              <button type="button" className="pa-routine-add" disabled={triggers.length>=20}
                onClick={()=>setTriggers(old=>[...old,newRoutineTriggerDraft()])}>{w.routineAddTrigger}</button>
            </>}
            {events && (mode === "webhook" || (mode === "advanced" && triggers.some(draft => draft.trigger.type === "webhook"))) &&
              <p className="pa-routine-webhook-note">{tr("webhookSave")}</p>}
            {!events&&<p className="pa-routine-caption" role="status">{tr("eventUnsupported")}</p>}
            <details className="pa-routine-run-settings">
              <summary><span>{tr("runSettings")}</span><small>{tr("timeoutSummary").replace("{time}",new Intl.NumberFormat(currentCatalogTag(),{style:"unit",unit:timeout===3600?"hour":"second",unitDisplay:"long"}).format(timeout===3600?1:timeout))}</small></summary>
              <div className="pa-routine-options">
                <label>{w.routineTimeout}<input type="number" min={1} max={86400} value={timeout} onChange={event=>setTimeout(Number(event.target.value))} required/></label>
                <label className="pa-routine-enabled"><input type="checkbox" checked={active} onChange={event=>setActive(event.target.checked)}/>{w.routineEnabled}</label>
              </div>
              {mode!=="advanced"&&<button type="button" className="pa-routine-advanced" onClick={()=>{setTriggers(selectedTriggers);setMode("advanced")}}>{tr("advancedTriggers")}</button>}
            </details>
          </fieldset>
          {editing&&hasHook&&<button type="button" className="pa-routine-secondary" disabled={!authority||busy||!events||!!held||journalBlocked} onClick={()=>setHookID(editing.id)}>{tr("manageHook")}</button>}
          {editing&&hookID===editing.id&&hookPanel}
          {editing&&<details className="pa-routine-run-settings pa-routine-history" open={historyOpen} onToggle={event=>{
            const open=event.currentTarget.open;setHistoryOpen(open)
            if(open)void load(editing.id).catch(e=>setError(errorCode(e)))
          }}>
            <summary><span>{w.routineHistory}</span></summary>
            {!editorHistory?<p className="pa-routine-caption" role="status">{w.loading}</p>:editorHistory.runs.length===0?<p className="pa-routine-caption">{w.routineNoRuns}</p>:<ol className="pa-routine-runs">{editorHistory.runs.map(run=><li className="pa-routine-run" key={run.id}>
              <p className="pa-routine-run-head"><strong data-state={run.state}>{w[run.state as keyof Words]??run.state}</strong><small>{shortTime(run.created_at)} · {run.source==="test"?tr("testRun"):run.source==="schedule"?tr("scheduledRun"):run.source==="webhook"?tr("eventRun"):run.source==="follow_up"?tr("followUp"):`${tr("otherRun")}: ${run.source}`}</small></p>
              {(run.delivery_id||run.retry_count!==undefined||run.not_before)&&<p className="pa-routine-caption">{[run.delivery_id&&`${tr("delivery")}: ${run.delivery_id}`,run.retry_count!==undefined&&`${tr("retryCount")}: ${run.retry_count}`,run.not_before&&`${tr("retryAt")}: ${shortTime(run.not_before)}`].filter(Boolean).join(" · ")}</p>}
              {run.state==="unknown"&&<p role="alert">{tr("unknownRun")}</p>}
              {run.result&&<p className="pa-routine-result">{run.result}</p>}
              {run.failure&&<p role="alert">{run.failure.code} ({run.failure.field})</p>}
            </li>)}</ol>}
            {editorHistory?.next_cursor&&<button type="button" disabled={busy} onClick={()=>void load(editing.id,editorHistory.next_cursor).catch(e=>setError(errorCode(e)))}>{w.more}</button>}
          </details>}
          {editError&&<p role="alert">{editError} ({error})</p>}
          {held&&held.action!=="hook"&&!busy&&!journalBlocked&&<div className="pa-routine-confirm" role="alert"><p>{w.routineHeld}</p><div><button type="button" disabled={busy||!authority} onClick={()=>void run(held,true)}>{w.routineReplay}</button></div></div>}
          {editing&&confirm===editing.id&&<div className="pa-routine-confirm" role="alert"><p>{w.routineDeleteConfirm}</p><div><button type="button" onClick={()=>setConfirm(null)}>{w.cancel}</button><button type="button" className="pa-routine-danger" disabled={!writable||busy} onClick={()=>action(editing,"delete")}>{w.routineConfirmDelete}</button></div></div>}
          <footer className="pa-routine-footer">
            {editing&&<>
              <button type="button" className="pa-routine-icon-button" aria-label={w.routineDelete} title={w.routineDelete} disabled={!writable||busy} onClick={()=>setConfirm(editing.id)}><TrashIcon/></button>
              <button type="button" className="pa-routine-icon-button" aria-label={started?tr("testStarted"):w.routineTest} title={started?tr("testStarted"):w.routineTest} disabled={!writable||busy||started} onClick={()=>action(editing,"test")}>{started?<CheckIcon/>:<PlayIcon/>}</button>
            </>}
            <span className="pa-routine-spacer"/>
            {/* As Codync's routine editor: a spinner while saving, and no shortcut
                hint then. An uncertain earlier save must be confirmed first, so the
                button is disabled rather than silently doing nothing. */}
            <button type="submit" className="pa-routine-save" aria-busy={busy||undefined} disabled={!writable||busy||!editorValid||!!held}>{busy&&<span className="pa-send-spinner" aria-hidden="true"/>}{busy?w.routineSaving:editing?w.save:tr("createRoutine")}{!busy&&<kbd aria-hidden="true">{macKeyboard()?"⌘S":"Ctrl+S"}</kbd>}</button>
          </footer>
        </form>
      </dialog>}
      {page&&routines.length===0&&<p className="pa-routines-empty">{tr("emptyRoutines")}</p>}
      {routines.length>0&&<ul className="pa-routine-list">{routines.map((d,i)=>{const line=summary(d);return <li className="pa-routine-card" key={d.id}>
        <button type="button" className="pa-routine-row" disabled={busy} aria-label={`${w.routineEdit}: ${d.name}`} aria-describedby={`${editorID}-line-${i}`} onClick={event=>edit(d,event.currentTarget)}>
          <strong>{d.name}</strong><small id={`${editorID}-line-${i}`} data-tone={line.tone}>{line.text}</small>
        </button>
        <button type="button" role="switch" className="pa-routine-switch" aria-checked={d.enabled} aria-busy={toggling?.id===d.id||undefined} aria-label={`${w.routineEnabled}: ${d.name}`} title={d.enabled?w.routinePause:w.routineEnable} disabled={!writable||busy} onClick={()=>action(d,"enable")}><span/></button>
      </li>})}</ul>}
      <p className="pa-visually-hidden" role="status" aria-live="polite">{toggling?`${toggling.enabled?w.routineEnabling:w.routinePausing}: ${routines.find(d=>d.id===toggling.id)?.name??""}`:""}</p>
      {editing===undefined&&hookPanel}
      {page?.next_cursor&&<button type="button" disabled={busy} onClick={()=>void load("",page.next_cursor).catch(e=>setError(errorCode(e)))}>{w.more}</button>}
      {journalBlocked&&<p role="alert">{w.routineJournalUnreadable}</p>}
      {held?.action==="hook"&&!hookID&&<button type="button" onClick={()=>setHookID(held.request.id)}>{tr("retryHook")}</button>}
      {editing===undefined&&held&&held.action!=="hook"&&!busy&&!journalBlocked&&<div role="alert"><p>{w.routineHeld}</p><button type="button" disabled={busy||!authority||!supported} onClick={()=>void run(held,true)}>{w.routineReplay}</button></div>}
    </>}
    {editing===undefined&&editError&&<p role="alert">{editError} ({error}) <button type="button" disabled={busy} onClick={()=>void refresh()}>{w.retry}</button></p>}
  </section>
}
