import type * as W from "@clawdline/contract"

type Option=W.ProjectAssistantConfigOption

/** Native switch values shown in the person's language. */
export function choiceName(name:string,w:{nativeOn:string;nativeOff:string}):string{
  if(name==="true"||name==="On")return w.nativeOn
  if(name==="false"||name==="Off")return w.nativeOff
  return name
}

/** The Agent's own "Default" entry is the same choice as leaving the setting unset (Codync folds it too). */
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
