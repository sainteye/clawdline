import assert from "node:assert/strict"
import { test } from "node:test"
import { createRequire } from "node:module"
import { fileURLToPath } from "node:url"
import { build } from "esbuild"
import React from "react"
import { renderToStaticMarkup } from "react-dom/server"

const {outputFiles}=await build({stdin:{contents:'export {BotRequestRow,BotRequestLine,BotRequestDetail} from "./BotRequestCard";export {words} from "./words"',resolveDir:fileURLToPath(new URL(".",import.meta.url)),loader:"ts"},bundle:true,platform:"node",format:"cjs",write:false,jsx:"automatic",external:["react","react-dom","react/jsx-runtime"],loader:{".css":"empty"}})
const compiled={exports:{}}
new Function("require","module","exports",outputFiles[0].text)(createRequire(import.meta.url),compiled,compiled.exports)
const {BotRequestRow,BotRequestLine,BotRequestDetail,words}=compiled.exports
const w=words("zh-Hant"),render=(c,p)=>renderToStaticMarkup(React.createElement(c,p))
const now=Date.parse("2026-11-24T12:00:00Z")
const bots=[{key:{machine_id:"m",bot_id:"atlas"},name:"Atlas",appearance:{shape:"blob",color:"#5b8def"}},{key:{machine_id:"m",bot_id:"nova"},name:"Nova",appearance:{shape:"hex",color:"#5b8def"}}]
const card=extra=>({id:"c1",title:"發布十一月報告",project_id:"p",project_path:"/p",created_by:"atlas",created_at:"2026-11-24T08:00:00Z",updated_at:"2026-11-24T11:55:00Z",authority:[],baton:{bot:"atlas",role:"maker"},chain:["atlas"],checklist:[],cycle:1,done:0,total:0,progress_state:"in_progress",state:"open",version:1,events:[],...extra})
const row=extra=>render(BotRequestRow,{card:card(extra),bots,w,now,onOpen(){}})
const detail=(extra,props={})=>render(BotRequestDetail,{card:card(extra),bots,w,language:"zh-Hant",onClose(){},...props})
const entries=[
  {key:"draft",label:"寫好草稿",state:"done",note:"commit 1a2b3c4d https://example.com/report",reported_by:"atlas",reported_at:"2026-11-24T10:00:00Z"},
  {key:"review",label:"請人檢查",state:"open"},
  {key:"translate",label:"翻成英文",state:"not_needed",note:"這次只發中文",reported_by:"nova",reported_at:"2026-11-24T10:30:00Z"},
]

test("a Board row in progress names the holder, the title, the state in words, the ticks and the last update", () => {
  const html=row({done:1,total:3})
  assert.match(html,/data-progress="in_progress"/)
  assert.match(html,/Atlas/)
  assert.match(html,/發布十一月報告/)
  assert.match(html,/進行中/)
  assert.match(html,/1\/3 ✓/)
  assert.match(html,/最後更新 5 分鐘前/)
  assert.doesNotMatch(html,/maker/)
})

test("a waiting row says it waits for the person; done and cancelled rows say so in words", () => {
  assert.match(row({progress_state:"waiting_for_you",waiting:"q1"}),/data-progress="waiting"[^]*等你/)
  assert.match(row({progress_state:"done",state:"done"}),/已完成/)
  assert.match(row({progress_state:"cancelled",state:"cancelled"}),/已取消/)
})

test("a row without a checklist shows no 0/0, and an unreadable holder is not given a made-up name", () => {
  const html=row({baton:{bot:"gone",role:"maker"}})
  assert.doesNotMatch(html,/✓/)
  assert.match(html,/這台裝置讀不到的 Bot/)
})

test("the chat line is one button with the title, the state and the ticks", () => {
  const html=render(BotRequestLine,{card:card({done:2,total:2,progress_state:"done",state:"done"}),w,onOpen(){}})
  assert.match(html,/^<button type="button"/)
  assert.match(html,/aria-label="開啟交辦卡：發布十一月報告"/)
  assert.match(html,/已完成/)
  assert.match(html,/2\/2 ✓/)
})

test("an open card's detail shows the checklist states and offers cancel, hand on and complete, never approve or reject", () => {
  const html=detail({checklist:entries,done:2,total:3})
  assert.match(html,/data-entry-state="done"[^]*?✓[^]*?寫好草稿/)
  assert.match(html,/<a href="https:\/\/example\.com\/report" target="_blank" rel="noreferrer noopener">/)
  assert.match(html,/由 Atlas 於 [^<]+ 回報/)
  assert.match(html,/data-entry-state="open"[^]*?請人檢查/)
  assert.match(html,/data-entry-state="not_needed"[^]*?<s>翻成英文<\/s>[^]*?不需要[^]*?這次只發中文[^]*?由 Nova 於/)
  assert.match(html,/取消交辦/)
  assert.match(html,/交給其他 Bot/)
  assert.match(html,/>完成<\/button>/)
  assert.doesNotMatch(html,/核准|拒絕|Approve|Reject/)
})

test("a waiting card's one extra action opens the holder's chat", () => {
  const html=detail({progress_state:"waiting_for_you",waiting:"q1"},{onOpenChat(){}})
  assert.match(html,/到 Atlas 的聊天回覆/)
  assert.doesNotMatch(html,/核准|拒絕/)
})

test("a done or cancelled card offers no write, and a card without a checklist says so", () => {
  for(const state of ["done","cancelled"]){
    const html=detail({state,progress_state:state})
    assert.doesNotMatch(html,/取消交辦|交給其他 Bot/)
    assert.match(html,/這張卡沒有檢查清單。/)
  }
})

test("the detail lists handoffs one per line, the verdict, the linked Board item and the dispatched Sessions", () => {
  const html=detail({chain:["atlas","nova"],baton:{bot:"nova",role:"reviewer"},
    events:[{id:2,kind:"baton.handed",actor:"bot:atlas",at:"2026-11-24T09:00:00Z",version:2,detail:"請幫忙檢查"}],
    verdicts:[{id:"v1",verdict:"conditional_pass",reviewer_bot:"nova",at:"2026-11-24T09:30:00Z",commit:"1a2b3c4d5e6f",text:"補上英文摘要"}]},
    {boardItem:React.createElement("button",{type:"button"},"月報"),links:[{id:"l1",task_id:"t1",title:"整理數據",state:"executing"}]})
  assert.match(html,/Atlas → Nova/)
  assert.match(html,/請幫忙檢查/)
  assert.match(html,/有條件通過[^]*Nova[^]*1a2b3c4d/)
  assert.match(html,/看板項目[^]*月報/)
  assert.match(html,/派出的分身[^]*整理數據/)
  assert.equal(detail({}).includes("還沒有交接過。"),true)
})
