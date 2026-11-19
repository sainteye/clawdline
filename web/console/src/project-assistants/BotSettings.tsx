import {useEffect,useRef,useState,type ReactNode} from "react"
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
import {configurationError,RuntimeSettings,useModelCatalog} from "./RuntimeSettings"
import {NetworkSettingsPanel} from "./NetworkSettingsPanel"
import {SettingsDisclosure} from "./SettingsDisclosure"
import {catalogWord} from "../catalog"
import {botFailureText} from "./source-errors"
import "./bot-settings.css"

export type BotSettingsPart="all"|"often"|"rarely"

function settingsSaveFailure({code,field}:{code:string;field?:string},w:Words):string {
  if(code==="pa_outcome_unknown")return w.fallbackSaveUnknown
  if(code==="pa_stale_generation"||code==="pa_stale_version")return w.fallbackSaveStale
  const detail=code==="pa_capacity_full"?configurationError(code,w,field):botFailureText(code,w)
  return `${detail} (${code})`
}

/** One mounted editor owns the draft across the Details and Settings tabs. */
export function BotSettings({bot,onSave,onClose,onFrequent,w,enabled,reason="",rules,manage,teamAvailable=false,embedded=false,part="all"}:{bot:W.ProjectAssistantBot;onSave:(settings:W.ProjectAssistantEditSettings)=>Promise<void>;onClose:()=>void;onFrequent?:()=>void;w:Words;enabled:boolean;reason?:string;rules?:ReactNode;manage?:ReactNode;teamAvailable?:boolean;embedded?:boolean;part?:BotSettingsPart}) {
  const [name,setName]=useState(bot.name),[appearance,setAppearance]=useState(bot.appearance),[instructions,setInstructions]=useState(bot.instructions.text)
  const instructionsField=useRef<HTMLTextAreaElement>(null),focusInstructions=useRef(false)
  useEffect(()=>{if(part!=="rarely"&&focusInstructions.current){focusInstructions.current=false;instructionsField.current?.focus()}},[part])
  const editFrequent=()=>{focusInstructions.current=true;onFrequent?.()}
  const [busy,setBusy]=useState(false),[error,setError]=useState<{code:string;field?:string}>(),[unknown,setUnknown]=useState(false)
  const [approval,setApproval]=useState<{permission_policy:W.ProjectAssistantPermissionPolicy;mode?:string}>(bot.mode?{permission_policy:bot.permission_policy,mode:bot.mode}:{permission_policy:bot.permission_policy})
  const [original]=useState(()=>({version:bot.version,fallbacks:bot.fallback_agents,steps:bot.fallback_steps??(bot.fallback_agents??[]).map(agent=>({agent}))}))
  const available=useFallbackSource(),source=useSettingsSource(),modern=source?.canMemorySettings?.()===true
  const [fallbacks,setFallbacks]=useState(bot.fallback_agents??[]),[fallbackSteps,setFallbackSteps]=useState<W.ProjectAssistantFallbackStep[]>(original.steps)
  const [agent,setAgent]=useState(bot.agent),[runtime,setRuntime]=useState<W.ProjectAssistantRuntimeSettings>(bot.runtime_settings??{model:"",options:{}})
  const [notify,setNotify]=useState(bot.notify!=="off"),[manageTeam,setManageTeam]=useState(bot.manage_team??false)
  const [extensions,setExtensions]=useState<W.ProjectAssistantExtensionSelection>(bot.extensions??{skills:[],connectors:[]})
  const [loginProfile,setLoginProfile]=useState(bot.login_profile??""),[loginState,setLoginState]=useState("")
  const selectedFallbacks=source?.canFallbackProfiles?.()===true,frequent=part!=="rarely",rare=part!=="often"
  const [modelsVisited,setModelsVisited]=useState(false)
  const discovery=useModelCatalog(source,bot,agent,runtime.model,loginProfile,loginState,modelsVisited&&enabled)
  const runtimeChanged=JSON.stringify(runtime)!==JSON.stringify(bot.runtime_settings??{model:"",options:{}})
  const awaitingModel=runtimeChanged&&discovery.pending
  const validFallbacks=fallbacks.filter(step=>step!==agent)
  const validSteps=fallbackSteps.filter(step=>step.agent!==agent||(step.login_profile??"")!==loginProfile)
  const fallbackFields=()=>{
    if(!selectedFallbacks)return fallbackEdit(agent===bot.agent?original.fallbacks:[],validFallbacks,available)
    if(agent===bot.agent&&JSON.stringify(original.steps)===JSON.stringify(validSteps))return {}
    return {fallback_steps:validSteps,...(validSteps.length===0?{fallback_agents:[]}: {})}
  }
  const save=async()=>{
    if(!enabled||busy||unknown||awaitingModel||!name.trim())return
    setBusy(true);setError(undefined)
    try{
      await onSave({name:name.trim(),appearance,instructions,expected_version:original.version,...approval,...fallbackFields(),
        ...(source?.canLoginProfiles?.()?{login_profile:{id:loginProfile},extensions}:{}),
        ...(modern?{agent,runtime_settings:runtime,notify:notify?"on" as const:"off" as const}:{}),
        ...(manageTeam!==(bot.manage_team??false)?{manage_team:manageTeam}:{})})
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
  const dirty=name!==bot.name||JSON.stringify(appearance)!==JSON.stringify(bot.appearance)||instructions!==bot.instructions.text||agent!==bot.agent||runtimeChanged||JSON.stringify(approval)!==JSON.stringify(bot.mode?{permission_policy:bot.permission_policy,mode:bot.mode}:{permission_policy:bot.permission_policy})||JSON.stringify(fallbacks)!==JSON.stringify(bot.fallback_agents??[])||JSON.stringify(fallbackSteps)!==JSON.stringify(original.steps)||notify!==(bot.notify!=="off")||manageTeam!==(bot.manage_team??false)||loginProfile!==(bot.login_profile??"")||JSON.stringify(extensions)!==JSON.stringify(bot.extensions??{skills:[],connectors:[]})
  const chain=selectedFallbacks?validSteps.map(step=>step.agent):validFallbacks
  return <section className={`${embedded?"pa-embedded-form":"pa-form-panel"} pa-settings-editor`} role={embedded?undefined:"dialog"} aria-modal={embedded?undefined:true} aria-label={w.settings}>
    <header className="pa-toolbar pa-settings-savebar" hidden={embedded&&!dirty&&!busy&&!error&&!unknown}><button type="button" onClick={onClose}>{w.cancel}</button>{!embedded&&<h2>{w.settings}</h2>}<button type="button" disabled={!enabled||busy||unknown||awaitingModel||!name.trim()} onClick={()=>void save()}>{w.save}</button></header>
    <div className="pa-form-body">
      {part==="rarely"&&<button type="button" className="pa-settings-often-link" onClick={editFrequent}>{w.settingsFrequent}</button>}
      <fieldset disabled={!enabled||busy||unknown}>
        <div className="pa-settings-part" hidden={!frequent}>
          <section className="pa-settings-instruction-section"><h3>{w.settingsInstructionsHeading}</h3><div className="pa-settings-instruction-card"><label className="pa-settings-instructions">{w.instructions}<small>{w.settingsInstructionsHint}</small><textarea ref={instructionsField} value={instructions} onChange={e=>setInstructions(e.target.value)} rows={4}/></label></div></section>
          {dirty&&<p className="pa-settings-hint">{w.settingsDraftHint}</p>}
          <section className="pa-settings-agent-section"><h3>{w.settingsAgentHeading}</h3><div className="pa-settings-agent-card"><div className="pa-settings-primary">
          <SettingsDisclosure title={w.primaryAgent} summary={`${agentName(agent)} · ${runtime.model||w.nativeDefault}`} onOpen={()=>setModelsVisited(true)}>
            <label>{w.primaryAgent}<select aria-label={w.primaryAgent} disabled={!modern} value={agent} onChange={e=>chooseAgent(e.target.value as W.ProjectAssistantAgent)}><option value="claude">Claude Code</option><option value="codex">Codex</option></select></label>
            <RuntimeSettings discovery={discovery} editable={enabled} reason={reason} value={runtime} onChange={setRuntime} w={w}/>
          </SettingsDisclosure>
          </div><LoginProfiles compact source={source} bot={bot} agent={agent} value={loginProfile} onChange={setLoginProfile} onStatusChange={setLoginState} w={w}/>
          <SettingsDisclosure title={w.fallbacks} summary={chain.length?chain.map(agentName).join(" → "):w.settingsNoFallbacks}>
            {selectedFallbacks?<SelectedFallbackSettings bot={{...bot,agent,login_profile:loginProfile}} steps={validSteps} onChange={setFallbackSteps} mode={approval.mode} w={w}/>:<FallbackSettings bot={{...bot,agent}} agents={validFallbacks} onChange={setFallbacks} available={available} mode={approval.mode} w={w}/>}
          </SettingsDisclosure></div></section>
        </div>
        <div className="pa-settings-part" hidden={!rare}>
          <SettingsDisclosure title={w.settingsIdentity} summary={name}>
            <label>{w.name}<input aria-label={w.name} value={name} onChange={e=>setName(e.target.value)}/></label>
            <AppearancePicker value={appearance} onChange={setAppearance} w={w}/>
          </SettingsDisclosure>
          <SettingsDisclosure title={w.settingsAccess}>
            <label className="pa-settings-check"><input type="checkbox" checked={notify} disabled={!modern} onChange={e=>setNotify(e.target.checked)}/>{w.botNotify}</label><p className="pa-settings-hint">{w.botNotifyHint}</p>
            <ApprovalSelect agent={agent} policy={approval.permission_policy} mode={approval.mode} w={w} onChange={id=>setApproval(withApproval(id))}/>
            <label className="pa-settings-check"><input type="checkbox" checked={manageTeam} disabled={!teamAvailable} onChange={e=>setManageTeam(e.target.checked)}/>{catalogWord("projectAssistants","teamManage")}</label>
            <p className="pa-settings-hint">{catalogWord("projectAssistants",!teamAvailable?"teamUnavailable":"teamManageHint")}</p>
          </SettingsDisclosure>
          <SettingsDisclosure title={w.extensions} summary={w.settingsExtensionsSummary.replace("{skills}",String(extensions.skills.length)).replace("{connectors}",String(extensions.connectors.length))}><ExtensionSettings source={source} bot={{...bot,agent}} value={extensions} onChange={setExtensions} w={w}/></SettingsDisclosure>
        </div>
      </fieldset>
      <div className="pa-settings-part" hidden={!rare}>
        {source&&<SettingsDisclosure title={w.settingsNetwork}><NetworkSettingsPanel source={source} bot={bot} enabled={enabled&&!busy&&!unknown} w={w}/></SettingsDisclosure>}
        {rules&&<SettingsDisclosure title={w.settingsRules}>{rules}</SettingsDisclosure>}
        {manage&&<SettingsDisclosure title={w.manageTitle}>{manage}</SettingsDisclosure>}
      </div>
      {!enabled&&reason&&<p role="status" className="pa-warning">{w.cannotEdit}: {reason}</p>}
      <p className="pa-settings-hint">{w.nextIdle}</p>
      {error&&<p role="alert">{settingsSaveFailure(error,w)}</p>}
    </div>
  </section>
}
