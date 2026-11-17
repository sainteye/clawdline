import {useEffect,useState} from "react"
import type * as W from "@clawdline/contract"
import type {SourceClient} from "./client"
import {errorCode} from "./client"
import type {Words} from "./words"

/** Discovery always owns a disposable native session, never the running turn. */
export function RuntimeSettings({source,bot,agent,loginProfile,loginState,value,onChange,w}:{source?:SourceClient;bot:W.ProjectAssistantBot;agent:W.ProjectAssistantAgent;loginProfile?:string;loginState?:string;value:W.ProjectAssistantRuntimeSettings;onChange:(value:W.ProjectAssistantRuntimeSettings)=>void;w:Words}){
  const [catalog,setCatalog]=useState<W.ProjectAssistantConfigurationCatalog|null>(null),[error,setError]=useState("")
  useEffect(()=>{
    const controller=new AbortController();let current=true
    setCatalog(null);setError("")
    if(!source?.canMemorySettings?.()||!source.configuration){setError("pa_unsupported");return}
    if(loginProfile&&loginState!=="signed_in"){setError("pa_auth_rejected");return}
    void source.configuration(bot.key,bot.conversation_id,agent,value.model,controller.signal,source.canLoginProfiles?.()?loginProfile:undefined).then(answer=>{if(current&&!controller.signal.aborted&&answer.catalog.agent===agent)setCatalog(answer.catalog)}).catch(e=>{if(current&&!controller.signal.aborted)setError(errorCode(e))})
    return()=>{current=false;controller.abort()}
  },[source,bot.key.machine_id,bot.key.bot_id,bot.conversation_id,agent,value.model,loginProfile,loginState])
  const model=catalog?.options.find(o=>o.category==="model")
  return <section className="pa-rules" aria-label={w.nativeSettings}>
    <h3>{w.nativeSettings}</h3>
    {error?<p role="status">{w.nativeUnavailable} ({error})</p>:!catalog?<p role="status">{w.nativeLoading}</p>:<>
      {model?<label>{w.model}<select aria-label={w.model} value={value.model} onChange={e=>onChange({model:e.target.value,options:{}})}><option value="">{w.nativeDefault}</option>{model.choices.map(c=><option key={c.value} value={c.value}>{c.name}</option>)}</select></label>:<p>{w.nativeNoModels}</p>}
      {catalog.options.filter(o=>o.category!=="model").map(o=><label key={o.id}>{o.name}<select aria-label={o.name} value={value.options[o.id]??""} onChange={e=>{const options={...value.options};if(e.target.value)options[o.id]=e.target.value;else delete options[o.id];onChange({...value,options})}}><option value="">{w.nativeDefault}</option>{o.choices.map(c=><option key={c.value} value={c.value}>{c.name==="true"?w.nativeOn:c.name==="false"?w.nativeOff:c.name}</option>)}</select></label>)}
    </>}
    <p className="pa-approval-detail">{w.nativeInherited}</p>
  </section>
}
