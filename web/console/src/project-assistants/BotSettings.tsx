import {useState,type ReactNode} from "react"
import type * as W from "@clawdline/contract"
import {AppearancePicker} from "./BotAvatar"
import type {Words} from "./words"
import {errorCode} from "./client"
import {ApprovalSelect} from "./ApprovalSelect"
import {withApproval} from "./approvals"
import {FallbackSettings,SelectedFallbackSettings,useFallbackSource,useSettingsSource} from "./FallbackSettings"
import {agentName,fallbackEdit} from "./fallback-settings"
import {ExtensionSettings} from "./ExtensionSettings"
import {LoginProfiles} from "./LoginProfiles"
import {configurationError,useModelCatalog} from "./RuntimeSettings"
import {AgentModelPicker} from "./AgentModelPicker"
import {NetworkSettingsPanel} from "./NetworkSettingsPanel"
import {SettingsDisclosure} from "./SettingsDisclosure"
import {catalogWord} from "../catalog"
import {botFailureText,providerSetupFailure} from "./source-errors"
import "./bot-settings.css"


export function settingsSaveFailure({code,field}:{code:string;field?:string},w:Words,conflict=w.fallbackSaveStale):string {
  if(code==="pa_outcome_unknown")return w.fallbackSaveUnknown
  if(code==="pa_stale_version")return conflict
  if(code==="pa_stale_generation")return w.fallbackSaveStale
  const detail=providerSetupFailure(field,w)??(code==="pa_capacity_full"?configurationError(code,w,field):botFailureText(code,w))
  return `${detail} (${code})`
}

/** Other Bot activity can advance the version without touching the settings draft. */
export function sameEditableSettings(a:W.ProjectAssistantBot,b:W.ProjectAssistantBot):boolean {
  const fields=(bot:W.ProjectAssistantBot)=>[
    bot.name,bot.appearance,bot.instructions.text,bot.agent,bot.permission_policy,bot.mode,
    bot.runtime_settings,bot.login_profile,bot.extensions,bot.fallback_agents,bot.fallback_steps,
    bot.notify,bot.background_threads,bot.manage_team,
  ]
  return JSON.stringify(fields(a))===JSON.stringify(fields(b))
}

/** One mounted editor owns the draft across the Details and Settings tabs. */
export function BotSettings({bot,onSave,onClose,w,enabled,reason="",rules,manage,data,workspace,teamAvailable=false,embedded=false}:{bot:W.ProjectAssistantBot;onSave:(settings:W.ProjectAssistantEditSettings,original:W.ProjectAssistantBot)=>Promise<void>;onClose:()=>void;w:Words;enabled:boolean;reason?:string;rules?:ReactNode;manage?:ReactNode;data?:ReactNode;workspace?:{project:string;machine:string};teamAvailable?:boolean;embedded?:boolean}) {
  const [original]=useState(bot)
  const [name,setName]=useState(bot.name),[appearance,setAppearance]=useState(bot.appearance),[instructions,setInstructions]=useState(bot.instructions.text)
  const [busy,setBusy]=useState(false),[error,setError]=useState<{code:string;field?:string}>(),[unknown,setUnknown]=useState(false)
  const [approval,setApproval]=useState<{permission_policy:W.ProjectAssistantPermissionPolicy;mode?:string}>(bot.mode?{permission_policy:bot.permission_policy,mode:bot.mode}:{permission_policy:bot.permission_policy})
  const [originalSteps]=useState(()=>bot.fallback_steps??(bot.fallback_agents??[]).map(agent=>({agent})))
  const available=useFallbackSource(),source=useSettingsSource(),modern=source?.canMemorySettings?.()===true
  const [fallbacks,setFallbacks]=useState(bot.fallback_agents??[]),[fallbackSteps,setFallbackSteps]=useState<W.ProjectAssistantFallbackStep[]>(originalSteps)
  const [agent,setAgent]=useState(bot.agent),[runtime,setRuntime]=useState<W.ProjectAssistantRuntimeSettings>(bot.runtime_settings??{model:"",options:{}})
  const [backgroundThreads,setBackgroundThreads]=useState(bot.background_threads??3)
  const [notify,setNotify]=useState(bot.notify!=="off"),[manageTeam,setManageTeam]=useState(bot.manage_team??false)
  const showTeamSetting=!(bot.lead&&source?.canLeadTeamDefault?.())
  const teamChanged=showTeamSetting&&manageTeam!==(original.manage_team??false)
  const [extensions,setExtensions]=useState<W.ProjectAssistantExtensionSelection>(bot.extensions??{skills:[],connectors:[]})
  const [loginProfile,setLoginProfile]=useState(bot.login_profile??""),[loginState,setLoginState]=useState("")
  const selectedFallbacks=source?.canFallbackProfiles?.()===true
  // As in Codync, the model list loads with the editor so the summary names the model in use.
  const discovery=useModelCatalog(source,bot,agent,runtime.model,loginProfile,loginState,enabled)
  const runtimeChanged=JSON.stringify(runtime)!==JSON.stringify(original.runtime_settings??{model:"",options:{}})
  const awaitingModel=runtimeChanged&&discovery.pending
  const validFallbacks=fallbacks.filter(step=>step!==agent)
  const validSteps=fallbackSteps.filter(step=>step.agent!==agent||(step.login_profile??"")!==loginProfile)
  const fallbackFields=()=>{
    if(!selectedFallbacks)return fallbackEdit(agent===original.agent?original.fallback_agents:[],validFallbacks,available)
    if(agent===original.agent&&JSON.stringify(originalSteps)===JSON.stringify(validSteps))return {}
    return {fallback_steps:validSteps,...(validSteps.length===0?{fallback_agents:[]}: {})}
  }
  const save=async()=>{
    if(!enabled||busy||unknown||awaitingModel||!name.trim())return
    setBusy(true);setError(undefined)
    try{
      await onSave({name:name.trim(),appearance,instructions,expected_version:original.version,...approval,...fallbackFields(),
        ...(source?.canLoginProfiles?.()?{login_profile:{id:loginProfile},extensions}:{}),
        ...(modern?{agent,runtime_settings:runtime,notify:notify?"on" as const:"off" as const}:{}),
        ...(backgroundThreads!==(original.background_threads??3)?{background_threads:backgroundThreads}:{}),
        ...(teamChanged?{manage_team:manageTeam}:{})},original)
    }catch(e){
      const code=errorCode(e),field=(e as Partial<W.ProjectAssistantErrorResponse>|null)?.error?.field
      setError({code,field})
      if(code==="pa_outcome_unknown")setUnknown(true)
    }
    finally{setBusy(false)}
  }
  const chooseAgent=(next:W.ProjectAssistantAgent)=>{
    setAgent(next);setLoginProfile("");setExtensions({skills:extensions.skills,connectors:[]})
    setRuntime({model:"",options:{}})
    setApproval({permission_policy:approval.permission_policy})
  }
  const dirty=backgroundThreads!==(original.background_threads??3)||name!==original.name||JSON.stringify(appearance)!==JSON.stringify(original.appearance)||instructions!==original.instructions.text||agent!==original.agent||runtimeChanged||JSON.stringify(approval)!==JSON.stringify(original.mode?{permission_policy:original.permission_policy,mode:original.mode}:{permission_policy:original.permission_policy})||JSON.stringify(fallbacks)!==JSON.stringify(original.fallback_agents??[])||JSON.stringify(fallbackSteps)!==JSON.stringify(originalSteps)||notify!==(original.notify!=="off")||teamChanged||loginProfile!==(original.login_profile??"")||JSON.stringify(extensions)!==JSON.stringify(original.extensions??{skills:[],connectors:[]})
  const agentChoices:W.ProjectAssistantAgent[]=["claude","codex",...(agent==="grok"||source?.canGrok?.()?["grok" as const]:[]),...(agent==="antigravity"||source?.canGemini?.()?["antigravity" as const]:[])]
  const chain=selectedFallbacks?validSteps.map(step=>step.agent):validFallbacks
  // Codync's Settings tab: titled groups, always open, one row per control.
  const switchRow=(title:string,hint:string,on:boolean,disabled:boolean,change:(on:boolean)=>void)=><div className="pa-settings-row">
    <div><strong>{title}</strong><small>{hint}</small></div>
    <button type="button" role="switch" className="pa-switch" aria-checked={on} aria-label={title} disabled={disabled} onClick={()=>change(!on)}><span/></button>
  </div>
  const savebar=<footer className="pa-toolbar pa-settings-savebar" hidden={embedded&&!dirty&&!busy&&!error&&!unknown}>
    {error&&<p className="pa-settings-save-error" role="alert">{settingsSaveFailure(error,w,embedded?w.settingsSaveConflict:undefined)}</p>}
    {embedded&&<small>{w.settingsDraftHint}</small>}
    <button type="button" disabled={busy} onClick={onClose}>{w.cancel}</button>{!embedded&&<h2>{w.settings}</h2>}<button type="button" className="pa-settings-save" disabled={!enabled||busy||unknown||awaitingModel||!name.trim()} aria-busy={busy||undefined} onClick={()=>void save()}>{busy?w.settingsSaving:w.save}</button>
  </footer>
  return <section className={`${embedded?"pa-embedded-form":"pa-form-panel"} pa-settings-editor`} role={embedded?undefined:"dialog"} aria-modal={embedded?undefined:true} aria-label={w.settings}>
    {!embedded&&savebar}
    <div className="pa-form-body">
      <fieldset disabled={!enabled||busy||unknown}>
        <SettingsGroup title={w.settingsInstructionsHeading} footer={w.nextIdle}>
          <label className="pa-settings-instructions">{w.instructions}<small>{w.settingsInstructionsHint}</small><textarea value={instructions} onChange={e=>setInstructions(e.target.value)} rows={4}/></label>
        </SettingsGroup>
        <SettingsGroup title={w.settingsAgentHeading}>
          <AgentModelPicker agent={agent} agents={agentChoices} onAgent={chooseAgent} agentEditable={modern} discovery={discovery} editable={enabled} reason={reason} value={runtime} onChange={setRuntime} w={w}/>
          {agent==="grok"||agent==="antigravity"?<p className="pa-approval-detail">{agent==="grok"?w.grokUsesExistingLogin:w.geminiUsesExistingLogin}</p>:<LoginProfiles compact source={source} bot={bot} agent={agent} value={loginProfile} onChange={setLoginProfile} onStatusChange={setLoginState} w={w}/>}
          <SettingsDisclosure title={w.fallbacks} summary={chain.length?chain.map(agentName).join(" → "):w.settingsNoFallbacks}>
            {selectedFallbacks?<SelectedFallbackSettings bot={{...bot,agent,login_profile:loginProfile}} steps={validSteps} onChange={setFallbackSteps} mode={approval.mode} w={w}/>:<FallbackSettings bot={{...bot,agent}} agents={validFallbacks} onChange={setFallbacks} available={available} mode={approval.mode} w={w}/>}
          </SettingsDisclosure>
        </SettingsGroup>
        <SettingsGroup title={w.settingsIdentity}>
          <label className="pa-settings-field">{w.name}<input aria-label={w.name} value={name} onChange={e=>setName(e.target.value)}/></label>
          <AppearancePicker value={appearance} onChange={setAppearance} w={w}/>
        </SettingsGroup>
        {workspace&&<SettingsGroup title={w.workspace}>
          <div className="pa-settings-value"><span>{w.workspace}</span><strong>{workspace.project||w.unnamedProject}</strong></div>
          <div className="pa-settings-value"><span>{w.machine}</span><strong>{workspace.machine||w.thisMachine}</strong></div>
        </SettingsGroup>}
        <SettingsGroup title={w.settingsAccess}>
          {switchRow(w.botNotify,w.botNotifyHint,notify,!modern,setNotify)}
          {bot.background_threads!==undefined&&<label className="pa-settings-field">{w.backgroundConcurrency}<select value={backgroundThreads} onChange={e=>setBackgroundThreads(Number(e.target.value))}>{[1,2,3,4,5,6].map(n=><option key={n} value={n}>{n}</option>)}</select></label>}
          <ApprovalSelect agent={agent} policy={approval.permission_policy} mode={approval.mode} w={w} onChange={id=>setApproval(withApproval(id))}/>
          {showTeamSetting&&switchRow(catalogWord("projectAssistants","teamManage"),catalogWord("projectAssistants",!teamAvailable?"teamUnavailable":"teamManageHint"),manageTeam,!teamAvailable,setManageTeam)}
        </SettingsGroup>
        <SettingsGroup title={w.extensions}><ExtensionSettings bare source={source} bot={{...bot,agent}} value={extensions} onChange={setExtensions} w={w}/></SettingsGroup>
      </fieldset>
      {source&&source.canNetworkSettings?.()===true&&<SettingsGroup title={w.settingsNetwork}><NetworkSettingsPanel bare source={source} bot={bot} enabled={enabled&&!busy&&!unknown} w={w}/></SettingsGroup>}
      {rules&&<SettingsGroup title={w.settingsRules}>{rules}</SettingsGroup>}
      {data&&<SettingsGroup>{data}</SettingsGroup>}
      {manage&&<SettingsGroup title={w.manageTitle}>{manage}</SettingsGroup>}
      {!enabled&&reason&&<p role="status" className="pa-warning">{w.cannotEdit}: {reason}</p>}
    </div>
    {embedded&&savebar}
  </section>
}

/** One titled settings group: the title above an outlined card whose rows are separated by hairlines. */
function SettingsGroup({title,footer,children}:{title?:string;footer?:string;children:ReactNode}) {
  return <section className="pa-settings-group" aria-label={title}>
    {title&&<h3>{title}</h3>}
    <div className="pa-settings-card">{children}</div>
    {footer&&<p className="pa-settings-footer">{footer}</p>}
  </section>
}
