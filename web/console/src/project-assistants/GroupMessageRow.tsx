import {useEffect,useRef,type PointerEvent,type ReactNode} from "react"
import type * as W from "@clawdline/contract"
import type {SourceClient} from "./client"
import {MessageReactions} from "./MessageReactions"

type RowProps={
 line:W.ProjectAssistantRoomEntry
 group:W.ProjectAssistantGroup
 source:SourceClient
 author:string
 continued:boolean
 arriving:boolean
 dismissKey:string
 writable:boolean
 readable:boolean
 replyLabel:string
 onArrivalEnd:()=>void
 onReply?:()=>void
 onReactionChanged:()=>void
 header?:ReactNode
 children:ReactNode
}

export function GroupMessageRow({line,group,source,author,continued,arriving,dismissKey,writable,readable,replyLabel,onArrivalEnd,onReply,onReactionChanged,header,children}:RowProps) {
 const trigger=useRef<HTMLButtonElement>(null)
 const press=useRef<{id:number;x:number;y:number;timer:number}|null>(null)
 const cancelPress=()=>{
  if(press.current)window.clearTimeout(press.current.timer)
  press.current=null
 }
 const open=()=>{
  if(trigger.current?.getAttribute("aria-expanded")==="false")trigger.current.click()
 }
 const startPress=(event:PointerEvent<HTMLDivElement>)=>{
  cancelPress()
  if(event.pointerType==="mouse"||!event.isPrimary||!trigger.current)return
  press.current={id:event.pointerId,x:event.clientX,y:event.clientY,timer:window.setTimeout(open,450)}
 }
 const movePress=(event:PointerEvent<HTMLDivElement>)=>{
  const start=press.current
  if(start?.id===event.pointerId&&Math.hypot(event.clientX-start.x,event.clientY-start.y)>10)cancelPress()
 }
 useEffect(()=>cancelPress,[dismissKey,readable])
 const person=line.kind==="person"
 return <article data-continued={continued||undefined} aria-label={continued&&!person?author:undefined}
  className={`pa-room-message${person?" pa-room-person":""}`} data-room-seq={line.seq}
  data-arrival={arriving||undefined}
  onAnimationEnd={event=>{if(event.animationName==="pa-bubble-enter")onArrivalEnd()}}
  tabIndex={readable?0:undefined}
  onKeyDown={event=>{
   if(event.nativeEvent.isComposing||event.target!==event.currentTarget||!trigger.current)return
   if(event.key==="Enter"||event.key===" "||event.key==="ContextMenu"||event.shiftKey&&event.key==="F10"){
    event.preventDefault();open()
   }
  }}>
  {header}
  <div className="pa-room-bubble" onPointerDown={startPress} onPointerMove={movePress}
   onPointerUp={cancelPress} onPointerCancel={cancelPress}
   onContextMenu={event=>{
    if(!trigger.current)return
    event.preventDefault()
    // A touch menu belongs to the stationary hold. Ignore a delayed native
    // context event after the finger has moved or the browser began scrolling.
    if((event.nativeEvent as globalThis.PointerEvent).pointerType!=="touch")open()
   }}>
   {children}
  </div>
  <MessageReactions message={line} room={group} source={source} triggerRef={trigger}
   dismissKey={dismissKey} writeAllowed={writable} onQuote={onReply} quoteLabel={replyLabel}
   onCopy={readable&&navigator.clipboard?.writeText?()=>{void navigator.clipboard.writeText(line.text).catch(()=>{})}:undefined}
   onReactionChanged={onReactionChanged}/>
 </article>
}
