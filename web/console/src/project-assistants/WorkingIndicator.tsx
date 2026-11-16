import { useEffect, useState } from "react"
import type * as W from "@clawdline/contract"
import { elapsed } from "./working"
import type { Words } from "./words"
import { ThinkingOrb } from "./ThinkingOrb"

/** Codync's working indicator: the running turn's activity and elapsed time. */
export function WorkingIndicator({turn,activity,w,animated=true,waiting=turn.state==="waiting_permission"}:{turn:W.ProjectAssistantReadTurnSummary;activity:string;w:Words;animated?:boolean;waiting?:boolean}) {
  const [now,setNow]=useState(Date.now())
  useEffect(()=>{const timer=setInterval(()=>setNow(Date.now()),1000);return()=>clearInterval(timer)},[])
  const text=waiting?w.waiting_permission:activity==="writing"?w.writingReply:activity||w.running
  const time=elapsed(turn.started_at,now)
  return <p className="pa-turn pa-working" data-waiting={waiting||undefined} role="status"><ThinkingOrb state={waiting?"listening":"working"} size={16} color={waiting?"var(--warning,#F0A030)":"var(--pa-dim,#999)"} animated={animated}/><span className="pa-working-text">{text}</span>{time&&<span className="pa-working-time">{time}</span>}</p>
}
