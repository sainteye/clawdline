import {useEffect, useState} from "react"
import type * as W from "@clawdline/contract"
import {rowActivity} from "./working"
import type {Words} from "./words"
import "./background-tasks.css"

export type BackgroundTask = W.ProjectAssistantBackgroundTaskSummary
type OpenTask = (task: BackgroundTask, button: HTMLButtonElement) => void

const threadSlots = 3

export function liveBackgroundTask(task: BackgroundTask) {
  return task.state === "waiting" || task.state === "active" || task.state === "waiting_permission"
}

export function backgroundTaskLabel(task: BackgroundTask, w: Words) {
  if (task.state === "waiting") return w.backgroundQueued
  if (task.state === "active") return task.activity?rowActivity(task.activity,w):w.backgroundWorking
  if (task.state === "waiting_permission") return w.backgroundPermission
  if (task.state === "succeeded") return w.backgroundCompleted
  if (task.state === "failed") return w.failed
  if (task.state === "cancelled") return w.backgroundStopped
  return w.unknown
}

export function BackgroundDuration({task}: {task: BackgroundTask}) {
  const [now, setNow] = useState(Date.now)
  const live = liveBackgroundTask(task) && !!task.started_at && !task.ended_at
  useEffect(() => {
    if (!live) return
    const timer = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(timer)
  }, [live])
  if (!task.started_at) return null
  const end = task.ended_at ? Date.parse(task.ended_at) : now
  const duration = Math.max(0, Math.floor((end - Date.parse(task.started_at)) / 1000))
  return <time dateTime={`PT${duration}S`}>{Math.floor(duration / 60)}:{String(duration % 60).padStart(2,"0")}</time>
}

export function BackgroundStop({action, disabled, w}: {action: () => void; disabled?: boolean; w: Words}) {
  return <button type="button" className="pa-background-stop" aria-label={w.backgroundStop} title={w.backgroundStop} disabled={disabled} onClick={action}>
    <svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true"><rect x="3" y="3" width="10" height="10" rx="2" fill="currentColor"/></svg>
  </button>
}

export function BackgroundCapsule({tasks, open, stop, stopping, writable, w}: {
  tasks: readonly BackgroundTask[]; open: OpenTask; stop: (task: BackgroundTask) => void; stopping: string; writable: boolean; w: Words
}) {
  const live = tasks.filter(liveBackgroundTask)
  const task = live.find(item => item.state === "waiting_permission") ?? live.find(item => item.worker) ?? live[0]
  if (!task) return null
  return <div className="pa-background-capsule">
    <button type="button" className="pa-background-open" onClick={event => open(task,event.currentTarget)}>
      <span className="pa-background-mark" data-active={task.state==="active"||undefined} aria-hidden="true"/>
      <span>{w.backgroundLabel} · <strong>{backgroundTaskLabel(task,w)}</strong></span>
      <span className="pa-background-title">{task.title}</span><BackgroundDuration task={task}/>
      {live.length>1&&<span aria-label={w.backgroundThreads}>+{live.length-1}</span>}
    </button>
    {task.worker&&<BackgroundStop w={w} disabled={!writable||!!stopping} action={()=>stop(task)}/>}
  </div>
}

export function BackgroundList({tasks, workspace, open, section, w}: {
  tasks: readonly BackgroundTask[]; workspace: string; open: OpenTask; section: "live" | "recent"; w: Words
}) {
  const active = tasks.filter(task => task.worker && liveBackgroundTask(task))
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
    {active.length>0&&<section className="pa-background-list"><h3>{w.backgroundThreads} <small>{active.length} / {threadSlots}</small></h3>
      <div>{rows(active)}{Array.from({length:Math.max(0,threadSlots-active.length)},(_,index)=><p className="pa-background-empty" key={index}>{w.backgroundFree}</p>)}</div>
    </section>}
    {waiting.length>0&&<section className="pa-background-list"><h3>{w.backgroundQueue}</h3><div>{rows(waiting)}</div></section>}
  </>
}
