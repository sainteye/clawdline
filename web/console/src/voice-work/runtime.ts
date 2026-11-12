import { onMachineChanged } from '../cloud/machine-switch.js'
import { fleetOnThisPage, machinesOnThisPage, openOnThisPage, openSnippetsOnThisPage, openDevStackOnThisPage } from '../realtime/copilot-open.js'
import { COPILOT_TOOLS, readRows, runCopilotTool, type ToolDeps } from '../realtime/copilot-tools.js'
import { VoiceWorkRuntime, workRequest, type VoiceTask } from './engine.js'
let callDeps: ToolDeps | undefined
export function useCallWorkTools(deps?: ToolDeps) { callDeps=deps }
function tools():ToolDeps {
  return callDeps ?? {fetch:(path,init)=>fetch(path,init),sleep:ms=>new Promise(resolve=>setTimeout(resolve,ms)),fleet:fleetOnThisPage,machines:machinesOnThisPage,openSession:openOnThisPage,snippets:openSnippetsOnThisPage,devstack:openDevStackOnThisPage}
}
export async function workDirectory() {
  const read=await readRows(tools())
  if('error' in read)throw new Error(read.error)
  return read.rows.map(r=>({id:r.id,label:r.label,state:r.state,assistant:r.assistant,conversation:r.conversation??'',machine:r.machine??''}))
}
export const voiceWork = new VoiceWorkRuntime({
  request:(change,signal)=>workRequest(fetch,change,signal),
  execute:async(task:VoiceTask,signal,progress)=>{
    const base=tools()
    const args={...task.args}
    const target=args._voice_target as {id:string;label:string;conversation:string;machine:string}|undefined
    delete args._voice_target
    if(target) {
      const rows=await workDirectory()
      const row=rows.find(r=>r.id===target.id)
      if(!row||row.label!==target.label||row.conversation!==target.conversation||row.machine!==target.machine) return {result:'',error:'voice_work_target_changed'}
    }
    if(signal.aborted)throw new Error('voice_work_machine_changed')
    if(task.approved_version===task.version)args.switch_confirmed=true
    const definition=COPILOT_TOOLS.find(t=>t.name===task.tool)
    if(!definition)return {result:'',error:'voice_work_unknown_tool'}
    // Confirmation is always an actual UI action on this exact spec version.
    // The planner cannot mint force-close approval.
    delete args.force_confirmed
    const result=await runCopilotTool(task.tool,JSON.stringify(args),{
      ...base,heard:()=>'',progress,
      newRequestID:()=>task.action_key!,
      fetch:(path,init)=>{
        if(signal.aborted)throw new Error('voice_work_machine_changed')
        return base.fetch(path,{...init,signal})
      },
    })
    let body:Record<string,unknown>={}
    try {body=JSON.parse(result.output)} catch {return {result:result.output,error:'voice_work_unreadable_result',unknown:true}}
    return {result:result.output,...(body.error?{error:String(body.error),unknown:true}:{})}
  },
})
onMachineChanged(()=>{callDeps=undefined;voiceWork.switchMachine()})
export async function receiveVoiceWork(text:string,call='',id:string=crypto.randomUUID()) {
  const received=await voiceWork.change({action:'input',id,call,text})
  void workDirectory().then(sessions=>voiceWork.change({action:'plan',sessions,input_ids:[id]})).catch(error=>{voiceWork.failure=String(error)})
  return received
}
