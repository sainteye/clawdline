import { test } from "node:test"
import assert from "node:assert/strict"
import type * as W from "@clawdline/contract"
import { createRequire } from "node:module"
import { fileURLToPath } from "node:url"
import { build } from "esbuild"
import React from "react"
import { renderToStaticMarkup } from "react-dom/server"
// @ts-expect-error Node executes source extensions.
import { botBrowserSupported, botOwnedBrowserTab, browserToolTrace, botReplyHasBrowser } from "./bot-browser-entry.ts"

const trace=(category="tool_call",text="browser_command",turn_id="browser-turn"):W.ProjectAssistantTrace=>({category,text,turn_id,complete:true})
const message=(kind:W.ProjectAssistantMessageKind="assistant",turn_id="browser-turn")=>({kind,turn_id}) as W.ProjectAssistantMessage

test("the Chrome entry requires a targeted browser source, including on read-only devices",()=>{
  assert.equal(botBrowserSupported({}),false)
  const browser=async<T,>()=>({}) as T
  assert.equal(botBrowserSupported({browser,browserUnavailable:()=>"pa_unsupported"}),false)
  assert.equal(botBrowserSupported({browser,browserUnavailable:()=>"pa_unpaired"}),false)
  assert.equal(botBrowserSupported({browser,browserUnavailable:()=>""}),true)
})

test("the chip follows the panel's current owned tab and selected browser profile",()=>{
  const tab=(id:number,conversation="owner",current=false,browser_mode="isolated")=>({tab:id,conversation,current,browser_mode})
  const status={owner:"owner",mode:"isolated",status:{tabs:[tab(1,"foreign",true),tab(2),tab(3,"owner",true),tab(4,"owner",true,"user_chrome")]}}
  assert.equal(botOwnedBrowserTab(status),3)
  status.status.tabs=status.status.tabs.filter(t=>!t.current)
  assert.equal(botOwnedBrowserTab(status),2)
  assert.equal(botOwnedBrowserTab({...status,owner:undefined}),null)
  assert.equal(botOwnedBrowserTab({...status,status:{tabs:[tab(1,"foreign")]}}),null)
  assert.equal(botOwnedBrowserTab(null),null)
})

test("only exact browser tools in tool traces mark the reply's own turn",()=>{
  for(const text of ["browser_status","browser_command","mcp__clawdline__browser_command","clawdline.browser_status (read)"]){
    assert.equal(browserToolTrace(trace("tool_call",text)),true,text)
    assert.equal(botReplyHasBrowser(message(),[trace("tool_call",text)]),true)
  }
  for(const value of [trace("agent_message_chunk"),trace("agent_thought_chunk"),trace("tool_call","browser_command_extra"),trace("tool_call","notbrowser_status"),trace("tool_call","Read notes.md")])assert.equal(botReplyHasBrowser(message(),[value]),false)
  assert.equal(botReplyHasBrowser(message("user"),[trace()]),false)
  assert.equal(botReplyHasBrowser(message("notice"),[trace()]),false)
  assert.equal(botReplyHasBrowser(message("assistant","other-turn"),[trace()]),false)
  assert.equal(botReplyHasBrowser(message(),[]),false)
  // Releasing a tab changes chip visibility, never the durable reply's entry.
  assert.equal(botReplyHasBrowser(message(),[trace()]),true)
})

test("a rendered browser reply has an action without a tab and other messages do not",async()=>{
  // The shared Markdown reader expects a document even for static rendering.
  if(typeof document==="undefined")Object.defineProperty(globalThis,"document",{configurable:true,value:{getElementById:()=>null,querySelector:()=>null,querySelectorAll:()=>[],addEventListener:()=>{},createElement:()=>({style:{},setAttribute(){},appendChild(){},addEventListener(){}}),documentElement:{style:{},setAttribute(){},lang:"en"},body:{appendChild(){},classList:{add(){},remove(){},toggle(){},contains:()=>false}}}})
  const {outputFiles}=await build({stdin:{contents:'export {ChatRows} from "./ChatRows";export {words} from "./words"',resolveDir:fileURLToPath(new URL(".",import.meta.url)),loader:"ts"},bundle:true,platform:"node",format:"cjs",packages:"external",write:false,jsx:"automatic",loader:{".css":"empty"}})
  const compiled={exports:{} as {ChatRows:typeof import("./ChatRows").ChatRows;words:typeof import("./words").words}}
  new Function("require","module","exports",outputFiles[0].text)(createRequire(import.meta.url),compiled,compiled.exports)
  const reply={...message(),id:"reply",seq:1,at:"2026-11-16T00:00:00Z",text:"Page ready",quote_message_id:"",fence:{} as W.ProjectAssistantFence}
  const render=(value:W.ProjectAssistantMessage,traces:W.ProjectAssistantTrace[],onOpenBrowser:(()=>void)|null=()=>{})=>renderToStaticMarkup(React.createElement(compiled.exports.ChatRows,{timeline:[{id:value.id,kind:"message",message:value,anchor_message_seq:1,observed_event_seq:1}],browserTraces:traces,onOpenBrowser:onOpenBrowser??undefined,onQuote:()=>{},w:compiled.exports.words("en"),language:"en"}))
  assert.match(render(reply,[trace()]),/<button[^>]*>Open browser<\/button>/)
  for(const value of [{...reply,kind:"user" as const},{...reply,turn_id:"another-turn"}])assert.doesNotMatch(render(value,[trace()]),/>Open browser</)
  assert.doesNotMatch(render(reply,[]),/>Open browser</)
  assert.doesNotMatch(render(reply,[trace()],null),/>Open browser</)
})
