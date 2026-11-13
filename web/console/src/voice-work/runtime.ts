import { onMachineChanged } from '../cloud/machine-switch.js'
import { fleetOnThisPage, machinesOnThisPage, openOnThisPage, openSnippetsOnThisPage, openDevStackOnThisPage } from '../realtime/copilot-open.js'
import { COPILOT_TOOLS, readRows, runCopilotTool, type ToolDeps } from '../realtime/copilot-tools.js'
import { menuFingerprint } from '../session/fingerprint.js'
import { loadCopilotHelp } from '../realtime/copilot-help-loader.js'
import { VoiceWorkRuntime, workRequest, type VoiceTask, type VoiceFocus } from './engine.js'
let callDeps: ToolDeps | undefined
let callID = ''
export function useCallWorkTools(deps?: ToolDeps, call='') { callDeps=deps;callID=call;voiceWork.capabilitiesChanged() }
function workspaceTools():ToolDeps {
  return {fetch:(path,init)=>fetch(path,init),sleep:ms=>new Promise(resolve=>setTimeout(resolve,ms)),fleet:fleetOnThisPage,machines:machinesOnThisPage,openSession:openOnThisPage,snippets:openSnippetsOnThisPage,devstack:openDevStackOnThisPage,help:loadCopilotHelp}
}
function tools():ToolDeps {return callDeps??workspaceTools()}
export async function workDirectory(deps:ToolDeps=tools()) {
  const read=await readRows(deps)
  if('error' in read)throw new Error(read.error)
  return read.rows.map(r=>({id:r.id,label:r.label,state:r.state,assistant:r.assistant,conversation:r.conversation??'',machine:r.machine??'',question:r.question??'',options:(r.choices??[]).map(({n,label})=>({n,label})),menu_fingerprint:r.menu?menuFingerprint(r.menu as unknown as Parameters<typeof menuFingerprint>[0]):''}))
}
export const voiceWork = new VoiceWorkRuntime({
  request:(change,signal)=>workRequest(fetch,change,signal),
  capabilities:()=>({call:callID,tools:COPILOT_TOOLS.map(t=>t.name)}),
  execute:async(task:VoiceTask,signal,progress,beginEffect)=>{
    let transportUnknown=false
    let effectAccepted=false
    const inCall=!!task.call&&task.call===callID&&!!callDeps
    if(task.call&&!inCall)return {result:'',error:'voice_work_call_unavailable'}
    const original=inCall?callDeps!:workspaceTools()
    const guarded:ToolDeps['fetch']=async(path,init)=>{
      if(signal.aborted)throw new Error('voice_work_stopped')
      const mutation=!!init?.method&&!['GET','HEAD'].includes(init.method.toUpperCase())
      if(mutation)await beginEffect()
      if(signal.aborted)throw new Error('voice_work_stopped')
      try {
        const response=await original.fetch(path,{...init,signal})
        if(mutation && response.status>=500)transportUnknown=true
        if(mutation && response.ok)effectAccepted=true
        return response
      } catch(error) {if(mutation)transportUnknown=true;throw error}
    }
    const effect=async<T>(perform:()=>Promise<T>):Promise<T>=>{
      if(signal.aborted||task.call&&task.call!==callID)throw new Error('voice_work_stopped')
      await beginEffect()
      if(signal.aborted||task.call&&task.call!==callID)throw new Error('voice_work_stopped')
      effectAccepted=true
      return perform()
    }
    const base:ToolDeps={...original,fetch:guarded,marks:undefined,
      openSession:original.openSession?(...args)=>effect(()=>original.openSession!(...args)):undefined,
      snippets:original.snippets?(...args)=>effect(()=>original.snippets!(...args)):undefined,
      devstack:original.devstack?(...args)=>effect(()=>original.devstack!(...args)):undefined,
      adjust:original.adjust?(change)=>effect(()=>original.adjust!(change)):undefined,
    }
    // A request outside a call changes only the next-call defaults.
    if(!inCall && ['set_speaking_speed','set_answer_length'].includes(task.tool)) {
      const response=await guarded('/v1/settings/copilot')
      if(!response.ok)return {result:'',error:'voice_work_settings_unavailable'}
      const settings=await response.json() as {speed?:string;brief?:boolean}
      base.delivery=()=>({speed:settings.speed??'1',brief:settings.brief!==false})
      base.adjust=async(change)=>{
        const saved=await guarded('/v1/settings/copilot',{method:'POST',headers:{'Content-Type':'application/json','Idempotency-Key':task.action_key!},body:JSON.stringify(change)})
        if(!saved.ok)throw new Error('voice_work_settings_refused')
        return {saved:true}
      }
    }
    const args={...task.args}
    const target=args._voice_target as {id:string;label:string;conversation:string;machine:string}|undefined
    delete args._voice_target
    if(target) {
      const rows=await workDirectory(base)
      const row=rows.find(r=>r.id===target.id)
      if(!row||row.label!==target.label||row.conversation!==target.conversation||row.machine!==target.machine) return {result:'',error:'voice_work_target_changed'}
    }
    if(signal.aborted)throw new Error('voice_work_stopped')
    if(task.approved_version===task.version)args.switch_confirmed=true
    const definition=COPILOT_TOOLS.find(t=>t.name===task.tool)
    if(!definition)return {result:'',error:'voice_work_unknown_tool'}
    // Confirmation is always an actual UI action on this exact spec version.
    // The planner cannot mint force-close approval.
    delete args.force_confirmed
    // A rename's consent is the same UI confirmation, never the planner's word.
    delete args.confirmed
    if(task.tool==='smart_rename_sessions'&&task.approved_version===task.version)args.confirmed=true
    if(signal.aborted)throw new Error('voice_work_stopped')
    const result=await runCopilotTool(task.tool,JSON.stringify(args),{
      ...base,heard:()=>'',progress,
      newRequestID:()=>task.action_key!,
    })
    let body:Record<string,unknown>={}
    try {body=JSON.parse(result.output)} catch {return {result:result.output,error:'voice_work_unreadable_result',unknown:true}}
    if(!inCall && ['set_speaking_speed','set_answer_length'].includes(task.tool) && !body.error)body.applies_to='next_calls_only'
    return {result:JSON.stringify(body),...(body.error?{error:String(body.error),unknown:transportUnknown||effectAccepted}:{})}
  },
})
onMachineChanged(()=>{callDeps=undefined;callID='';voiceWork.switchMachine()})
export async function receiveVoiceWork(text:string,call='',id:string=crypto.randomUUID()) {
  const received=await voiceWork.change({action:'input',id,call,text})
  void workDirectory().then(sessions=>voiceWork.change({action:'plan',sessions,input_ids:[id],tool_contracts:COPILOT_TOOLS})).catch(error=>{voiceWork.failure=String(error)})
  return received
}

export async function retryVoicePlanning(inputIDs:string[]) {
  const sessions=await workDirectory()
  return voiceWork.change({action:'plan',sessions,input_ids:inputIDs,retry:true,tool_contracts:COPILOT_TOOLS})
}


/** Save conversation without starting the second model. The current input keeps
 * the target it had when spoken; a later screen change cannot rewrite it. */
export async function receiveCallInput(text:string,call:string,id:string,focus?:VoiceFocus,context='') {
  return voiceWork.change({action:'input',id,call,text,deferred:true,...(focus?{focus}:{}),...(context?{context}:{})})
}
export async function queueVoiceInputs(ids:string[],retry=false) {
  const sessions=await workDirectory()
  return voiceWork.change({action:'plan',sessions,input_ids:ids,tool_contracts:COPILOT_TOOLS,...(retry?{retry:true}:{})})
}
export function answerVoiceInput(id:string,call:string) {
  return voiceWork.change({action:'answer',id,call})
}

export function parkVoiceInput(id:string,call:string) {return voiceWork.change({action:'park',id,call})}
export function parkVoiceCall(call:string) {return voiceWork.change({action:'park_call',call})}
