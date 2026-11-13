import {useEffect,useState} from 'react'
import {nextWord as W} from '../next-strings.js'
import {WorkingIndicator} from '../ui/WorkingIndicator.js'
import {WORK_POLL_MS} from '../voice-work/engine.js'
import {activeProgress,type CallProgress} from './copilot-feedback.js'
const labels={saving:'copilotProgressSaving',reading:'copilotProgressReading',responding:'copilotProgressResponding',planning:'copilotProgressPlanning',running:'copilotProgressRunning',queued:'copilotProgressQueued',waiting_batch:'voiceWorkBatchWaiting',waiting_confirmation:'copilotProgressConfirmation',failed:'copilotProgressFailed'} as const
export function CopilotProgress({progress,compact=false}:{progress?:CallProgress;compact?:boolean}) {
 const [now,setNow]=useState(Date.now())
 useEffect(()=>{if(!progress||!activeProgress(progress.phase))return;setNow(Date.now());const timer=setInterval(()=>setNow(Date.now()),WORK_POLL_MS);return()=>clearInterval(timer)},[progress?.phase,progress?.since])
 if(!progress)return null
 const active=activeProgress(progress.phase),seconds=Math.max(0,Math.floor((now-progress.since)/1000))
 const detail=<>{W(labels[progress.phase])}{active&&seconds>0?<span className="copilot-progress-elapsed" aria-hidden="true">{W('copilotProgressElapsed',{seconds})}</span>:null}</>
 return <div className={`copilot-progress${compact?' is-compact':''}${active?' is-active':''}`}>
  {active&&!compact?<WorkingIndicator detail={detail}/>:<p role="status" aria-live="polite">{detail}</p>}
  {!active?<a href="#page=voice-assistant&tab=work">{W('copilotProgressOpen')}</a>:null}
 </div>
}
