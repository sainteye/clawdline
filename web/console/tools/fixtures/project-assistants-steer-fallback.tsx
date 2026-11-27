/** Real Chat and Bot settings with a steering Bot whose source put messages back in the queue; synthetic data only. */
import React from "react"
import {createRoot} from "react-dom/client"
import type * as W from "@clawdline/contract"
import {Chat} from "../../src/project-assistants/Chat"
import {BotSettings} from "../../src/project-assistants/BotSettings"
import {FallbackSourceScope} from "../../src/project-assistants/FallbackSettings"
import {contractVersion,fixtureSource} from "../../src/project-assistants/fixtures"
import {createViewState} from "../../src/project-assistants/view-state"
import {words} from "../../src/project-assistants/words"
import "../../src/legacy/tokens.css"
import "../../src/project-assistants/styles.css"

const q=new URL(location.href).searchParams,language=q.get("lang")??"en",w=words(language)
const source=fixtureSource("steer-fallback"),bot=source.bots[0],signal=new AbortController().signal
delete bot.reply_input
source.canReplyInput=()=>true
source.canSendNow=()=>true
source.canUnqueue=()=>true
source.sendNow=async()=>{throw Error("not used")}
const initial=await source.view!({key:bot.key,conversation_id:bot.conversation_id,mode:"snapshot",source_epoch:"",cursor:"",after_seq:0,page_size:100},signal)
const fence=initial.head.current_fence
const turn=(id:string,state:W.ProjectAssistantTurnState,steer_fallback?:string)=>source.push({kind:"turn_updated",id:crypto.randomUUID(),seq:0,at:new Date().toISOString(),fence,version:contractVersion,
  turn:{id,fence,message_id:"person-"+id,state,started_at:state==="running"?new Date().toISOString():undefined,idempotency_key:"fixture-"+id,memory_revision:1,instructions:bot.instructions,...(steer_fallback?{steer_fallback}:{})} as W.ProjectAssistantTurn})
const say=(id:string,text:string)=>source.scriptMessage(bot,"person-"+id,"user",text,id)
say("t1",language==="en"?"Start the synthetic running reply":"開始一段合成的回覆");turn("t1","running")
say("t2",language==="en"?"Waiting for the permission answer":"等權限確認的這則");turn("t2","queued","permission_pending")
say("t3",language==="en"?"This one carries a picture":"這則帶了圖片");turn("t3","queued","images_unsupported")
say("t4",language==="en"?"Queued before the upgrade":"升級前已排隊的訊息");turn("t4","queued")
const summary=(await source.list({machine_id:bot.key.machine_id,cursor:"",query:"",page_size:100},signal)).items[0]
const saves:W.ProjectAssistantEditSettings[]=[]
Object.assign(window,{steerFixture:{saves}})
const root=createRoot(document.querySelector("#app")!)
if(q.get("view")==="settings")root.render(<div className="project-assistants" style={{minHeight:"100dvh",background:"var(--pa-bg)",padding:"env(safe-area-inset-top) 0 env(safe-area-inset-bottom)"}}>
  <FallbackSourceScope source={source}><BotSettings bot={bot} w={w} enabled onSave={async settings=>{saves.push(settings)}} onClose={()=>{}}/></FallbackSourceScope></div>)
else root.render(<div className="project-assistants" style={{height:"100dvh",background:"var(--pa-bg)"}}>
  <Chat summary={summary} source={source} current={()=>[source]} accountKey="steer-fallback" view={createViewState(window.localStorage)} w={w} language={language} onBack={()=>{}} onBotChanged={()=>{}}/></div>)
