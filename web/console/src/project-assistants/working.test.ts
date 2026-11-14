import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error Node executes source extensions.
import { activityOf, elapsed, rowActivity } from "./working.ts"
const trace = (category: string, text: string) => ({ turn_id: "t", category, text, complete: false })
test("a running Bot shows its latest tool call, or that it is writing",()=>{
  assert.equal(activityOf(trace("tool_call","Read notes.md") as never),"Read notes.md")
  assert.equal(activityOf(trace("agent_message_chunk","Hel") as never),"writing")
  assert.equal(activityOf(undefined),"")
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
