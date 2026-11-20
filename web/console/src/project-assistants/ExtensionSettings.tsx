import {useEffect,useId,useState} from "react"
import {builtinDisplay} from "../builtin-display"
import type * as W from "@clawdline/contract"
import {errorCode,type SourceClient} from "./client"
import type {Words} from "./words"
import {ConnectorCredential} from "./ConnectorCredential"

export function ExtensionSettings({source,bot,value,onChange,w,bare=false}:{bare?:boolean;source?:SourceClient;bot:W.ProjectAssistantBot;value:W.ProjectAssistantExtensionSelection;onChange:(v:W.ProjectAssistantExtensionSelection)=>void;w:Words}){
 const [catalog,setCatalog]=useState<W.ProjectAssistantExtensionCatalog|null>(null),[error,setError]=useState("")
 useEffect(()=>{const c=new AbortController();setCatalog(null);setError("");if(source?.canLoginProfiles?.()&&source.extensions)void source.extensions(bot.key,bot.conversation_id,c.signal).then(v=>{if(!c.signal.aborted)setCatalog(v.catalog)}).catch(e=>{if(!c.signal.aborted)setError(errorCode(e))});return()=>c.abort()},[source,bot.key.machine_id,bot.key.bot_id,bot.conversation_id,bot.agent])
 const purposeId=useId()
 const choose=(offer:W.ProjectAssistantExtensionOffer,on:boolean)=>{const field=offer.kind==="skill"?"skills":"connectors",refs=value[field].filter(r=>r.id!==offer.id);if(on)refs.push({id:offer.id,fingerprint:offer.fingerprint});onChange({...value,[field]:refs})}
 return <section className="pa-rules" aria-label={w.extensions}>{!bare&&<h3>{w.extensions}</h3>}<p className="pa-settings-note">{w.extensionScope}</p>
 {catalog?<>{catalog.items.length?catalog.items.map((offer,index)=>{
   const ref=value[offer.kind==="skill"?"skills":"connectors"].find(r=>r.id===offer.id)
   const on=ref?.fingerprint===offer.fingerprint
   const purpose=offer.kind==="skill"
     ?builtinDisplay("skill",offer.id.slice("skill:".length),{en:offer.name},offer.purpose).detail
     :""
   const descriptionId=purpose?purposeId+"-"+index:undefined
   return <div key={offer.id} className="pa-settings-row">
     <div>
       <strong>{offer.name}</strong>
       {purpose&&<small className="pa-extension-purpose" id={descriptionId}>{purpose}</small>}
       {ref&&ref.fingerprint!==offer.fingerprint&&<small>{w.extensionChanged}</small>}
     </div>
     <button type="button" role="switch" className="pa-switch" aria-checked={on} aria-label={offer.name} aria-describedby={descriptionId} onClick={()=>choose(offer,!on)}><span/></button>
   </div>
 }):<p>{w.extensionEmpty}</p>}{!catalog.connectors_available&&<p>{w.connectorEmpty}</p>}{[...value.skills,...value.connectors].filter(ref=>!catalog.items.some(offer=>offer.id===ref.id)).map(ref=><p key={ref.id}>{w.extensionMissing}<button type="button" onClick={()=>onChange({skills:value.skills.filter(r=>r.id!==ref.id),connectors:value.connectors.filter(r=>r.id!==ref.id)})}>{w.extensionDeselect}</button></p>)}</>:<p role="status">{error?`${w.unavailable} (${error})`:w.nativeLoading}</p>}
 {source?.canConnectorCredentials?.()&&catalog?.items.filter(offer=>offer.credential_required&&bot.extensions?.connectors.some(ref=>ref.id===offer.id&&ref.fingerprint===offer.fingerprint)).map(offer=><ConnectorCredential key={offer.id+offer.fingerprint} source={source} bot={bot} offer={offer} w={w}/>)}
 {catalog?.items.some(offer=>offer.credential_required&&value.connectors.some(ref=>ref.id===offer.id)&&!bot.extensions?.connectors.some(ref=>ref.id===offer.id&&ref.fingerprint===offer.fingerprint))&&<p>{w.connectorSaveFirst}</p>}
 </section>
}
