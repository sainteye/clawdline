import type * as W from "@clawdline/contract"
import {agentName} from "./fallback-settings"
import {botTurnOutcome} from "./source-errors"
import type {Words} from "./words"

const stages = ["opening", "opened", "refused_before_send", "sending", "sent", "answered", "closed", "held_unknown"] as const
const reasons = ["usage_limit", "out_of_credits", "signed_out", "rate_limited", "overloaded", "model_missing"] as const

function reasonLabel(reason: string | undefined, w: Words) {
  const known = reasons.find(value => reason === value)
  return known ? w[`fallbackReason_${known}`] : undefined
}

function stageLabel(attempt: W.ProjectAssistantRoomAttempt, finished: boolean, w: Words) {
  // A closed attempt retains a reason only through the source's pre-Send refusal path.
  if (attempt.stage === "closed" && reasonLabel(attempt.reason, w)) return w.fallbackStage_refused_before_send
  if (attempt.stage === "answered" || attempt.stage === "closed" && finished) return w.roomAttemptFinished
  const known = stages.find(value => attempt.stage === value)
  return known ? w[`fallbackStage_${known}`] : w.fallbackStageUnknown
}

export function showGroupMemberOutcome(outcome: W.ProjectAssistantRoomMemberOutcome) {
  return outcome.disposition === "quiet" || outcome.state === "failed" ||
    outcome.state === "unknown" || outcome.state === "cancelled" ||
    isWaitingForLateReply(outcome) ||
    !!outcome.attempts?.some(attempt => attempt.fallback || attempt.reason)
}

function isWaitingForLateReply(outcome: W.ProjectAssistantRoomMemberOutcome) {
  return outcome.late === true && ["accepted", "queued", "running", "waiting_permission"].includes(outcome.state)
}

export function GroupMemberOutcome({outcome, w}: {outcome: W.ProjectAssistantRoomMemberOutcome; w: Words}) {
  if (!showGroupMemberOutcome(outcome)) return null
  const state = outcome.state === "unknown" || outcome.failure_field === "acknowledged" ? "roomOutcomeUnknown" :
    outcome.disposition === "quiet" ? "roomOutcomeQuiet" :
    outcome.disposition === "published" ? "roomOutcomePublished" : outcome.state
  const explanation = outcome.failure_field ? botTurnOutcome({
    state: "failed",
    failure_code: outcome.failure_code || "pa_unsupported",
    failure_field: outcome.failure_field,
  }, w) : outcome.failure_code === "pa_offline" ? w.roomOutcomeUnavailable :
    outcome.failure_code === "pa_scope_denied" || outcome.failure_code === "pa_revoked" ? w.roomOutcomeRefused :
    outcome.failure_code === "pa_capacity_full" ? w.failFull : ""
  const latest = outcome.attempts?.at(-1)
  const progress = latest && outcome.attempts?.some(attempt => attempt.fallback || attempt.reason)
  const finished = outcome.state === "succeeded"
  return <div className="pa-turn" role="status" data-room-member-outcome={outcome.turn_id} data-outcome-state={outcome.state}>
    <strong>{outcome.name}</strong> · {w[state]}
    {isWaitingForLateReply(outcome) && <><br />{w.roomOutcomeLate}</>}
    {explanation && <><br />{explanation}</>}
    {progress && <details>
      <summary>{w.fallbackProgress} · {latest.fallback ? `${w.roomFallbackAttempt} · ` : ""}{agentName(latest.agent)} · {stageLabel(latest, finished, w)}</summary>
      <ol>{outcome.attempts?.map(attempt => {
        const reason = reasonLabel(attempt.reason, w)
        const rest = reasonLabel(attempt.primary_rest_reason, w)
        return <li key={attempt.ordinal} value={attempt.ordinal}>
          {agentName(attempt.agent)} · {stageLabel(attempt, attempt === latest && finished, w)}
          {reason && <> · {reason}</>}
          {rest && <><br />{w.roomFallbackRestReason.replace("{reason}", rest)}</>}
        </li>
      })}</ol>
    </details>}
  </div>
}
