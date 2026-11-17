import {useEffect,useId,useRef,useState,type ReactNode} from "react"
import type * as W from "@clawdline/contract"
import {BotAvatar} from "./BotAvatar"
import type {Words} from "./words"

export type BotInfoTab="details"|"library"|"memory"|"settings"
const tabs:BotInfoTab[]=["details","library","memory","settings"]

/** Keep visited editors mounted so switching tabs cannot discard an unsaved form. */
export function BotSidePanel({bot,tab,onTab,onClose,children,w}:{bot:W.ProjectAssistantBot;tab:BotInfoTab|null;onTab:(tab:BotInfoTab)=>void;onClose:()=>void;children:Record<BotInfoTab,ReactNode>;w:Words}) {
  const id=useId(),panel=useRef<HTMLElement>(null),opener=useRef<HTMLElement|null>(null),wasOpen=useRef(false)
  const [visited,setVisited]=useState<BotInfoTab[]>([])
  const close=useRef(onClose);close.current=onClose
  useEffect(()=>{
    if(!tab)return
    const escape=(event:KeyboardEvent)=>{if(event.key==="Escape"&&!event.isComposing&&!event.defaultPrevented&&event.target===document.body)close.current()}
    document.addEventListener("keydown",escape)
    return()=>document.removeEventListener("keydown",escape)
  },[!!tab])
  useEffect(()=>{if(tab)setVisited(old=>old.includes(tab)?old:[...old,tab])},[tab])
  useEffect(()=>{
    if(tab&&!wasOpen.current){opener.current=document.activeElement as HTMLElement;panel.current?.querySelector<HTMLElement>('[role="tab"][aria-selected="true"]')?.focus()}
    if(!tab&&wasOpen.current)opener.current?.focus()
    wasOpen.current=!!tab
  },[tab])
  const label=(value:BotInfoTab)=>value==="details"?w.infoDetails:value==="library"?w.library:value==="memory"?w.memory:w.settings
  return <aside ref={panel} hidden={!tab} className="pa-info-panel" aria-label={w.botInformation} onKeyDown={event=>{if(event.key==="Escape"&&!event.nativeEvent.isComposing&&!event.defaultPrevented){event.stopPropagation();onClose()}}}>
    <header className="pa-info-identity"><button type="button" className="pa-info-close" onClick={onClose} aria-label={w.closeInformation}>×</button><BotAvatar appearance={bot.appearance} size={56}/><h2>{bot.name}</h2><small>{bot.agent==="claude"?"Claude Code":"Codex"}{bot.runtime_settings?.model?` · ${bot.runtime_settings.model}`:""}</small></header>
    <div role="tablist" aria-label={w.botInformation} className="pa-info-tabs">{tabs.map(value=><button key={value} id={`${id}-${value}`} type="button" role="tab" aria-selected={tab===value} aria-controls={`${id}-${value}-panel`} tabIndex={tab===value?0:-1} onClick={()=>onTab(value)} onKeyDown={event=>{
      const offset=event.key==="ArrowRight"?1:event.key==="ArrowLeft"?-1:0
      const next=event.key==="Home"?tabs[0]:event.key==="End"?tabs.at(-1):offset?tabs[(tabs.indexOf(value)+offset+tabs.length)%tabs.length]:undefined
      if(next){event.preventDefault();onTab(next);panel.current?.querySelector<HTMLElement>(`[id="${id}-${next}"]`)?.focus()}
    }}>{label(value)}</button>)}</div>
    <div className="pa-info-content">{tabs.filter(value=>visited.includes(value)||tab===value).map(value=><section key={value} role="tabpanel" id={`${id}-${value}-panel`} aria-labelledby={`${id}-${value}`} hidden={tab!==value}>{children[value]}</section>)}</div>
  </aside>
}
