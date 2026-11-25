import {useRef,useState} from "react"
import type * as W from "@clawdline/contract"
import type {Words} from "./words"
import {AgentMark} from "./AgentMark"
import {agentName} from "./fallback-settings"
import {PaSelect,type PaOption} from "./PaSelect"
import {configurationError,type useBotConfiguration} from "./RuntimeSettings"
import {retryableSourceError} from "./source-errors"
import {catalogRuntime,choiceName,hasEffortOption,joinChoice,nativeDefault,resolvedDefault,runtimeModelPicker,settingSummary,splitChoice} from "./agent-model-summary"
import type {AgentModels} from "../model-catalog/reader"
import {paSelectStatus} from "../model-catalog/picker"
import {pickerWords} from "../model-catalog/ui"
import {catalogWord} from "../catalog"

type Discovery=ReturnType<typeof useBotConfiguration>

/** The words of a Bot's catalog model and effort drop-downs. */
export function runtimeModelWords(){
  return {...pickerWords(),effortDefault:catalogWord("botsSettings","effortDefault")}
}

/**
 * A Bot's model and effort from the machine's model catalog. The saved value is a
 * catalog choice ("model" or "model/effort") or an older ACP value, which reads as its
 * catalog entry; the daemon maps either to the Agent's ACP choice when it applies it.
 */
export function CatalogModelSelect({agent,models,retry,value,onChange,w,busy,disabled,effortOption}:{
  agent:W.ProjectAssistantAgent;models:AgentModels;retry:()=>void;value:string;onChange:(value:string)=>void;w:Words;busy?:boolean;disabled?:boolean;effortOption?:boolean
}){
  const p=runtimeModelPicker(models,value,agentName(agent),runtimeModelWords(),effortOption)
  return <>
    <PaSelect label={w.model} value={p.model} placeholder={p.model} busy={busy||p.loading} disabled={disabled} status={paSelectStatus(p.status,retry)}
      options={p.options} onChange={next=>onChange(joinChoice(next,next?p.effort:""))}/>
    {p.efforts.length>0&&<PaSelect label={catalogWord("botsSettings","effort")} value={p.effort} busy={busy} disabled={disabled||!p.model}
      options={p.efforts} onChange={next=>onChange(joinChoice(p.model,next))}/>}
  </>
}

/** Gemini's model list as its own ACP adapter answers it (catalogRuntime). */
export function AgentModelSelect({model,value,defaultLabel,onChange,w,busy,disabled}:{
  model:W.ProjectAssistantConfigOption;value:string;defaultLabel:string;onChange:(value:string)=>void;w:Words;busy?:boolean;disabled?:boolean
}){
  const own=nativeDefault(model)
  const selected=value===own?.value?"":value
  return <PaSelect label={w.model} value={selected} placeholder={selected} busy={busy} disabled={disabled} onChange={onChange}
    options={[{value:"",label:defaultLabel},...model.choices.filter(choice=>choice.value!==own?.value)
      .map(choice=>({value:choice.value,label:choice.name||choice.value,hint:choice.description}))]}/>
}

/**
 * Codync's agent and model entry: one summary row (logo, Agent, the model in use, its
 * settings) that opens into drop-downs. An unset model names what the Agent actually
 * runs instead of only saying "default". The models come from the machine's model
 * catalog (`models`); the Agent's other options from this Bot's ACP answer (`discovery`).
 */
export function AgentModelPicker({agent,agents,onAgent,agentEditable,discovery,models,modelsRetry,editable,reason,value,onChange,w}:{
  agent:W.ProjectAssistantAgent;agents:readonly W.ProjectAssistantAgent[];onAgent:(agent:W.ProjectAssistantAgent)=>void;agentEditable:boolean
  discovery:Discovery;models:AgentModels;modelsRetry:()=>void;editable:boolean;reason:string;value:W.ProjectAssistantRuntimeSettings;onChange:(value:W.ProjectAssistantRuntimeSettings)=>void;w:Words
}){
  const [open,setOpen]=useState(false)
  const {catalog,error,field,pending,retry}=discovery
  const fromCatalog=catalogRuntime(agent)
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
  const effortOption=hasEffortOption(settings)
  const listed=fromCatalog?runtimeModelPicker(models,value.model,agentName(agent),runtimeModelWords(),effortOption):undefined
  const modelName=listed?[listed.modelLabel,listed.effort].filter(Boolean).join(" · "):
    selectedModel?(model?.choices.find(c=>c.value===selectedModel)?.name??selectedModel):defaultLabel
  const summary=settings.map(o=>settingSummary(o,value.options[o.id])).filter(Boolean).join(" · ")
  const agentOptions:PaOption[]=agents.map(a=>({value:a,label:agentName(a),icon:<AgentMark agent={a} size={16}/>}))
  const setOption=(id:string,next:string)=>{const options={...value.options};if(next)options[id]=next;else delete options[id];onChange({...value,options})}
  const failure=error&&<div className="pa-agent-picker-error">
    <p role="status">{configurationError(error,w,field)} <small>({error})</small></p>
    {(error!=="pa_auth_rejected"||(field==="grok_login"||field==="gemini_login"))&&retryableSourceError(error)&&<button type="button" onClick={retry}>{w.nativeRetry}</button>}
    {(value.model||Object.keys(value.options).length>0)&&<button type="button" onClick={()=>onChange({model:"",options:{}})}>{w.nativeDefault}</button>}
  </div>
  const options=settings.map(o=>{
    const own=nativeDefault(o),resolved=resolvedDefault(o),chosen=value.options[o.id]
    return <PaSelect key={o.id} label={o.name} value={chosen===own?.value?"":chosen??""} busy={pending} onChange={next=>setOption(o.id,next)}
      options={[{value:"",label:resolved?w.nativeDefaultCurrent.replace("{name}",choiceName(resolved,w)):w.nativeDefault},...o.choices.filter(c=>c.value!==own?.value).map(c=>({value:c.value,label:choiceName(c.name,w)}))]}/>
  })
  return <div className="pa-agent-picker">
    <button type="button" className="pa-agent-picker-summary" aria-expanded={open} aria-label={`${w.primaryAgent}: ${[agentName(agent),modelName,summary].filter(Boolean).join(" · ")}`} onClick={()=>setOpen(!open)}>
      <span className="pa-agent-picker-identity"><AgentMark agent={agent} size={16}/><span>{agentName(agent)}</span></span>
      <span className="pa-agent-picker-model">{modelName}</span>
      {summary&&<span className="pa-agent-picker-effort">{summary}</span>}
      <svg className="pa-agent-picker-chevron" viewBox="0 0 16 16" width="12" height="12" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="M4 6l4 4 4-4"/></svg>
    </button>
    {open&&<div className="pa-agent-picker-panel">
      <PaSelect label={w.primaryAgent} value={agent} options={agentOptions} disabled={!agentEditable} onChange={next=>onAgent(next as W.ProjectAssistantAgent)}/>
      {!editable?<p role="status" className="pa-approval-detail">{reason||w.cannotEdit}</p>:fromCatalog?<>
        {/* A model change starts from that model's own options, as the Agent answers them; an effort change keeps them. */}
        <CatalogModelSelect agent={agent} models={models} retry={modelsRetry} value={value.model} effortOption={effortOption} w={w}
          onChange={next=>onChange({model:next,options:splitChoice(next).model===splitChoice(value.model).model?value.options:{}})}/>
        {failure||(pending&&!catalog?<p role="status" className="pa-approval-detail">{w.nativeLoading}</p>:options)}
      </>:failure||(pending&&!catalog?<p role="status" className="pa-approval-detail">{w.nativeLoading}</p>:catalog&&<>
        {model?<AgentModelSelect model={model} value={value.model} busy={pending} onChange={next=>onChange({model:next,options:{}})}
          defaultLabel={defaultLabel} w={w}/>:<p className="pa-approval-detail">{w.nativeNoModels}</p>}
        {options}
      </>)}
      <p className="pa-approval-detail">{w.nativeNextTurn}</p>
    </div>}
  </div>
}
