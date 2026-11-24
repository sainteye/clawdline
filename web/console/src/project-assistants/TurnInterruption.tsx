import type { ProjectAssistantReadTurnSummary } from "@clawdline/contract"
import { botFailureText, botTurnReason } from "./source-errors"
import type { Words } from "./words"

export function TurnInterruption({turn,w,checking,onCheck,queueCount=0,onProgress}: {
  turn: ProjectAssistantReadTurnSummary
  w: Words
  checking: boolean
  onCheck?: () => Promise<void>
  queueCount?: number
  onProgress?: () => void
}) {
  const unknown=turn.state==="unknown"
  if(!turn.failure_code&&!unknown) return <p className="pa-turn">{w[turn.state]??w.failed}</p>

  if ((turn.failure_field==="restart"||turn.failure_field==="restart_confirmed") && unknown) return <div className="pa-restart-state">
    <span role="status">{w.restartReviewNeeded}{queueCount>0&&` · ${w.restartQueueKept.replace("{count}",String(queueCount))}`}</span>
    <details>
      <summary>{w.details}</summary>
      <p>{w.turnRepliesKept} {w.turnUnknownNext}</p>
      <code>{[turn.failure_code,turn.failure_field].filter(Boolean).join(" · ")}</code>
      {onProgress&&<button className="pa-pill" type="button" onClick={onProgress}>{w.restartReviewProgress}</button>}
    </details>
  </div>
  const title=botTurnReason(turn.failure_field,w)??(unknown?w.turnUnconfirmed:botFailureText(turn.failure_code,w))
  return <div className="pa-interruption">
    <div className="pa-interruption-copy" role="status">
      <span className="pa-interruption-icon" aria-hidden="true">!</span>
      <div>
        <strong>{title}</strong>
        {unknown&&<p>{w.turnRepliesKept}</p>}
      </div>
    </div>
    {(turn.failure_code||onCheck)&&<div className="pa-interruption-actions">
      {turn.failure_code&&<details>
        <summary>{w.details}</summary>
        <code>{[turn.failure_code,turn.failure_field].filter(Boolean).join(" · ")}</code>
      </details>}
      {onCheck&&<button type="button" onClick={()=>void onCheck()} disabled={checking} aria-busy={checking||undefined}>{w.refresh}</button>}
    </div>}
  </div>
}
