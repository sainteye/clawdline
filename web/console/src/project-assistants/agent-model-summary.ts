import type * as W from "@clawdline/contract"
import type {AgentModels} from "../model-catalog/reader"
// @ts-expect-error -- Node's strip-types test runner needs the source extension.
import {catalogPicker,type PickerOption,type PickerStatus,type PickerWords} from "../model-catalog/picker.ts"

type Option=W.ProjectAssistantConfigOption

/** Native switch values shown in the person's language. */
export function choiceName(name:string,w:{nativeOn:string;nativeOff:string}):string{
  if(name==="true"||name==="On")return w.nativeOn
  if(name==="false"||name==="Off")return w.nativeOff
  return name
}

/**
 * The Agent's own "Default" entry is the same choice as leaving the setting unset (Codync folds it too).
 * Only the Agent's own non-model options use it now; a model's default is the catalog's
 * `default_name`, and a saved model "default" reads as no model (splitChoice).
 */
export function nativeDefault(o:Option):W.ProjectAssistantConfigChoice|undefined{
  return o.choices.find(c=>c.value==="default"||c.name.toLowerCase().startsWith("default"))
}

/**
 * What an unset setting runs: the default entry's note (Claude names the resolved
 * model there), or the Agent's current value when it is a concrete choice. Empty
 * when the Agent does not say.
 */
export function resolvedDefault(o:Option):string{
  const own=nativeDefault(o)
  if(own?.description)return own.description
  if(own&&o.current===own.value)return ""
  const current=o.choices.find(c=>c.value===o.current)
  return current?.description||current?.name||""
}

/** The summary-row text of a non-model setting: a chosen value, or an enabled switch by its name. */
export function settingSummary(o:Option,chosen:string|undefined):string{
  const own=nativeDefault(o)
  const value=chosen??o.current
  if(!value||value===own?.value||value==="false"||value==="off")return ""
  if(value==="true"||value==="on")return o.name
  return o.choices.find(c=>c.value===value)?.name??value
}

// Effort names a saved runtime model may carry after "/", as the daemon reads
// them (modelcatalog.FromChoice). A model id that merely contains "/" stays one model.
const EFFORTS=new Set(["none","minimal","low","medium","high","xhigh","max"])

/**
 * A saved runtime model as the catalog entry a picker shows: "gpt-5.5/high" is model
 * gpt-5.5 with effort high, the Agent's own "default" is no model (the Agent default),
 * anything else is a model as saved. The daemon applies the same string
 * (modelcatalog.ToConfiguration), so old ACP values and catalog choices read alike.
 */
export function splitChoice(value:string|undefined):{model:string;effort:string}{
  const v=value??""
  if(v==="default")return {model:"",effort:""}
  const i=v.lastIndexOf("/")
  if(i>0&&EFFORTS.has(v.slice(i+1)))return {model:v.slice(0,i),effort:v.slice(i+1)}
  return {model:v,effort:""}
}

/** What a picker saves for a catalog model and effort; an effort needs a model. */
export function joinChoice(model:string,effort:string):string{
  return model&&effort?`${model}/${effort}`:model
}

/**
 * Whether a Bot's runtime model list comes from the daemon model catalog. Gemini
 * (antigravity) stays on its own ACP list: the catalog's fast tier is `agy models`,
 * whose names are not proved to be the values Gemini's ACP adapter accepts.
 */
export function catalogRuntime(agent:string):boolean{
  return agent!=="antigravity"
}

/** Whether the Agent takes effort as its own ACP option, which its own drop-down then sets. */
export function hasEffortOption(settings:readonly Option[]):boolean{
  return settings.some(o=>o.category==="thought_level"||o.id==="effort"||o.id==="reasoning_effort"||o.id==="reasoning-effort"||o.id==="thought_level")
}

export type RuntimeModelWords=PickerWords&{effortDefault:string}

export type RuntimeModelPicker={
  /** The saved choice as the drop-downs show it. */
  model:string;effort:string
  options:PickerOption[];status?:PickerStatus;loading:boolean
  /** The row for no model: the Agent default, named when the catalog names it. */
  defaultLabel:string
  /** What the summary row names: the chosen model's label, or the default row. */
  modelLabel:string
  /** The effort drop-down's rows; empty when it is not shown. */
  efforts:PickerOption[]
}

/**
 * A Bot's runtime model drop-downs from the machine's model catalog: the catalog's
 * models (Agent default first) and its efforts. The effort drop-down is left out when
 * the Agent takes effort as its own option (shown with its other options) unless the
 * saved choice already names one; a saved effort the catalog does not list stays visible.
 */
export function runtimeModelPicker(models:AgentModels,value:string|undefined,agentName:string,words:RuntimeModelWords,effortOption=false):RuntimeModelPicker{
  const {model,effort}=splitChoice(value)
  const picker=catalogPicker(models,model||undefined,agentName,words)
  const defaultLabel=picker.options[0]?.label??words.defaultPlain
  const modelLabel=model?(picker.options.find((o:PickerOption)=>o.value===model)?.label??model):defaultLabel
  let efforts:PickerOption[]=[]
  if(effort||(picker.efforts.length&&!effortOption)){
    efforts=[{value:"",label:words.effortDefault},...picker.efforts.map((e:string)=>({value:e,label:e}))]
    if(effort&&!efforts.some(o=>o.value===effort))efforts.push({value:effort,label:effort,hint:models.status==="ready"&&!picker.loading?words.missing:undefined})
  }
  return {model,effort,options:picker.options,status:picker.status,loading:picker.loading,defaultLabel,modelLabel,efforts}
}

/** The chat header's model text for a saved choice, without reading any list. */
export function choiceLabel(value:string|undefined,defaultLabel:string):string{
  const {model,effort}=splitChoice(value)
  if(!model)return defaultLabel
  return effort?`${model} · ${effort}`:model
}
