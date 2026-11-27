import { useRef, useState } from "react"
import type { ProjectAssistantReadTurnSummary, ProjectAssistantReadViewHead } from "@clawdline/contract"
import { botFailureText, botTurnReason } from "./source-errors"
import type { Words } from "./words"

export function TurnInterruption({turn,w,onProgress}: {
  turn: ProjectAssistantReadTurnSummary
  w: Words
  onProgress?: () => void
}) {
  const unknown=turn.state==="unknown"
  if(!turn.failure_code&&!unknown) return <p className="pa-turn">{w[turn.state]??w.failed}</p>

  if ((turn.failure_field==="restart"||turn.failure_field==="restart_confirmed") && unknown) return <div className="pa-restart-state">
    <span role="status">{w.restartReviewNeeded}</span>
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
    {turn.failure_code&&<div className="pa-interruption-actions">
      <details>
        <summary>{w.details}</summary>
        <code>{[turn.failure_code,turn.failure_field].filter(Boolean).join(" · ")}</code>
      </details>
    </div>}
  </div>
}

export function useTurnReviewDismissal({draftKey,head,turns,restartInterruption,visible}: {
  draftKey: string
  head: ProjectAssistantReadViewHead|null
  turns: readonly ProjectAssistantReadTurnSummary[]
  restartInterruption?: ProjectAssistantReadTurnSummary
  visible: boolean
}) {
  const storageKey="clawdline.bot-turn-review-dismissed."+draftKey
  const [dismissed,setDismissed]=useState(()=>{
    try{return localStorage.getItem(storageKey)}catch{return null}
  })
  const interruptedTurn=restartInterruption??turns.find(turn=>turn.state==="unknown")
  const identity=head&&JSON.stringify([head.conversation.id,head.unknown_turn_count,interruptedTurn?.id??head.conversation.last_message_seq])
  const clearTurnReview=()=>{
    if(!identity)return
    setDismissed(identity)
    try{localStorage.setItem(storageKey,identity)}catch{/* The current view can still dismiss the notice. */}
  }
  const showTurnReview=visible&&!!head?.unknown_turn_count&&dismissed!==identity
  return {showTurnReview,clearTurnReview}
}

export function TurnReviewNotice({w,restarted,queued,enabled,canRequest,pending,onReview,onClear}: {
  w: Words
  restarted: boolean
  queued: number
  enabled: boolean
  canRequest: boolean
  pending: boolean
  onReview: () => Promise<"accepted"|"refused"|"unknown">
  onClear: () => void
}) {
  const [state,setState]=useState<"sending"|"accepted"|"refused"|"unknown"|null>(null)
  const sending=useRef(false)
  const request=async()=>{
    if(sending.current||!canRequest||pending)return
    sending.current=true
    setState("sending")
    try{setState(await onReview())}finally{sending.current=false}
  }
  return <div className="pa-turn-review">
    <div role="status">
      <strong>{w.turnReviewTitle}</strong>
      <p>{restarted?w.turnReviewRestart:w.turnReviewUnknown}</p>
      {queued>0&&<p>{w.turnReviewQueue.replace("{count}",String(queued))}</p>}
      {state==="accepted"&&<p>{w.turnReviewAccepted}</p>}
      {state==="refused"&&<p>{w.turnReviewRefused}</p>}
      {pending&&state!=="sending"&&<p>{w.turnReviewUnconfirmed}</p>}
    </div>
    <div className="pa-turn-review-actions">
      {enabled&&state!=="accepted"&&(!pending||state==="sending")&&
        <button type="button" className="pa-pill" disabled={!canRequest||state==="sending"}
          aria-busy={state==="sending"||undefined} onClick={()=>void request()}>
          {state==="sending"?w.turnReviewSending:w.turnReviewAction}
        </button>}
      <button type="button" className="pa-pill pa-turn-review-clear" onClick={onClear}>{w.turnReviewClear}</button>
    </div>
  </div>
}
