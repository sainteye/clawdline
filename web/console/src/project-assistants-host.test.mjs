import { test } from "node:test"
import assert from "node:assert/strict"
import { hostBundle } from "../tools/project-assistants-host.e2e.mjs"
import { build } from "esbuild"
import { createRequire } from "node:module"
import { fileURLToPath } from "node:url"
import React from "react"
import { renderToStaticMarkup } from "react-dom/server"

test("the synthetic host builds the actual App, Session and existing voice owners together",async () => {
  const {html,files} = await hostBundle()
  assert.ok(html.includes('<div id="root">'))
  assert.ok(files.get("/host.js")?.length > 0)
  assert.ok(files.get("/host.css")?.length > 0)
})

test("task-only results keep their real receipt without an Open action, while Board results can open",async () => {
  const {outputFiles} = await build({stdin:{contents:'export {WorkResultCard} from "./project-assistants/WorkResultCard";export {words} from "./project-assistants/words"',resolveDir:fileURLToPath(new URL(".",import.meta.url)),loader:"ts"},bundle:true,platform:"node",format:"cjs",packages:"external",write:false,jsx:"automatic"})
  const compiled={exports:{}}
  new Function("require","module","exports",outputFiles[0].text)(createRequire(import.meta.url),compiled,compiled.exports)
  const {WorkResultCard,words}=compiled.exports
  for (const language of ["en","zh-Hant"]) {
    const work={board_item_id:"",task_id:"real-task-id",state:"accepted",landing_commit:"commit",id:"link"}
    const props={work,w:words(language),onOpen:() => assert.fail("render must not open")}
    const task=renderToStaticMarkup(React.createElement(WorkResultCard,props))
    assert.match(task,/real-task-id/);assert.match(task,/commit/);assert.doesNotMatch(task,/<button/)
    const board=renderToStaticMarkup(React.createElement(WorkResultCard,{...props,work:{...work,board_item_id:"real-board-id"}}))
    assert.match(board,/real-board-id/);assert.match(board,/<button/)
  }
})

test("a work card whose Session never started says why in the reader's language, and nothing more otherwise",async () => {
  const {outputFiles} = await build({stdin:{contents:'export {WorkResultCard} from "./project-assistants/WorkResultCard";export {words} from "./project-assistants/words"',resolveDir:fileURLToPath(new URL(".",import.meta.url)),loader:"ts"},bundle:true,platform:"node",format:"cjs",packages:"external",write:false,jsx:"automatic"})
  const compiled={exports:{}}
  new Function("require","module","exports",outputFiles[0].text)(createRequire(import.meta.url),compiled,compiled.exports)
  const {WorkResultCard,words}=compiled.exports
  const work={board_item_id:"",task_id:"task",state:"failed",landing_commit:"",id:"link"}
  const wants={en:/A dialog in the Session&#x27;s tab was never answered/,"zh-Hant":/Session 分頁裡有對話框一直沒人回答/}
  for (const [language,want] of Object.entries(wants)) {
    const failed=renderToStaticMarkup(React.createElement(WorkResultCard,{work:{...work,failure_reason:"dialog_unanswered"},w:words(language)}))
    assert.match(failed,want);assert.match(failed,/pa-work-task-failure/);assert.doesNotMatch(failed,/dialog_unanswered/)
    const plain=renderToStaticMarkup(React.createElement(WorkResultCard,{work,w:words(language)}))
    assert.doesNotMatch(plain,/pa-work-task-failure/)
  }
})

test("peer dispatch cards localize the requester, keep legacy mode and replace notices in place", async () => {
  const {outputFiles} = await build({stdin:{contents:'export * from "./project-assistants/WorkResultCard";export * from "./project-assistants/chat-items";export {words} from "./project-assistants/words"',resolveDir:fileURLToPath(new URL(".",import.meta.url)),loader:"ts"},bundle:true,platform:"node",format:"cjs",packages:"external",write:false,jsx:"automatic"})
  const compiled={exports:{}}
  new Function("require","module","exports",outputFiles[0].text)(createRequire(import.meta.url),compiled,compiled.exports)
  const {buildTimelineChat,WorkResultCard,PeerDispatchStatusCard,peerDispatchNoticeText,words}=compiled.exports
  const at="2026-11-12T00:00:00Z"
  const notice=(task,seq,text,ref)=>({id:"peer-dispatch-"+task,seq,kind:"notice",text,at,quote_message_id:"",notice:ref})
  const message=m=>({id:m.id,kind:"message",anchor_message_seq:m.seq,observed_event_seq:6,message:m})
  const legacy=message(notice("first",2,'Dispatched task "Repair header" at Rhea ｜ Reviewer\'s request. / 應 Rhea ｜ Reviewer 的請求派出任務「Repair header」。'))
  const failed=message(notice("failed-third",4,"wrong stored failure text",{key:"peer_dispatch_failed",cause:"third",task_title:"Rejected request",requester:"Rhea ｜ Reviewer",failure_code:"pa_scope_denied",failure_field:"claims"}))
  const pending=message(notice("second",3,"wrong stored admission text",{key:"peer_dispatch",cause:"second",task_title:"Collect screenshots",requester:"Rhea ｜ Reviewer"}))
  const work=(task,seq,title)=>({id:task,kind:"work",anchor_message_seq:seq,observed_event_seq:7,work:{id:task,task_id:task,state:"accepted",board_item_id:"",title}})
  const render=(language,on,timeline)=>{
    const w=words(language)
    return renderToStaticMarkup(React.createElement(React.Fragment,{},...buildTimelineChat(timeline,on).map(item=>{
      if(item.kind==="work")return React.createElement(WorkResultCard,{key:item.id,work:item.work,dispatch:item.dispatch,w})
      if(item.kind==="dispatch")return React.createElement(PeerDispatchStatusCard,{key:item.id,dispatch:item.dispatch,showRequester:on,w})
      if(item.kind==="message")return React.createElement("p",{key:item.id},item.dispatch&&peerDispatchNoticeText?peerDispatchNoticeText(item.dispatch,w):item.message.text)
    })))
  }
  for(const [language,chip,failure,oldNotice] of [
    ["en",/Requested by Rhea/,/Dispatch didn’t go through/,/Dispatched task/],
    ["zh-Hant",/Rhea 請求/,/派工沒有成功/,/應 Rhea 的請求派出任務/],
  ]){
    const before=render(language,true,[legacy,work("first",2,"Repair header"),pending,failed])
    assert.match(before,chip);assert.match(before,/title="Rhea ｜ Reviewer"/);assert.match(before,failure)
    assert.match(before,/pa_scope_denied.*claims/);assert.doesNotMatch(before,oldNotice)
    assert.equal((before.match(/Repair header/g)??[]).length,1)
    assert.match(before,/Repair header.*Collect screenshots.*Rejected request/)
    const arrived=render(language,true,[legacy,work("first",2,"Repair header"),pending,work("second",3,"Collect screenshots"),failed])
    assert.equal((arrived.match(/Collect screenshots/g)??[]).length,1)
    assert.doesNotMatch(arrived,/data-state="missing"/)
    const off=render(language,false,[legacy,work("first",2,"Repair header"),pending,failed])
    assert.match(off,oldNotice);assert.doesNotMatch(off,/pa-work-task-requester/)
    assert.doesNotMatch(off,/wrong stored| \/ 應/)
    assert.match(off,failure)
  }
})
