import assert from "node:assert/strict"
import { test } from "node:test"
import { createRequire } from "node:module"
import { fileURLToPath } from "node:url"
import { build } from "esbuild"
import React from "react"
import { renderToStaticMarkup } from "react-dom/server"

const {outputFiles}=await build({stdin:{contents:'export {BotRow} from "./BotRow";export {DispatchedWorkStrip,dispatchedOpenable,openDispatchedSession} from "./DispatchedWorkStrip";export {words} from "./words"',resolveDir:fileURLToPath(new URL(".",import.meta.url)),loader:"ts"},bundle:true,platform:"node",format:"cjs",packages:"external",write:false,jsx:"automatic",loader:{".css":"empty"}})
const compiled={exports:{}}
new Function("require","module","exports",outputFiles[0].text)(createRequire(import.meta.url),compiled,compiled.exports)
const {BotRow,DispatchedWorkStrip,dispatchedOpenable,openDispatchedSession,words}=compiled.exports
const w=words("zh-Hant"),render=(c,p)=>renderToStaticMarkup(React.createElement(c,p))
const live={fresh:true,state:"online",readable:true,writable:true,access:"interactive"}
const item=extra=>({bot:{key:{machine_id:"m",bot_id:"atlas"},name:"Atlas",project:{kind:"repository",machine_id:"m",project_id:"p"},appearance:{shape:"blob",color:"#5b8def"},agent:"claude"},status:"idle",activity:"",latest_message:"Last reply",activity_at:"2026-11-23T09:00:00Z",pending_permissions:0,unread_count:0,source:live,...extra})
const rowProps=(summary,showDispatched=true)=>({item:summary,selected:false,onSelect(){},w,language:"zh-Hant",now:Date.parse("2026-11-23T10:00:00Z"),projectName:"clawdline",showDispatched})

test("a roster row shows its clone count under the time only when Sessions are running", () => {
  const busy=render(BotRow,rowProps(item({dispatched_work:{running:2,needs_you:1,delivered:1}})))
  assert.match(busy,/class="pa-clones"[^>]*>.*?3<\/span>/)
  assert.match(busy,/data-status="needsInput"/)
  assert.match(busy,/aria-label="[^"]*3 個分身在跑/)
  const waiting=render(BotRow,rowProps(item({dispatched_work:{running:2,needs_you:0,delivered:5}})))
  assert.match(waiting,/data-status="waitingOnWork"/)
  assert.match(waiting,/data-mood="waitingOnWork"/)
  assert.match(waiting,/等 2 個分身完成/)
  assert.doesNotMatch(waiting,/待整合/)
  for(const quiet of [item({}),item({dispatched_work:{running:0,needs_you:0,delivered:3}})]){
    assert.doesNotMatch(render(BotRow,rowProps(quiet)),/pa-clones/)
  }
})

test("with the setting off a row renders exactly as it does without dispatched work", () => {
  const plain=render(BotRow,rowProps(item({}),false))
  assert.equal(render(BotRow,rowProps(item({dispatched_work:{running:2,executing:1,needs_you:1,delivered:1}}),false)),plain)
})

test("a roster preview uses the reply orb only while a dispatched Session is executing", () => {
  const previewOrb = summary => render(BotRow, rowProps(summary))
    .match(/class="pa-row-preview"[^>]*>(<canvas[^>]*><\/canvas>)/)?.[1]
  const replyOrb = previewOrb(item({status:"working", activity:"writing"}))
  assert.ok(replyOrb)
  assert.equal(previewOrb(item({dispatched_work:{running:2, executing:1, needs_you:0, delivered:4}})), replyOrb)
  for (const dispatched_work of [
    {running:2, executing:0, needs_you:0, delivered:4},
    {running:0, executing:0, needs_you:0, delivered:4},
    {running:2, needs_you:0, delivered:4},
  ]) {
    assert.equal(previewOrb(item({dispatched_work})), undefined)
  }
  assert.equal(previewOrb(item({
    dispatched_work:{running:1, executing:1, needs_you:0, delivered:0}, source:{...live, fresh:false},
  })), undefined)
})

test("the chat strip animates executing work and keeps the clone mark for other states", () => {
  const queued = {id:"q", task_id:"q", state:"accepted"}
  const running = {...queued, state:"executing"}
  const needsYou = {id:"n", task_id:"n", state:"waiting_for_person"}
  const active = render(DispatchedWorkStrip, {links:[queued], work:{running:2, executing:1, needs_you:0, delivered:4}, w})
  assert.match(active, /<canvas/)
  assert.doesNotMatch(active, /pa-clone-mark/)
  for (const props of [
    {links:[running], work:{running:1, executing:0, needs_you:0, delivered:0}},
    {links:[running], work:{running:1, needs_you:0, delivered:0}},
    {links:[running]},
    {links:[needsYou]},
  ]) {
    const html = render(DispatchedWorkStrip, {...props, w})
    assert.match(html, /pa-clone-mark/)
    assert.doesNotMatch(html, /<canvas/)
  }
})

test("the chat strip takes no space without work, keeps the background capsule, and opens the Session", () => {
  const capsule=React.createElement("span",{className:"capsule"},"bg")
  assert.equal(render(DispatchedWorkStrip,{links:[],w,background:capsule}),'<span class="capsule">bg</span>')
  assert.equal(render(DispatchedWorkStrip,{links:[],w}),"")
  const html=render(DispatchedWorkStrip,{links:[{id:"a",task_id:"a",state:"executing",title:"Build",started_at:"2026-11-23T09:00:00Z"},{id:"b",task_id:"b",state:"executing"}],w,background:capsule})
  assert.match(html,/class="capsule"/)
  assert.match(html,/aria-expanded="false"/)
  assert.match(html,/2 個工作在跑/)
  // With the roster's count, the strip says what the roster row says.
  const counted=render(DispatchedWorkStrip,{links:[{id:"r",task_id:"r",state:"executing"},{id:"d",task_id:"d",state:"completed",landing:"pending"}],work:{running:1,needs_you:0,delivered:14},w})
  assert.match(counted,/1 個工作在跑/)
  assert.doesNotMatch(counted,/待整合/)
  const delivered = {id:"d", task_id:"d", state:"completed", landing:"pending"}
  assert.equal(render(DispatchedWorkStrip,{links:[delivered], work:{running:0,needs_you:0,delivered:14}, w}), "")
  assert.equal(render(DispatchedWorkStrip,{links:[delivered], w, background:capsule}), '<span class="capsule">bg</span>')
  const calls=[]
  const open=(link,view)=>calls.push([link.id,view])
  openDispatchedSession({id:"linked",worker_terminal:"trm_a&b",worker_session:"conversation-a"},open)
  openDispatchedSession({id:"unbound",worker_terminal:"trm_a&b"},open)
  openDispatchedSession({id:"unopened"},open)
  assert.deepEqual(calls,[["linked","session"],["unbound",undefined],["unopened",undefined]])
})

test("a dispatched row with nothing to open says so instead of being a dead button", () => {
  assert.equal(dispatchedOpenable({worker_terminal:"t",worker_session:"s"}),true)
  assert.equal(dispatchedOpenable({board_item_id:"item"}),true)
  assert.equal(dispatchedOpenable({worker_terminal:"t"}),false)
  assert.equal(dispatchedOpenable({}),false)
  // The expanded list starts closed; render its rows through the exported pieces.
  const zh=words("zh-TW")
  assert.equal(zh.dispatchedNoSession,"還沒有可開啟的 Session")
  assert.equal(words("en").dispatchedNoSession,"no Session to open yet")
})
