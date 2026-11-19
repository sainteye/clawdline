// Adapted from Codync 9f67eb09084db61b3cca9c84a11bc03df9de29b7.
// Copyright 2026 Po Kai Lee and the Codync contributors. Apache-2.0.
// See third_party/codync/{LICENSE,NOTICE}.
import {useLayoutEffect,useRef,useState,type ReactNode} from "react"
import type * as W from "@clawdline/contract"
import type {Words} from "./words"
import {useReduceMotion} from "./codync-rendering"
import {WorkingIndicator} from "./WorkingIndicator"

/** Keep the last activity and its space until exit finishes; a quick restart
 * reverses the same transition. The initial snapshot has no entrance motion. */
export function WorkingTransition({turn,activity,steps,stopStatus,waiting,live,ready,w}:{turn?:W.ProjectAssistantReadTurnSummary;activity:string;steps?:(expanded:boolean)=>ReactNode;stopStatus?:string;waiting:boolean;live:boolean;ready:boolean;w:Words}) {
  const active=!!turn&&live,reduce=useReduceMotion()
  const [mounted,setMounted]=useState(active),[shown,setShown]=useState(active)
  const initialized=useRef(ready),last=useRef<{turn:W.ProjectAssistantReadTurnSummary;activity:string;steps?:(expanded:boolean)=>ReactNode;stopStatus?:string;waiting:boolean}|null>(null)
  const indicator=active?{turn,activity,steps,stopStatus,waiting}:last.current
  useLayoutEffect(()=>{if(active)last.current={turn,activity,steps,stopStatus,waiting}},[active,turn,activity,steps,stopStatus,waiting])
  useLayoutEffect(()=>{
    if(!ready)return
    if(!initialized.current||reduce){initialized.current=true;setMounted(active);setShown(active);return}
    if(active){
      setMounted(true)
      let reveal=0
      const frame=requestAnimationFrame(()=>{reveal=requestAnimationFrame(()=>setShown(true))})
      return()=>{cancelAnimationFrame(frame);cancelAnimationFrame(reveal)}
    }
    setShown(false)
    // Matches --pa-working-duration; this is a presentation duration, not a read deadline.
    const timer=window.setTimeout(()=>setMounted(false),420)
    return()=>clearTimeout(timer)
  },[active,ready,reduce])
  return <div className="pa-working-transition" data-shown={shown||undefined} aria-hidden={!active} inert={!active}>
    <div className="pa-working-clip"><div className="pa-working-content">
      {(active||mounted)&&indicator&&<WorkingIndicator key={indicator.turn.id} turn={indicator.turn} activity={indicator.activity} steps={indicator.steps} stopStatus={indicator.stopStatus} waiting={indicator.waiting} animated={active} w={w}/>}
    </div></div>
  </div>
}
