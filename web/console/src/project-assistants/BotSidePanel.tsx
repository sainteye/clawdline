import {useEffect,useId,useLayoutEffect,useRef,useState,type ReactNode,type RefObject} from "react"
import type * as W from "@clawdline/contract"
import {BotAvatar} from "./BotAvatar"
import type {Words} from "./words"

export type BotInfoTab="details"|"library"|"memory"|"settings"
const tabs:BotInfoTab[]=["details","library","memory","settings"]

/** Keep visited editors mounted so switching tabs cannot discard an unsaved form. */
export function BotSidePanel({inactive,opener,bot,open,tab,onTab,onClose,children,editor,detailsActions,w}:{inactive:boolean;opener:RefObject<HTMLButtonElement|null>;bot:W.ProjectAssistantBot;open:boolean;tab:BotInfoTab;onTab:(tab:BotInfoTab)=>void;onClose:()=>void;children:Record<BotInfoTab,ReactNode>;editor:ReactNode;detailsActions:ReactNode;w:Words}) {
  const id=useId(),panel=useRef<HTMLElement>(null),wasOpen=useRef(false)
  const [visited,setVisited]=useState<BotInfoTab[]>([])
  const close=useRef(onClose);close.current=onClose
  useEffect(()=>{
    if(!open||inactive)return
    const escape=(event:KeyboardEvent)=>{if(event.key==="Escape"&&!event.isComposing&&!event.defaultPrevented&&event.target===document.body)close.current()}
    document.addEventListener("keydown",escape)
    return()=>document.removeEventListener("keydown",escape)
  },[open,inactive])
  useEffect(()=>{if(open)setVisited(old=>old.includes(tab)?old:[...old,tab])},[open,tab])
  useLayoutEffect(()=>{
    if(open&&!wasOpen.current)panel.current?.querySelector<HTMLElement>('[role="tab"][aria-selected="true"]')?.focus({preventScroll:true})
    if(!open&&wasOpen.current)opener.current?.focus({preventScroll:true})
    wasOpen.current=open
  },[opener,open])
  const label=(value:BotInfoTab)=>value==="details"?w.infoDetails:value==="library"?w.library:value==="memory"?w.memory:w.settings
  return <aside ref={panel} hidden={!open} inert={!open||inactive} className="pa-info-panel pa-side-inspector" aria-label={w.botInformation} onKeyDown={event=>{if(event.key==="Escape"&&!event.nativeEvent.isComposing&&!event.defaultPrevented){event.stopPropagation();onClose()}}}>
    <header className="pa-info-identity"><button type="button" className="pa-info-close" onClick={onClose} aria-label={w.closeInformation}>×</button><BotAvatar appearance={bot.appearance} size={56}/><h2>{bot.name}</h2><small>{bot.agent==="claude"?"Claude Code":"Codex"}{bot.runtime_settings?.model?` · ${bot.runtime_settings.model}`:""}</small></header>
    <div role="tablist" aria-label={w.botInformation} className="pa-info-tabs">{tabs.map(value=><button key={value} id={`${id}-${value}`} type="button" role="tab" aria-selected={tab===value} aria-controls={`${id}-${value}-panel`} tabIndex={tab===value?0:-1} onClick={()=>onTab(value)} onKeyDown={event=>{
      const offset=event.key==="ArrowRight"?1:event.key==="ArrowLeft"?-1:0
      const next=event.key==="Home"?tabs[0]:event.key==="End"?tabs.at(-1):offset?tabs[(tabs.indexOf(value)+offset+tabs.length)%tabs.length]:undefined
      if(next){event.preventDefault();onTab(next);panel.current?.querySelector<HTMLElement>(`[id="${id}-${next}"]`)?.focus()}
    }}>{label(value)}</button>)}</div>
    <div className="pa-info-content">
      {editor&&(visited.some(value=>value==="details"||value==="settings")||open&&(tab==="details"||tab==="settings"))&&<section role="tabpanel" id={`${id}-${tab==="settings"?"settings":"details"}-panel`} aria-labelledby={`${id}-${tab==="settings"?"settings":"details"}`} hidden={tab!=="details"&&tab!=="settings"}>
        {tab==="details"&&detailsActions}{editor}{tab==="settings"?children.settings:children.details}
      </section>}
      {tabs.filter(value=>(!editor||value!=="details"&&value!=="settings")&&(visited.includes(value)||open&&tab===value)).map(value=><section key={value} role="tabpanel" id={`${id}-${value}-panel`} aria-labelledby={`${id}-${value}`} hidden={tab!==value}>{children[value]}</section>)}
    </div>
  </aside>
}
