import { test } from "node:test"
import assert from "node:assert/strict"
import type { SessionProtocol } from "@clawdline/contract"
// @ts-expect-error -- node's strip-types runner.
import { protocolStateHTML } from "./protocol-state.ts"

const escape = (text:string) => text.replaceAll("&","&amp;").replaceAll("<","&lt;").replaceAll('"',"&quot;")
const render = (state: Partial<SessionProtocol> & {phase:string}) => protocolStateHTML({sequence:1,unrecognized:0,trace_enabled:false,rpc_error:null,...state}, (key:string)=>key, escape, (seconds:number)=>String(seconds))

test("old output and a cancel receipt never render completed or idle", () => {
  const pending = render({phase:"prompt_pending", last_received_at:1e9})
  assert.match(pending,/promptPending/)
  assert.doesNotMatch(pending,/stopReason|idle/)
  const cancelled = render({phase:"cancel_requested", last_received_at:1e9, trace_enabled:true})
  assert.match(cancelled,/cancelRequested/)
  assert.match(cancelled,/traceEnabled/)
  assert.doesNotMatch(cancelled,/stopReason|idle/)
})

test("official completion, permission and missing evidence remain distinct", () => {
  assert.match(render({phase:"permission_pending"}),/permissionPending/)
  assert.match(render({phase:"not_connected"}),/notConnected/)
  assert.match(render({phase:"idle", completed_at:1,stop_reason:"cancelled"}),/stopReason · cancelled/)
  assert.match(render({phase:"closed",trace_enabled:true,trace_error:"trace_write_failed"}),/traceGap/)
})

test("provider stop reasons are displayed as text", () => {
  const html=render({phase:"idle",completed_at:1,stop_reason:'<img src=x onerror="bad">'})
  assert.doesNotMatch(html,/<img/)
  assert.match(html,/&lt;img/)
})


test("quota and control requests do not impersonate user turn completion", () => {
  assert.match(render({phase:"control_pending",operation:"session/set_mode"}),/controlPending/)
  assert.match(render({phase:"prompt_pending",operation:"usage"}),/quotaPending/)
  assert.doesNotMatch(render({phase:"idle",operation:"usage",completed_at:1,stop_reason:"end_turn"}),/stopReason/)
  assert.match(render({phase:"request_failed",rpc_error:-32800}),/requestFailed · -32800/)
})
