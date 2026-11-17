import {test} from "node:test"
import assert from "node:assert/strict"
import type * as W from "@clawdline/contract"
// @ts-expect-error -- Node loads source extensions.
import {fallbackProgress} from "./fallback-progress.ts"
// @ts-expect-error -- Node loads source extensions.
import {turnSummary,validateAttempts} from "./read-views.ts"

const fence:W.ProjectAssistantFence={key:{machine_id:"machine",bot_id:"bot"},project:{machine_id:"machine",project_id:"project",canonical_id:"canonical",kind:"repository"},conversation_id:"chat",binding_id:"binding",generation:1}
const fact:W.ProjectAssistantAttemptFact={ordinal:1,agent:"codex",fence,stage:"sent",send_started:true,effect:"unknown",cleanup_confirmed:false,opened:true,send_returned:true,carry_kind:"catch_up",carry_after_seq:12,resume_binding_id:"historical-binding"}
const turn:W.ProjectAssistantReadTurnSummary={id:"turn",fence,message_id:"input",state:"running",failure_code:"",started_at:"",attempts:[fact]}

test("Send return is waiting for an answer, independent of primary identity",()=>{
  const view=fallbackProgress(turn,"claude")
  assert.equal(view.primary,"claude");assert.equal(view.effective,"codex")
  assert.equal(view.attempts[0].stage,"fallbackStage_sent")
  assert.equal(view.attempts[0].effect,"fallbackEffectUnknown")
  assert.equal(view.attempts[0].fact.answer_observed,undefined)
  assert.equal(view.attempts[0].carry,"fallbackCarryCatchUp")
  assert.equal(view.attempts[0].fact.carry_after_seq,12)
})

test("latest immutable fact per attempt keeps cleanup separate from unknown effects",()=>{
  const closed={...fact,stage:"closed",cleanup_confirmed:true,answer_observed:true,tool_activity:true}
  const view=fallbackProgress({...turn,attempts:[fact,closed]},"claude")
  assert.equal(view.attempts.length,1)
  assert.equal(view.attempts[0].stage,"fallbackStage_closed")
  assert.equal(view.attempts[0].cleanup,"fallbackCleanupConfirmed")
  assert.equal(view.attempts[0].effect,"fallbackEffectUnknown")
  assert.equal(view.attempts[0].fact.tool_activity,true)
})

test("old optional facts remain absent; unfamiliar stage and reason stay normalized",()=>{
  assert.deepEqual(fallbackProgress({...turn,attempts:undefined},"claude").attempts,[])
  const view=fallbackProgress({...turn,attempts:[{...fact,stage:"provider says complete",reason:"repeat it",effect:"provider says safe",carry_kind:"future"}]},"claude")
  assert.equal(view.attempts[0].stage,"fallbackStageUnknown")
  assert.equal(view.attempts[0].reason,"fallbackReason_unknown")
  assert.equal(view.attempts[0].effect,"fallbackEffectUnknown")
  assert.equal(view.attempts[0].carry,"fallbackCarryUnknown")
})

test("event-to-summary keeps progress; snapshot validator rejects malformed or foreign facts",()=>{
  const full={...turn,idempotency_key:"request",memory_revision:1,instructions:{text:"",revision:1,role_snapshot_ref:""}} as W.ProjectAssistantTurn
  assert.deepEqual(turnSummary(full).attempts,[fact])
  validateAttempts([fact],fence)
  for(const change of [{opened:"true"},{carry_after_seq:-1},{send_returned:1},{fence:{...fence,key:{...fence.key,bot_id:"foreign"}}}]) {
    assert.throws(()=>validateAttempts([{...fact,...change}] as W.ProjectAssistantAttemptFact[],fence))
  }
})
