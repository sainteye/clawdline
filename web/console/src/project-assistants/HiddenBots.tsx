import { useState } from "react"
import type * as W from "@clawdline/contract"
import type { Words } from "./words"
import { BotAvatar } from "./BotAvatar"

/** Bots hidden from the list, each with Unhide, as Codync's Hidden bots. */
export function HiddenBots({items,onUnhide,w}:{items:readonly W.ProjectAssistantBotSummary[];onUnhide:(item:W.ProjectAssistantBotSummary)=>void;w:Words}) {
  const [open,setOpen]=useState(false)
  return <section className="pa-hidden-bots"><button type="button" className="pa-hidden-toggle" aria-expanded={open} onClick={()=>setOpen(v=>!v)}>{w.hiddenBots} · {items.length}</button>
    {open&&<ul>{items.map(i=><li key={i.bot.key.machine_id+i.bot.key.bot_id}><BotAvatar appearance={i.bot.appearance} size={28}/><span>{i.bot.name}</span><button type="button" onClick={()=>onUnhide(i)}>{w.unhideBot}</button></li>)}</ul>}
  </section>
}
