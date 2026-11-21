import {useRef,useState} from "react"
import type * as W from "@clawdline/contract"
import type {Words} from "./words"
import {AgentMark} from "./AgentMark"
import {agentName} from "./fallback-settings"
import {PaSelect,type PaOption} from "./PaSelect"
import {configurationError,type useModelCatalog} from "./RuntimeSettings"
import {retryableSourceError} from "./source-errors"
import {choiceName,nativeDefault,resolvedDefault,settingSummary} from "./agent-model-summary"

type Discovery=ReturnType<typeof useModelCatalog>

/**
 * Codync's agent and model entry: one summary row (logo, Agent, the model in use, its
 * settings) that opens into drop-downs. An unset model names what the Agent actually
 * runs instead of only saying "default".
 */
export function AgentModelPicker({agent,agents,onAgent,agentEditable,discovery,editable,reason,value,onChange,w}:{
  agent:W.ProjectAssistantAgent;agents:readonly W.ProjectAssistantAgent[];onAgent:(agent:W.ProjectAssistantAgent)=>void;agentEditable:boolean
  discovery:Discovery;editable:boolean;reason:string;value:W.ProjectAssistantRuntimeSettings;onChange:(value:W.ProjectAssistantRuntimeSettings)=>void;w:Words
}){
  const [open,setOpen]=useState(false)
  const {catalog,error,field,pending,retry}=discovery
  const model=catalog?.options.find(o=>o.category==="model")
  const settings=catalog?.options.filter(o=>o.category!=="model")??[]
  const modelDefault=model&&nativeDefault(model)
  const selectedModel=value.model===modelDefault?.value?"":value.model
  // What an unset model runs, kept while another model is tried: the catalog then describes that model.
  const defaults=useRef(new Map<string,string>())
  if(model&&!selectedModel){
    const resolved=resolvedDefault(model)
    if(resolved)defaults.current.set(agent,resolved)
  }
  const defaultModel=defaults.current.get(agent)
  const defaultLabel=defaultModel?w.nativeDefaultCurrent.replace("{name}",defaultModel):w.nativeDefault
  const modelName=selectedModel?(model?.choices.find(c=>c.value===selectedModel)?.name??selectedModel):defaultLabel
  const summary=settings.map(o=>settingSummary(o,value.options[o.id])).filter(Boolean).join(" · ")
  const agentOptions:PaOption[]=agents.map(a=>({value:a,label:agentName(a),icon:<AgentMark agent={a} size={16}/>}))
  const setOption=(id:string,next:string)=>{const options={...value.options};if(next)options[id]=next;else delete options[id];onChange({...value,options})}
  return <div className="pa-agent-picker">
    <button type="button" className="pa-agent-picker-summary" aria-expanded={open} aria-label={`${w.primaryAgent}: ${[agentName(agent),modelName,summary].filter(Boolean).join(" · ")}`} onClick={()=>setOpen(!open)}>
      <span className="pa-agent-picker-identity"><AgentMark agent={agent} size={16}/><span>{agentName(agent)}</span></span>
      <span className="pa-agent-picker-model">{modelName}</span>
      {summary&&<span className="pa-agent-picker-effort">{summary}</span>}
      <svg className="pa-agent-picker-chevron" viewBox="0 0 16 16" width="12" height="12" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="M4 6l4 4 4-4"/></svg>
    </button>
    {open&&<div className="pa-agent-picker-panel">
      <PaSelect label={w.primaryAgent} value={agent} options={agentOptions} disabled={!agentEditable} onChange={next=>onAgent(next as W.ProjectAssistantAgent)}/>
      {!editable?<p role="status" className="pa-approval-detail">{reason||w.cannotEdit}</p>:error?<div className="pa-agent-picker-error">
        <p role="status">{configurationError(error,w,field)} <small>({error})</small></p>
        {(error!=="pa_auth_rejected"||(field==="grok_login"||field==="gemini_login"))&&retryableSourceError(error)&&<button type="button" onClick={retry}>{w.nativeRetry}</button>}
        {(value.model||Object.keys(value.options).length>0)&&<button type="button" onClick={()=>onChange({model:"",options:{}})}>{w.nativeDefault}</button>}
      </div>:pending&&!catalog?<p role="status" className="pa-approval-detail">{w.nativeLoading}</p>:catalog&&<>
        {model?<PaSelect label={w.model} value={selectedModel} busy={pending} onChange={next=>onChange({model:next,options:{}})}
          options={[{value:"",label:defaultLabel},...model.choices.filter(c=>c.value!==modelDefault?.value).map(c=>({value:c.value,label:c.name,hint:c.description}))]}/>:<p className="pa-approval-detail">{w.nativeNoModels}</p>}
        {settings.map(o=>{
          const own=nativeDefault(o),resolved=resolvedDefault(o),chosen=value.options[o.id]
          return <PaSelect key={o.id} label={o.name} value={chosen===own?.value?"":chosen??""} busy={pending} onChange={next=>setOption(o.id,next)}
            options={[{value:"",label:resolved?w.nativeDefaultCurrent.replace("{name}",choiceName(resolved,w)):w.nativeDefault},...o.choices.filter(c=>c.value!==own?.value).map(c=>({value:c.value,label:choiceName(c.name,w)}))]}/>
        })}
      </>}
      <p className="pa-approval-detail">{w.nativeNextTurn}</p>
    </div>}
  </div>
}
