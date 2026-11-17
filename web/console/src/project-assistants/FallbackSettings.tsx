import {errorCode} from "./client"
import { createContext, useContext, useEffect, useState, type ReactNode } from "react"
import type * as W from "@clawdline/contract"
import type { SourceClient } from "./client"
import type { Words } from "./words"
import { PaSelect } from "./PaSelect"
import { agentName, fallbackChoices, fallbackModeSupported, FALLBACK_AGENTS, MAX_FALLBACK_AGENTS, moveFallback, previousAgent } from "./fallback-settings"

const FallbackSource = createContext<SourceClient | undefined>(undefined)
/** Bots supplies the exact selected source; direct/older injected forms default unavailable. */
export function FallbackSourceScope({ source, children }: { source: SourceClient; children: ReactNode }) {
  return <FallbackSource.Provider value={source}>{children}</FallbackSource.Provider>
}
export function useSettingsSource(){return useContext(FallbackSource)}
export function useFallbackSource(): boolean { return useContext(FallbackSource)?.canFallbacks?.() === true }

/**
 * Codync `FallbacksRow`: the alternates in order, added, moved and removed here and
 * saved with the ordinary settings form. Each row says when it takes over.
 */
export function FallbackSettings({bot,agents,onChange,available,mode,w}:{bot:W.ProjectAssistantBot;agents:W.ProjectAssistantAgent[];onChange:(agents:W.ProjectAssistantAgent[])=>void;available:boolean;mode?:string;w:Words}) {
  const supported=fallbackModeSupported(mode),choices=available&&supported?fallbackChoices(bot.agent,agents):[]
  const locked=!available
  return <section className="pa-rules pa-fallback-settings" aria-label={w.fallbacks}>
    <h3>{w.fallbacks}</h3>
    <p className="pa-approval-detail">{w.fallbackIntro.replace("{agent}",agentName(bot.agent))}</p>
    {agents.length===0?<p className="pa-approval-detail">{w.fallbackEmpty}</p>:<ol aria-label={w.fallbackOrder} style={{listStyle:"none",margin:0,padding:0,display:"flex",flexDirection:"column"}}>{agents.map((agent,i)=>{
      const name=agentName(agent),known=FALLBACK_AGENTS.includes(agent)
      return <li key={agent}>
        <span className="pa-rule-kind" aria-hidden="true">{i+1}</span>
        <span style={{flex:1,minWidth:0}}><strong>{name}</strong><small className="pa-approval-detail">{known?w.fallbackStepWhat.replace("{previous}",agentName(previousAgent(bot.agent,agents,i))):w.fallbackUnknownAgent}</small></span>
        {i>0&&<button type="button" disabled={locked} aria-label={w.fallbackMoveUp.replace("{agent}",name)} onClick={()=>onChange(moveFallback(agents,i,-1))}>↑</button>}
        {i<agents.length-1&&<button type="button" disabled={locked} aria-label={w.fallbackMoveDown.replace("{agent}",name)} onClick={()=>onChange(moveFallback(agents,i,1))}>↓</button>}
        <button type="button" disabled={locked} aria-label={w.fallbackRemove.replace("{agent}",name)} onClick={()=>onChange(agents.filter((_,j)=>j!==i))}>{w.fallbackRemoveShort}</button>
      </li>})}</ol>}
    {choices.length>0&&<PaSelect ariaLabel={w.fallbackAdd} value="" placeholder={w.fallbackAdd} options={choices.map(a=>({value:a,label:agentName(a)}))} onChange={value=>onChange([...agents,value as W.ProjectAssistantAgent])}/>}
    <small className="pa-approval-detail">{w.fallbackHint}</small>
    {!available&&<p role="status">{w.fallbackUnavailable}</p>}
    {available&&!supported&&<p role="status">{w.fallbackMode}</p>}
  </section>
}


/** Each selected alternate has its own source-owned login, including another
 * login of the same Agent. Rendering never signs in or probes a native model. */
export function SelectedFallbackSettings({bot,steps,onChange,mode,w}:{bot:W.ProjectAssistantBot;steps:W.ProjectAssistantFallbackStep[];onChange:(steps:W.ProjectAssistantFallbackStep[])=>void;mode?:string;w:Words}) {
 const source=useSettingsSource(),[profiles,setProfiles]=useState<W.ProjectAssistantLoginProfile[]>([]),[error,setError]=useState(""),[refresh,setRefresh]=useState(0)
 const available=source?.canFallbackProfiles?.()===true,supported=fallbackModeSupported(mode)
 useEffect(()=>{const c=new AbortController();setProfiles([]);setError("");if(available&&source?.loginProfiles)void Promise.all(FALLBACK_AGENTS.map(a=>source.loginProfiles!(bot.key,bot.conversation_id,a,c.signal))).then(replies=>{if(!c.signal.aborted)setProfiles(replies.flatMap(r=>r.profiles))}).catch(e=>{if(!c.signal.aborted)setError(w.loginUnavailable+" ("+errorCode(e)+")")});return()=>c.abort()},[source,bot.key.machine_id,bot.key.bot_id,bot.conversation_id,bot.login_profile,available,w.loginUnavailable,refresh])
 const key=(s:W.ProjectAssistantFallbackStep)=>s.agent+":"+(s.login_profile??"")
 const primary={agent:bot.agent,login_profile:bot.login_profile??""}
 const options:W.ProjectAssistantFallbackStep[]=FALLBACK_AGENTS.flatMap(agent=>[{agent},...profiles.filter(p=>p.agent===agent&&(p.state==="signed_in"||p.state==="signed_out")).map(p=>({agent,login_profile:p.id}))])
 const name=(step:W.ProjectAssistantFallbackStep)=>agentName(step.agent)+" · "+(step.login_profile?profiles.find(p=>p.id===step.login_profile&&p.agent===step.agent)?.name??w.loginMissing:w.loginDefault)
 const choices=steps.length<MAX_FALLBACK_AGENTS&&available&&supported?options.filter(s=>key(s)!==key(primary)&&!steps.some(existing=>key(existing)===key(s))):[]
 return <section className="pa-rules pa-fallback-settings" aria-label={w.fallbacks}>
  <h3>{w.fallbacks}</h3><button type="button" aria-label={w.fallbacks+" · "+w.refresh} disabled={!available} onClick={()=>setRefresh(n=>n+1)}>{w.refresh}</button><p>{w.fallbackIntro.replace("{agent}",name(primary))}</p>
  {steps.length===0?<p>{w.fallbackEmpty}</p>:<ol aria-label={w.fallbackOrder}>{steps.map((step,i)=><li key={key(step)}>
   <span style={{flex:1,minWidth:0,overflowWrap:"anywhere"}}><strong style={{display:"block"}}>{name(step)}</strong><small style={{display:"block"}}>{w.fallbackStepWhat.replace("{previous}",name(i===0?primary:steps[i-1]))}</small></span>
   {i>0&&<button type="button" disabled={!available} aria-label={w.fallbackMoveUp.replace("{agent}",name(step))} onClick={()=>onChange(moveFallback(steps,i,-1))}>↑</button>}
   {i<steps.length-1&&<button type="button" disabled={!available} aria-label={w.fallbackMoveDown.replace("{agent}",name(step))} onClick={()=>onChange(moveFallback(steps,i,1))}>↓</button>}
   <button type="button" disabled={!available} aria-label={w.fallbackRemove.replace("{agent}",name(step))} onClick={()=>onChange(steps.filter((_,j)=>i!==j))}>{w.fallbackRemoveShort}</button>
  </li>)}</ol>}
  {choices.length>0&&<PaSelect ariaLabel={w.fallbackAdd} value="" placeholder={w.fallbackAdd} options={choices.map(s=>({value:key(s),label:name(s)}))} onChange={value=>{const chosen=choices.find(s=>key(s)===value);if(chosen)onChange([...steps,chosen])}}/>}
  <small>{w.fallbackHint}</small>{error&&<p role="alert">{error}</p>}{!available&&<p role="status">{w.unavailable}</p>}{available&&!supported&&<p role="status">{w.fallbackMode}</p>}
 </section>
}
