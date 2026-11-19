import { useEffect, useState, type ReactNode } from "react"
import type * as W from "@clawdline/contract"
import { elapsed } from "./working"
import type { Words } from "./words"
import { ThinkingOrb } from "./ThinkingOrb"

export function WorkingIndicator({turn,activity,steps,stopStatus,w,animated=true,waiting=turn.state==="waiting_permission"}:{
  turn:W.ProjectAssistantReadTurnSummary;activity:string;steps?:ReactNode;stopStatus?:string;w:Words;animated?:boolean;waiting?:boolean
}) {
  const [now,setNow]=useState(Date.now())
  useEffect(()=>{const timer=setInterval(()=>setNow(Date.now()),1000);return()=>clearInterval(timer)},[])
  const text=stopStatus||(waiting?w.waiting_permission:activity==="writing"?w.writingReply:activity==="thinking"?w.thinking:activity||w.running)
  const time=elapsed(turn.started_at,now)
  return <details className="pa-working-steps">
    <summary className="pa-turn pa-working" data-waiting={waiting||undefined} title={w.trace}>
      <ThinkingOrb state={waiting?"listening":"working"} size={16} color={waiting?"var(--warning,#F0A030)":"var(--pa-dim,#999)"} animated={animated}/>
      <span className="pa-working-text" role="status">{text}</span>
      {time&&<span className="pa-working-time" aria-hidden="true">{time}</span>}
      <span className="pa-working-chevron" aria-label={w.trace}>⌄</span>
    </summary>
    <div className="pa-working-steps-body">{steps??<p>{w.workNoSteps}</p>}</div>
  </details>
}
