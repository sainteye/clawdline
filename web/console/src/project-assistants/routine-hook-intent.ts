import type * as W from "@clawdline/contract"

/** Account, source authority and full durable Bot scope identify a journal, never a secret. */
export function routineScopeIdentity(source:{machineID:string;intentAccount?:string;authorizationRevision:string},bot:Pick<W.ProjectAssistantBot,"key"|"project"|"conversation_id">) {
  return JSON.stringify([source.intentAccount??"",source.machineID,source.authorizationRevision,bot.key.machine_id,bot.key.bot_id,bot.project.machine_id,bot.project.project_id,bot.project.canonical_id,bot.project.kind,bot.conversation_id])
}

export function validRoutineHookAnswer(answer:W.ProjectAssistantRoutineHookResponse,id:string,machine?:string):boolean {
  if(answer?.contract_version!=="project-assistants.v1"||answer.url!=="/hooks/routines/"+encodeURIComponent(id)||!/^[0-9a-f]{64}$/.test(answer.key))return false
  if(answer.public_url===undefined)return true
  if(typeof answer.public_url!=="string"||!machine)return false
  try {
    const url=new URL(answer.public_url)
    return url.protocol==="https:"&&!url.username&&!url.password&&!url.search&&!url.hash&&url.href===answer.public_url&&url.pathname==="/v1/hooks/"+encodeURIComponent(machine)+"/"+encodeURIComponent(id)
  } catch {return false}
}

/** No automatic replay: the caller invokes this only after an explicit human action. */
export async function performRoutineHook(request:W.ProjectAssistantRoutineHookRequest,options:{
  current:()=>boolean
  persist:(intent:{action:"hook";request:W.ProjectAssistantRoutineHookRequest})=>void
  send:(request:W.ProjectAssistantRoutineHookRequest)=>Promise<W.ProjectAssistantRoutineHookResponse>
}):Promise<W.ProjectAssistantRoutineHookResponse|null> {
  if(!options.current())return null
  // Capture exact original bytes before persistence/transport; callbacks cannot mutate the command.
  const original=JSON.parse(JSON.stringify(request)) as W.ProjectAssistantRoutineHookRequest
  options.persist({action:"hook",request:original})
  if(!options.current())return null
  const answer=await options.send(original)
  return options.current()&&validRoutineHookAnswer(answer,original.id,original.command.key.machine_id)?answer:null
}
