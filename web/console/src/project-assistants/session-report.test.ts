import {test} from "node:test"
import assert from "node:assert/strict"
import {createRequire} from "node:module"
import {fileURLToPath} from "node:url"
import {build} from "esbuild"
import React from "react"
import {renderToStaticMarkup} from "react-dom/server"
import type * as W from "@clawdline/contract"

// A Bot's latest message is often a Session report. The roster shows its first
// sentence, so the chat must show that same sentence without opening the card.
const project:W.ProjectAssistantProjectScope={machine_id:"machine",project_id:"project",canonical_id:"canonical",kind:"repository"}
const key={machine_id:"machine",bot_id:"xavia"},at="2026-11-24T02:57:17Z"
const fence:W.ProjectAssistantFence={key,project,conversation_id:"xavia-chat",binding_id:"",generation:0}
const bot:W.ProjectAssistantBot={key,project,conversation_id:"xavia-chat",name:"Xavia",agent:"claude",
  appearance:{shape:"blob",color:"#e39567"},permission_policy:"ask",instructions:{revision:1,text:"",role_snapshot_ref:""},created_at:at,updated_at:at,version:1}
const report="停在推送這一步：四件已依序合併在本任務分支、測試全綠，但 `git push origin HEAD` 被拒絕。\n\n## 細節\n- 第一件：a1b2c3\n- 第二件：d4e5f6"
const sentence="停在推送這一步：四件已依序合併在本任務分支、測試全綠，但 git push origin HEAD 被拒絕。"
const message:W.ProjectAssistantMessage={id:"work-result-task",seq:9,at,kind:"work",text:report,turn_id:"t",quote_message_id:"",fence}
const summary={bot,latest_message:report,latest_message_kind:"work",status:"idle",activity:"",activity_at:at,unread_count:0,pending_permissions:0,version:1,
  read_cursor:{key,conversation_id:"xavia-chat",reader_id:"",message_seq:0},
  source:{machine_id:"machine",state:"online",readable:true,writable:true,fresh:true,observed_at:at,access:"interactive",capabilities:null}} as unknown as W.ProjectAssistantBotSummary

async function load() {
  globalThis.document??={getElementById:()=>null,querySelector:()=>null,querySelectorAll:()=>[],addEventListener:()=>{},createElement:()=>({style:{},setAttribute(){},appendChild(){},addEventListener(){}}),documentElement:{style:{},setAttribute(){},lang:"en"},body:{appendChild(){},classList:{add(){},remove(){},toggle(){},contains:()=>false}}} as never
  globalThis.location??=new URL("http://synthetic.invalid/") as never
  const {outputFiles}=await build({stdin:{contents:'export {ChatRows} from "./ChatRows";export {BotRow} from "./BotRow";export {words} from "./words"',
    resolveDir:fileURLToPath(new URL(".",import.meta.url)),loader:"ts"},bundle:true,platform:"node",format:"cjs",packages:"external",write:false,jsx:"automatic",loader:{".css":"empty"}})
  const compiled={exports:{} as {ChatRows:typeof import("./ChatRows").ChatRows;BotRow:typeof import("./BotRow").BotRow;words:typeof import("./words").words}}
  new Function("require","module","exports",outputFiles[0].text)(createRequire(import.meta.url),compiled,compiled.exports)
  return compiled.exports
}
const decode=(s:string)=>s.replace(/&quot;/g,'"').replace(/&#x27;/g,"'").replace(/&lt;/g,"<").replace(/&gt;/g,">").replace(/&amp;/g,"&")
const text=(html:string)=>decode(html.replace(/<[^>]*>/g,""))

test("a Session report shows the roster's sentence in the chat without being opened",async()=>{
  const {ChatRows,BotRow,words}=await load()
  for(const [language,label] of [["zh-Hant","Session 回報"],["en","Session report"]] as const){
    const w=words(language)
    const row=renderToStaticMarkup(React.createElement(BotRow,{item:summary,selected:false,onSelect:()=>{},w,language,now:Date.parse(at)}))
    const preview=decode(/class="pa-row-preview[^"]*" title="([^"]*)"/.exec(row)?.[1]??"")
    assert.equal(preview,`${label}${language==="en"?": ":"："}${sentence}`,`${language}: the roster says where its line comes from`)
    const chat=renderToStaticMarkup(React.createElement(ChatRows,{timeline:[{id:message.id,kind:"message",message,anchor_message_seq:9,observed_event_seq:1}],w,language,botKey:key,roster:[bot]}))
    const closed=text(/<summary>([\s\S]*?)<\/summary>/.exec(chat)?.[1]??"")
    assert.ok(closed.includes(label),`${language}: the closed card still names itself`)
    assert.ok(closed.includes(sentence),`${language}: the closed card shows the roster's sentence; got ${closed}`)
    assert.ok(!closed.includes("第一件"),`${language}: the closed card keeps the rest folded`)
  }
})
