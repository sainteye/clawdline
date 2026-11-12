import { BotAvatar } from "./BotAvatar"
import { words } from "./words"
export interface BotOrbProps {onOpen:(opener?:HTMLElement)=>void; language?:string; unread?:number; pendingPermissions?:number}
/** Mount inside the Session list; host controls feature admission and voice-control placement. */
export function BotOrb({onOpen,language,unread=0,pendingPermissions=0}:BotOrbProps) {
  const w=words(language), count=unread+pendingPermissions
  return <span className="project-assistants pa-entry"><button type="button" className="pa-orb" onClick={event=>onOpen(event.currentTarget)} aria-label={`${w.allBots}${count?`, ${w.pending}: ${pendingPermissions}, ${w.unread}: ${unread}`:""}`}>
    <BotAvatar appearance={{shape:"blob",color:"#e39567"}} size={36}/>{count>0&&<span className="pa-orb-badge" aria-hidden="true">{count}</span>}
  </button></span>
}
