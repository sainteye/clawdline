import { createContext, useContext, type ReactNode } from "react"
import type * as W from "@clawdline/contract"
import type { SourceClient } from "./client"
import type { Words } from "./words"
import { PaSelect } from "./PaSelect"
import { agentName, fallbackChoices, fallbackModeSupported, FALLBACK_AGENTS, moveFallback, previousAgent } from "./fallback-settings"

const FallbackSource = createContext<SourceClient | undefined>(undefined)
/** Bots supplies the exact selected source; direct/older injected forms default unavailable. */
export function FallbackSourceScope({ source, children }: { source: SourceClient; children: ReactNode }) {
  return <FallbackSource.Provider value={source}>{children}</FallbackSource.Provider>
}
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
