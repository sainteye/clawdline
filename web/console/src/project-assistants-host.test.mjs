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
