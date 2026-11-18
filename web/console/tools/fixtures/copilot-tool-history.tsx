import React,{useState} from 'react'
import {createRoot} from 'react-dom/client'
import '../../src/legacy/tokens.css'
import '../../src/legacy/base.css'
import '../../src/realtime/realtime.css'
import {activateCatalog} from '../../src/catalog.js'
import {loadStrings} from '../../src/legacy/bridge.js'
import {RealtimeCopilotModal} from '../../src/realtime/RealtimeCopilotModal.js'
import {realtimeCopilot} from '../../src/realtime/realtime-copilot.js'
import {emptyCost} from '../../src/realtime/copilot-cost.js'

const lang=new URLSearchParams(location.search).get('lang')==='en'?'en':'zh-Hant'
const words=await(await fetch(`/catalogs/${lang}.json`)).json()
activateCatalog(words,lang);await loadStrings(async()=>words)
const nativeFetch=window.fetch.bind(window)
const journal:unknown[]=[]
// No fixture operation reaches a daemon or a provider, even if a code path changes.
window.fetch=async(input,init)=>{
 const path=String(input)
 if(path.startsWith('/v1/')){
  if(path==='/v1/voice/copilot-journal'){journal.push(JSON.parse(String(init?.body)));return Response.json({ok:true})}
  throw new Error('Fixture blocked a real API request: '+path)
 }
 return nativeFetch(input,init)
}
// Transport-only substitutions keep the actual client, ledger, modal and styles.
const client=realtimeCopilot as unknown as {
 state:ReturnType<typeof realtimeCopilot.getState>;journalCall:string;historyCall:string;live:boolean;call:unknown;dc:unknown;meter:unknown;
 toolDeps:()=>unknown;observeOperation:(spec:unknown)=>unknown;updateState:(patch:unknown)=>void;
 prepareListReport:()=>Promise<void>;runLiveTool:(id:string,name:string,args:string)=>Promise<unknown>;
 handleLiveEvent:(event:unknown)=>void;handleRealtimeEvent:(event:unknown)=>Promise<void>;
 flushJournal:()=>void;
 operations:{reset:(call:string)=>void};resetSessionReports:()=>void;
}
const engine=new URLSearchParams(location.search).get('engine')==='live'?'live':'realtime'
let release!:()=>void,released=false
const held=new Promise<void>(resolve=>release=()=>{released=true;resolve()})
const rows=Array.from({length:3},(_,i)=>({id:`fixture-${i}`,label:`Session ${i+1}`,assistant:'codex',state:'idle',sessionId:`fixture-conversation-${i}`,cwd:'/fixture/project'}))
client.call={memory:'',snapshot:'',options:{workEnabled:false}}
client.journalCall='fixture-history-call';client.historyCall=client.journalCall;client.operations.reset(client.journalCall)
client.live=engine==='live'
client.dc={readyState:'open',send(){},close(){}}
client.meter={read:()=>({input:0,output:0}),pending(){},chime(){},close(){}}
client.toolDeps=()=>({observe:spec=>client.observeOperation(spec),sleep:async()=>{},recapMode:()=> 'on_demand',fetch:async(path:string,init?:RequestInit)=>{
 if(path==='/v1/sessions')return Response.json({sessions:rows})
 if(path.startsWith('/v1/transcript?')){if(!released)await held;return Response.json({entries:[{role:'assistant',text:'Fixture latest reply'}]})}
 if(path==='/v1/voice/copilot-recap'){if(!released)await held;const id=JSON.parse(String(init?.body)).session;return Response.json({recap:'Fixture summary',current:true,at:Date.now()/1000,model:id==='fixture-1'?'kept':'codex-fork'})}
 return Response.json({rows:[],items:[]})
}})
client.updateState({status:'connected',connectionReady:true,engine,speakingState:'listening',lastTranscript:'請介紹這些 Session。',lastAssistantSpeech:'這是本機假資料驗收，不會建立語音連線。',cost:{...emptyCost('fixture'),responses:1}})

function Fixture(){
 const [open,setOpen]=useState(true),[result,setResult]=useState('Ready')
 const run=(action:()=>unknown)=>{try{void Promise.resolve(action()).catch(error=>setResult(String(error)))}catch(error){setResult(String(error))}}
 const validate=()=>{
  client.flushJournal()
  const records=client.state.tools??[]
  const summary=records.filter(row=>row.name==='read_session_summary')
  const transcripts=records.filter(row=>row.name==='read_session_transcript')
  if(summary.length!==3||transcripts.length!==3)throw Error(`Expected three summaries and transcripts, got ${summary.length}/${transcripts.length}`)
  if(!summary.some(row=>row.outcome==='cached'))throw Error('Existing summary was not identified')
  if(new Set(records.map(row=>row.id)).size!==records.length)throw Error('Duplicate operation IDs')
  const saved=journal.flatMap((batch:any)=>batch.entries).filter((entry:any)=>entry.text==='call_tool_operation')
  if(saved.length!==records.filter(row=>row.outcome!=='running').length)throw Error('Journal and visible history differ')
  const history=document.querySelector('.copilot-tool-history')!
  if(history.querySelector('details, summary'))throw Error('Tool history must not use disclosure triangles')
  const chips=history.querySelectorAll('.copilot-tools-trail > li')
  if(chips.length!==records.length)throw Error('Every invocation must retain its capsule')
  if([...chips].map(chip=>chip.getAttribute('data-operation-id')).join()!==[...records].reverse().map(row=>row.id).join())throw Error('Newest invocations must appear first regardless of completion order')
  const chipStyle=getComputedStyle(chips[0])
  if(parseFloat(chipStyle.borderRadius)<100)throw Error('Tool entries must retain their capsule shape')
  const trail=history.getBoundingClientRect()
  const speech=document.querySelector('.realtime-transcript-box')!.getBoundingClientRect()
  const usage=document.querySelector('.realtime-secondary')!.getBoundingClientRect()
  if(trail.top<speech.bottom||trail.bottom>usage.top)throw Error('History moved out of its required location')
  if(document.documentElement.scrollWidth>window.innerWidth)throw Error('Page overflows horizontally')
  setResult(`PASS · ${records.length} records · 3 summaries · 3 transcripts · existing summary · correct placement · ${journal.length} journal batches · provider calls 0`)
 }
 return <>
  <div style={{position:'fixed',top:0,left:0,right:0,zIndex:99999,background:'#fff',color:'#111',padding:4,fontSize:11}}>
   <button onClick={()=>run(()=>client.prepareListReport())}>開始背景讀取</button>
   <button onClick={()=>release()}>完成背景讀取</button>
   <button onClick={()=>run(()=>client.runLiveTool('fixture-model-read','list_sessions','{}'))}>助理查詢清單</button>
   <button onClick={()=>run(async()=>{
    if(engine==='live'){client.handleLiveEvent({type:'session.input_transcript.delta',delta:'再說明一次'});client.handleLiveEvent({type:'session.output_transcript.delta',delta:'保留工具紀錄。'})}
    else{await client.handleRealtimeEvent({type:'input_audio_buffer.speech_started',item_id:'fixture-next'});await client.handleRealtimeEvent({type:'input_audio_buffer.speech_stopped'})}
   })}>新的一句話</button>
   <button onClick={()=>setOpen(value=>!value)}>收合／展開</button>
   <button onClick={()=>run(validate)}>驗證畫面</button>
   <output style={{display:'block'}}>{result}</output>
  </div>
  <RealtimeCopilotModal open={open} onClose={()=>setOpen(false)} onMinimize={()=>setOpen(false)}/>
 </>
}
createRoot(document.getElementById('root')!).render(<Fixture/> )
