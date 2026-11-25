import {useEffect, useId, useLayoutEffect, useRef, useState} from "react"
import type * as W from "@clawdline/contract"
import {BotAvatar} from "./BotAvatar"
import {rowActivity} from "./working"
import type {Words} from "./words"
import "./background-tasks.css"

export type BackgroundTask = W.ProjectAssistantBackgroundTaskSummary
type OpenTask = (task: BackgroundTask, button: HTMLButtonElement) => void

export function liveBackgroundTask(task: BackgroundTask) {
  return task.state === "waiting" || task.state === "active" || task.state === "waiting_permission"
}

// A raw shell line (Codex reports the command itself) can carry the person's home
// directory and runs far wider than a phone; name the action instead of echoing it.
const shellLine = /;|&&|\|\||\||(^|[\s'"=])(\/Users\/|\/home\/|~\/|[A-Za-z]:\\)/

export function backgroundActivityLabel(activity: string, w: Words) {
  const shown = rowActivity(activity,w)
  return shown === activity && shellLine.test(activity) ? w.backgroundRunningCommand : shown
}

export function backgroundTaskLabel(task: BackgroundTask, w: Words) {
  if (task.state === "waiting") return w.backgroundQueued
  if (task.state === "active") return task.activity?backgroundActivityLabel(task.activity,w):w.backgroundWorking
  if (task.state === "waiting_permission") return w.backgroundPermission
  if (task.state === "succeeded") return w.backgroundCompleted
  if (task.state === "failed") return w.failed
  if (task.state === "cancelled") return w.backgroundStopped
  return w.unknown
}

export function BackgroundDuration({task}: {task: BackgroundTask}) {
  const [now, setNow] = useState(Date.now)
  const live = liveBackgroundTask(task) && task.state !== "waiting" && !!task.started_at && !task.ended_at
  useEffect(() => {
    if (!live) return
    const timer = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(timer)
  }, [live])
  if (!task.started_at || task.state === "waiting") return null
  const end = task.ended_at ? Date.parse(task.ended_at) : now
  const duration = Math.max(0, Math.floor((end - Date.parse(task.started_at)) / 1000))
  return <time dateTime={`PT${duration}S`}>{Math.floor(duration / 60)}:{String(duration % 60).padStart(2,"0")}</time>
}

export function BackgroundStop({action, disabled, w}: {action: () => void; disabled?: boolean; w: Words}) {
  const [confirming, setConfirming] = useState(false)
  const button = useRef<HTMLButtonElement>(null)
  const dialog = useRef<HTMLDialogElement>(null)
  useLayoutEffect(() => {
    if (!confirming) return
    const node = dialog.current
    const opener = button.current
    node?.showModal()
    return () => {
      node?.close()
      opener?.focus({preventScroll:true})
    }
  }, [confirming])
  return <>
    <button ref={button} type="button" className="pa-background-stop" aria-label={w.backgroundStop} title={w.backgroundStop} disabled={disabled} onClick={()=>setConfirming(true)}>
      <svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true"><rect x="3" y="3" width="10" height="10" rx="2" fill="currentColor"/></svg>
    </button>
    {confirming&&<dialog ref={dialog} className="pa-roster-confirm" aria-label={w.backgroundStopConfirm}
      onCancel={event=>{event.preventDefault();setConfirming(false)}}>
      <h2>{w.backgroundStopConfirm}</h2>
      <p>{w.backgroundStopEffects}</p>
      <div className="pa-actions">
        <button type="button" autoFocus onClick={()=>setConfirming(false)}>{w.cancel}</button>
        <button type="button" disabled={disabled} className="pa-danger" onClick={()=>{setConfirming(false);action()}}>{w.backgroundStop}</button>
      </div>
    </dialog>}
  </>
}

export type BackgroundStopOutcome = "acknowledged" | "unknown"

/** A live task's own avatar motion: approval waits, a running turn works. */
function taskMood(task: BackgroundTask) {
  return task.state === "waiting_permission" ? "needsInput" as const : task.state === "active" ? "working" as const : "idle" as const
}

function liveOrder(tasks: readonly BackgroundTask[]) {
  const live = tasks.filter(liveBackgroundTask)
  return [...live.filter(item => item.state === "waiting_permission"), ...live.filter(item => item.state === "active"), ...live.filter(item => item.state === "waiting")]
}

function StopStatus({task, stopping, outcomes, w}: {task: BackgroundTask; stopping: string; outcomes: Readonly<Record<string, BackgroundStopOutcome>>; w: Words}) {
  if (stopping !== task.id && !outcomes[task.id]) return null
  return <span className="pa-background-stop-status" role="status">
    {stopping===task.id?w.stopSending:outcomes[task.id]==="unknown"?w.stopUnconfirmed:w.stopAcknowledged}
  </span>
}

/** The chip above the composer: every live background thread, one line for one
 *  thread and a count that opens the list for several. */
export function BackgroundCapsule({tasks, appearance, open, stop, stopping, outcomes, writable, w}: {
  tasks: readonly BackgroundTask[]; appearance: W.ProjectAssistantAppearance; open: OpenTask; stop: (task: BackgroundTask) => void; stopping: string; outcomes: Readonly<Record<string, BackgroundStopOutcome>>; writable: boolean; w: Words
}) {
  const [expanded, setExpanded] = useState(false)
  const listID = useId()
  const live = liveOrder(tasks)
  if (!live.length) return null
  const row = (task: BackgroundTask, avatar: boolean) => <div className="pa-background-item" key={task.id}>
    <button type="button" className="pa-background-open" onClick={event => open(task,event.currentTarget)}>
      {avatar?<BotAvatar appearance={appearance} size={24} mood={taskMood(task)}/>:<span className="pa-background-mark" data-active={task.state==="active"||undefined} aria-hidden="true"/>}
      <strong className="pa-background-activity">{backgroundTaskLabel(task,w)}</strong>
      <span className="pa-background-title">{task.title}</span><BackgroundDuration task={task}/>
    </button>
    {task.worker&&<BackgroundStop key={task.id} w={w} disabled={!writable||!!stopping||!!outcomes[task.id]} action={()=>stop(task)}/>}
    <StopStatus task={task} stopping={stopping} outcomes={outcomes} w={w}/>
  </div>
  if (live.length === 1) return <div className="pa-background-capsule">{row(live[0]!,true)}</div>
  const lead = live[0]!
  return <div className="pa-background-capsule" data-expanded={expanded||undefined}>
    {expanded&&<div className="pa-background-items" id={listID}>{live.map(task => row(task,false))}</div>}
    <button type="button" className="pa-background-toggle" aria-expanded={expanded} aria-controls={expanded?listID:undefined} onClick={()=>setExpanded(!expanded)}>
      <BotAvatar appearance={appearance} size={22} mood={taskMood(lead)}/>
      <span>{w.backgroundCount.replace("{count}",String(live.length))}</span>
      <svg viewBox="0 0 24 24" width="12" height="12" aria-hidden="true" fill="none" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round"><path d={expanded?"M6 9l6 6 6-6":"M6 15l6-6 6 6"}/></svg>
    </button>
  </div>
}

/** How long the return flow plays when a reply goes back to the sender. */
const HandoffReturnMs = 1200

/** Another Bot's message being answered in a background thread, under that
 *  message: the sender, a flow toward this Bot, and this Bot at work. A
 *  finished one stays as a single line that opens its thread. */
export function BackgroundHandoff({task, sender, senderName, self, open, stop, stopping, outcomes, writable, w}: {
  task: BackgroundTask; sender?: W.ProjectAssistantBot; senderName: string; self: W.ProjectAssistantBot; open: OpenTask; stop: (task: BackgroundTask) => void; stopping: string; outcomes: Readonly<Record<string, BackgroundStopOutcome>>; writable: boolean; w: Words
}) {
  const live = liveBackgroundTask(task)
  const seenLive = useRef(live)
  const [returning, setReturning] = useState(false)
  useEffect(() => {
    if (live) { seenLive.current = true; return }
    if (!seenLive.current || task.state !== "succeeded") return
    seenLive.current = false
    setReturning(true)
    const timer = window.setTimeout(() => setReturning(false), HandoffReturnMs)
    return () => window.clearTimeout(timer)
  }, [live, task.state])
  if (live || returning) {
    const label = returning ? w.backgroundHandoffDone.replace("{name}", senderName) : backgroundTaskLabel(task,w)
    const message = w.backgroundHandoffMessage.replace("{name}", senderName)
    return <div className="pa-background-handoff" data-task-id={task.id} data-returning={returning||undefined}>
      <button type="button" className="pa-background-handoff-open" onClick={event => open(task,event.currentTarget)}>
        <span className="pa-background-handoff-cast" aria-hidden="true">
          <span className="pa-background-handoff-party">{sender?<BotAvatar appearance={sender.appearance} size={36} mood={returning?"idle":"waitingOnWork"}/>:<span className="pa-background-handoff-unknown"/>}<small>{senderName}</small></span>
          <span className="pa-background-flow"><i/><i/><i/><i/></span>
          <span className="pa-background-handoff-party"><BotAvatar appearance={self.appearance} size={44} mood={returning?"idle":taskMood(task)}/><small>{self.name}</small></span>
        </span>
        <span className="pa-background-handoff-line"><strong>{label}</strong>{!returning&&<span>{message}</span>}<BackgroundDuration task={task}/></span>
      </button>
      {live&&task.worker&&<BackgroundStop key={task.id} w={w} disabled={!writable||!!stopping||!!outcomes[task.id]} action={()=>stop(task)}/>}
      <StopStatus task={task} stopping={stopping} outcomes={outcomes} w={w}/>
    </div>
  }
  const done = task.state === "succeeded"
  const word = done ? w.backgroundHandoffDone : task.state === "cancelled" ? w.backgroundHandoffStopped : w.backgroundHandoffFailed
  return <button type="button" className="pa-background-handoff-done" data-task-id={task.id} data-state={task.state} disabled={!task.worker} onClick={event => open(task,event.currentTarget)}>
    <span className="pa-background-handoff-mark" aria-hidden="true">{done?"✓":"!"}</span>
    <span>{word.replace("{name}", senderName)}</span><BackgroundDuration task={task}/><span aria-hidden="true">›</span>
  </button>
}

export function BackgroundList({tasks, workspace, open, section, slots, w}: {
  tasks: readonly BackgroundTask[]; workspace: string; open: OpenTask; section: "live" | "recent"; slots: number; w: Words
}) {
  const active = tasks.filter(task => task.worker && (task.state === "active" || task.state === "waiting_permission"))
  const waiting = tasks.filter(task => task.state==="waiting")
  const recent = tasks.filter(task => task.worker && !liveBackgroundTask(task))
  const rows = (group: readonly BackgroundTask[]) => group.map(task =>
    <button type="button" className="pa-background-row" key={task.id} onClick={event => open(task,event.currentTarget)}>
      <span className="pa-background-mark" data-active={task.state==="active"||undefined} aria-hidden="true"/>
      <span><strong>{task.title}</strong><small>{backgroundTaskLabel(task,w)}</small>{workspace&&task.worker&&<small title={workspace}>{workspace}</small>}</span>
      <BackgroundDuration task={task}/><span aria-hidden="true">›</span>
    </button>)
  if(section==="recent")return recent.length>0?<section className="pa-background-list"><h3>{w.backgroundRecent}</h3><div>{rows(recent)}</div></section>:null
  return <>
    {active.length>0&&<section className="pa-background-list"><h3>{w.backgroundThreads} <small>{active.length} / {slots}</small></h3>
      <div>{rows(active)}{Array.from({length:Math.max(0,slots-active.length)},(_,index)=><p className="pa-background-empty" key={index}>{w.backgroundFree}</p>)}</div>
    </section>}
    {waiting.length>0&&<section className="pa-background-list"><h3>{w.backgroundQueue}</h3><div>{rows(waiting)}</div></section>}
  </>
}
