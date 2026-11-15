import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error Node executes source extensions.
import { activityOf, elapsed, rowActivity, rosterActivity } from "./working.ts"
// @ts-expect-error Node executes source extensions.
import {words} from "./words.ts"
const trace = (category: string, text: string) => ({ turn_id: "t", category, text, complete: false })
test("a running Bot shows its latest tool call, or that it is writing",()=>{
  assert.equal(activityOf(trace("tool_call","Read notes.md") as never),"Read notes.md")
  assert.equal(activityOf(trace("agent_message_chunk","Hel") as never),"writing")
  assert.equal(activityOf(undefined),"")
})

test("roster state is independent of unread and cached source activity",()=>{
  const w=words("zh-Hant"),base={latest_message:"Last reply",activity:"Read notes.md",status:"working",pending_permissions:0,unread_count:3,source:{fresh:true,state:"online",readable:true,access:"interactive"}}
  assert.deepEqual(rosterActivity(base as never,w),{state:"working",caption:"Read notes.md",live:true})
  assert.equal(rosterActivity({...base,activity:"writing"} as never,w).caption,w.writingReply)
  assert.equal(rosterActivity({...base,activity:"thinking"} as never,w).caption,w.thinking)
  assert.equal(rosterActivity({...base,status:"needsInput",activity:"Needs approval: Read notes.md"} as never,w).caption,"需要允許：Read notes.md")
  assert.equal(rosterActivity({...base,status:"idle",activity:""} as never,w).caption,"Last reply")
  for(const source of [{...base.source,fresh:false},{...base.source,state:"offline"},{...base.source,access:"hidden"},{...base.source,readable:false}]) {
    assert.deepEqual(rosterActivity({...base,source} as never,w),{state:"idle",caption:"Last reply",live:false})
  }
})

test("older daemons keep a localized state fallback and unknown effects stay explicit",()=>{
  const w=words("en"),base={latest_message:"Old reply",activity:"running",pending_permissions:1,source:{fresh:true,state:"online",readable:true,access:"interactive"}}
  assert.equal(rosterActivity(base as never,w).caption,w.pending)
  assert.equal(rosterActivity({...base,pending_permissions:0} as never,w).caption,w.running)
  const unknown=rosterActivity({...base,status:"error",failure:{code:"execution_effect_unknown"}} as never,w)
  assert.equal(unknown.state,"error");assert.equal(unknown.caption,w.failUnknown)
})
test("elapsed time reads as Codync's timer and stays empty without a start",()=>{
  const start = "2026-11-14T08:00:00Z", at = Date.parse(start)
  assert.equal(elapsed(start, at + 42_000), "0:42")
  assert.equal(elapsed(start, at + 725_000), "12:05")
  assert.equal(elapsed(start, at + 3_753_000), "1:02:33")
  assert.equal(elapsed("", at), "")
  assert.equal(elapsed(undefined, at), "")
})
test("the roster shows a running Bot's state in the person's language",()=>{
  const w = { running: "工作中", waiting_permission: "等待允許" }
  assert.equal(rowActivity("running", w), "工作中")
  assert.equal(rowActivity("waiting_permission", w), "等待允許")
  assert.equal(rowActivity("", w), "")
})
