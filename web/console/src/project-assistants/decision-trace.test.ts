import {test} from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node's strip-types runner uses source extensions.
import {decisionTraceBody,newDecisionAttempt} from "./decision-trace.ts"

test("a decision trace carries identifiers only, never the reply text", () => {
  const body=decisionTraceBody({attempt:newDecisionAttempt(),phase:"failed",bot_id:"bot-a",offer_id:"decision-1",option:"decision-2",idempotency_key:"k 1",code:"先等 7c4e7ff3\nforged",ms:-4.6})
  assert.match(body.attempt,/^dt-[0-9a-f]{12}$/)
  for(const value of [body.bot_id,body.offer_id,body.option,body.idempotency_key,body.code])assert.match(value,/^[A-Za-z0-9._:-]*$/)
  assert.ok(!body.code.includes("先等"))
  assert.equal(body.ms,0)
  assert.equal(decisionTraceBody({attempt:"dt-1",phase:"press",bot_id:"b",offer_id:"o"}).option,"")
})
