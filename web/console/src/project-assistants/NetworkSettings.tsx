import {useEffect,useRef,useState} from "react"
import {words} from "./words"
import "./network-settings.css"

export interface NetworkPreference {
 scope:{account_id:string;key:{machine_id:string;bot_id:string};project:{machine_id:string;project_id:string;canonical_id:string;kind:string}}
 revision:number
 offered_to_network:boolean
 reachable_by_assistants:boolean
}
export interface NetworkCapability {state:string;reason?:string}
export interface NetworkSettingsView {settings:NetworkPreference;capabilities:{peer:NetworkCapability;assistants:NetworkCapability}}
export interface NetworkSettingsPatch {offered_to_network?:boolean;reachable_by_assistants?:boolean}

/** Only a source with current authenticated infrastructure offers a switch. */
export function NetworkSettings({value,readOnly,language="en",onSave}:{value:NetworkSettingsView;readOnly:boolean;language?:string;onSave:(patch:NetworkSettingsPatch)=>Promise<NetworkSettingsView>}){
 const w=words(language),[current,setCurrent]=useState(value),[busy,setBusy]=useState(false),[status,setStatus]=useState<"saved"|"error"|"">("")
 const generation=useRef(0),scope=JSON.stringify(value.settings.scope)
 useEffect(()=>{generation.current++;setCurrent(value);setBusy(false);setStatus("");return()=>{generation.current++}},[value,scope])
 const save=async(patch:NetworkSettingsPatch)=>{
  const n=generation.current;setBusy(true);setStatus("")
  try{const next=await onSave(patch);if(n===generation.current){setCurrent(next);setStatus("saved")}}
  catch{if(n===generation.current)setStatus("error")}
  finally{if(n===generation.current)setBusy(false)}
 }
 const row=(kind:"peer"|"assistants",title:string,hint:string,on:boolean,key:keyof NetworkSettingsPatch)=>{
  const capability=current.capabilities[kind]
  return <div className="pa-network-row"><div><strong>{title}</strong><p>{hint}</p>{capability.state!=="ready"&&<p role="status">{capability.state==="setup_required"?w.networkSetup:w.networkUnavailable}{on&&<> {w.networkSavedInactive}</>}</p>}</div>{capability.state==="ready"&&<button type="button" role="switch" aria-label={title} aria-checked={on} disabled={readOnly||busy||status==="error"} onClick={()=>void save({[key]:!on})}>{on?w.nativeOn:w.nativeOff}</button>}</div>
 }
 return <section className="pa-network-settings" aria-label={w.networkTitle}><h3>{w.networkTitle}</h3><p>{w.networkHint}</p>{row("peer",w.networkPeer,w.networkPeerHint,current.settings.offered_to_network,"offered_to_network")}{row("assistants",w.networkAssistants,w.networkAssistantsHint,current.settings.reachable_by_assistants,"reachable_by_assistants")}{busy&&<p role="status">{w.networkSaving}</p>}{status==="saved"&&<p role="status">{w.networkSaved}</p>}{status==="error"&&<p role="alert">{w.networkSaveFailed}</p>}</section>
}
