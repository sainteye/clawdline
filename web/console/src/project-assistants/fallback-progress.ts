import type * as W from "@clawdline/contract"
import type {WordKey} from "./words"

const stages = ["opening", "opened", "sending", "sent", "answered", "closed", "refused_before_send", "held_unknown"] as const
const reasons = ["usage_limit", "out_of_credits", "signed_out", "rate_limited", "overloaded", "model_missing"] as const
/** Only source facts determine this projection. Text and successful cleanup
 * never turn unknown effects into replay authority. Old readers may omit facts. */
export function fallbackProgress(turn:W.ProjectAssistantReadTurnSummary|undefined, primary:W.ProjectAssistantAgent) {
  const latest = new Map<number,W.ProjectAssistantAttemptFact>()
  for(const fact of turn?.attempts??[])latest.set(fact.ordinal,fact)
  const attempts = [...latest.values()].map(fact=>({
    fact,
    stage: (stages.some(stage=>stage===fact.stage)?`fallbackStage_${fact.stage}`:"fallbackStageUnknown") as WordKey,
    reason: (fact.reason?reasons.some(reason=>reason===fact.reason)?`fallbackReason_${fact.reason}`:"fallbackReason_unknown":null) as WordKey|null,
    cleanup: (fact.cleanup_confirmed?"fallbackCleanupConfirmed":"fallbackCleanupPending") as WordKey,
    effect: (fact.effect==="none"?"fallbackEffectNone":"fallbackEffectUnknown") as WordKey,
    carry: (fact.carry_kind?fact.carry_kind==="recap"?"fallbackCarryRecap":fact.carry_kind==="catch_up"?"fallbackCarryCatchUp":"fallbackCarryUnknown":null) as WordKey|null,
  }))
  return {primary,effective:attempts.at(-1)?.fact.agent??primary,attempts}
}
