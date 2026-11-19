import { useEffect, useRef, useState, type ReactNode } from "react"
import type * as W from "@clawdline/contract"
import { elapsed } from "./working"
import type { Words } from "./words"
import { ThinkingOrb } from "./ThinkingOrb"

export function WorkingIndicator({turn,activity,steps,stopStatus,w,animated=true,waiting=turn.state==="waiting_permission"}:{
  turn:W.ProjectAssistantReadTurnSummary;activity:string;steps?:(expanded:boolean)=>ReactNode;stopStatus?:string;w:Words;animated?:boolean;waiting?:boolean
}) {
  const [now,setNow]=useState(Date.now())
  const [expanded,setExpanded]=useState(false)
  const [opened,setOpened]=useState(false)
  const scroller=useRef<HTMLDivElement>(null),content=useRef<HTMLDivElement>(null),follow=useRef(true)
  useEffect(()=>{
    const panel=scroller.current,body=content.current
    if(!expanded||!panel||!body)return
    follow.current=true
    const observer=new ResizeObserver(()=>{if(follow.current)panel.scrollTop=panel.scrollHeight})
    observer.observe(body)
    return()=>observer.disconnect()
  },[expanded,turn.id])
  useEffect(()=>{
    if(!animated)return
    const timer=setInterval(()=>setNow(Date.now()),1000)
    return()=>clearInterval(timer)
  },[animated])
  const text=stopStatus||(waiting?w.waiting_permission:activity==="writing"?w.writingReply:activity==="thinking"?w.thinking:activity||w.running)
  const time=elapsed(turn.started_at,now)
  return <details className="pa-working-steps pa-flow-disclosure" onToggle={event=>{
    const open=event.currentTarget.open
    if(open)setOpened(true)
    setExpanded(open)
  }}>
    <summary className="pa-turn pa-working" data-waiting={waiting||undefined} title={w.trace}>
      <ThinkingOrb state={waiting?"listening":"working"} size={16} color={waiting?"var(--warning,#F0A030)":"var(--pa-dim,#999)"} animated={animated}/>
      <span className="pa-working-text" role="status">{text}</span>
      {time&&<span className="pa-working-time" aria-hidden="true">{time}</span>}
      <svg className="pa-working-chevron" viewBox="0 0 12 12" width="12" height="12" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="m4.5 2.5 3.5 3.5-3.5 3.5"/></svg>
    </summary>
    <div ref={scroller} className="pa-working-steps-body" onScroll={event=>{
      const node=event.currentTarget
      follow.current=node.scrollHeight-node.scrollTop-node.clientHeight<8
    }}><div ref={content}>{opened&&(steps?steps(expanded):<p>{w.workNoSteps}</p>)}</div></div>
  </details>
}
