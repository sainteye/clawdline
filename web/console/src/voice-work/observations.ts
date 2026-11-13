import type { VoiceState } from './engine.js'

/** A call-local watermark over durable evidence, independent of which device ran it. */
export class WorkObservations {
  private workspace = ''
  private seen = new Set<string>()
  private primed = false
  private reply = ''
  constructor(initial:VoiceState|null) { if(initial)this.update(initial,true) }
  update(state:VoiceState|null,baseline=false):{key:string;value:string}[] {
    if(!state)return []
    if(this.workspace && this.workspace!==state.id){this.seen.clear();this.primed=false}
    this.workspace=state.id
    const silent=baseline||!this.primed
    this.primed=true
    const output:{key:string;value:string}[]=[]
    for(const task of state.tasks) {
      const prefix=`${task.id}:${task.version}:${task.attempt??0}`
      const emit=(key:string,value:unknown)=>{
        if(this.seen.has(key))return
        this.seen.add(key)
        if(!silent)output.push({key,value:JSON.stringify(value)})
      }
      for(const part of task.progress??[])emit(`${prefix}:progress:${part.sequence}`,{task:task.text,prepared:part.value})
      if(task.result_revision)emit(`${prefix}:result:${task.result_revision}`,{task:task.text,status:task.status,error:task.error,result:task.progress?.length?{prepared_updates:task.progress.length,completion:task.result==='Completed by the person.'?'confirmed_by_person':task.status}:task.result})
    }
    if(state.reply && state.reply !== this.reply && !silent)output.push({key:'plan',value:JSON.stringify({
      planner_reply:state.reply,tasks:state.tasks.filter(t=>t.status==='waiting_confirmation').map(t=>({text:t.text,status:t.status})),
    })})
    this.reply=state.reply??''
    return output
  }
}
