/** Real host, sound and browser panel with synthetic source-owned facts only. */
import React,{useCallback,useState} from "react"
import {createRoot} from "react-dom/client"
import {activateCatalog} from "../../src/catalog"
import {loadStrings} from "../../src/legacy/bridge"
import {useProjectAssistantsHost} from "../../src/pages/project-assistants"
import {fixtureSource} from "../../src/project-assistants/fixtures"
import {BotBrowser} from "../../src/project-assistants/BotBrowser"
import {createViewState} from "../../src/project-assistants/view-state"
import type * as W from "@clawdline/contract"
import type {SourceClient} from "../../src/project-assistants/client"
import "../../src/legacy/tokens.css"
import "../../src/legacy/base.css"
import "../../src/legacy/composer.css"

const language=new URL(location.href).searchParams.get("lang")==="en"?"en":"zh-Hant"
const catalog=await fetch(`/catalogs/${language}.json`).then(r=>r.json());activateCatalog(catalog,language);await loadStrings(async()=>catalog)
let failExit=false
let tones=0,marker="old",revision=1,person=true,hold=false,held:(()=>void)|undefined
const writes:{path:string;body:unknown}[]=[]
const NativeAudio=window.AudioContext
class ProbeAudio extends NativeAudio {
  createOscillator(){tones++;return super.createOscillator()}
}
Object.defineProperty(window,"AudioContext",{configurable:true,value:ProbeAudio})
const base=fixtureSource("studio"),receivers=new Set<(frame:W.ProjectAssistantRosterFrame)=>void>()
const summaries=async()=>{const p=await base.list({machine_id:"studio",query:"",cursor:"",page_size:100},new AbortController().signal);return p.items.map(item=>({...item,completed_turn_id:item.bot.key.bot_id==="bot-0"?marker:undefined}))}
const source:SourceClient={...base,canRoster:()=>true,
  async list(request,signal){const page=await base.list(request,signal);return {...page,items:page.items.map(item=>({...item,completed_turn_id:item.bot.key.bot_id==="bot-0"?marker:undefined}))}},
  async subscribeRoster(_request,receive,signal){
    receive({type:"hello",source_epoch:"feedback"});receive({type:"snapshot-complete",source_epoch:"feedback",cursor:String(revision),changes:(await summaries()).map(summary=>({key:summary.bot.key,revision,removed:false,summary}))})
    receivers.add(receive);await new Promise<void>(resolve=>{const done=()=>{receivers.delete(receive);resolve()};if(signal.aborted)done();else signal.addEventListener("abort",done,{once:true})})
  },
  browserRemote:false,
  async browser(_key,_conversation,method,path,body){
    const route=new URL(path,location.href).pathname
    if(method==="POST"){
      writes.push({path:route,body})
      if(route==="/v1/browser/control"){if(hold)await new Promise<void>(r=>{held=r});person=!!(body as {person:boolean}).person;return {ok:true} as never}
    }
    if(path==="/v1/browser/status"&&failExit){failExit=false;throw {code:"pa_offline"}}
    if(route==="/v1/browser/status")return {owner:"bot-owner",mode:"user_chrome",enabled:true,person_navigate:true,status:{connected:true,stopped:false,host:{version:"0.5.0"},tabs:[{tab:9,conversation:"bot-owner",browser_mode:"user_chrome",current:true,person},{tab:10,conversation:"foreign",browser_mode:"user_chrome",current:false,person:true}]}} as never
    if(route==="/v1/browser/screenshot")return {value:{data:"data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVQIHWP4z8DwHwAFgAI/ScLbtAAAAABJRU5ErkJggg==",width:390,height:640,url:"https://synthetic.example",title:"Synthetic browser",layout:"mobile"}} as never
    if(route==="/v1/browser/activity")return {activity:[]} as never
    if(route==="/v1/browser/audit")return {entries:[]} as never
    throw {code:"browser_unavailable"}
  },
}
const sources=[source],registry={accountKey:"feedback",readerID:"reader",viewState:createViewState(localStorage),getSnapshot:()=>sources,subscribe:()=>()=>{}}
function Feedback(){
  const host=useProjectAssistantsHost({registry:registry as never,cloud:false,language})
  const [browser,setBrowser]=useState(new URL(location.href).searchParams.has("browser"))
  const current=useCallback(()=>sources,[])
  return <><div className="fixture-controls"><button onClick={()=>{}}>Unlock audio</button><button onClick={()=>setBrowser(true)}>Open browser fixture</button><button onClick={()=>setBrowser(false)}>Leave browser fixture</button></div>{host.dock}{host.content}{browser&&<BotBrowser source={source} current={current} bot={base.bots[0]} writable reason="" onClose={()=>setBrowser(false)} onStale={async()=>{}}/>}</>
}
createRoot(document.getElementById("root")!).render(<React.StrictMode><Feedback/></React.StrictMode>)
Object.assign(window,{botFeedback:{
  async complete(id:string){marker=id;revision++;for(const receive of receivers)receive({type:"changes",source_epoch:"feedback",cursor:String(revision),changes:(await summaries()).map(summary=>({key:summary.bot.key,revision,removed:false,summary}))})},
  failExit(){failExit=true},hold(){hold=true},release(){hold=false;held?.();held=undefined},person(on:boolean){person=on},snapshot:()=>({tones,person,writes,subscribers:receivers.size}),
}})
