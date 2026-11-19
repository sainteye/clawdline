import {useEffect,useRef,useState,type RefObject} from "react"
import type * as W from "@clawdline/contract"
import type {SourceClient} from "./client"
import {command,errorCode,provablyNotSent} from "./client"
import {sameConversation} from "./view-state"
import {catalogWord} from "../catalog"
import "./message-reactions.css"
import {BotAvatar} from "./BotAvatar"
import {MessageMenu} from "./MessageMenu"

const tr=(key:string)=>catalogWord("projectAssistants",key)
const refused=new Set(["pa_invalid","pa_scope_denied","pa_stale_version","pa_stale_generation","pa_read_only","pa_disabled","pa_unpaired","pa_unsupported","pa_not_found","pa_idempotency_conflict","pa_capacity_full"])
const palette=["👍","❤️","😂","🎉","👀","✅"]
type Intent=W.ProjectAssistantReactRequest|W.ProjectAssistantGroupReactRequest
type Target={message:W.ProjectAssistantMessage;room?:never}|{message:W.ProjectAssistantRoomEntry;room:W.ProjectAssistantGroup}
type ActionProps={
 source?:SourceClient
 triggerRef?:RefObject<HTMLButtonElement|null>
 dismissKey?:string
 onQuote?:()=>void
 quoteLabel?:string
 writeAllowed?:boolean
 onCopy?:()=>void
 copyLabel?:string
 onReactionChanged?:()=>void
}
const apply=(emojis:readonly string[],intent:Intent)=>intent.present?[...new Set([...emojis,intent.emoji])]:emojis.filter(e=>e!==intent.emoji)
const sameKey=(a:W.ProjectAssistantBotKey|undefined,b:W.ProjectAssistantBotKey)=>a?.machine_id===b.machine_id&&a.bot_id===b.bot_id

/** One desired-state journal and receipt check for Bot and room messages. */
export function MessageReactions(props:Target&ActionProps) {
 const {source,dismissKey,onQuote,onCopy,copyLabel,quoteLabel,onReactionChanged,room,message}=props
 const direct=room?null:props.message
 const key=room?.key??direct!.fence.key
 const conversation=room?.conversation_id??direct!.fence.conversation_id
 const project=room?.project??direct!.fence.project
 const id=room?String(props.message.seq):direct!.id
 const builtInTrigger=useRef<HTMLButtonElement>(null),trigger=props.triggerRef??builtInTrigger
 const [open,setOpen]=useState(false),[busy,setBusy]=useState(false),[held,setHeld]=useState<Intent|null>(null),[error,setError]=useState(""),[blocked,setBlocked]=useState(false)
 const control=useRef<AbortController|null>(null),epoch=useRef(0),inflight=useRef(false)
 const scope=JSON.stringify([source?.intentAccount??"local",source?.machineID,source?.authorizationRevision,key,project,conversation,id])
 const storageKey=(room?"clawdline.room-reaction.":"clawdline.bot-reaction.")+JSON.stringify([source?.intentAccount??"local",key,project,conversation,id])
 const transport=room?source?.groupReact:source?.react
 const canWrite=()=>props.writeAllowed!==false&&!!transport&&(room?source?.canGroupReactions?.():source?.canReactions?.())===true&&source?.current?.()!==false
 const enabled=canWrite()
 const savedIntent=(raw:string):Intent=>{
  const q=JSON.parse(raw) as Intent
  if(q.command?.operation!=="react"||!sameKey(q.command.key,key)||typeof q.command.idempotency_key!=="string"||!q.command.idempotency_key||
     typeof q.present!=="boolean"||typeof q.emoji!=="string"||!Number.isSafeInteger(q.expected_revision)||q.expected_revision<0)throw Error("intent")
  if(room){
   if(!("message_seq" in q)||String(q.message_seq)!==id||q.conversation_id!==conversation||
      !Number.isSafeInteger(q.group_version)||q.group_version<1)throw Error("intent")
  }else if(!("fence" in q)||q.message_id!==id||!sameConversation(q.fence,direct!.fence))throw Error("intent")
  return q
 }
 useEffect(()=>{
  const current=++epoch.current;inflight.current=false;setHeld(null);setBusy(false);setError("");setBlocked(false);setOpen(false)
  try{
   const raw=localStorage.getItem(storageKey)
   if(raw){setHeld(savedIntent(raw));setError("pa_outcome_unknown")}
  }catch{setBlocked(true);setError("pa_outcome_unknown")}
  return()=>{if(epoch.current===current)epoch.current++;control.current?.abort()}
 },[scope,storageKey])
 useEffect(()=>{setOpen(false)},[dismissKey])
 const [confirmed,setConfirmed]=useState<{scope:string;emojis:string[];revision:number}|null>(null)
 const storedOwn=message.reactions?.find(a=>a.own)
 const own=confirmed?.scope===scope&&confirmed.revision>(storedOwn?.revision??0)?confirmed:storedOwn
 const close=(focus:boolean)=>{setOpen(false);if(focus)trigger.current?.focus({preventScroll:true})}
 async function send(emoji?:string){
  if(!source||!canWrite()||inflight.current||busy||blocked)return
  inflight.current=true
  const current=epoch.current,c=new AbortController();control.current=c;setBusy(true);setError("")
  let q=held,sent=false
  try{
   if(!q){
    // Another mounted view may already own an unresolved request.
    const existing=localStorage.getItem(storageKey)
    if(existing){
     try{setHeld(savedIntent(existing))}catch{setBlocked(true)}
     setError("pa_outcome_unknown")
     return
    }
    if(!emoji)return
    const change={command:command(key,"react"),emoji,present:!own?.emojis.includes(emoji),expected_revision:own?.revision??0}
    q=room?{...change,conversation_id:conversation,group_version:room.version,message_seq:message.seq}:{...change,fence:direct!.fence,message_id:id}
    localStorage.setItem(storageKey,JSON.stringify(q));setHeld(q)
   }
   close(true)
   sent=true
   const r="message_seq" in q?await source.groupReact!(q,c.signal):await source.react!(q,c.signal)
   const receipt=r?.receipt
   if(r?.contract_version!=="project-assistants.v1"||receipt?.command?.idempotency_key!==q.command.idempotency_key||
     receipt.command.operation!=="react"||!sameKey(receipt.command.key,key)||receipt.resource_id!==id||receipt.state!=="delivered"||!receipt.id||
     !room&&(!Number.isSafeInteger(receipt.event_seq)||receipt.event_seq<1))throw {error:{code:"pa_outcome_unknown"}}
   if(JSON.parse(localStorage.getItem(storageKey)??"null")?.command?.idempotency_key===q.command.idempotency_key)localStorage.removeItem(storageKey)
   if(epoch.current===current&&!c.signal.aborted){
    if(canWrite()){
     setConfirmed({scope,emojis:apply(own?.emojis??[],q),revision:q.expected_revision+1})
     onReactionChanged?.()
    }
    setHeld(null);setOpen(false)
   }
  }catch(e){
   const code=errorCode(e)
   if(!sent||provablyNotSent(e)||refused.has(code)&&canWrite()){
    try{if(q&&JSON.parse(localStorage.getItem(storageKey)??"null")?.command?.idempotency_key===q.command.idempotency_key)localStorage.removeItem(storageKey)}catch{}
    if(epoch.current===current){setHeld(null);if(code==="pa_stale_version")onReactionChanged?.()}
   }
   if(epoch.current===current)setError(code)
  }finally{if(epoch.current===current){inflight.current=false;setBusy(false)}}
 }
 const reactable=!!room||message.kind==="user"||message.kind==="assistant"
 const preview=held&&(busy||error)&&enabled?held:null
 const shown=preview?apply(own?.emojis??[],preview):(own?.emojis??[])
 const paletteVisible=reactable&&!!transport&&(!room||enabled)
 const hasMenu=paletteVisible||onQuote||onCopy
 const other=(message.reactions??[]).filter(a=>!a.own&&a.emojis.length>0)
 return <>
  {hasMenu&&<div className="pa-message-controls"><button ref={trigger} className="pa-message-more pa-reaction-add" type="button" aria-haspopup="menu" aria-expanded={open} aria-label={tr("messageActions")} title={tr("messageActions")} onClick={()=>{trigger.current?.focus({preventScroll:true});setOpen(v=>!v)}}><svg viewBox="0 0 16 16" width="16" height="16" fill="currentColor" aria-hidden="true"><circle cx="3" cy="8" r="1.4"/><circle cx="8" cy="8" r="1.4"/><circle cx="13" cy="8" r="1.4"/></svg></button></div>}
  {(!room||shown.length>0||other.length>0||busy||error)&&<div className="pa-reactions" aria-label={tr("reactions")} aria-busy={busy||undefined} data-reaction-state={busy?"pending":held||blocked?"unknown":error?"refused":"confirmed"}>
   {shown.map(emoji=><button type="button" className="pa-reaction pa-reaction-own" key={emoji} aria-label={tr("removeReaction")+" "+emoji} title={tr("yourReaction")} disabled={!enabled||busy||!!held||blocked} data-pending={busy||!!held||undefined} onClick={()=>void send(emoji)}>{emoji}<span>{tr("youReaction")}</span></button>)}
   {other.flatMap(a=>a.emojis.map(emoji=><span className="pa-reaction" key={a.id+emoji} title={a.kind==="bot"?a.name:tr("otherReaction")}>{a.kind==="bot"&&a.appearance&&<BotAvatar appearance={a.appearance} size={18}/>} {emoji}<span>{a.kind==="bot"?a.name:tr("otherReaction")}</span></span>))}
   {busy&&<span className="pa-reaction-progress" role="status">{tr("reactionSaving")}</span>}
   {error&&!busy&&<span className="pa-reaction-error" role="status">{held||blocked?tr("reactionUnknown"):tr("reactionFailed")} ({error}) {held&&<button type="button" disabled={!enabled||busy} onClick={()=>void send()}>{tr("retryReaction")}</button>}</span>}
  </div>}
  <MessageMenu open={open&&!!hasMenu} anchor={trigger} label={tr("messageActions")} onClose={close}>
   {paletteVisible&&<div className="pa-reaction-palette" role="group" aria-label={tr("chooseReaction")}>{palette.map(emoji=><button type="button" key={emoji} aria-label={tr("reactWith")+" "+emoji} aria-pressed={own?.emojis.includes(emoji)??false} disabled={!enabled||busy||!!held||blocked} onClick={()=>void send(emoji)}>{emoji}</button>)}</div>}
   {onQuote&&<button type="button" role="menuitem" onClick={()=>{close(false);onQuote()}}>{quoteLabel??tr("quote")}</button>}
   {onCopy&&<button type="button" role="menuitem" onClick={()=>{close(true);onCopy()}}>{copyLabel??tr("copyMessage")}</button>}
  </MessageMenu>
 </>
}
