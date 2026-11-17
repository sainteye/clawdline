import type * as W from "@clawdline/contract"
import type {Words} from "./words"
import {agentName} from "./fallback-settings"
import {fallbackProgress} from "./fallback-progress"

const observations = [
  ["opened","fallbackOpened"], ["send_started","fallbackSendStarted"],
  ["send_returned","fallbackSendReturned"], ["native_activity","fallbackNativeActivity"],
  ["tool_activity","fallbackToolActivity"], ["answer_observed","fallbackAnswerObserved"],
] as const

export function FallbackProgress({primary,turn,w}:{primary:W.ProjectAssistantAgent;turn?:W.ProjectAssistantReadTurnSummary;w:Words}) {
  const progress=fallbackProgress(turn,primary)
  if(!progress.attempts.length)return null
  return <details className="pa-turn" data-fallback-progress={turn?.id}>
    <summary>{w.fallbackProgress} · {w.fallbackPrimary}: {agentName(primary)} · {w.fallbackEffective}: {agentName(progress.effective)} · {w[progress.attempts[progress.attempts.length-1].stage]}</summary>
    <ol>{progress.attempts.map(({fact,stage,reason,cleanup,effect,carry})=><li key={fact.ordinal}>
      <p>{w.fallbackAttempt} {fact.ordinal} · {agentName(fact.agent)} · {w.loginProfile}: {fact.login_profile??w.loginDefault} · {w[stage]}{reason&&<> · {w[reason]}</>}</p>
      <ul>{observations.filter(([field])=>fact[field]).map(([field,label])=><li key={field}>{w[label]}</li>)}</ul>
      <p>{w[cleanup]} · {w[effect]}</p>
      {(carry||fact.resume_binding_id)&&<div>
        {carry&&<p>{w[carry]} · {w.fallbackWatermark.replace("{seq}",String(fact.carry_after_seq??0))}</p>}
        {fact.resume_binding_id&&<p>{w.fallbackResumeCandidate}</p>}
        <p>{w.fallbackCarryLimit}</p>
      </div>}
    </li>)}</ol>
  </details>
}
