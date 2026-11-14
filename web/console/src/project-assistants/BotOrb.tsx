import { useEffect, useRef, useState, useSyncExternalStore, type MouseEvent as ReactMouseEvent, type PointerEvent as ReactPointerEvent } from "react"
import { BotAvatar } from "./BotAvatar"
import { words } from "./words"
import { orbPlace, quotaBarTop, setOrbPlace, subscribeOrbPlace } from "./orb-dock"

export interface BotOrbProps {onOpen:(opener?:HTMLElement)=>void; language?:string; unread?:number; pendingPermissions?:number; variant?:"float"|"dock"}

const DRAG_START = 5
const ORB = 60

/** The way into Bots. Floating, it is a draggable orb over the Session list;
 * dropped on the quota bar it docks there as a small icon, and dragging that
 * icon back up floats it again. A press without a drag opens Bots either way. */
export function BotOrb({onOpen,language,unread=0,pendingPermissions=0,variant="float"}:BotOrbProps) {
  const w=words(language), count=unread+pendingPermissions
  const place=useSyncExternalStore(subscribeOrbPlace,orbPlace,orbPlace)
  const dock=useSyncExternalStore(subscribeDock,dockAvailable,dockAvailable)
  const drag=useRef<{x:number;y:number;right:number;bottom:number;moved:boolean}|null>(null)
  const dragged=useRef(false)
  const [offset,setOffset]=useState<{right:number;bottom:number}|null>(null)
  const [lifted,setLifted]=useState<{x:number;y:number}|null>(null)
  const ringing=useRinging(count)
  const docked=place.docked&&dock
  if(variant==="float"&&docked)return null
  if(variant==="dock"&&!docked)return null
  const label=`${w.allBots}${count?`, ${w.pending}: ${pendingPermissions}, ${w.unread}: ${unread}`:""}`
  const reset=()=>{drag.current=null;setOffset(null);setLifted(null)}
  const down=(event:ReactPointerEvent<HTMLButtonElement>)=>{
    if(event.button!==0)return
    event.currentTarget.setPointerCapture?.(event.pointerId)
    dragged.current=false
    drag.current={x:event.clientX,y:event.clientY,right:place.right,bottom:place.bottom,moved:false}
  }
  // The newest offset lives in the drag record: a release can arrive before
  // React renders the last move, and a quick drag may report no move at all.
  const track=(event:ReactPointerEvent<HTMLButtonElement>)=>{
    const d=drag.current;if(!d)return null
    const dx=event.clientX-d.x,dy=event.clientY-d.y
    if(!d.moved&&Math.hypot(dx,dy)<DRAG_START)return null
    d.moved=true
    if(variant!=="float")return null
    const pane=event.currentTarget.closest(".pane-list")?.getBoundingClientRect()
    return {right:clamp(d.right-dx,0,(pane?.width??innerWidth)-ORB),bottom:clamp(d.bottom-dy,0,(pane?.height??innerHeight)-ORB)}
  }
  const move=(event:ReactPointerEvent<HTMLButtonElement>)=>{
    const next=track(event)
    if(next)setOffset(next)
    else if(drag.current?.moved&&variant==="dock")setLifted({x:event.clientX,y:event.clientY})
  }
  const up=(event:ReactPointerEvent<HTMLButtonElement>)=>{
    const next=track(event),d=drag.current
    if(!d?.moved){drag.current=null;return}
    dragged.current=true
    const top=quotaBarTop()
    if(variant==="float"){
      if(top!==null&&event.clientY>=top-8)setOrbPlace({...place,docked:true})
      else if(next)setOrbPlace({docked:false,...next})
    }else if(top===null||event.clientY<top-24){
      const pane=document.querySelector(".pane-list")?.getBoundingClientRect()
      setOrbPlace({docked:false,
        right:pane?clamp(pane.right-event.clientX-ORB/2,0,pane.width-ORB):place.right,
        bottom:pane?clamp(pane.bottom-event.clientY-ORB/2,0,pane.height-ORB):place.bottom})
    }
    reset()
  }
  const click=(event:ReactMouseEvent<HTMLButtonElement>)=>{
    // A drag also ends in a click; only a press without movement opens Bots.
    if(dragged.current){dragged.current=false;return}
    onOpen(event.currentTarget)
  }
  const handlers={onPointerDown:down,onPointerMove:move,onPointerUp:up,onPointerCancel:reset,onClick:click}
  if(variant==="dock")return <span className="project-assistants pa-entry pa-dock-entry">
    <button type="button" className="pa-dock" data-ringing={ringing||undefined} data-lifted={lifted?true:undefined} aria-label={label} title={w.allBots} {...handlers}
      style={lifted?{position:"fixed",left:lifted.x-16,top:lifted.y-16,zIndex:20}:undefined}>
      <BotAvatar appearance={{shape:"blob",color:"#e39567"}} size={20}/>{count>0&&<span key={count} className="pa-dock-badge" aria-hidden="true">{count}</span>}
    </button></span>
  const at=offset??place
  return <span className="project-assistants pa-entry pa-orb-float" style={{right:at.right,bottom:at.bottom}} data-dragging={offset?true:undefined}>
    <button type="button" className="pa-orb" data-ringing={ringing||undefined} aria-label={label} {...handlers}>
      <BotAvatar appearance={{shape:"blob",color:"#e39567"}} size={36}/>{count>0&&<span key={count} className="pa-orb-badge" aria-hidden="true">{count}</span>}
    </button></span>
}

function clamp(value:number,low:number,high:number){return Math.min(Math.max(value,low),Math.max(low,high))}

/** True for a moment after the count of new messages rises. */
function useRinging(count:number){
  const last=useRef(count),[ringing,setRinging]=useState(false)
  useEffect(()=>{
    const rose=count>last.current;last.current=count
    if(!rose)return
    setRinging(true)
    const timer=window.setTimeout(()=>setRinging(false),1200)
    return()=>clearTimeout(timer)
  },[count])
  return ringing
}

// The quota bar says whether it is on screen, so a docked orb never vanishes
// while that bar is hidden: it floats again until the bar returns.
let available=false
const dockListeners=new Set<()=>void>()
export function setDockAvailable(value:boolean){if(available===value)return;available=value;for(const l of dockListeners)l()}
function dockAvailable(){return available}
function subscribeDock(listener:()=>void){dockListeners.add(listener);return()=>{dockListeners.delete(listener)}}
