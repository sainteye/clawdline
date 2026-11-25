import assert from "node:assert/strict"
import test from "node:test"
import type * as W from "@clawdline/contract"
// @ts-expect-error -- Node's strip-types runner uses source extensions.
import {chatEventDelay, chatPushSignal} from "./chat-push.ts"

const summary=(over:Partial<W.ProjectAssistantBotSummary>={})=>({version:4,activity_at:"2026-11-25T00:00:00Z",unread_count:0,latest_message:"hi",status:"idle",activity:"",pending_permissions:0,...over}) as W.ProjectAssistantBotSummary

test("a connected live roster turns the idle chat timer into a slow fallback",()=>{
  assert.equal(chatEventDelay(true,false),15000)
})

test("without the live roster, or while a send awaits its receipt, the chat keeps its 3 s cadence",()=>{
  assert.equal(chatEventDelay(false,false),3000)
  assert.equal(chatEventDelay(true,true),3000)
  assert.equal(chatEventDelay(false,true),3000)
})

test("a repeated roster frame for the same state asks for no further read",()=>{
  assert.equal(chatPushSignal(summary()),chatPushSignal(summary()))
})

test("a committed reply changes the signal even when the summary text repeats",()=>{
  assert.notEqual(chatPushSignal(summary()),chatPushSignal(summary({version:5})))
  assert.notEqual(chatPushSignal(summary()),chatPushSignal(summary({unread_count:1})))
})
