import {test} from "node:test"
import assert from "node:assert/strict"
import type * as W from "@clawdline/contract"
// @ts-expect-error Node executes source extensions.
import {encodeBotTemplate,readableTemplate} from "./bot-template.ts"

function fixture():W.ProjectAssistantGetResponse {
  const key={machine_id:"synthetic-machine",bot_id:"synthetic-bot"}
  return {contract_version:"project-assistants.v1",bot:{key,project:{machine_id:key.machine_id,project_id:"synthetic-project",canonical_id:"synthetic-canonical",kind:"place"},conversation_id:"synthetic-conversation",name:"Synthetic template",appearance:{shape:"blob",color:"#e39567"},agent:"codex",permission_policy:"ask",mode:"",notify:"off",instructions:{text:'</textarea><script>throw "text only"</script>\nhttps://invalid.example/',revision:1,role_snapshot_ref:"private-ref"},version:1,created_at:"2026-11-17T00:00:00Z",updated_at:"2026-11-17T00:00:00Z",runtime_settings:{model:"",options:{effort:"high",fast:"false","fast-mode":"",login_profile:"private-login",project:"private-project",token:"private-token"}},login_profile:"private-login",hidden:true,manage_team:false,fallback_agents:["claude"],fallback_steps:[{agent:"claude",login_profile:"private-fallback"}],extensions:{skills:[{id:"private-skill",fingerprint:"private-fingerprint"}],connectors:[]}},conversation:{key,id:"synthetic-conversation",binding_id:"private-binding",generation:1,last_message_seq:0,memory_revision:1,version:1,active_turn_id:"private-turn"},source:{machine_id:key.machine_id,state:"online",access:"history_only",readable:true,writable:false,fresh:true,observed_at:"2026-11-17T00:00:00Z",capabilities:[]}}
}
const scope=(a:W.ProjectAssistantGetResponse)=>({key:a.bot.key,project:a.bot.project,conversation_id:a.conversation.id})
test("template explicitly includes portable settings and excludes identities and runtime authority",()=>{
  const a=fixture(),encoded=readableTemplate(a,scope(a)),value=JSON.parse(encoded)
  assert.deepEqual(value,{format:"clawdline.bot-template.v1",settings:{name:a.bot.name,appearance:{color:"#e39567",shape:"blob"},agent:"codex",instructions:a.bot.instructions.text,permission_policy:"ask",mode:"",notify:"off",runtime_settings:{model:"",options:{effort:"high",fast:"false","fast-mode":""}}}})
  assert.equal(encoded.includes("private-"),false)
  assert.equal(encoded.includes("synthetic-machine"),false)
  assert.equal(encoded.includes("synthetic-conversation"),false)
  // Future runtime fields cannot become portable through object spreading.
  const extra=Object.assign(a.bot,{history:["private-history"],token:"private-token",offeredToNetwork:true})
  assert.equal(encodeBotTemplate(extra),encoded)
})
test("template has exact recursively sorted JSON and retains safe text, empty values and disabled controls",()=>{
  const a=fixture();a.bot.instructions.text="";a.bot.runtime_settings={model:"",options:{fast:"false",effort:""}}
  const expected='{\n  "format": "clawdline.bot-template.v1",\n  "settings": {\n    "agent": "codex",\n    "appearance": {\n      "color": "#e39567",\n      "shape": "blob"\n    },\n    "instructions": "",\n    "mode": "",\n    "name": "Synthetic template",\n    "notify": "off",\n    "permission_policy": "ask",\n    "runtime_settings": {\n      "model": "",\n      "options": {\n        "effort": "",\n        "fast": "false"\n      }\n    }\n  }\n}'
  assert.equal(encodeBotTemplate(a.bot),expected)
  a.bot.runtime_settings.options={effort:"",fast:"false"}
  assert.equal(encodeBotTemplate(a.bot),expected)
  a.bot.mode=null as unknown as string;a.bot.notify=undefined;a.bot.runtime_settings=null as unknown as W.ProjectAssistantRuntimeSettings
  const settings=JSON.parse(encodeBotTemplate(a.bot)).settings
  assert.equal("mode" in settings,false);assert.equal("notify" in settings,false);assert.equal("runtime_settings" in settings,false)
})
test("template refuses unavailable or mismatched source, Bot, Project and conversation",()=>{
  for(const change of [
    (a:W.ProjectAssistantGetResponse)=>{a.source.readable=false},
    (a:W.ProjectAssistantGetResponse)=>{a.source.fresh=false},
    (a:W.ProjectAssistantGetResponse)=>{a.source.state="revoked"},
    (a:W.ProjectAssistantGetResponse)=>{a.source.machine_id="other"},
    (a:W.ProjectAssistantGetResponse)=>{a.bot.key.bot_id="other"},
    (a:W.ProjectAssistantGetResponse)=>{a.bot.project.project_id="other"},
    (a:W.ProjectAssistantGetResponse)=>{a.bot.conversation_id="other"},
    (a:W.ProjectAssistantGetResponse)=>{a.conversation.id="other"},
    (a:W.ProjectAssistantGetResponse)=>{a.conversation.key.bot_id="other"},
    (a:W.ProjectAssistantGetResponse)=>{a.bot.runtime_settings!.options.fast=false as unknown as string},
  ]){const a=fixture(),expected=structuredClone(scope(a));change(a);assert.throws(()=>readableTemplate(a,expected))}
})
