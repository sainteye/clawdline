import {WORK_POLL_MS} from '../voice-work/engine.js'
import {useEffect,useState,type ReactNode} from 'react'
import {nextWord} from '../next-strings.js'
import {historySummary,type CallOperation,type OperationOutcome} from './copilot-history.js'

const OUTCOMES:Record<OperationOutcome,Parameters<typeof nextWord>[0]>={running:'copilotHistoryRunning',done:'copilotToolDone',queued:'copilotToolQueued',asked:'copilotToolAsked',many:'copilotToolMany',failed:'copilotToolFailed',cancelled:'copilotHistoryCancelled',cached:'copilotHistoryCached',unavailable:'copilotHistoryUnavailable',unknown:'copilotHistoryUnknown'}
const READ_NAMES:Record<string,Parameters<typeof nextWord>[0]>={read_session_boards:'copilotHistoryBoards',read_session_directory:'copilotHistoryDirectory',read_session_transcript:'copilotHistoryTranscript',read_session_summary:'copilotHistorySummary',read_session_history:'copilotHistoryPast',read_session_progress:'copilotHistoryProgress',read_session_question:'copilotHistoryQuestion',read_session_notes:'copilotHistoryNotes'}
const seconds=(ms:number)=>(ms/1000).toFixed(1)

/** Both the persistent trail and expanded diagnostics read these same operation IDs. */
export function CopilotToolHistory({records,diagnostic=false,actions}:{records:readonly CallOperation[];diagnostic?:boolean;actions?:ReactNode}){
 const [now,setNow]=useState(()=>performance.now())
 const running=records.some(row=>row.outcome==='running')
 // Reuse the workspace's registered refresh interval; recording is event-driven.
 useEffect(()=>{
  if(!running)return
  const timer=setInterval(()=>setNow(performance.now()),WORK_POLL_MS)
  return()=>clearInterval(timer)
 },[running])
 if(!records.length&&!actions)return null
 const current=records.map(row=>row.outcome==='running'?{...row,ms:Math.max(0,now-row.offset),ended:now}:row)
 const stats=historySummary(current)
 const newest=[...current].reverse()
 if(diagnostic)return <div className="copilot-history-diagnostics">{newest.map(row=><div key={row.id} className="log-item" data-operation-id={row.id}>
  <time className="log-time">[{new Date(row.started).toLocaleTimeString(undefined,{hour12:false})}]</time>
  <span className="log-text"><code>{row.name}</code> · {nextWord(row.source==='tool'?'copilotHistoryTool':'copilotHistoryBackground')}{row.target?` · ${row.target}`:''} · {nextWord(OUTCOMES[row.outcome])} · {seconds(row.ms)}s</span>
 </div>)}</div>
 return <section className="copilot-tool-history" aria-label={nextWord('copilotHistoryTitle')} title={`${nextWord('copilotHistoryElapsed',{seconds:seconds(stats.activeMs)})} · ${nextWord('copilotHistoryTimingHint')}`}>
  <div className="copilot-history-heading"><div className="copilot-history-heading-info"><strong>{nextWord('copilotHistoryTitle')}</strong><span>{nextWord('copilotHistoryCounts',{tools:stats.tools,background:stats.background})}</span></div>{actions}</div>
  <ol className="copilot-tools-trail" tabIndex={0} aria-label={nextWord('copilotHistoryEntries')}>
   {newest.map(row=><li key={row.id} data-operation-id={row.id} className={row.outcome}
    title={`${new Date(row.started).toLocaleTimeString(undefined,{hour12:false})} · ${row.name} · ${nextWord(row.source==='tool'?'copilotHistoryTool':'copilotHistoryBackground')}${row.target?` · ${row.target}`:''} · ${nextWord(OUTCOMES[row.outcome])} · ${seconds(row.ms)}s`}>
    <span className="copilot-tool-mark" aria-hidden="true">{row.outcome==='done'||row.outcome==='cached'?'✓':row.outcome==='failed'?'!':row.outcome==='running'?'·':'—'}</span>
    <code>{READ_NAMES[row.name]?nextWord(READ_NAMES[row.name]):row.name}</code>
    {row.target?<span className="copilot-tool-target">{row.target}</span>:null}
    {row.outcome==='done'?null:<span className="copilot-tool-outcome">{nextWord(OUTCOMES[row.outcome])}</span>}
    <span className="copilot-tool-ms">{seconds(row.ms)}s</span>
   </li>)}
  </ol>
 </section>
}
