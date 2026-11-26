import type * as W from "@clawdline/contract"
import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error Node executes source extensions.
import { activityOf, elapsed, rowActivity, rosterActivity, botRosterActivity, chatMood } from "./working.ts"
// @ts-expect-error Node executes source extensions.
import {words} from "./words.ts"
const trace = (category: string, text: string) => ({ turn_id: "t", category, text, complete: false })
test("a running Bot shows its latest tool call, or that it is writing",()=>{
  assert.equal(activityOf(trace("tool_call","Read notes.md") as never),"Read notes.md")
  assert.equal(activityOf(trace("agent_message_chunk","Hel") as never),"writing")
  assert.equal(activityOf(undefined),"")
})

test("system-generated roster captions use the reader's language",()=>{
  const base={status:"idle",activity:"",pending_permissions:0,source:{fresh:true,state:"online",readable:true,access:"interactive"}}
  const compacted={...base,latest_message_kind:"notice",latest_message:"English fallback",
    latest_message_notice:{key:"compacted",tokens_before:26182,tokens_after:22113}} as unknown as W.ProjectAssistantBotSummary
  const memory={...compacted,status:"needsInput",activity:"Asks: Save this to memory?",pending_decision_kind:"memory_save"} as W.ProjectAssistantBotSummary
  const ordinary={...memory,pending_decision_kind:"question"} as W.ProjectAssistantBotSummary
  const legacyNotice={...compacted,latest_message_notice:undefined,latest_message:"Earlier context was summarized to make room: 345K → 41K"}
  const legacyMemory={...memory,pending_decision_kind:undefined}

  for(const [locale,compaction,proposal,legacy] of [
    ["en","Earlier context was summarized to make room: 26.2K → 22.1K","Save to memory?","Earlier context was summarized to make room: 345K → 41K"],
    ["zh-TW","已摘要較早的內容，騰出空間：26.2K → 22.1K","要記下這條嗎？","已摘要較早的內容，騰出空間：345K → 41K"],
  ]){
    const w=words(locale)
    assert.equal(rosterActivity(compacted,w).caption,compaction)
    assert.equal(rosterActivity(legacyNotice,w).caption,legacy)
    assert.equal(rosterActivity(memory,w).caption,proposal)
    assert.equal(rosterActivity(legacyMemory,w).caption,proposal)
    assert.equal(rosterActivity(ordinary,w).caption,w.decisionActivity.replace("{question}","Save this to memory?"))
    assert.equal(rosterActivity({...legacyNotice,latest_message_kind:"assistant"},w).caption,legacyNotice.latest_message)
  }
})

test("the native approval review has a localized activity label",()=>{
  assert.equal(rowActivity("Guardian Review",words("zh-TW")),"正在審查操作")
  assert.equal(rowActivity("guardian_review",words("en")),"Reviewing an action")
})

test("live trace and daemon activity use the same localized tool captions", () => {
  const cases = [
    ["mcp__clawdline__browser_command", "Using the browser", "正在使用瀏覽器"],
    ["clawdline.browser_status (read)", "Using the browser", "正在使用瀏覽器"],
    ["send_message", "Sending a message", "正在傳送訊息"],
    ["mcp__clawdline__dispatch_work", "Dispatching work", "正在派工"],
    ["mcp__clawdline__edit_memory", "Updating memory", "正在更新記憶"],
    ["Bash", "Running a command", "正在執行指令"],
    ["Read", "Reading a file", "正在讀取檔案"],
    ["Edit", "Editing a file", "正在修改檔案"],
    ["Grep", "Searching", "正在搜尋"],
    ["functions.exec_command", "Running a command", "正在執行指令"],
    ["mcp__other__browser_command", "Using a tool", "正在使用工具"],
    ["mcp__clawdline__browser_command_extra", "Using a tool", "正在使用工具"],
    ["unknown_tool (details)", "Using a tool", "正在使用工具"],
  ] as const
  const working = {
    status:"working", pending_permissions:0,
    source:{fresh:true,state:"online",readable:true,access:"interactive"},
  }
  for (const [title, en, zh] of cases) {
    const activity = activityOf(trace("tool_call", title + "\nin_progress\nTool output"))
    for (const [locale, expected] of [["en",en],["zh-TW",zh]]) {
      const w = words(locale)
      assert.equal(rowActivity(activity,w), expected, `${locale} live trace: ${title}`)
      assert.equal(rosterActivity({...working,activity:title} as W.ProjectAssistantBotSummary,w).caption, expected, `${locale} daemon activity: ${title}`)
    }
  }
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

test("background work replaces the Bot row preview while the chat is idle", () => {
  const w = words("zh-Hant")
  const row = {
    status:"idle", activity:"", latest_message:"Last reply",
    active_work:5, pending_permissions:0,
    source:{fresh:true,state:"online",readable:true,access:"interactive"},
  } as W.ProjectAssistantBotSummary
  const queued = {...row,background_work:{working:0,waiting:1,needs_input:0}}
  assert.deepEqual(botRosterActivity(queued,w),{state:"working",caption:"1 等待中",live:true,queuedOnly:true})
  const working = {...row,background_work:{working:2,waiting:1,needs_input:0}}
  assert.deepEqual(botRosterActivity(working,w),{state:"working",caption:"2 進行中 · 1 等待中",live:true,queuedOnly:false})
  assert.equal(botRosterActivity({...working,status:"working",activity:"Thinking"},w).caption,"Thinking")
  const waiting = {...working,status:"working",activity:"Thinking",background_work:{...working.background_work,needs_input:1}}
  assert.deepEqual(botRosterActivity(waiting,w),{state:"needsInput",caption:"需要你",live:true,queuedOnly:false})
  assert.equal(botRosterActivity({...waiting,status:"needsInput",activity:"Needs approval: Run tests"},w).caption,"需要允許：Run tests")
  assert.equal(botRosterActivity({...working,source:{...row.source,fresh:false}},w).caption,"Last reply")
  assert.equal(botRosterActivity(row,w).caption,"Last reply", "a legacy total can include unrelated Board work")
  assert.equal(chatMood(row.source,undefined,0),"idle")
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

test("chat avatars require a current readable source and a live turn or pending input",()=>{
  const source={machine_id:"machine",fresh:true,state:"online",readable:true,access:"interactive"} as W.ProjectAssistantSourceStatus
  assert.equal(chatMood(source,{state:"running"} as never,0),"working")
  assert.equal(chatMood(source,{state:"waiting_permission"} as never,0),"needsInput")
  assert.equal(chatMood(source,{state:"running"} as never,1),"needsInput")
  assert.equal(chatMood(source,undefined,0),"idle")
  assert.equal(chatMood(source,{state:"succeeded"} as never,0),"idle")
  for(const delta of [{fresh:false},{state:"offline"},{readable:false},{access:"hidden"}])assert.equal(chatMood({...source,...delta} as never,{state:"running"} as never,1),"idle")
})

test("a quiet chat's avatar follows the Bot's live background threads",()=>{
  const source={machine_id:"machine",fresh:true,state:"online",readable:true,access:"interactive"} as W.ProjectAssistantSourceStatus
  const task=(state:string)=>({state}) as W.ProjectAssistantBackgroundTaskSummary
  assert.equal(chatMood(source,undefined,0,[task("active")]),"working")
  assert.equal(chatMood(source,undefined,0,[task("active"),task("waiting_permission")]),"needsInput")
  assert.equal(chatMood(source,undefined,0,[task("waiting"),task("succeeded")]),"idle")
  assert.equal(chatMood(source,{state:"waiting_permission"} as never,0,[task("active")]),"needsInput")
  assert.equal(chatMood({...source,fresh:false},undefined,0,[task("active")]),"idle")
})

test("tool detail stays out of the activity caption and thought events become thinking",()=>{
  assert.equal(activityOf(trace("tool_call","Read source\ncompleted\nSource found")),"Read source")
  assert.equal(activityOf(trace("agent_thought_chunk","Check source")),"thinking")
})
