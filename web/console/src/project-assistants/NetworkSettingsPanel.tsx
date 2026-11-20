import {useEffect,useRef,useState} from "react"
import type * as W from "@clawdline/contract"
import {currentCatalogTag} from "../catalog"
import {command,errorCode,type SourceClient} from "./client"
import {NetworkSettings} from "./NetworkSettings"
import type {Words} from "./words"

/** Each read and save stays on the source captured by the settings dialog. */
export function NetworkSettingsPanel({source,bot,enabled,w,bare=false}:{bare?:boolean;source:SourceClient;bot:W.ProjectAssistantBot;enabled:boolean;w:Words}){
 const [view,setView]=useState<W.ProjectAssistantNetworkSettingsView|null>(null),[error,setError]=useState("")
 const epoch=useRef(0),supported=source.canNetworkSettings?.()===true
 useEffect(()=>{const n=++epoch.current,c=new AbortController();setView(null);setError("");if(supported&&source.networkSettings)void source.networkSettings(bot.key,bot.conversation_id,c.signal).then(answer=>{if(n===epoch.current&&!c.signal.aborted)setView(answer.network)}).catch(e=>{if(n===epoch.current&&!c.signal.aborted)setError(errorCode(e))});return()=>{epoch.current++;c.abort()}},[source,bot.key.machine_id,bot.key.bot_id,bot.conversation_id,supported])
 if(!supported)return null
 if(error)return <p role="status">{w.networkUnavailable} ({error})</p>
 if(!view)return <p role="status">{w.loading}</p>
 return <NetworkSettings bare={bare} value={view} readOnly={!enabled} language={currentCatalogTag()} onSave={async patch=>{
  const n=epoch.current
  const response=await source.saveNetworkSettings!({command:command(bot.key,"network-settings"),fence:view.fence,expected_version:view.bot_version,expected_revision:view.settings.revision,patch:{...(patch.offered_to_network!==undefined?{offered_to_network:{enabled:patch.offered_to_network}}:{}),...(patch.reachable_by_assistants!==undefined?{reachable_by_assistants:{enabled:patch.reachable_by_assistants}}:{})}},new AbortController().signal)
  if(n!==epoch.current)throw Error("network_source_changed")
  setView(response.network);return response.network
 }}/>
}
