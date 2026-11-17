import {useEffect,useState} from "react"
import type * as W from "@clawdline/contract"
import {errorCode,type SourceClient} from "./client"
import type {Words} from "./words"
import {ConnectorCredential} from "./ConnectorCredential"

export function ExtensionSettings({source,bot,value,onChange,w}:{source?:SourceClient;bot:W.ProjectAssistantBot;value:W.ProjectAssistantExtensionSelection;onChange:(v:W.ProjectAssistantExtensionSelection)=>void;w:Words}){
 const [catalog,setCatalog]=useState<W.ProjectAssistantExtensionCatalog|null>(null),[error,setError]=useState("")
 useEffect(()=>{const c=new AbortController();setCatalog(null);setError("");if(source?.canLoginProfiles?.()&&source.extensions)void source.extensions(bot.key,bot.conversation_id,c.signal).then(v=>{if(!c.signal.aborted)setCatalog(v.catalog)}).catch(e=>{if(!c.signal.aborted)setError(errorCode(e))});return()=>c.abort()},[source,bot.key.machine_id,bot.key.bot_id,bot.conversation_id,bot.agent])
 const choose=(offer:W.ProjectAssistantExtensionOffer,on:boolean)=>{const field=offer.kind==="skill"?"skills":"connectors",refs=value[field].filter(r=>r.id!==offer.id);if(on)refs.push({id:offer.id,fingerprint:offer.fingerprint});onChange({...value,[field]:refs})}
 return <section className="pa-rules" aria-label={w.extensions}><h3>{w.extensions}</h3><p>{w.extensionScope}</p>
 {catalog?<>{catalog.items.length?catalog.items.map(offer=>{const ref=value[offer.kind==="skill"?"skills":"connectors"].find(r=>r.id===offer.id);return <label key={offer.id} style={{display:"flex",flexDirection:"row",alignItems:"center",gap:8}}><input style={{flex:"none",width:20,height:20,minHeight:20,minWidth:20,padding:0,margin:0}} type="checkbox" checked={ref?.fingerprint===offer.fingerprint} onChange={e=>choose(offer,e.target.checked)}/>{offer.name}{ref&&ref.fingerprint!==offer.fingerprint&&<span> — {w.extensionChanged}</span>}</label>}):<p>{w.extensionEmpty}</p>}{!catalog.connectors_available&&<p>{w.connectorEmpty}</p>}{[...value.skills,...value.connectors].filter(ref=>!catalog.items.some(offer=>offer.id===ref.id)).map(ref=><p key={ref.id}>{w.extensionMissing}<button type="button" onClick={()=>onChange({skills:value.skills.filter(r=>r.id!==ref.id),connectors:value.connectors.filter(r=>r.id!==ref.id)})}>{w.extensionDeselect}</button></p>)}</>:<p role="status">{error?`${w.unavailable} (${error})`:w.nativeLoading}</p>}
 {source?.canConnectorCredentials?.()&&catalog?.items.filter(offer=>offer.credential_required&&bot.extensions?.connectors.some(ref=>ref.id===offer.id&&ref.fingerprint===offer.fingerprint)).map(offer=><ConnectorCredential key={offer.id+offer.fingerprint} source={source} bot={bot} offer={offer} w={w}/>)}
 {catalog?.items.some(offer=>offer.credential_required&&value.connectors.some(ref=>ref.id===offer.id)&&!bot.extensions?.connectors.some(ref=>ref.id===offer.id&&ref.fingerprint===offer.fingerprint))&&<p>{w.connectorSaveFirst}</p>}
 </section>
}
