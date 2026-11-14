import { useEffect, useState } from "react"
import type * as W from "@clawdline/contract"
import { elapsed } from "./working"
import type { Words } from "./words"

/** Codync's working indicator: the running turn's activity and elapsed time. */
export function WorkingIndicator({turn,activity,w}:{turn:W.ProjectAssistantReadTurnSummary;activity:string;w:Words}) {
  const [now,setNow]=useState(Date.now())
  useEffect(()=>{const timer=setInterval(()=>setNow(Date.now()),1000);return()=>clearInterval(timer)},[])
  const waiting=turn.state==="waiting_permission"
  const text=waiting?w.waiting_permission:activity==="writing"?w.writingReply:activity||w.running
  const time=elapsed(turn.started_at,now)
  return <p className="pa-turn pa-working" data-waiting={waiting||undefined} role="status"><span className="pa-working-dot" aria-hidden="true"/><span className="pa-working-text">{text}</span>{time&&<span className="pa-working-time">{time}</span>}</p>
}
