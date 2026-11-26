import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error Node executes source extensions.
import { botWorkspaceName, capabilityHint, retryableSourceError, settingsReadOnlyReason, shownSourceError } from "./source-errors.ts"
test("a machine without Bots is left out of the roster's errors, a real failure is not",()=>{
  assert.equal(shownSourceError("pa_unsupported"), false)
  assert.equal(shownSourceError("pa_disabled"), false)
  assert.equal(shownSourceError(undefined), false)
  assert.equal(shownSourceError("pa_offline"), true)
  assert.equal(shownSourceError("pa_revoked"), true)
})
// @ts-expect-error Node executes source extensions.
import { readOnlyReason } from "./source-errors.ts"
test("a Bot that cannot be changed says why",()=>{
  const w = { unavailable: "目前無法使用", history_only: "唯讀歷史", readOnlySource: "這台機器關閉了從 Clawdline Cloud 操作", loading: "正在載入" }
  const ok = { state: "online", fresh: true, writable: true, access: "interactive" }
  assert.equal(readOnlyReason(ok, w), "")
  assert.equal(readOnlyReason({ ...ok, state: "offline" }, w), "目前無法使用")
  assert.equal(readOnlyReason({ ...ok, fresh: false }, w), "目前無法使用")
  assert.equal(readOnlyReason({ ...ok, access: "history_only" }, w), "唯讀歷史")
  assert.equal(readOnlyReason({ ...ok, writable: false }, w), "這台機器關閉了從 Clawdline Cloud 操作")
  assert.equal(readOnlyReason(undefined, w), "正在載入")
})
// @ts-expect-error Node executes source extensions.
import { botFailureText, botTurnOutcome } from "./source-errors.ts"
test("a failed Bot action says what to do, never only its code",()=>{
  const w = { failRetry: "再按一次", failOffline: "連不上", failGeneric: "失敗了" } as Record<string,string>
  assert.equal(botFailureText("pa_stale_version", w), "再按一次")
  assert.equal(botFailureText("pa_offline", w), "連不上")
  assert.equal(botFailureText("something_new", w), "失敗了")
})

test("retryable source reads are separate from permanent authority or protocol errors",()=>{
  for(const code of ["pa_offline","unavailable","pa_capacity_full","pa_cursor_expired"])assert.equal(retryableSourceError(code),true)
  for(const code of ["","pa_revoked","pa_scope_denied","pa_unpaired","pa_invalid","pa_unsupported","pa_disabled"])assert.equal(retryableSourceError(code),false)
})

test("an interrupted turn explains a known cause and preserves uncertainty",()=>{
  const w={unknown:"unknown",failed:"failed",failUnknown:"unknown effect",failGeneric:"generic",failTimeout:"time limit",failConnection:"connection lost",failCleanupTimeout:"cleanup timed out",failUsage:"usage exhausted",failNativeUpdate:"invalid Agent activity update",turnUnknownNext:"check results; send follow-up; no automatic replay",failAcknowledged:"follow-up received; earlier result unconfirmed"}
  for(const [field,cause] of [["prompt_timeout","time limit"],["owned_cleanup_timeout","cleanup timed out"],["acp_eof","connection lost"],["provider_usage_limit","usage exhausted"],["native_update","invalid Agent activity update"]]){
    const text=botTurnOutcome({state:"unknown",failure_code:"execution_effect_unknown",failure_field:field},w)
    assert.equal(text,`${cause} ${w.turnUnknownNext}`)
  }
  for(const field of [undefined,"execution","private native error narrative"]){
    assert.equal(botTurnOutcome({state:"unknown",failure_code:"pa_unsupported",failure_field:field},w),w.turnUnknownNext)
  }
  assert.equal(botTurnOutcome({state:"failed",failure_code:"execution_effect_unknown",failure_field:"acknowledged"},w),w.failAcknowledged)
})

test("a workspace without a readable name shows the fallback word, never its raw ID",()=>{
  const w={unnamedProject:"Project"}
  assert.equal(botWorkspaceName("clawdline",w),"clawdline")
  assert.equal(botWorkspaceName(undefined,w),"Project")
  assert.equal(botWorkspaceName("  ",w),"Project")
})

test("a setting a source cannot offer says why instead of always asking for an update",()=>{
  const w={capabilitiesPending:"pending",sourceOffline:"offline"}
  assert.equal(capabilityHint({capabilityGap:()=>"pending"},"update",w),"pending")
  assert.equal(capabilityHint({capabilityGap:()=>"offline"},"update",w),"offline")
  assert.equal(capabilityHint({capabilityGap:()=>"outdated"},"update",w),"update")
  assert.equal(capabilityHint({},"update",w),"update")
  assert.equal(capabilityHint(undefined,"update",w),"update")
})

test("an offline source says it cannot be reached rather than a bare unavailable",()=>{
  const ok={state:"online",fresh:true,writable:true,access:"interactive"}
  const w={unavailable:"目前無法使用",sourceOffline:"暫時連不上這台機器。"}
  assert.equal(readOnlyReason({...ok,state:"offline"},w),"暫時連不上這台機器。")
  assert.equal(readOnlyReason({...ok,fresh:false},w),"目前無法使用")
})

test("read-only Bot settings on Cloud name a pending or offline source before the generic reason",()=>{
  const ok={state:"online",fresh:true,writable:true,access:"interactive"}
  const w={unavailable:"目前無法使用",sourceOffline:"暫時連不上這台機器。",capabilitiesPending:"還沒回報"}
  const pending={capabilityGap:()=>"pending" as const},older={capabilityGap:()=>"outdated" as const}
  assert.equal(settingsReadOnlyReason(true,pending,true,ok,w),"")
  assert.equal(settingsReadOnlyReason(false,pending,true,{...ok,state:"unsupported"},w),"還沒回報")
  assert.equal(settingsReadOnlyReason(false,older,true,ok,w),"目前無法使用")
  assert.equal(settingsReadOnlyReason(false,undefined,false,{...ok,writable:false},{...w,readOnlySource:"唯讀"}),"唯讀")
})
